package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"example.com/mini-http-server-go/internal/server"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	addr := flag.String("addr", env("HTTP_ADDR", "127.0.0.1:42069"), "TCP listen address")
	video := flag.String("video", env("VIDEO_PATH", "assets/vim.mp4"), "local MP4 file")
	upstream := flag.String("upstream", env("UPSTREAM_URL", "https://httpbin.org"), "fixed proxy origin")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	l, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	log.Printf("HTTP/1.1 listening on http://%s; upstream=%s; Ctrl+C to stop", l.Addr(), *upstream)
	s := server.Server{Handler: server.Routes(server.Config{VideoPath: *video, Upstream: *upstream}), Logger: log.Default()}
	if err = s.Serve(ctx, l); err != nil {
		log.Fatal(err)
	}
	log.Print("server stopped")
}
