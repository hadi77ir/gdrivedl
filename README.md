# gdrivedl

`gdrivedl` is a Go library and CLI for downloading shared Google Drive files and folders.

It supports:

- Shared file downloads without OAuth
- Shared folder downloads for public links, with or without an API key
- Resumable downloads for large shared files
- Optional aggregate progress output with speed and ETA
- Per-file connection state in the progress view
- Concurrent downloads for URL lists and folder downloads
- Optional exit reports with per-file completion status
- Optional per-file completion reports after successful downloads
- Optional dry-run request testing without saving files
- Configurable HTTP timeout and verbosity-controlled transport logs
- Configurable delay between HTTP requests
- Optional HTTP request and response header dumps
- HTTP domain fronting
- HTTP and SOCKS5 proxies
- `uTLS` ClientHello profiles for HTTPS requests
- Optional HTTP/2 preference, HTTP/1.1 forcing, and shared HTTP/2 TLS connections
- Per-request server IP overrides with `--resolve-to`
- Multiple downloads from a URL list file or standard input

## Install The CLI

```bash
go install github.com/hadi77ir/gdrivedl/cmd/gdrivedl@latest
```

## Use As A Go Package

Import the root module in another Go program:

```go
import "github.com/hadi77ir/gdrivedl"
```

Minimal example:

```go
ctx := context.Background()
err := gdrivedl.Download(ctx, gdrivedl.Request{
    URL:     "https://drive.google.com/file/d/FILE_ID/view?usp=sharing",
    WorkDir: "/tmp/downloads",
    Proxy:   "http://127.0.0.1:2089",
})
```

For UI integrations, use `gdrivedl.DownloadWithObserver` to receive periodic task snapshots and aggregate progress updates.

## API Key

Public shared folder downloads do not require a Google Drive API key.
API keys are still used for resumable downloads and Drive API metadata lookups.

`gdrivedl` reads the key from:

- `--apikey`
- `GDRIVEDL_APIKEY`
- `GOODLS_APIKEY` as a legacy fallback

Example:

```bash
export GDRIVEDL_APIKEY='your-api-key'
```

## Basic Usage

Download a shared file:

```bash
gdrivedl -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

Note:

- Shared-link downloads must be publicly accessible without an interactive Google sign-in.

Download a Google Docs file as plain text:

```bash
gdrivedl -u 'https://docs.google.com/document/d/FILE_ID/edit?usp=sharing' -e txt
```

Download a public shared folder without an API key:

```bash
gdrivedl -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing'
```

Download a shared folder with an API key:

```bash
gdrivedl -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' -key "$GDRIVEDL_APIKEY"
```

Show folder metadata/listing without an API key:

```bash
gdrivedl -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' -i
```

Show file metadata with an API key:

```bash
gdrivedl -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing' -key "$GDRIVEDL_APIKEY" -i
```

Run a resumable download:

```bash
gdrivedl -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing' -key "$GDRIVEDL_APIKEY" -r 100m
```

Test a download request without writing any files:

```bash
gdrivedl --dry-run -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

Download multiple URLs from a file:

```bash
gdrivedl --url-list urls.txt
```

Download multiple URLs from standard input:

```bash
cat urls.txt | gdrivedl --url-list -
```

Download a list concurrently with progress and an exit report:

```bash
gdrivedl \
  --url-list urls.txt \
  --concurrency 4 \
  --progress \
  --exit-report \
  --proxy 'http://127.0.0.1:2089'
```

## Progress And Reporting

`--progress`

- Shows the current file, its current connection/download state, total progress, speed, and estimated remaining time.

`--concurrency`

- Sets the maximum number of concurrent downloads.
- Applies to `--url-list` downloads and folder downloads.
- Defaults to `1`.

`--exit-report`

- Prints a final per-file report with file name, percent downloaded, and final status.

`--completion-report`

- Prints a per-file completion line after each successful download.

`--dry-run`

- Sends the download request without saving the response body to disk.
- Does not create files or directories.
- Useful for testing connectivity, request routing, and remote accessibility.

Notes:

- `--progress` cannot be combined with `--NoProgress`.
- `--concurrency` must be greater than `0`.

## HTTP Logging

