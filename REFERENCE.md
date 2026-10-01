# Reference fidelity and attribution

Access date: 2026-10-01. This repository is an independent implementation, not an upstream source-code copy.

## Material actually accessed

- [From TCP to HTTP — full course](https://www.youtube.com/watch?v=FknTw9bJsXM): fetched the public watch-page metadata/description and creator chapter timestamps; downloaded the public English automatic captions and read relevant segments, especially the final response/proxy/trailer/video chapters. **No claim of watching all 4h38m of video playback.** Automatic captions may contain transcription errors; exact identifiers were cross-checked with official written lessons.
- [Official course overview](https://www.boot.dev/courses/learn-http-protocol-golang) and its public lesson text: HTTP/1.1 scope, chapter sequence, handler routes, response refactor, streaming, trailers, binary data. Some pages failed in the browsing extractor; their public HTML/embedded page data was retrieved using curl. No account restriction was bypassed and no private solutions were obtained.
- User-provided `Build a Mini HTTP Server.png`: visually inspected. It shows the course thumbnail and high-level project description, not a separate application UI.
- [RFC 9112](https://www.rfc-editor.org/rfc/rfc9112.html): consulted for HTTP/1.1 framing and incomplete-message handling. The code is original, not extracted RFC code.
- A search surfaced third-party student projects, but these were not used as authoritative technical specifications or copied into this implementation.

Downloaded lesson HTML, complete captions, and the tutorial video remain outside version control. They are not redistributed as project assets.

## Feature map

| Demonstrated behavior | Timestamp / primary source | Implementation | Verification |
| --- | --- | --- | --- |
| Read a byte stream; accept TCP connections | 04:08, 14:46 video chapters | `internal/server/server.go`, `internal/protocol/message.go` | `TestRawFragmentedAndMalformedTCP`, concurrent clients |
| Parse request lines, then headers | 40:26, 1:30:11 | `ReadRequest`, `ReadHeaders`, byte-oriented CRLF parser | `TestFragmentedRequest`, `TestMalformedRequests` |
| Content-Length bodies | 2:21:04 | `ReadRequest`, `Framing` | binary and Unicode POST, truncated/duplicate length cases |
| Handler routes and HTTP status/body | 2:50:20; [Handler lesson](https://www.boot.dev/lessons/d28c5dad-56da-45a7-8b4b-12ac65b1365e) | `internal/server/routes.go` | `TestDemonstratedRoutes`, actual CLI/curl flow |
| Final HTML success, 400, 500; writer ordering | Chapter 7 final refactor; [Refactor lesson](https://www.boot.dev/lessons/94905076-3b1d-45fe-88a2-4b513757b077) | `page`, `internal/protocol/response.go` | status/content checks, screenshot, writer state tests |
| `/httpbin/stream/100`, 1024-byte reads and hexadecimal chunks | 4:00:46 chapter; 4:04:50–4:06:52 captions; [Chunked Encoding](https://www.boot.dev/lessons/b18e8b1b-fe6e-4cf3-bd1c-301886d0b4b8) | `internal/upstream/client.go`, `stream`, `WriteChunkedBody` | local socket proxy, raw wire assertions, live public service |
| `/httpbin/range/4096`, announced SHA-256 and length trailers | 4:22:30–4:30:45 captions; [Trailers](https://www.boot.dev/lessons/42541d8e-ad08-49b3-a5ac-96f4f5ee18f0) | `stream` hashes/counts bytes and calls `Finish` | local and live trailer equality checks |
| `/video`, `video/mp4`, downloaded course clip | 4:32:24–4:34:00; [Binary Data](https://www.boot.dev/lessons/5baa092f-9ca4-4178-b2a5-4a215a9e570c) | `/video` branch in routes | exact equality of 20,183,615 response bytes with course asset |

## Deliberate adaptations and remaining fidelity limits

- The video's original upstream is `httpbin.org` (caption at ~4:05), retained as default. The live official lesson now uses `httpbingo.org`; `-upstream` supports this update. Responses are real service data and naturally vary over time.
- The video uses a standard HTTP client for its outbound proxy. This project also implements the outbound HTTP framing from raw TCP/TLS, strengthening the user's requirement to avoid `net/http` in core implementation. Only verification clients use that package.
- The final HTML headings and three short result messages follow the official refactor lesson. Markup was independently written; byte-for-byte whitespace/indentation is not reproduced. The original catch-all success behavior is preserved.
- Go 1.24.4 and standard-library tests replace a dependency on Testify. There is no dependency installation requirement beyond the pinned Go runtime. Native PowerShell commands replace the course's Unix/netcat/Boot.dev CLI exercises.
- Added explicit bounds, timeouts, strict ambiguous-framing rejection, chunked **request** decoding, HEAD handling, and deterministic `/echo`, `/stream`, and `/healthz` checks. They do not replace any demonstrated route.
- Video uses streaming file reads instead of loading the entire clip into memory. Range requests are not implemented; normal complete GET works. The official MP4 is an optional local download because redistribution permission was not verified.
- Chapter timestamps come from video description; narrower times come from inspected captions. No private graded tests or exact instructor repository were available. The current course text is accessible and enough to verify all implemented final routes, but internal source layout is our own.

## Licenses

The original Go code here is released under [MIT](LICENSE). Boot.dev's lesson prose, transcript, video and screenshots are not covered by that license and are not republished. The course and its creator ThePrimeagen / Boot.dev retain their rights. Attribution does not imply endorsement. The three short UI messages are attributed to the official response-refactor lesson; no full lesson or source file has been copied. Standard protocol syntax and status phrases are implemented from the HTTP specification. No third-party code dependency is vendored.
