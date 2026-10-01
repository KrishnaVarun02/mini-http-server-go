package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"example.com/mini-http-server-go/internal/protocol"
)

type Handler func(*protocol.Writer, *protocol.Request) error
type Server struct {
	Handler        Handler
	Logger         *log.Logger
	Timeout        time.Duration
	MaxConnections int
}

// Serve handles one request per connection and explicitly advertises close.
// Cancellation closes listeners and active connections, then waits for workers.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	if s.Handler == nil {
		return errors.New("handler required")
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 40 * time.Second
	}
	limit := s.MaxConnections
	if limit <= 0 {
		limit = 128
	}
	slots := make(chan struct{}, limit)
	var workers sync.WaitGroup
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
			mu.Lock()
			for c := range connections {
				_ = c.Close()
			}
			mu.Unlock()
		case <-stopped:
		}
	}()
	defer close(stopped)
	defer workers.Wait()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			_ = conn.Close()
			return nil
		}
		mu.Lock()
		if ctx.Err() != nil {
			mu.Unlock()
			<-slots
			_ = conn.Close()
			return nil
		}
		connections[conn] = true
		mu.Unlock()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { _ = conn.Close(); mu.Lock(); delete(connections, conn); mu.Unlock(); <-slots }()
			_ = conn.SetDeadline(time.Now().Add(timeout))
			w := protocol.NewWriter(conn)
			req, err := protocol.ReadRequest(bufio.NewReaderSize(conn, protocol.MaxLine))
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				status := protocol.ErrorStatus(err)
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					status = 408
					_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
				}
				_ = w.Bytes(status, "text/plain; charset=utf-8", []byte(protocol.StatusText(status)+"\n"), false)
				return
			}
			err = s.Handler(w, req)
			if err != nil {
				if s.Logger != nil {
					s.Logger.Printf("handler error: %v", err)
				}
				if !w.Started() {
					_ = w.Bytes(500, "text/plain; charset=utf-8", []byte("Internal Server Error\n"), req.Method == "HEAD")
				}
			}
		}()
	}
}
