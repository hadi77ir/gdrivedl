# gdrivedl

English | [فارسی](README.fa.md)

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
- Optional connectivity scanning with reusable fronting targets, IPs, and `uTLS` profiles
- Structured event output with timestamps for GUI integrations and JSON-mode CLI use
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

For richer integrations, set `gdrivedl.Request.EventObserver` to receive timestamped `Event` records for logs, progress updates, and reports.

## API Key

Public shared folder downloads do not require a Google Drive API key.
API keys are still used for resumable downloads and Drive API metadata lookups.

`gdrivedl` reads the key from:

- `--api-key`
- `GDRIVEDL_APIKEY`
- `GOODLS_APIKEY` as a legacy fallback

Example:

```bash
export GDRIVEDL_APIKEY='your-api-key'
```

## Basic Usage

Download a shared file:

```bash
gdrivedl get -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

Shared-link downloads must be publicly accessible without an interactive Google sign-in.

Download a Google Docs file as plain text:

```bash
gdrivedl get -u 'https://docs.google.com/document/d/FILE_ID/edit?usp=sharing' -e txt
```

Download a public shared folder without an API key:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing'
```

Download a shared folder with an API key:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' --api-key "$GDRIVEDL_APIKEY"
```

Show folder metadata/listing without an API key:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' --file-info
```

Show file metadata with an API key:

```bash
gdrivedl get -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing' --api-key "$GDRIVEDL_APIKEY" --file-info
```

Run a resumable download:

```bash
gdrivedl get -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing' --api-key "$GDRIVEDL_APIKEY" --resumable-download 100m
```

Resume a folder download and keep already completed files:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' --api-key "$GDRIVEDL_APIKEY" --resumable-download 100m
```

Force matching completed files inside a folder to be downloaded again:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' --enable-redownload
```

- Existing directories are reused automatically on later folder runs.
- Completed files are kept and skipped by default when the local size matches the folder listing or resolved remote size.
- Use `--enable-redownload` to download already completed matching files again.
- Partial files are resumed when range downloads are supported.
- If a file cannot be resumed, `gdrivedl` preserves the partial file temporarily and re-downloads that file from the beginning instead of failing the whole folder resume path.
- If resumable folder mode is not enabled or not available, mismatched partial files are re-downloaded instead.
- Pressing `Ctrl+C` cancels the current job through the shared context, keeps the closest safe partial state on disk, and lets you rerun later with `-r` to continue from the nearest possible point.

Test a download request without writing any files:

```bash
gdrivedl get --dry-run -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

Emit structured JSON events instead of plain log lines:

```bash
gdrivedl get --json -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

Download multiple URLs from a file:

```bash
gdrivedl get --url-list urls.txt
```

Download multiple URLs from standard input:

```bash
cat urls.txt | gdrivedl get --url-list -
```

Download a list concurrently with progress and an exit report:

```bash
gdrivedl get \
  --url-list urls.txt \
  --concurrency 4 \
  --progress \
  --exit-report \
  --proxy 'http://127.0.0.1:2089'
```

## Scan And Test

Probe viable direct and fronted routes with the standalone scanner:

```bash
gdrivedl scan
```

Run only the IP-discovery phase:

```bash
gdrivedl scan --scan-mode only-ip
```

Run only the domain/SNI phase against previously discovered IPs:

```bash
gdrivedl scan \
  --scan-mode only-domains \
  --resolve-to 203.0.113.10,203.0.113.11 \
  --fronting-target google.com \
  --fronting-sni www.google.com
```

Run the scanner with parallel workers and a per-probe round-trip timeout:

```bash
gdrivedl scan --scan-concurrency 8 --roundtrip-timeout 8s
```

Add extra candidate domains from a file or from standard input:

```bash
gdrivedl scan --scan-domain-list extra-domains.txt
cat extra-domains.txt | gdrivedl scan --scan-domain-list -
```

Add extra IPs or IPv4 CIDR ranges, or sample a few random IPs from each CIDR:

```bash
gdrivedl scan --scan-ip-list extra-ips.txt --scan-ip-random-count 16
```

Send one transport probe without starting a download:

```bash
gdrivedl test --fronting-enable --fronting-target google.com --utls-profile firefox_auto
```

Save reusable scan results into a YAML config file, merging into it when it already exists:

```bash
gdrivedl scan --save gdrivedl.yml
```

The scanner probes `https://gstatic.com/generate_204` and reports reusable values for:

- `--fronting-target`
- `--fronting-sni`
- `--resolve-to`
- `--utls-profile`

Scanner runs are cancellable with `Ctrl+C`. `--verbosity 1` shows phase-level live logs, `--verbosity 2` adds step-level details, and `--json` emits structured timestamped `log` and `progress` events while the scan is running.

`--scan-mode full` runs both phases sequentially. `--scan-mode only-ip` scans DNS results, configured `--resolve-to` values, and IP ranges. `--scan-mode only-domains` accepts explicit `--resolve-to` IPs, and when `--fronting-target` is provided it also widens the scan with system-DNS IPs resolved from those fronting targets, even if `--resolve-to` is already set.

`--scan-concurrency` controls how many scan workers run in parallel for direct probes, DNS resolution, dial checks, and fronting probes. `--roundtrip-timeout` limits each individual transport probe/request round trip without changing the higher-level HTTP client timeout.

`--save` writes the scan results back into a YAML config file. It preserves unrelated sections such as `defaults`, `get`, and `merge`, while updating reusable `transport` and `scan` values such as `fronting-target`, `fronting-sni`, `resolve-to`, and `utls-profile`.

