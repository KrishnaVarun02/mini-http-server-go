// demo is a verification client. Its net/http use is isolated from the server.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"example.com/mini-http-server-go/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}
func run() error {
	base := flag.String("url", "", "verify an existing server; omit to start a self-contained local fixture demo")
	flag.Parse()
	if *base == "" {
		fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(bytes.Repeat([]byte("fixture\x00"), 512))
		}))
		defer fixture.Close()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		s := server.Server{Handler: server.Routes(server.Config{Upstream: fixture.URL})}
		go func() { done <- s.Serve(ctx, l) }()
		defer func() { cancel(); <-done }()
		*base = "http://" + l.Addr().String()
		fmt.Println("MODE: deterministic local fixture; no external service contacted")
	} else {
		fmt.Println("MODE: existing server; /httpbin uses its configured real or fixture upstream")
	}
	client := http.Client{Timeout: 35 * time.Second}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/", 200}, {"/yourproblem", 400}, {"/myproblem", 500}, {"/stream", 200}, {"/httpbin/range/4096", 200}} {
		r, err := client.Get(*base + tc.path)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			return err
		}
		if r.StatusCode != tc.status {
			return fmt.Errorf("%s: status %d, body %.200s", tc.path, r.StatusCode, body)
		}
		fmt.Printf("PASS GET %-25s status=%d bytes=%d\n", tc.path, r.StatusCode, len(body))
		if len(r.TransferEncoding) > 0 {
			digest := fmt.Sprintf("%x", sha256.Sum256(body))
			if r.Trailer.Get("X-Content-SHA256") != digest || r.Trailer.Get("X-Content-Length") != fmt.Sprint(len(body)) {
				return fmt.Errorf("invalid trailers for %s", tc.path)
			}
			fmt.Printf("     chunked trailers: length=%s sha256=%s\n", r.Trailer.Get("X-Content-Length"), digest)
		}
	}
	payload := []byte("binary\x00\xff and Unicode 世界")
	r, err := client.Post(*base+"/echo", "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return err
	}
	if r.StatusCode != 200 || !bytes.Equal(body, payload) {
		return fmt.Errorf("binary echo mismatch")
	}
	fmt.Printf("PASS POST /echo binary round trip: %d bytes\n", len(body))
	return nil
}
