package server

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"example.com/mini-http-server-go/internal/protocol"
	"example.com/mini-http-server-go/internal/upstream"
)

type Config struct {
	VideoPath string
	Upstream  string
}

func Routes(config Config) Handler {
	if config.VideoPath == "" {
		config.VideoPath = "assets/vim.mp4"
	}
	if config.Upstream == "" {
		config.Upstream = "https://httpbin.org"
	}
	return func(w *protocol.Writer, r *protocol.Request) error {
		if r.Path == "/echo" && r.Method == "POST" {
			return w.Bytes(200, "application/octet-stream", r.Body, false)
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			return w.Bytes(405, "text/plain; charset=utf-8", []byte("Use GET or HEAD; POST is supported on /echo\n"), false)
		}
		head := r.Method == "HEAD"
		switch {
		case r.Path == "/yourproblem":
			return page(w, 400, "Bad Request", "Your request honestly kinda sucked.", head)
		case r.Path == "/myproblem":
			return page(w, 500, "Internal Server Error", "Okay, you know what? This one is on me.", head)
		case r.Path == "/healthz":
			return w.Bytes(200, "text/plain", []byte("ok\n"), head)
		case r.Path == "/video":
			f, err := os.Open(config.VideoPath)
			if err != nil {
				return w.Bytes(404, "text/plain", []byte("Video unavailable. Download assets/vim.mp4 using the README instructions or set VIDEO_PATH.\n"), head)
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("video path is not a regular file")
			}
			if err = w.WriteStatusLine(200); err != nil {
				return err
			}
			h := protocol.Headers{}
			h.Set("content-type", "video/mp4")
			h.Set("content-length", strconv.FormatInt(info.Size(), 10))
			h.Set("connection", "close")
			if err = w.WriteHeaders(h); err != nil {
				return err
			}
			if head {
				return nil
			}
			if _, err = io.Copy(&fixedBodyWriter{w}, f); err != nil {
				return err
			}
			return w.Finish(nil)
		case strings.HasPrefix(r.Path, "/httpbin/"):
			// The origin is configured by the operator, never taken from user input.
			resp, err := upstream.Get(config.Upstream, strings.TrimPrefix(r.Target, "/httpbin"))
			if err != nil {
				return w.Bytes(502, "text/plain", []byte("Upstream request failed: "+err.Error()+"\n"), head)
			}
			defer resp.Body.Close()
			contentType := resp.Headers.Get("content-type")
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			if resp.Status == 204 || resp.Status == 304 {
				return w.Bytes(resp.Status, contentType, nil, true)
			}
			return stream(w, resp.Status, contentType, resp.Body, head)
		case r.Path == "/stream":
			return stream(w, 200, "application/x-ndjson", strings.NewReader("{\"mode\":\"local-fixture\",\"id\":0}\n{\"mode\":\"local-fixture\",\"id\":1}\n"), head)
		default:
			return page(w, 200, "Success!", "Your request was an absolute banger.", head)
		}
	}
}

func page(w *protocol.Writer, code int, heading, message string, head bool) error {
	b := []byte(fmt.Sprintf("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>%d %s</title></head><body><h1>%s</h1><p>%s</p></body></html>\n", code, protocol.StatusText(code), heading, message))
	return w.Bytes(code, "text/html; charset=utf-8", b, head)
}

type fixedBodyWriter struct{ w *protocol.Writer }

func (f *fixedBodyWriter) Write(p []byte) (int, error) { return f.w.WriteBody(p) }

func stream(w *protocol.Writer, status int, contentType string, body io.Reader, head bool) error {
	if err := w.WriteStatusLine(status); err != nil {
		return err
	}
	h := protocol.Headers{}
	h.Set("content-type", contentType)
	h.Set("connection", "close")
	h.Set("transfer-encoding", "chunked")
	h.Set("trailer", "X-Content-SHA256, X-Content-Length")
	if err := w.WriteHeaders(h); err != nil {
		return err
	}
	if head {
		return nil
	}
	hash := sha256.New()
	var total int64
	buffer := make([]byte, 1024)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > upstream.MaxBody {
				return fmt.Errorf("upstream body exceeds 32 MiB")
			}
			_, _ = hash.Write(buffer[:n])
			if _, e := w.WriteChunkedBody(buffer[:n]); e != nil {
				return e
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	t := protocol.Headers{}
	t.Set("x-content-sha256", fmt.Sprintf("%x", hash.Sum(nil)))
	t.Set("x-content-length", strconv.FormatInt(total, 10))
	return w.Finish(t)
}