`--timeout`

- Sets the HTTP client timeout.
- Accepts Go duration strings like `30s`, `2m`, and `1h`.
- Plain integer values are treated as seconds.

`--request-delay`

- Sets the minimum delay between HTTP requests.
- Accepts Go duration strings like `500ms`, `2s`, and `1m`.
- Plain integer values are treated as seconds.

`--verbosity`

- `0` disables stage logs.
- `1` enables per-request connection stage logs and HTTP status logs.
- `2` adds detailed connection logs such as resolved dial targets and response-body completion.

`--dump-request`

- Dumps outgoing HTTP requests before they are sent.

`--dump-response`

- Dumps received HTTP response headers after they are received.

Connection stages include examples such as:

- `resolving`
- `dialing`
- `proxy connect`
- `request delay`
- `tls handshake`
- `sending request`
- `waiting for response`
- `response headers received`
- `downloading`

Example:

```bash
gdrivedl \
  --progress \
  --verbosity 2 \
  --timeout 45s \
  --request-delay 1s \
  --dump-request \
  --dump-response \
  --proxy 'http://127.0.0.1:2089' \
  -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

## Network Transport

### Proxy

Use an upstream proxy with `--proxy`.

Supported schemes:

- `http://`
- `socks5://`

Example HTTP proxy:

```bash
gdrivedl --proxy 'http://127.0.0.1:2089' -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### `uTLS`

HTTPS requests use `uTLS`. Select the ClientHello profile with `--utls-profile`.

When domain fronting is enabled, `gdrivedl` retries alternate `uTLS` handshakes automatically. If you want an explicit profile for fronted traffic, `firefox_auto` is usually a good first choice.

Supported values:

- `chrome_auto`
- `firefox_auto`
- `safari_auto`
- `ios_auto`
- `edge_auto`
- `360_auto`
- `qq_auto`
- `randomized`
- `randomized_alpn`
- `randomized_no_alpn`

Example:

```bash
gdrivedl --utls-profile firefox_auto -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### HTTP Version Selection

`--prefer-http2`

- Prefer HTTP/2 over HTTP/1.1 for HTTPS requests when ALPN negotiation succeeds.
- Falls back to HTTP/1.1 if HTTP/2 is not available.

`--force-http1`

- Forces HTTP/1.1 for HTTPS requests.
- Disables HTTP/2 negotiation.
- Cannot be combined with `--prefer-http2` or `--share-http2-connection`.

`--share-http2-connection`

- Reuses a negotiated HTTP/2 TLS connection for multiple requests to the same target.
- Implies HTTP/2 preference.
- Cannot be combined with `--force-http1`.

Example:

```bash
gdrivedl \
  --prefer-http2 \
  --share-http2-connection \
  --utls-profile chrome_auto \
  --proxy 'http://127.0.0.1:2089' \
  -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### `--resolve-to`

Override the network dial IP while preserving the original request port and logical host.

Example:

```bash
gdrivedl --resolve-to 203.0.113.10 -u 'https://www.googleapis.com/drive/v3/files/FILE_ID?alt=media'
```

### Domain Fronting

`--fronting-enable`

- Enables HTTP domain fronting in the shared transport for all requests.

`--fronting-target`

- Fronting target hostname used for network dial.
- The original request port is preserved.
- Requires `--fronting-enable`.

`--fronting-sni`

- Optional TLS SNI override for fronted requests.
- Defaults to `--fronting-target` when omitted.
- Requires `--fronting-enable`.

Example:

```bash
gdrivedl \
  --fronting-enable \
  --fronting-target front.example.com \
  --fronting-sni front.example.com \
  --proxy 'http://127.0.0.1:2089' \
  --utls-profile chrome_auto \
  -u 'https://www.googleapis.com/drive/v3/files/FILE_ID?alt=media'
```

## Notes

- Public shared folder downloads work without an API key.
- Direct nested public-folder links may not expose folder-title metadata; when that happens, `gdrivedl` falls back to the folder ID for the top-level directory name while still downloading the folder contents.
- Google Apps Script project files cannot be downloaded from folder links.
- When downloading folders, `-m` filters by source Drive `mimeType`.

## Help

```bash
gdrivedl --help
```
