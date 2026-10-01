package upstream

import (
	"io"
	"net"
	"strings"
	"testing"
)

func origin(t *testing.T, wire string) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		buffer := make([]byte, 8192)
		_, _ = c.Read(buffer)
		_, _ = io.WriteString(c, wire)
	}()
	return "http://" + l.Addr().String()
}

func TestResponseFraming(t *testing.T) {
	for _, tc := range []struct {
		name, wire, want string
		wantErr          bool
	}{
		{"length", "HTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\nabc", "abc", false},
		{"EOF", "HTTP/1.0 200 OK\r\n\r\nabc", "abc", false},
		{"chunks", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n1\r\na\r\n2\r\nbc\r\n0\r\n\r\n", "abc", false},
		{"interim", "HTTP/1.1 103 Early Hints\r\nLink: x\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\nabc", "abc", false},
		{"truncated", "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nabc", "", true},
		{"bad size", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nZ\r\n", "", true},
		{"smuggling", "HTTP/1.1 200 OK\r\nContent-Length: 3\r\nTransfer-Encoding: chunked\r\n\r\n", "", true},
		{"bad status", "BAD 200 OK\r\n\r\n", "", true},
		{"huge", "HTTP/1.1 200 OK\r\nContent-Length: 999999999\r\n\r\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, e := Get(origin(t, tc.wire), "/get")
			var b []byte
			if e == nil {
				defer r.Body.Close()
				b, e = io.ReadAll(r.Body)
			}
			if tc.wantErr {
				if e == nil {
					t.Fatal("expected error")
				}
				return
			}
			if e != nil || string(b) != tc.want {
				t.Fatalf("%q %v", b, e)
			}
		})
	}
}
func TestOriginValidation(t *testing.T) {
	for _, base := range []string{"ftp://x", "https://user:pass@x", "https://x/path", "https://x?q=1", "https://x#f"} {
		if _, e := Get(base, "/"); e == nil {
			t.Fatal(base)
		}
	}
	if _, e := Get("http://localhost", "/\r\nHost: evil"); e == nil {
		t.Fatal("injection accepted")
	}
	if _, e := Get("x", strings.Repeat("/", 3)); e == nil {
		t.Fatal("invalid base")
	}
}