It uses embedded default scan sources generated from `assets/known-domains.txt` and `assets/known-ip-ranges.txt`, and you can extend them with `--scan-domain-list` and `--scan-ip-list`. By default, scan samples up to `16` IPs from each CIDR; set `--scan-ip-random-count 0` to expand ranges fully.

## Merge

Merge chunk files from the current folder into one output file:

```bash
gdrivedl merge -o merged.bin
```

Merge multiple `split_nnnn` folders into one output file:

```bash
gdrivedl merge -o merged.bin split_0001 split_0002 split_0003
```

Merge from a parent folder containing `split_nnnn` subfolders:

```bash
gdrivedl merge -o merged.bin ./download-parts
```

Delete chunks only after a successful safe merge completes:

```bash
gdrivedl merge -o merged.bin --delete-chunks ./download-parts
```

Use the older streaming mode that writes directly to the final output and deletes chunks as it goes:

```bash
gdrivedl merge -o merged.bin --unsafe ./download-parts
```

Preview the merge order without writing the output file:

```bash
gdrivedl merge -o merged.bin --dry-run ./download-parts
```

Notes:

- `gdrivedl merge` uses safe mode by default: it writes to a temporary output first, renames it into place after success, and keeps source chunks unless `--delete-chunks` is set.
- `gdrivedl merge --unsafe` restores the old direct-write behavior and deletes chunks while merging, but cancellation is not supported in that mode.
- `gdrivedl merge --dry-run` prints the chunk paths in the same alphanumeric ascending order that the real merge will use, and does not create the output file or delete any inputs.

## YAML Config

Each subcommand accepts `--config`.

- Default path: `$XDG_CONFIG_DIR/gdrivedl.yml`
- Fallback path: `$XDG_CONFIG_HOME/gdrivedl.yml`, then the user config directory's `gdrivedl.yml`
- Disable loading explicitly: `--config ''`

Example:

```yaml
defaults:
  json: false

transport:
  proxy: http://127.0.0.1:2089
  timeout: 45s
  roundtrip-timeout: 10s
  retry-count: 2
  verbosity: 1

get:
  progress: true
  concurrency: 4
  directory: /tmp/downloads
  api-key: your-api-key
  enable-redownload: false
  fronting-enable: true
  fronting-target: google.com
  utls-profile: firefox_auto,chrome_auto

scan:
  scan-mode: full
  scan-concurrency: 8
  fronting-enable: true
  fronting-target: google.com
  scan-ip-random-count: 16

merge:
  progress: true
  delete-chunks: false
  dry-run: false
```

Config files intentionally do not accept one-shot CLI inputs such as `url`, `url-list`, `help`, `version`, or merge input arguments.

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

`--json`

- Emits one JSON object per line.
- Includes timestamped `log`, `progress`, and `report` events.
- Makes CLI output easier to consume from GUI wrappers and automation.

`--no-json`

- Disables JSON event output even when `json: true` comes from config.

`--dry-run`

- Sends the download request without saving the response body to disk.
- Does not create files or directories.
- Useful for testing connectivity, request routing, and remote accessibility.

Common negative overrides:

- `--no-progress` disables both aggregate progress and the legacy single-file progress bar.
- `--no-exit-report` and `--no-completion-report` disable report output inherited from config.
- `--no-fronting`, `--no-prefer-http2`, and `--no-force-http1` disable transport booleans inherited from config.
- `--safe` disables merge `unsafe` mode inherited from config.
- `--keep-chunks` disables merge `delete-chunks` inherited from config.

Notes:

- `--progress` cannot be combined with `--no-progress`.
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
gdrivedl get \
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
gdrivedl get --proxy 'http://127.0.0.1:2089' -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### `--retry-count`

Set how many times `gdrivedl` retries after the initial request attempt.

- Retries transient network failures.
- Retries HTTP `408`, `425`, `429`, `500`, `502`, `503`, and `504` responses.
- Defaults to `0`.

Example:

```bash
gdrivedl get --retry-count 2 --proxy 'http://127.0.0.1:2089' -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
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

`--utls-profile` accepts one profile or a comma-separated list used in round-robin order.

```bash
gdrivedl get --utls-profile 'firefox_auto,chrome_auto' -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
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
gdrivedl get \
  --prefer-http2 \
  --share-http2-connection \
  --utls-profile chrome_auto \
  --proxy 'http://127.0.0.1:2089' \
  -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### `--resolve-to`

Override the network dial IP while preserving the original request port and logical host.

You can pass a comma-separated list of IPs. Requests select from that list in round-robin order.

Example:

```bash
gdrivedl get --resolve-to 203.0.113.10 -u 'https://www.googleapis.com/drive/v3/files/FILE_ID?alt=media'
```

### Domain Fronting

`--fronting-enable`

- Enables HTTP domain fronting in the shared transport for all requests.

`--fronting-target`

- Fronting target hostname used for network dial.
- The original request port is preserved.
- Accepts a comma-separated list of hostnames and rotates through them in round-robin order.
- Requires `--fronting-enable`.

`--fronting-sni`

- Optional TLS SNI override for fronted requests.
- Defaults to `--fronting-target` when omitted.
- Requires `--fronting-enable`.

Example:

```bash
gdrivedl get \
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
gdrivedl help get
gdrivedl help scan
```

## Final Note

This codebase has been developed with the help of AI assistants, including Codex and GPT-5.4.
