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

Windows, macOS Intel, and Linux have not been run locally on this machine. `.github/workflows/test.yml` contains native Windows/macOS/Linux tests, race checks, executable demonstrations, and builds. Actual hosted CI results must be read from the latest pushed commit; merely generating this workflow does not prove a pass. Any final delivery CI links supplement this local record.

The default origin `httpbin.org` was verified; the optional current-course origin `httpbingo.org` was not separately verified in this record. The server has no cloud deployment dependency or paid API key. This is an educational HTTP/1.1 subset, with limitations listed in README, not a claim of full RFC compliance or production server hardening.
