package protocol

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

type fragmentReader struct {
	data []byte
	size int
}

func (f *fragmentReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, io.EOF
	}
	if len(p) > f.size {
		p = p[:f.size]
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}
func reader(s string) *bufio.Reader { return bufio.NewReaderSize(strings.NewReader(s), MaxLine) }

func TestFragmentedRequest(t *testing.T) {
	body := []byte("hello\x00世界\xff\r\n")
	raw := append([]byte("POST /echo?q=1 HTTP/1.1\r\nHost: localhost\r\nContent-Length: 15\r\nX-Test: one\r\nx-test: two\r\n\r\n"), body...)
	for size := 1; size <= len(raw); size++ {
		r, err := ReadRequest(bufio.NewReaderSize(&fragmentReader{append([]byte(nil), raw...), size}, MaxLine))
		if err != nil {
			t.Fatalf("fragment size %d: %v", size, err)
		}
		if !bytes.Equal(r.Body, body) || r.Path != "/echo" || r.Headers.Get("X-TEST") != "one, two" {
			t.Fatalf("fragment size %d: %#v", size, r)
		}
	}
}

func TestFramingConsumesExactlyOneRequest(t *testing.T) {
	r := reader("POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 3\r\n\r\nabcGET / HTTP/1.1\r\nHost: x\r\n\r\n")
	first, err := ReadRequest(r)
	if err != nil || string(first.Body) != "abc" {
		t.Fatal(first, err)
	}
	second, err := ReadRequest(r)
	if err != nil || second.Method != "GET" {
		t.Fatal(second, err)
	}
}

func TestChunkedRequestAndTrailers(t *testing.T) {
	raw := "POST /echo HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\nTrailer: X-Check\r\n\r\n3;note=ok\r\na\x00b\r\n2\r\ncd\r\n0\r\nX-Check: done\r\n\r\n"
	for size := 1; size < 30; size++ {
		r, err := ReadRequest(bufio.NewReaderSize(&fragmentReader{[]byte(raw), size}, MaxLine))
		if err != nil {
			t.Fatal(err)
		}
		if string(r.Body) != "a\x00bcd" || r.Trailers.Get("X-Check") != "done" {
			t.Fatalf("unexpected %#v", r)
		}
	}
}

func TestMalformedRequests(t *testing.T) {
	cases := map[string]string{
		"no host":            "GET / HTTP/1.1\r\n\r\n",
		"duplicate host":     "GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\n\r\n",
		"invalid host":       "GET / HTTP/1.1\r\nHost: a b\r\n\r\n",
		"LF only":            "GET / HTTP/1.1\nHost: x\n\n",
		"space before colon": "GET / HTTP/1.1\r\nHost : x\r\n\r\n",
		"obs fold":           "GET / HTTP/1.1\r\nHost: x\r\n continuation\r\n\r\n",
		"control":            "GET / HTTP/1.1\r\nHost: x\r\nX: bad\x00value\r\n\r\n",
		"version":            "GET / HTTP/2.0\r\nHost: x\r\n\r\n",
		"bad method":         "G(ET / HTTP/1.1\r\nHost: x\r\n\r\n",
		"bad target":         "GET /%ZZ HTTP/1.1\r\nHost: x\r\n\r\n",
		"extra spaces":       "GET  / HTTP/1.1\r\nHost: x\r\n\r\n",
		"absolute target":    "GET http://x/ HTTP/1.1\r\nHost: x\r\n\r\n",
		"truncated headers":  "GET / HTTP/1.1\r\nHost: x",
		"negative length":    "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: -1\r\n\r\n",
		"plus length":        "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: +1\r\n\r\na",
		"duplicate length":   "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 1\r\nContent-Length: 1\r\n\r\na",
		"combined length":    "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 1,1\r\n\r\na",
		"overflow length":    "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 999999999999999999999\r\n\r\n",
		"huge body":          "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 1048577\r\n\r\n",
		"truncated body":     "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 2\r\n\r\na",
		"absent body":        "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 2\r\n\r\n",
		"TE and CL":          "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n",
		"unknown TE":         "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: gzip\r\n\r\n",
		"TE chain":           "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: gzip, chunked\r\n\r\n",
		"expect":             "POST / HTTP/1.1\r\nHost: x\r\nExpect: 100-continue\r\n\r\n",
		"long line":          "GET /" + strings.Repeat("a", MaxLine) + " HTTP/1.1\r\nHost: x\r\n\r\n",
		"many headers":       "GET / HTTP/1.1\r\nHost: x\r\n" + strings.Repeat("X: "+strings.Repeat("a", 4000)+"\r\n", 9) + "\r\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadRequest(reader(raw)); err == nil {
				t.Fatal("accepted malformed request")
			}
		})
	}
	for _, body := range []string{"", "1\r\nx", "1\r\nx\r\n", "0\r\n", "Q\r\nx\r\n0\r\n\r\n", "+1\r\nx\r\n0\r\n\r\n", "1\r\nx!\r0\r\n\r\n", "2\r\nx", "0\r\nContent-Length: 99\r\n\r\n", "100001\r\n", "fffffffffffffffffff\r\n"} {
		raw := "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n" + body
		if _, err := ReadRequest(reader(raw)); err == nil {
			t.Fatalf("accepted chunk %q", body)
		}
	}
}

func FuzzReadRequest(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	f.Add([]byte("POST /echo HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxBody+MaxHeaders+MaxLine {
			t.Skip()
		}
		r, err := ReadRequest(bufio.NewReaderSize(bytes.NewReader(data), MaxLine))
		if err == nil && len(r.Body) > MaxBody {
			t.Fatal("body limit bypass")
		}
	})
}
