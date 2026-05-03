package gdrivedl

import "github.com/urfave/cli"

type configPathOptionFlags struct {
	ConfigPath string
}

type jsonOutputOptionFlags struct {
	JSONOutput bool
}

type transportOptionFlags struct {
	DumpRequest       bool
	DumpResponse      bool
	FrontingEnable    bool
	FrontingSNI       string
	FrontingTarget    string
	ForceHTTP1        bool
	PreferHTTP2       bool
	Proxy             string
	RoundTripTimeout  string
	RequestDelay      string
	ResolveTo         string
	RetryCount        int
	ShareHTTP2Conn    bool
	Timeout           string
	UTLSProfile       string
	Verbosity         int
	ScanIPRandomCount int
	ScanDomainList    string
	ScanIPList        string
}

type downloadProgressOptionFlags struct {
	CompletionReport bool
	EnableProgress   bool
	ExitReport       bool
	NoProgress       bool
	MaxConcurrency   int
}

type downloadOptionFlags struct {
	APIKey                string
	Directory             string
	DryRun                bool
	EnableRedownload      bool
	Extension             string
	Filename              string
	MimeTypes             []string
	NotCreateTopDirectory bool
	Overwrite             bool
	ResumableDownload     string
	ShowFileInfo          bool
	Skip                  bool
	SkipError             bool
	URL                   string
	URLList               string
}

type mergeOptionFlags struct {
	DeleteChunks bool
	DryRun       bool
	ExitReport   bool
	Inputs       []string
	Output       string
	Overwrite    bool
	Progress     bool
	Unsafe       bool
	Verbosity    int
}

type getCommandOptions struct {
	configPathOptionFlags
	jsonOutputOptionFlags
	transportOptionFlags
	downloadProgressOptionFlags
	downloadOptionFlags
}

type scanCommandOptions struct {
	configPathOptionFlags
	jsonOutputOptionFlags
	transportOptionFlags
	ScanConcurrency int
	ScanMode        scanMode
	SavePath        string
}

type testCommandOptions struct {
	configPathOptionFlags
	jsonOutputOptionFlags
	transportOptionFlags
}

type mergeCommandOptions struct {
	configPathOptionFlags
	jsonOutputOptionFlags
	mergeOptionFlags
}

func concatCLIFlags(groups ...[]cli.Flag) []cli.Flag {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	flags := make([]cli.Flag, 0, total)
	for _, group := range groups {
		flags = append(flags, group...)
	}
	return flags
}

func configCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "config",
			Usage: "Path to the YAML config file. Defaults to $XDG_CONFIG_DIR/gdrivedl.yml when available, otherwise to $XDG_CONFIG_HOME/gdrivedl.yml, then the user config directory's gdrivedl.yml. Set to an empty string to disable config loading.",
		},
	}
}

func jsonCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "json",
			Usage: "Emit structured JSON events with timestamps for logs, progress updates, and reports.",
		},
		&cli.BoolFlag{
			Name:  "no-json",
			Usage: "Disable JSON event output even when it is enabled by config.",
		},
	}
}

func downloadTargetCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "url, u",
			Usage: "Shared Google Drive file or folder URL. Required unless --url-list is used or URLs are piped on standard input.",
		},
		&cli.StringFlag{
			Name:  "url-list",
			Usage: "Path to a newline-delimited URL list. Use '-' to read the list from standard input.",
		},
	}
}

func downloadContentCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "extension, e",
			Usage: "Export format for Google Docs files such as documents, spreadsheets, and presentations.",
			Value: "pdf",
		},
		&cli.StringFlag{
			Name:  "filename, f",
			Usage: "Override the output filename. By default the remote filename is used.",
		},
		&cli.StringSliceFlag{
			Name:  "mime-type, mimetype, m",
			Usage: "Comma-separated Google Drive mimeTypes to keep when downloading from a folder.",
		},
		&cli.StringFlag{
			Name:  "resumable-download, resumabledownload, r",
			Usage: "Enable resumable downloads with the given chunk size such as 1m or 100m. API key is required for resumable file mode.",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Send the download request without saving files or creating directories.",
		},
		&cli.BoolFlag{
			Name:  "no-dry-run",
			Usage: "Disable dry-run mode even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "file-info, fileinf, i",
			Usage: "Print metadata instead of downloading. API key is required for file metadata; public folder links can be listed without an API key.",
		},
		&cli.BoolFlag{
			Name:  "no-file-info",
			Usage: "Disable metadata-only mode even when it is enabled by config.",
		},
		&cli.StringFlag{
			Name:  "api-key, apikey, key",
			Usage: "Optional Google API key used for resumable downloads, file metadata, and Drive API fallback access.",
		},
		&cli.StringFlag{
			Name:  "directory, d",
			Usage: "Directory used for saved files. Defaults to the current working directory.",
		},
		&cli.BoolFlag{
			Name:  "no-top-directory, notcreatetopdirectory, ntd",
			Usage: "When downloading a folder tree, place its contents directly in the working directory instead of creating the top-level folder.",
		},
		&cli.BoolFlag{
			Name:  "create-top-directory",
			Usage: "Create the top-level directory when downloading a folder tree, even if no-top-directory is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "skip-errors, skiperror, se",
			Usage: "Continue downloading folder contents when an item fails.",
		},
		&cli.BoolFlag{
			Name:  "no-skip-errors",
			Usage: "Stop on folder item errors even when skip-errors is enabled by config.",
		},
	}
}

func downloadProgressCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "no-progress, NoProgress, np",
			Usage: "Disable all progress output, including the aggregate progress view and the legacy single-file progress bar.",
		},
		&cli.BoolFlag{
			Name:  "progress",
			Usage: "Show aggregate download progress with current file, total progress, speed, and ETA.",
		},
		&cli.IntFlag{
			Name:  "concurrency",
			Usage: "Maximum number of concurrent downloads for URL lists and folder downloads.",
			Value: 1,
		},
		&cli.BoolFlag{
			Name:  "exit-report",
			Usage: "Print a final per-file download report on exit.",
		},
		&cli.BoolFlag{
			Name:  "no-exit-report",
			Usage: "Disable the final per-file download report even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "completion-report",
			Usage: "Print a per-file completion report line after each successful download.",
		},
		&cli.BoolFlag{
			Name:  "no-completion-report",
			Usage: "Disable per-file completion reports even when they are enabled by config.",
		},
	}
}

func downloadOverwriteCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "overwrite, o",
			Usage: "Overwrite existing files with the same name.",
		},
		&cli.BoolFlag{
			Name:  "no-overwrite",
			Usage: "Disable overwrite mode even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "skip, s",
			Usage: "Skip existing files with the same name.",
		},
		&cli.BoolFlag{
			Name:  "enable-redownload, redownload",
			Usage: "When downloading folders, redownload files whose local size already matches the remote size instead of skipping them.",
		},
		&cli.BoolFlag{
			Name:  "no-enable-redownload, no-redownload",
			Usage: "Disable folder redownload mode even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "no-skip",
			Usage: "Disable skip-existing mode even when it is enabled by config.",
		},
	}
}

func transportCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "proxy",
			Usage: "Upstream proxy URL. Supported schemes are http:// and socks5://. Example: http://127.0.0.1:2089.",
		},
		&cli.StringFlag{
			Name:  "timeout",
			Usage: "HTTP client timeout. Accepts Go duration strings like 30s, 2m, or plain seconds like 60.",
		},
		&cli.StringFlag{
			Name:  "request-delay",
			Usage: "Minimum delay between HTTP requests. Accepts Go duration strings like 500ms, 2s, or plain seconds like 1.",
		},
		&cli.StringFlag{
			Name:  "roundtrip-timeout",
			Usage: "Per-request round-trip timeout inside the shared transport. Accepts Go duration strings like 10s, 500ms, or plain seconds like 30.",
		},
		&cli.IntFlag{
			Name:  "retry-count",
			Usage: "Number of retries after the initial HTTP request attempt. Retries network failures and retryable HTTP statuses.",
			Value: 0,
		},
		&cli.IntFlag{
			Name:  "verbosity",
			Usage: "HTTP logging verbosity level. 0 disables stage logs, 1 enables stage and status logs, 2 adds detailed connection logs.",
			Value: 0,
		},
		&cli.BoolFlag{
			Name:  "dump-request",
			Usage: "Dump outgoing HTTP requests before they are sent.",
		},
		&cli.BoolFlag{
			Name:  "no-dump-request",
			Usage: "Disable HTTP request dumping even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "dump-response",
			Usage: "Dump received HTTP response headers after they are received.",
		},
		&cli.BoolFlag{
			Name:  "no-dump-response",
			Usage: "Disable HTTP response dumping even when it is enabled by config.",
		},
		&cli.StringFlag{
			Name:  "resolve-to",
			Usage: "Override the network dial IP for requests while preserving the original request port and logical host. Accepts an IP or a comma-separated list of IPs used in round-robin order.",
		},
		&cli.StringFlag{
			Name:  "utls-profile",
			Usage: "uTLS ClientHello profile for HTTPS requests. Accepts one value or a comma-separated list used in round-robin order. Supported values include chrome_auto, firefox_auto, safari_auto, ios_auto, edge_auto, 360_auto, qq_auto, randomized, randomized_alpn, and randomized_no_alpn.",
		},
		&cli.BoolFlag{
			Name:  "prefer-http2",
			Usage: "Prefer HTTP/2 over HTTP/1.1 for HTTPS requests when the server supports it.",
		},
		&cli.BoolFlag{
			Name:  "no-prefer-http2",
			Usage: "Disable HTTP/2 preference even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "force-http1",
			Usage: "Force HTTP/1.1 for HTTPS requests and disable HTTP/2 negotiation.",
		},
		&cli.BoolFlag{
			Name:  "no-force-http1",
			Usage: "Disable forced HTTP/1.1 mode even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "share-http2-connection",
			Usage: "Reuse a negotiated HTTP/2 TLS connection for multiple requests to the same target. Implies HTTP/2 preference and cannot be combined with --force-http1.",
		},
		&cli.BoolFlag{
			Name:  "no-share-http2-connection",
			Usage: "Disable shared HTTP/2 connection reuse even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "fronting-enable",
			Usage: "Enable HTTP domain fronting in the shared transport for all requests.",
		},
		&cli.BoolFlag{
			Name:  "no-fronting",
			Usage: "Disable HTTP domain fronting even when it is enabled by config.",
		},
		&cli.StringFlag{
			Name:  "fronting-sni",
			Usage: "Optional TLS SNI override for fronted requests. Defaults to the fronting target hostname. Requires --fronting-enable.",
		},
		&cli.StringFlag{
			Name:  "fronting-target",
			Usage: "Fronting target hostname used for network dial. The original request port is preserved. Accepts a hostname or a comma-separated list of hostnames used in round-robin order. Requires --fronting-enable.",
		},
	}
}

func scanCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "scan-mode",
			Usage: "Select the scanner phase. Supported values are full, only-ip, and only-domains.",
			Value: string(scanModeFull),
		},
		&cli.StringFlag{
			Name:  "save",
			Usage: "Save scan results into a YAML config file. If the file exists, merge the saved transport and scan values into it.",
		},
		&cli.StringFlag{
			Name:  "scan-domain-list",
			Usage: "Optional path to a newline-delimited list of extra scan domains. Use '-' to read them from standard input.",
		},
		&cli.StringFlag{
			Name:  "scan-ip-list",
			Usage: "Optional path to a newline-delimited list of extra scan IPs or IPv4 CIDR ranges. Use '-' to read them from standard input.",
		},
		&cli.IntFlag{
			Name:  "scan-ip-random-count",
			Usage: "When greater than 0, randomly select up to this many IPs from each CIDR entry used by scan instead of probing the full range. Explicit IP entries are still tested. Set 0 to expand ranges fully.",
			Value: 16,
		},
		&cli.IntFlag{
			Name:  "scan-concurrency",
			Usage: "Maximum number of concurrent scan workers used for probing, DNS resolution, and dial checks.",
			Value: 1,
		},
	}
}

func mergeCLIFlags() []cli.Flag {
	return concatCLIFlags(
		configCLIFlags(),
		jsonCLIFlags(),
		mergeOutputCLIFlags(),
		mergeBehaviorCLIFlags(),
		mergeProgressCLIFlags(),
		mergeLoggingCLIFlags(),
	)
}

func mergeOutputCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "output, o",
			Usage: "Path of the merged output file.",
		},
		&cli.BoolFlag{
			Name:  "overwrite",
			Usage: "Overwrite the output file if it already exists.",
		},
		&cli.BoolFlag{
			Name:  "no-overwrite",
			Usage: "Disable overwrite mode even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "delete-chunks",
			Usage: "In safe mode, delete source chunks and empty split_nnnn folders only after a successful final output is created.",
		},
		&cli.BoolFlag{
			Name:  "keep-chunks",
			Usage: "Keep source chunks after a safe merge even when delete-chunks is enabled by config.",
		},
	}
}

func mergeBehaviorCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Print the chunk files in merge order without creating the output file or deleting any source files.",
		},
		&cli.BoolFlag{
			Name:  "no-dry-run",
			Usage: "Disable merge dry-run mode even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "unsafe",
			Usage: "Use the old streaming merge mode that writes directly to the final output and deletes chunks as it goes. Cancellation is not supported in this mode.",
		},
		&cli.BoolFlag{
			Name:  "safe",
			Usage: "Use safe merge mode even when unsafe mode is enabled by config.",
		},
	}
}

func mergeProgressCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "progress",
			Usage: "Show merge progress with current chunk, total progress, speed, and ETA.",
		},
		&cli.BoolFlag{
			Name:  "no-progress",
			Usage: "Disable merge progress output even when it is enabled by config.",
		},
		&cli.BoolFlag{
			Name:  "exit-report",
			Usage: "Print a final per-file report on exit.",
		},
		&cli.BoolFlag{
			Name:  "no-exit-report",
			Usage: "Disable the final merge report even when it is enabled by config.",
		},
	}
}

func mergeLoggingCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.IntFlag{
			Name:  "verbosity",
			Usage: "Merge logging verbosity level. 0 disables logs, 1 logs chunk writes, 2 adds chunk deletion logs.",
			Value: 0,
		},
	}
}

func getCommandCLIFlags() []cli.Flag {
	return concatCLIFlags(
		configCLIFlags(),
		downloadTargetCLIFlags(),
		downloadContentCLIFlags(),
		downloadProgressCLIFlags(),
		downloadOverwriteCLIFlags(),
		jsonCLIFlags(),
		transportCLIFlags(),
	)
}

func scanCommandCLIFlags() []cli.Flag {
	return concatCLIFlags(
		configCLIFlags(),
		jsonCLIFlags(),
		transportCLIFlags(),
		scanCLIFlags(),
	)
}

func testCommandCLIFlags() []cli.Flag {
	return concatCLIFlags(
		configCLIFlags(),
		jsonCLIFlags(),
		transportCLIFlags(),
	)
}
