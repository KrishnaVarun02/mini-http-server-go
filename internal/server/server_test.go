package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/mini-http-server-go/internal/protocol"
)

func start(t *testing.T, c Config) (string, context.CancelFunc, <-chan error) {
	return startHandler(t, Routes(c))
}

func startHandler(t *testing.T, handler Handler) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	s := &Server{Handler: handler, Timeout: 3 * time.Second}
	go func() { done <- s.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("shutdown timed out")
		}
	})
	return l.Addr().String(), cancel, done
}
func fetch(t *testing.T, addr, path string) (*http.Response, []byte) {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	r, e := client.Get("http://" + addr + path)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	return r, b
}

func TestDemonstratedRoutes(t *testing.T) {
	video := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 255, 0, 1} // byte fixture, not a playable tutorial video
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	if err := os.WriteFile(path, video, 0600); err != nil {
		t.Fatal(err)
	}
	addr, _, _ := start(t, Config{VideoPath: path})
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{{"/", 200, "Success!"}, {"/yourproblem", 400, "Your request honestly kinda sucked."}, {"/myproblem", 500, "This one is on me."}, {"/other", 200, "absolute banger"}, {"/healthz", 200, "ok"}} {
		t.Run(tc.path, func(t *testing.T) {
			r, b := fetch(t, addr, tc.path)
			if r.StatusCode != tc.status || !bytes.Contains(b, []byte(tc.contains)) {
				t.Fatalf("%d %s", r.StatusCode, b)
			}
			if !r.Close || r.ContentLength != int64(len(b)) {
				t.Fatal("incorrect framing")
			}
		})
	}
	r, b := fetch(t, addr, "/video")
	if !bytes.Equal(video, b) || r.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("binary corruption: %x", b)
	}
	client := http.Client{Timeout: 5 * time.Second}
	r, err := client.Head("http://" + addr + "/video")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err = io.ReadAll(r.Body)
	if err != nil || len(b) != 0 || r.ContentLength != int64(len(video)) {
		t.Fatal("HEAD framing", err)
	}
}

func TestMissingVideo(t *testing.T) {
	addr, _, _ := start(t, Config{VideoPath: filepath.Join(t.TempDir(), "missing.mp4")})
	r, _ := fetch(t, addr, "/video")
	if r.StatusCode != 404 {
		t.Fatal(r.Status)
	}
}

func TestProxyAndTrailers(t *testing.T) {
	want := bytes.Repeat([]byte("binary\x00\xff\n"), 1200)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/range/4096?test=yes" {
			t.Errorf("target %s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(200)
		for i := 0; i < len(want); i += 31 {
			end := i + 31
			if end > len(want) {
				end = len(want)
			}
			_, _ = w.Write(want[i:end])
			w.(http.Flusher).Flush()
		}
	}))
	defer fixture.Close()
	addr, _, _ := start(t, Config{Upstream: fixture.URL})
	r, b := fetch(t, addr, "/httpbin/range/4096?test=yes")
	if !bytes.Equal(b, want) {
		t.Fatal("proxy corrupted payload")
	}
	if len(r.TransferEncoding) != 1 || r.TransferEncoding[0] != "chunked" || r.Header.Get("Content-Length") != "" {
		t.Fatal("wrong chunked framing")
	}
	if r.Trailer.Get("X-Content-SHA256") != fmt.Sprintf("%x", sha256.Sum256(want)) || r.Trailer.Get("X-Content-Length") != fmt.Sprint(len(want)) {
		t.Fatal(r.Trailer)
	}
}

func TestProxyUnavailable(t *testing.T) {
	addr, _, _ := start(t, Config{Upstream: "bad://origin"})
	r, b := fetch(t, addr, "/httpbin/get")
	if r.StatusCode != 502 || !strings.Contains(string(b), "Upstream request failed") {
		t.Fatalf("%d %s", r.StatusCode, b)
	}
}

