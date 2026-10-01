# Mini HTTP server in Go

An independent implementation of ThePrimeagen / Boot.dev's **From TCP to HTTP** course: HTTP/1.1 parsed and written directly over TCP, HTML success/error routes, an HTTP proxy with chunked encoding and integrity trailers, and a binary video route. There is no framework and **no `net/http` import in the server, parser, response writer, or upstream client**. Standard clients appear only in tests and the demonstration command.

Reference access, timestamps, attribution, and differences are in [REFERENCE.md](REFERENCE.md). Actual test evidence is in [VERIFICATION.md](VERIFICATION.md).

## Requirements

- [Go 1.24.4](https://go.dev/dl/#go1.24.4), pinned in `go.mod` and CI. Install the native macOS ARM64 package for Apple Silicon, macOS AMD64 for Intel, or Windows AMD64/ARM64 matching your computer. `go version` reports your installed architecture.
- Git and a terminal. Windows examples use PowerShell; `curl.exe` avoids PowerShell's older `curl` alias. macOS examples use zsh/bash.
- No third-party Go dependencies, paid credentials, Docker Desktop, WSL2, database, cloud service, or container image is required. Go modules isolate the project; the commands below isolate its build/module caches as well. There is intentionally no `go.sum` because the module has no external dependencies.
- Internet access is needed only for the optional tutorial video and real `/httpbin/*` proxy. The default tests and self-contained demo are offline.

The original course recommends WSL2 for its Unix command-line exercises. This repository's native Windows adaptation uses portable Go commands and PowerShell, so WSL2 is optional.

## macOS setup, run, and test

From this repository directory:

```bash
go version
export GOCACHE="$PWD/.cache/go-build"
export GOMODCACHE="$PWD/.cache/go-mod"
go mod download                 # no external modules to download
go vet ./...
go test -race -count=1 ./...
go run ./cmd/demo               # self-contained, clearly marked fixture upstream
go run ./cmd/httpserver         # keep this terminal open; Ctrl+C stops it
```

In another terminal, from this same directory:

```bash
curl -i http://127.0.0.1:42069/
curl -i http://127.0.0.1:42069/yourproblem
curl -i http://127.0.0.1:42069/myproblem
curl --raw -i http://127.0.0.1:42069/stream
curl --raw -i http://127.0.0.1:42069/httpbin/range/4096
curl http://127.0.0.1:42069/httpbin/stream/100
curl --data-binary 'hello from TCP' http://127.0.0.1:42069/echo
go run ./cmd/demo -url http://127.0.0.1:42069
```

Open `http://127.0.0.1:42069/` in a browser to see the same minimal success page, not a new dashboard.

## Windows PowerShell setup, run, and test

From this repository directory:

```powershell
go version
$env:GOCACHE = Join-Path $PWD '.cache/go-build'
$env:GOMODCACHE = Join-Path $PWD '.cache/go-mod'
go mod download
go vet ./...
go test -race -count=1 ./...
go run ./cmd/demo
go run ./cmd/httpserver          # keep this terminal open; Ctrl+C stops it
```

The race detector requires a C compiler on Windows; GitHub Actions supplies one. If your local Go reports a missing `gcc`, run `go test -count=1 ./...` and install a compatible C toolchain before running `-race`. The application itself needs no C compiler (`CGO_ENABLED=0` is supported).

In a second PowerShell terminal:

```powershell
curl.exe -i http://127.0.0.1:42069/
curl.exe -i http://127.0.0.1:42069/yourproblem
curl.exe -i http://127.0.0.1:42069/myproblem
curl.exe --raw -i http://127.0.0.1:42069/stream
curl.exe --raw -i http://127.0.0.1:42069/httpbin/range/4096
curl.exe http://127.0.0.1:42069/httpbin/stream/100
curl.exe --data-binary 'hello from TCP' http://127.0.0.1:42069/echo
go run ./cmd/demo -url http://127.0.0.1:42069
Start-Process 'http://127.0.0.1:42069/'
```

## Routes and wire behavior

| Request | Result |
| --- | --- |
| `GET /` or an unmatched path | Course's HTML success response, status 200 |
| `GET /yourproblem` | Course's HTML client-error response, status 400 |
| `GET /myproblem` | Course's HTML server-error response, status 500 |
| `GET /httpbin/stream/100` | Stream the configured upstream's 100 JSON records using manually written chunks |
| `GET /httpbin/range/4096` | Binary proxy response with `X-Content-SHA256` and `X-Content-Length` trailers |
| `GET /video` | Raw MP4 bytes with `video/mp4` and exact byte length |
| `POST /echo` | Additional binary body-framing verification endpoint; echoes exact bytes |
| `GET /stream` | Additional deterministic local fixture, explicitly labelled `local-fixture` in every record |
| `GET /healthz` | Additional local readiness probe |
| `HEAD` | Same route headers, no response body |

The proxy fixes the upstream origin in server configuration. It preserves path/query and upstream status/content type, bounds responses to 32 MiB, streams with a 1024-byte buffer, and hashes incrementally. Network failures before headers yield 502. A failure after streaming starts closes the connection without a terminal chunk, so clients detect a truncated response instead of accepting a false success. Redirects are returned as statuses (this small proxy does not follow or forward redirect locations).

Request lines/headers are read incrementally and accept arbitrary packet boundaries; field names are case-insensitive. Bodies support Content-Length and chunked framing. Malformed or conflicting framing is rejected. Headers are limited to 32 KiB (8 KiB per line), requests to 1 MiB, connections to 128, and a request/response exchange to 40 seconds. Each connection serves one request and advertises `Connection: close`, matching the tutorial's simple lifecycle. Keep-alive, HTTP/2/3, TLS termination for inbound connections, multipart parsing, HTTP range requests, and `Expect: 100-continue` are outside this educational server's scope.

## Tutorial video asset

The official lesson downloads a ~20 MB MP4. Its redistribution license was not established, so **the file is not committed**. Download it locally from the course's own URL; the `assets` directory is ignored. Until downloaded, `/video` clearly returns 404 with setup instructions. You can also supply an MP4 you own with `-video`.

macOS:

```bash
mkdir -p assets
curl --fail --location --output assets/vim.mp4 \
  https://storage.googleapis.com/qvault-webapp-dynamic-assets/lesson_videos/vim-vs-neovim-prime.mp4
curl --fail http://127.0.0.1:42069/video --output assets/received.mp4
shasum -a 256 assets/vim.mp4 assets/received.mp4
open http://127.0.0.1:42069/video
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force assets | Out-Null
curl.exe --fail --location --output assets/vim.mp4 https://storage.googleapis.com/qvault-webapp-dynamic-assets/lesson_videos/vim-vs-neovim-prime.mp4
curl.exe --fail http://127.0.0.1:42069/video --output assets/received.mp4
Get-FileHash assets/vim.mp4, assets/received.mp4 -Algorithm SHA256
Start-Process 'http://127.0.0.1:42069/video'
```

## Configuration and real-service verification

Flags override environment variables. `.env.example` is documentation; this dependency-free application does **not** automatically load `.env` files.

| Environment | Flag | Default |
| --- | --- | --- |
| `HTTP_ADDR` | `-addr` | `127.0.0.1:42069` |
| `UPSTREAM_URL` | `-upstream` | `https://httpbin.org` (video's origin) |
| `VIDEO_PATH` | `-video` | `assets/vim.mp4` |

The current official course page now uses `https://httpbingo.org`. To use that update on either OS:

```text
go run ./cmd/httpserver -upstream https://httpbingo.org
```

Default tests never contact external services. Opt into the separate live test:

```bash
# macOS
LIVE_HTTPBIN=1 go test -v -run TestLiveHTTPBin ./internal/server
```

```powershell
# Windows PowerShell
$env:LIVE_HTTPBIN = '1'
go test -v -run TestLiveHTTPBin ./internal/server
Remove-Item Env:LIVE_HTTPBIN
```

Provider downtime is reported as a failure and is never replaced by fixture data. To test the current course origin, also set `UPSTREAM_URL=https://httpbingo.org` in your shell. Live requests have no paid credentials and are excluded from CI.

## Build and cleanup

```bash
# macOS
mkdir -p bin
go build -o bin/httpserver ./cmd/httpserver
./bin/httpserver
# Stop with Ctrl+C, then remove only generated project files:
rm -rf bin .cache assets
```

```powershell
# Windows PowerShell
New-Item -ItemType Directory -Force bin | Out-Null
go build -o bin/httpserver.exe ./cmd/httpserver
.\bin\httpserver.exe
# Stop with Ctrl+C, then remove only generated project files:
Remove-Item -Recurse -Force bin, .cache, assets -ErrorAction SilentlyContinue
```

Shutdown closes the listener and active sockets and waits for handlers. The cleanup commands remove downloaded video copies as well as build outputs. There are no cloud resources to destroy.

## Tests and CI

`go test -race ./...` checks all fragmentation sizes, malformed lines and headers, duplicate/conflicting lengths, truncated/chunked bodies, trailers, response state/order, short writes, raw TCP errors, binary equality, concurrency, cancellation, upstream chunked/fixed/EOF framing, and all demonstrated routes. `go run ./cmd/demo` starts genuine TCP listeners with a deterministic local fixture upstream, verifies statuses and trailer hashes, then shuts them down. It is explicitly separate from live service checks.

`.github/workflows/test.yml` runs tests, vet, build, and the executable demo natively on Windows, macOS, and Linux. Workflow configuration alone is not evidence of a pass; see [VERIFICATION.md](VERIFICATION.md) and the repository's actual Actions runs.
