package protocol

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Writer enforces status -> headers -> body -> completed transitions.
type Writer struct {
	w         io.Writer
	state     int
	status    int
	chunked   bool
	remaining int64
	trailers  map[string]bool
}

func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }
func (w *Writer) Started() bool     { return w.state != 0 }

func WriteAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

func (w *Writer) WriteStatusLine(code int) error {
	if w.state != 0 {
		return fmt.Errorf("status line already written")
	}
	if code < 200 || code > 599 {
		return fmt.Errorf("invalid final status")
	}
	w.state = 1
	w.status = code
	return WriteAll(w.w, []byte(fmt.Sprintf("HTTP/1.1 %d %s\r\n", code, StatusText(code))))
}

func headerBytes(h Headers) ([]byte, error) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		if !IsToken(k) {
			return nil, fmt.Errorf("invalid response header")
		}
		for _, v := range h[k] {
			if !ValidValue(v) {
				return nil, fmt.Errorf("invalid response header value")
			}
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	return []byte(b.String()), nil
}

func (w *Writer) WriteHeaders(h Headers) error {
	if w.state != 1 {
		return fmt.Errorf("headers require status line")
	}
	chunked, n, present, err := Framing(h)
	if err != nil {
		return err
	}
	if w.status == 204 || w.status == 304 {
		if chunked || n != 0 {
			return fmt.Errorf("body not permitted for this status")
		}
	} else if !present {
		return fmt.Errorf("explicit body framing required")
	}
	w.trailers = map[string]bool{}
	if names := h.Get("trailer"); names != "" {
		if !chunked {
			return fmt.Errorf("trailers require chunked encoding")
		}
		for _, name := range strings.Split(names, ",") {
			w.trailers[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	b, err := headerBytes(h)
	if err != nil {
		return err
	}
	w.chunked, w.remaining, w.state = chunked, n, 2
	return WriteAll(w.w, b)
}

func (w *Writer) WriteBody(p []byte) (int, error) {
	if w.state != 2 || w.chunked {
		return 0, fmt.Errorf("fixed body requires fixed-length headers")
	}
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("body exceeds content-length")
	}
	if err := WriteAll(w.w, p); err != nil {
		return 0, err
	}
	w.remaining -= int64(len(p))
	return len(p), nil
}

func (w *Writer) WriteChunkedBody(p []byte) (int, error) {
	if w.state != 2 || !w.chunked {
		return 0, fmt.Errorf("chunk requires chunked headers")
	}
	if len(p) == 0 {
		return 0, nil
	} // Only Finish writes the terminal chunk.
	if err := WriteAll(w.w, []byte(strconv.FormatInt(int64(len(p)), 16)+"\r\n")); err != nil {
		return 0, err
	}
	if err := WriteAll(w.w, p); err != nil {
		return 0, err
	}
	if err := WriteAll(w.w, []byte("\r\n")); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *Writer) Finish(trailers Headers) error {
	if w.state != 2 {
		return fmt.Errorf("body not started or already finished")
	}
	if !w.chunked {
		if w.remaining != 0 {
			return fmt.Errorf("body shorter than content-length")
		}
		if len(trailers) != 0 {
			return fmt.Errorf("fixed body cannot have trailers")
		}
		w.state = 3
		return nil
	}
	for key := range trailers {
		if !w.trailers[strings.ToLower(key)] {
			return fmt.Errorf("unannounced trailer %s", key)
		}
	}
	b, err := headerBytes(trailers)
	if err != nil {
		return err
	}
	w.state = 3
	if err := WriteAll(w.w, []byte("0\r\n")); err != nil {
		return err
	}
	return WriteAll(w.w, b)
}

func (w *Writer) Bytes(code int, contentType string, body []byte, head bool) error {
	if err := w.WriteStatusLine(code); err != nil {
		return err
	}
	h := Headers{}
	h.Set("content-type", contentType)
	if code != 204 && code != 304 {
		h.Set("content-length", strconv.Itoa(len(body)))
	}
	h.Set("connection", "close")
	if err := w.WriteHeaders(h); err != nil {
		return err
	}
	if head {
		w.state = 3
		return nil
	}
	if _, err := w.WriteBody(body); err != nil {
		return err
	}
	return w.Finish(nil)
}
