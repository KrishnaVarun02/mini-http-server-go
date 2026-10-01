// Package protocol implements the wire protocol directly, without net/http.
package protocol

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

const MaxLine = 8192
const MaxHeaders = 32768
const MaxBody = 1024 * 1024

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }
func bad(message string) error { return &Error{400, message} }

// Header keys are normalized to lower case. Values retain their wire order.
type Headers map[string][]string

func (h Headers) Get(key string) string { return strings.Join(h[strings.ToLower(key)], ", ") }
func (h Headers) Set(key, value string) { h[strings.ToLower(key)] = []string{value} }

type Request struct {
	Method   string
	Target   string
	Path     string
	Headers  Headers
	Trailers Headers
	Body     []byte
}

func IsToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func ValidValue(s string) bool {
	for i := range s {
		if (s[i] < 32 && s[i] != '\t') || s[i] == 127 {
			return false
		}
	}
	return true
}

// ReadLine tolerates arbitrary TCP fragmentation but requires CRLF on the wire.
// ReadSlice's fixed buffer bounds the allocation even before a newline arrives.
func ReadLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxLine {
		return "", &Error{431, "line too long"}
	}
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) != 0 {
			return "", io.ErrUnexpectedEOF
		}
		return "", err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", bad("expected CRLF")
	}
	return string(line[:len(line)-2]), nil
}

func ReadHeaders(r *bufio.Reader) (Headers, error) {
	h := Headers{}
	total := 0
	for {
		line, err := ReadLine(r)
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		total += len(line) + 2
		if total > MaxHeaders {
			return nil, &Error{431, "headers too large"}
		}
		if line == "" {
			return h, nil
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || !IsToken(name) || !ValidValue(value) {
			return nil, bad("invalid header")
		}
		name = strings.ToLower(name)
		h[name] = append(h[name], strings.Trim(value, " \t"))
	}
}

// Framing rejects ambiguous duplicate Content-Length and TE+CL instead of
// allowing competing interpretations (request smuggling).
func Framing(h Headers) (chunked bool, length int64, present bool, err error) {
	cl, hasCL := h["content-length"]
	te, hasTE := h["transfer-encoding"]
	if hasCL && hasTE {
		return false, 0, false, bad("both transfer-encoding and content-length")
	}
	if hasTE {
		if len(te) != 1 || !strings.EqualFold(te[0], "chunked") {
			return false, 0, false, bad("only chunked transfer-encoding is supported")
		}
		return true, 0, true, nil
	}
	if !hasCL {
		return false, 0, false, nil
	}
	if len(cl) != 1 || cl[0] == "" {
		return false, 0, false, bad("duplicate or empty content-length")
	}
	for _, c := range cl[0] {
		if c < '0' || c > '9' {
			return false, 0, false, bad("invalid content-length")
		}
	}
	n, e := strconv.ParseInt(cl[0], 10, 64)
	if e != nil {
		return false, 0, false, bad("content-length overflow")
	}
	return false, n, true, nil
}

func ReadRequest(r *bufio.Reader) (*Request, error) {
	line, err := ReadLine(r)
	if err != nil {
		return nil, err
	}
	// RFC 9112 recommends accepting a leading empty line.
	if line == "" {
		line, err = ReadLine(r)
		if err != nil {
			return nil, err
		}
	}
	parts := strings.Split(line, " ")
	if len(parts) != 3 || !IsToken(parts[0]) {
		return nil, bad("invalid request line")
	}
	if parts[2] != "HTTP/1.1" {
		return nil, &Error{505, "only HTTP/1.1 is supported"}
	}
	if !strings.HasPrefix(parts[1], "/") || strings.ContainsAny(parts[1], "\r\n\t#") {
		return nil, bad("expected origin-form request target")
	}
	u, err := url.ParseRequestURI(parts[1])
	if err != nil {
		return nil, bad("invalid request target")
	}
	h, err := ReadHeaders(r)
	if err != nil {
		return nil, err
	}
	if len(h["host"]) != 1 || h.Get("host") == "" || strings.ContainsAny(h.Get("host"), " ,/\t") {
		return nil, bad("exactly one valid Host header is required")
	}
	if _, present := h["expect"]; present {
		return nil, &Error{417, "Expect is not supported"}
	}
	chunked, n, _, err := Framing(h)
	if err != nil {
		return nil, err
	}
	req := &Request{Method: parts[0], Target: parts[1], Path: u.Path, Headers: h, Trailers: Headers{}}
	if chunked {
		cr := NewChunkReader(r, MaxBody)
		req.Body, err = io.ReadAll(cr)
		req.Trailers = cr.Trailers
	} else {
		if n > MaxBody {
			return nil, &Error{413, "request body exceeds 1 MiB"}
		}
		req.Body = make([]byte, int(n))
		_, err = io.ReadFull(r, req.Body)
	}
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return req, nil
}

type ChunkReader struct {
	r         *bufio.Reader
	remaining int64
	total     int64
	limit     int64
	needCRLF  bool
	done      bool
	Trailers  Headers
}

func NewChunkReader(r *bufio.Reader, limit int64) *ChunkReader {
	return &ChunkReader{r: r, limit: limit}
}

func (c *ChunkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if c.done {
		return 0, io.EOF
	}
	if c.remaining == 0 {
		if c.needCRLF {
			var end [2]byte
			if _, err := io.ReadFull(c.r, end[:]); err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return 0, err
			}
			if string(end[:]) != "\r\n" {
				return 0, bad("missing chunk CRLF")
			}
		}
		line, err := ReadLine(c.r)
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		// Extensions are optional, bounded by MaxLine, and not interpreted.
		size, ext, hasExt := strings.Cut(line, ";")
		if hasExt && !ValidValue(ext) {
			return 0, bad("invalid chunk extension")
		}
		if size == "" {
			return 0, bad("empty chunk size")
		}
		for _, b := range size {
			if !strings.ContainsRune("0123456789abcdefABCDEF", b) {
				return 0, bad("invalid chunk size")
			}
		}
		n, err := strconv.ParseInt(size, 16, 64)
		if err != nil {
			return 0, bad("chunk size overflow")
		}
		if n == 0 {
			c.Trailers, err = ReadHeaders(c.r)
			if err != nil {
				return 0, err
			}
			for _, key := range []string{"content-length", "transfer-encoding", "host", "trailer", "connection", "content-type", "authorization"} {
				if _, exists := c.Trailers[key]; exists {
					return 0, bad("forbidden trailer field")
				}
			}
			c.done = true
			return 0, io.EOF
		}
		if n > c.limit-c.total {
			return 0, &Error{413, "chunked body exceeds limit"}
		}
		c.remaining, c.total, c.needCRLF = n, c.total+n, true
	}
	if int64(len(p)) > c.remaining {
		p = p[:int(c.remaining)]
	}
	n, err := c.r.Read(p)
	c.remaining -= int64(n)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func ErrorStatus(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 400
}

func StatusText(code int) string {
	s := map[int]string{200: "OK", 204: "No Content", 304: "Not Modified", 400: "Bad Request", 404: "Not Found", 405: "Method Not Allowed", 408: "Request Timeout", 413: "Content Too Large", 417: "Expectation Failed", 431: "Request Header Fields Too Large", 500: "Internal Server Error", 502: "Bad Gateway", 505: "HTTP Version Not Supported"}[code]
	if s == "" {
		return fmt.Sprintf("Status %d", code)
	}
	return s
}
