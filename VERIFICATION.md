# Verification record

Date: 2026-10-01. Native host: **macOS 26.6.2, Apple Silicon arm64**, Go **1.24.4 darwin/arm64**. Go module and build caches were isolated to this project. The repository has no third-party Go dependencies to install.

| Check actually run | Result / evidence |
| --- | --- |
| `go vet ./...` | Pass |
| `go test -race -count=1 ./...` | Pass: protocol, server socket integration, upstream socket integration; [terminal output](evidence/tests-macos.txt) |
| `go test ./internal/protocol -fuzz FuzzReadRequest -fuzztime 3s` | Pass: 1,577,975 fuzz executions; [output](evidence/fuzz-macos.txt) |
| `go run ./cmd/demo` | Pass: real local TCP listeners and deterministic, labelled fixture upstream; [output](evidence/demo-local-macos.txt) |
| `go run ./cmd/httpserver`, then `go run ./cmd/demo -url http://127.0.0.1:42069` | Pass: actual CLI server and real `https://httpbin.org/range/4096`; [output](evidence/demo-live-macos.txt) |
| `LIVE_HTTPBIN=1 go test -v -run TestLiveHTTPBin ./internal/server` | Pass against actual public HTTPS endpoint; [output](evidence/live-httpbin-macos.txt) |
| `GET /video` with official downloaded course asset | Pass: 20,183,615 bytes, `video/mp4`, exact SHA-256 match; [evidence](evidence/video-macos.txt). Asset excluded from Git. |
| Browser rendering of `/` | Actual headless Chrome screenshot, visually inspected: [homepage](evidence/homepage-macos.png) |

The initial sandbox run could not bind local TCP ports. The same tests were rerun with permitted local networking and passed; tests were not skipped or changed to hide that environment restriction. A first asset download timed out; it was resumed to completion before the video equality check.

The binary unit-test fixture is a short byte sequence, **not a playable video**. Actual full MP4 transfer was checked separately. Browser video playback was not visually verified. The reference video was examined through its description and captions, not watched end-to-end.

## Hosted native CI actually completed

[Run 36819860518](https://github.com/KrishnaVarun02/mini-http-server-go/actions/runs/36819860518) passed all three jobs on 2026-10-01 for commit `2daafc63f80e4e793b079a49e13c95c85a2ff673`. The run's logs and job conclusions were inspected, including the reported `go version`, operating system, test output, and executable demonstration output.

| Actual runner / architecture | Shell | Result |
| --- | --- | --- |
| Windows Server 2025, `windows/amd64`, Go 1.24.4 | PowerShell 7 | [Passed](https://github.com/KrishnaVarun02/mini-http-server-go/actions/runs/36819860518/job/110232900767) |
| macOS 26.6.2, `darwin/arm64`, Go 1.24.4 | bash | [Passed](https://github.com/KrishnaVarun02/mini-http-server-go/actions/runs/36819860518/job/110232900571) |
| Ubuntu 24.04.5, `linux/amd64`, Go 1.24.4 | bash | [Passed](https://github.com/KrishnaVarun02/mini-http-server-go/actions/runs/36819860518/job/110232900832) |

Each runner actually executed `go vet ./...`, `go test -race -count=1 ./...`, `go run ./cmd/demo`, and `go build ./cmd/httpserver`. All three test packages passed, and each executable demo printed successful 200/400/500 HTML routes, chunked fixtures and proxy data with verified trailer hashes, and the 27-byte binary POST round trip. These were native socket tests, not cross-compiles or Linux-container substitutes.

The Windows evidence covers the Go commands in PowerShell on a hosted Windows Server machine. A personal Windows desktop installation, Windows ARM64, macOS Intel, Windows browser playback, and Windows live external service calls were not tested. Hosted demos used their explicitly labelled local fixture upstream; live `httpbin.org` and full video transfer were verified separately on the local macOS arm64 host above. This record identifies the exact tested implementation commit; any later documentation-only delivery commit has its own Actions run.

## TCP teardown regression and repair

A subsequent [run on the documentation commit](https://github.com/KrishnaVarun02/mini-http-server-go/actions/runs/36820288193) exposed an intermittent Windows failure: `TestRawFragmentedAndMalformedTCP` received a connection reset instead of the early error response. Immediate full close after rejecting a request line left the client's remaining header bytes unread, allowing TCP to reset the connection. The earlier successful run did not establish that this race was absent.

The server now half-closes its write side after the response, drains remaining input without interpreting another request, then fully closes. Draining is bounded by one second and the configured request-size limits; shutdown cancellation still closes active sockets immediately. This follows [RFC 9112 section 9.6](https://www.rfc-editor.org/rfc/rfc9112.html#section-9.6).

`TestEarlyRejectionPreservesResponse` deterministically reproduced the old behavior on macOS (broken pipe when finishing an upload after early response headers), then passed with the fix. A separate test verifies a response whose declared body is truncated still produces `io.ErrUnexpectedEOF`; the fix does not suppress transport or framing errors. After the change, 100 repetitions of the three targeted socket tests and 10 complete race-enabled suite repetitions passed on macOS arm64, as did `go vet ./...`. [Local evidence](evidence/tcp-teardown-macos.txt) records the commands. Windows CI now repeats the targeted cases 20 times; its post-fix result must be checked on the latest pushed commit rather than inferred from local testing.

The default origin `httpbin.org` was verified; the optional current-course origin `httpbingo.org` was not separately verified in this record. The server has no cloud deployment dependency or paid API key. This is an educational HTTP/1.1 subset, with limitations listed in README, not a claim of full RFC compliance or production server hardening.
