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
			reader := bufio.NewReaderSize(conn, protocol.MaxLine)
			defer func() { closeConnection(conn, reader); mu.Lock(); delete(connections, conn); mu.Unlock(); <-slots }()
			_ = conn.SetDeadline(time.Now().Add(timeout))
			w := protocol.NewWriter(conn)
			req, err := protocol.ReadRequest(reader)
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

// closeConnection follows HTTP/1.1's staged TCP teardown (RFC 9112, 9.6).
// An early parse error can leave a client's headers/body in flight. Closing
// both directions immediately would reset the connection and can discard the
// response before a Windows client reads it. Send FIN on the response side,
// then drain without interpreting any further requests. Both time and bytes
// are bounded, and cancellation still closes the tracked socket immediately.
func closeConnection(conn net.Conn, reader io.Reader) {
	defer conn.Close()
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	if err := tcp.CloseWrite(); err != nil {
		return
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(reader, protocol.MaxBody+protocol.MaxHeaders+protocol.MaxLine))
}
