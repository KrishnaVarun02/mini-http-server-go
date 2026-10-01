package protocol

import (
	"bytes"
	"strings"
	"testing"
)

type shortWriter struct{ b bytes.Buffer }

func (s *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 2 {
		p = p[:2]
	}
	return s.b.Write(p)
}
func TestResponseStateAndShortWrites(t *testing.T) {
	out := &shortWriter{}
	w := NewWriter(out)
	if _, err := w.WriteBody([]byte("bad")); err == nil {
		t.Fatal("body before headers")
	}
	h := Headers{}
	h.Set("content-length", "3")
	if err := w.WriteHeaders(h); err == nil {
		t.Fatal("headers before status")
	}
	if err := w.WriteStatusLine(200); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteStatusLine(200); err == nil {
		t.Fatal("duplicate status")
	}
	if err := w.WriteHeaders(h); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteBody([]byte("toolong")); err == nil {
		t.Fatal("overflow")
	}
	if _, err := w.WriteBody([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(nil); err == nil {
		t.Fatal("underflow")
	}
	if _, err := w.WriteBody([]byte("bc")); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteBody([]byte("x")); err == nil {
		t.Fatal("write after finish")
	}
	if out.b.String() != "HTTP/1.1 200 OK\r\ncontent-length: 3\r\n\r\nabc" {
		t.Fatal(out.b.String())
	}
}

func TestChunkedWire(t *testing.T) {
	var b bytes.Buffer
	w := NewWriter(&b)
	h := Headers{}
	h.Set("transfer-encoding", "chunked")
	h.Set("trailer", "X-Check")
	if err := w.WriteStatusLine(200); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteHeaders(h); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteChunkedBody([]byte("hello\x00")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteChunkedBody(nil); err != nil {
		t.Fatal(err)
	}
	bad := Headers{}
	bad.Set("unannounced", "x")
	if err := w.Finish(bad); err == nil {
		t.Fatal("unannounced trailer accepted")
	}
	trailer := Headers{}
	trailer.Set("x-check", "yes")
	if err := w.Finish(trailer); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(b.String(), "\r\n\r\n6\r\nhello\x00\r\n0\r\nx-check: yes\r\n\r\n") {
		t.Fatal(b.String())
	}
	if err := w.Finish(nil); err == nil {
		t.Fatal("duplicate end chunk")
	}
}

func TestResponseHeaderInjection(t *testing.T) {
	for _, h := range []Headers{{"good": []string{"x\r\nEvil: yes"}}, {"bad key": []string{"x"}}} {
		if _, err := headerBytes(h); err == nil {
			t.Fatal("injection accepted")
		}
	}
}