func TestRawFragmentedAndMalformedTCP(t *testing.T) {
	addr, _, _ := start(t, Config{})
	for _, tc := range []struct {
		raw    string
		status int
		body   string
	}{
		{"POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\n\r\na\x00\xffb", 200, "a\x00\xffb"},
		{"POST /echo HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\n2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n", 200, "abcd"},
		{"POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 2\r\nContent-Length: 3\r\n\r\n", 400, "Bad Request\n"},
		{"POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 3\r\n\r\na", 400, "Bad Request\n"},
		{"GET / HTTP/9.9\r\nHost: x\r\n\r\n", 505, "HTTP Version Not Supported\n"},
	} {
		c, e := net.Dial("tcp", addr)
		if e != nil {
			t.Fatal(e)
		}
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		for _, b := range []byte(tc.raw) {
			if _, e = c.Write([]byte{b}); e != nil {
				break
			}
		}
		_ = c.(*net.TCPConn).CloseWrite()
		r, e := http.ReadResponse(bufio.NewReader(c), nil)
		if e != nil {
			_ = c.Close()
			t.Fatal(e)
		}
		b, e := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = c.Close()
		if e != nil || r.StatusCode != tc.status || string(b) != tc.body {
			t.Fatalf("got %v %s %q", e, r.Status, b)
		}
	}
}

// A client can still be uploading when the server rejects its request line.
// Sending a response and fully closing with unread bytes can reset the socket,
// destroying that response on Windows. After seeing rejection headers, finish
// the already-started upload before reading the body, forcing this ordering.
func TestEarlyRejectionPreservesResponse(t *testing.T) {
	addr, _, _ := start(t, Config{})
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = io.WriteString(c, "GET / HTTP/9.9\r\n"); err != nil {
		t.Fatal(err)
	}
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	// Deliberately leave data in flight after the early rejection. The server
	// must discard it, never parse or dispatch it as a second request.
	for i := 0; i < 128; i++ {
		if _, err = io.WriteString(c, strings.Repeat("x", 256)); err != nil {
			t.Fatalf("server reset an upload before the client read the response body: %v", err)
		}
	}
	if err = c.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil || r.StatusCode != 505 || string(body) != "HTTP Version Not Supported\n" {
		t.Fatalf("response lost: status=%d body=%q error=%v", r.StatusCode, body, err)
	}
}

func TestTruncatedHandlerResponseRemainsAnError(t *testing.T) {
	addr, _, _ := startHandler(t, func(w *protocol.Writer, _ *protocol.Request) error {
		if err := w.WriteStatusLine(200); err != nil {
			return err
		}
		h := protocol.Headers{}
		h.Set("content-length", "100")
		h.Set("connection", "close")
		if err := w.WriteHeaders(h); err != nil {
			return err
		}
		if _, err := w.WriteBody([]byte("partial")); err != nil {
			return err
		}
		return fmt.Errorf("authored failure after a partial response")
	})
	client := http.Client{Timeout: 5 * time.Second}
	r, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if string(body) != "partial" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated body accepted: %q %v", body, err)
	}
}

func TestConcurrentClients(t *testing.T) {
	addr, _, _ := start(t, Config{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := http.Client{Timeout: 5 * time.Second}
			r, e := client.Get("http://" + addr + "/healthz")
			if e != nil {
				t.Error(e)
				return
			}
			defer r.Body.Close()
			_, _ = io.Copy(io.Discard, r.Body)
			if r.StatusCode != 200 {
				t.Error(r.Status)
			}
		}()
	}
	wg.Wait()
}

func TestShutdownClosesIncompleteConnections(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	s := Server{Handler: Routes(Config{})}
	go func() { done <- s.Serve(ctx, l) }()
	c, e := net.Dial("tcp", l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	_, _ = c.Write([]byte("GET /"))
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server hung during shutdown")
	}
}

func TestLiveHTTPBin(t *testing.T) {
	if os.Getenv("LIVE_HTTPBIN") != "1" {
		t.Skip("set LIVE_HTTPBIN=1 to contact the real public service")
	}
	base := os.Getenv("UPSTREAM_URL")
	if base == "" {
		base = "https://httpbin.org"
	}
	addr, _, _ := start(t, Config{Upstream: base})
	r, b := fetch(t, addr, "/httpbin/range/4096")
	if r.StatusCode != 200 || len(b) != 4096 {
		t.Fatalf("live upstream status=%d bytes=%d body=%.200s", r.StatusCode, len(b), b)
	}
	if r.Trailer.Get("X-Content-SHA256") != fmt.Sprintf("%x", sha256.Sum256(b)) {
		t.Fatal("live digest mismatch")
	}
}
