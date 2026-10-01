// Package upstream is a minimal HTTP/1.1 GET client built from TCP and TLS.
// It deliberately does not use net/http, including for parsing upstream bodies.
package upstream

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"example.com/mini-http-server-go/internal/protocol"
)

const MaxBody int64 = 32 * 1024 * 1024

type Response struct {
	Status  int
	Headers protocol.Headers
	Body    io.ReadCloser
}
type body struct {
	io.Reader
	io.Closer
}
type exactReader struct {
	r         io.Reader
	remaining int64
}

func (e *exactReader) Read(p []byte) (int, error) {
	if e.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > e.remaining {
		p = p[:int(e.remaining)]
	}
	n, err := e.r.Read(p)
	e.remaining -= int64(n)
	if err == io.EOF && e.remaining > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func Get(base, target string) (*Response, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("upstream must be an http(s) origin without credentials, query or path")
	}
	if !strings.HasPrefix(target, "/") || strings.ContainsAny(target, "\r\n\t ") {
		return nil, fmt.Errorf("invalid upstream target")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	if u.Scheme == "https" {
		conn, err = tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(u.Hostname(), port), &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
	} else {
		conn, err = dialer.Dial("tcp", net.JoinHostPort(u.Hostname(), port))
	}
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()
	if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if err = protocol.WriteAll(conn, []byte("GET "+target+" HTTP/1.1\r\nHost: "+u.Host+"\r\nConnection: close\r\nAccept-Encoding: identity\r\nUser-Agent: mini-http-server-go/1.0\r\n\r\n")); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(conn, protocol.MaxLine)
	var status int
	var h protocol.Headers
	for interim := 0; interim < 6; interim++ {
		line, e := protocol.ReadLine(r)
		if e != nil {
			return nil, e
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 || (parts[0] != "HTTP/1.1" && parts[0] != "HTTP/1.0") || len(parts[1]) != 3 {
			return nil, fmt.Errorf("invalid upstream status line")
		}
		status, e = strconv.Atoi(parts[1])
		if e != nil || status < 100 || status > 599 {
			return nil, fmt.Errorf("invalid upstream status")
		}
		h, e = protocol.ReadHeaders(r)
		if e != nil {
			return nil, e
		}
		if status == 101 {
			return nil, fmt.Errorf("protocol upgrade not supported")
		}
		if status >= 200 {
			break
		}
	}
	if status < 200 {
		return nil, fmt.Errorf("too many interim responses")
	}
	chunked, n, present, err := protocol.Framing(h)
	if err != nil {
		return nil, err
	}
	var reader io.Reader = r
	if status == 204 || status == 304 {
		reader = strings.NewReader("")
	} else if chunked {
		reader = protocol.NewChunkReader(r, MaxBody)
	} else if present {
		if n > MaxBody {
			return nil, fmt.Errorf("upstream body exceeds 32 MiB")
		}
		reader = &exactReader{r: r, remaining: n}
	}
	ok = true
	return &Response{Status: status, Headers: h, Body: &body{Reader: reader, Closer: conn}}, nil
}
