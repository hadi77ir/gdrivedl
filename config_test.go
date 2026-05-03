package gdrivedl

import (
	"flag"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli"
)

func TestParseGetCommandConfigTransportOptions(t *testing.T) {
	ctx := newTestCommandContext(t, "get", []string{
		"--completion-report",
		"--dry-run",
		"--json",
		"--progress",
		"--exit-report",
		"--concurrency", "3",
		"--timeout", "45s",
		"--roundtrip-timeout", "12s",
		"--request-delay", "1500ms",
		"--retry-count", "4",
		"--verbosity", "2",
		"--dump-request",
		"--dump-response",
		"--prefer-http2",
		"--share-http2-connection",
		"--fronting-enable",
		"--fronting-target", "front-a.example.com,front-b.example.com",
		"--resolve-to", "203.0.113.10,203.0.113.11",
		"--proxy", "socks5://127.0.0.1:1080",
		"--utls-profile", "firefox_auto,chrome_auto",
		"--url-list", "-",
	})

	cfg, err := parseConfig(ctx, func(string) string { return "" }, func(path string) (string, error) {
		return "/tmp/workdir", nil
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.WorkDir != "/tmp/workdir" {
		t.Fatalf("workdir = %q, want /tmp/workdir", cfg.WorkDir)
	}
	if !cfg.EnableProgress {
		t.Fatal("progress should be enabled")
	}
	if !cfg.CompletionReport {
		t.Fatal("completion report should be enabled")
	}
	if !cfg.JSONOutput {
		t.Fatal("json output should be enabled")
	}
	if !cfg.DryRun {
		t.Fatal("dry run should be enabled")
	}
	if !cfg.ExitReport {
		t.Fatal("exit report should be enabled")
	}
	if cfg.MaxConcurrency != 3 {
		t.Fatalf("concurrency = %d, want 3", cfg.MaxConcurrency)
	}
	if cfg.Transport.Timeout.Seconds() != 45 {
		t.Fatalf("timeout = %v, want 45s", cfg.Transport.Timeout)
	}
	if cfg.Transport.RoundTripTimeout != 12*time.Second {
		t.Fatalf("roundtrip timeout = %v, want 12s", cfg.Transport.RoundTripTimeout)
	}
	if cfg.Transport.RequestDelay != 1500*time.Millisecond {
		t.Fatalf("request delay = %v, want 1500ms", cfg.Transport.RequestDelay)
	}
	if cfg.Transport.RetryCount != 4 {
		t.Fatalf("retry count = %d, want 4", cfg.Transport.RetryCount)
	}
	if cfg.Transport.Verbosity != 2 {
		t.Fatalf("verbosity = %d, want 2", cfg.Transport.Verbosity)
	}
	if !cfg.Transport.PreferHTTP2 {
		t.Fatal("prefer-http2 should be enabled")
	}
	if !cfg.Transport.ShareHTTP2Conn {
		t.Fatal("share-http2-connection should be enabled")
	}
	if cfg.Transport.ForceHTTP1 {
		t.Fatal("force-http1 should be disabled")
	}
	if !cfg.Transport.DumpRequest || !cfg.Transport.DumpResponse {
		t.Fatal("request/response dump flags should be enabled")
	}
	if cfg.Transport.Proxy == nil || cfg.Transport.Proxy.Scheme != "socks5" {
		t.Fatalf("proxy = %#v, want socks5 proxy", cfg.Transport.Proxy)
	}
	if cfg.Transport.ResolveTo != "203.0.113.10" {
		t.Fatalf("resolve-to = %q, want 203.0.113.10", cfg.Transport.ResolveTo)
	}
	if !reflect.DeepEqual(cfg.Transport.ResolveToAddrs, []string{"203.0.113.10", "203.0.113.11"}) {
		t.Fatalf("resolve-to addrs = %#v", cfg.Transport.ResolveToAddrs)
	}
	if cfg.Transport.UTLSProfileName != "firefox_auto" {
		t.Fatalf("utls profile = %q, want firefox_auto", cfg.Transport.UTLSProfileName)
	}
	if got := len(cfg.Transport.UTLSProfiles); got != 2 {
		t.Fatalf("utls profiles len = %d, want 2", got)
	}
	if !cfg.Transport.Fronting.Enable {
		t.Fatal("fronting should be enabled")
	}
	if cfg.Transport.Fronting.Target != "front-a.example.com" {
		t.Fatalf("fronting target = %q", cfg.Transport.Fronting.Target)
	}
	if cfg.Transport.Fronting.SNI != "front-a.example.com" {
		t.Fatalf("fronting sni = %q, want front-a.example.com", cfg.Transport.Fronting.SNI)
	}
	if !reflect.DeepEqual(cfg.Transport.Fronting.Targets, []string{"front-a.example.com", "front-b.example.com"}) {
		t.Fatalf("fronting targets = %#v", cfg.Transport.Fronting.Targets)
	}
	if cfg.URLList != "-" {
		t.Fatalf("url-list = %q, want -", cfg.URLList)
	}
	if !cfg.Transport.Fronting.Match("www.googleapis.com") {
		t.Fatal("fronting should apply when enabled")
	}
	plan := cfg.Transport.buildRequestPlan(&http.Request{URL: mustParseURL(t, "https://www.googleapis.com/path")})
	if plan.ConnectAddress != "203.0.113.10:443" {
		t.Fatalf("connect address = %q, want 203.0.113.10:443", plan.ConnectAddress)
	}
	if plan.ServerName != "front-a.example.com" {
		t.Fatalf("server name = %q, want front-a.example.com", plan.ServerName)
	}
}

func TestParseScanCommandOptions(t *testing.T) {
	ctx := newTestCommandContext(t, "scan", []string{
		"--json",
		"--scan-mode", "only-domains",
		"--scan-concurrency", "5",
		"--save", "scan-results.yml",
		"--scan-domain-list", "domains.txt",
		"--scan-ip-list", "ips.txt",
		"--scan-ip-random-count", "7",
		"--timeout", "45s",
		"--roundtrip-timeout", "6s",
		"--request-delay", "1500ms",
		"--retry-count", "4",
		"--verbosity", "2",
		"--dump-request",
		"--dump-response",
		"--prefer-http2",
		"--share-http2-connection",
		"--fronting-enable",
		"--fronting-target", "front-a.example.com,front-b.example.com",
		"--resolve-to", "203.0.113.10,203.0.113.11",
		"--proxy", "socks5://127.0.0.1:1080",
		"--utls-profile", "firefox_auto,chrome_auto",
	})
	options, err := parseScanCommandOptionsWithEnv(ctx, cliParseEnvironment{
		getenv:        func(string) string { return "" },
		userConfigDir: func() (string, error) { return "/tmp", nil },
		absPath:       filepath.Abs,
	})
	if err != nil {
		t.Fatalf("parseScanCommandOptionsWithEnv() error = %v", err)
	}
	if !options.JSONOutput {
		t.Fatal("json output should be enabled")
	}
	if options.ScanMode != scanModeOnlyDomains {
		t.Fatalf("scan-mode = %q, want %q", options.ScanMode, scanModeOnlyDomains)
	}
	if options.ScanConcurrency != 5 {
		t.Fatalf("scan-concurrency = %d, want 5", options.ScanConcurrency)
	}
	if options.SavePath != "scan-results.yml" {
		t.Fatalf("save = %q, want scan-results.yml", options.SavePath)
	}
	if options.ScanDomainList != "domains.txt" {
		t.Fatalf("scan-domain-list = %q, want domains.txt", options.ScanDomainList)
	}
	if options.ScanIPList != "ips.txt" {
		t.Fatalf("scan-ip-list = %q, want ips.txt", options.ScanIPList)
	}
	if options.ScanIPRandomCount != 7 {
		t.Fatalf("scan-ip-random-count = %d, want 7", options.ScanIPRandomCount)
	}
	transport, err := buildTransportConfigFromOptions(options.transportOptionFlags)
	if err != nil {
		t.Fatalf("buildTransportConfigFromOptions() error = %v", err)
	}
	if transport.Timeout.Seconds() != 45 {
		t.Fatalf("timeout = %v, want 45s", transport.Timeout)
	}
	if transport.RoundTripTimeout != 6*time.Second {
		t.Fatalf("roundtrip timeout = %v, want 6s", transport.RoundTripTimeout)
	}
	if transport.RequestDelay != 1500*time.Millisecond {
		t.Fatalf("request delay = %v, want 1500ms", transport.RequestDelay)
	}
	if transport.RetryCount != 4 {
		t.Fatalf("retry count = %d, want 4", transport.RetryCount)
	}
	if transport.Verbosity != 2 {
		t.Fatalf("verbosity = %d, want 2", transport.Verbosity)
	}
	if !transport.PreferHTTP2 || !transport.ShareHTTP2Conn {
		t.Fatalf("unexpected HTTP2 flags = %#v", transport)
	}
	if transport.Proxy == nil || transport.Proxy.Scheme != "socks5" {
		t.Fatalf("proxy = %#v, want socks5 proxy", transport.Proxy)
	}
	if transport.ResolveTo != "203.0.113.10" {
		t.Fatalf("resolve-to = %q, want 203.0.113.10", transport.ResolveTo)
	}
	if !reflect.DeepEqual(transport.ResolveToAddrs, []string{"203.0.113.10", "203.0.113.11"}) {
		t.Fatalf("resolve-to addrs = %#v", transport.ResolveToAddrs)
	}
	if transport.UTLSProfileName != "firefox_auto" {
		t.Fatalf("utls profile = %q, want firefox_auto", transport.UTLSProfileName)
	}
	if got := len(transport.UTLSProfiles); got != 2 {
		t.Fatalf("utls profiles len = %d, want 2", got)
	}
	if !transport.Fronting.Enable {
		t.Fatal("fronting should be enabled")
	}
	if transport.Fronting.Target != "front-a.example.com" {
		t.Fatalf("fronting target = %q", transport.Fronting.Target)
	}
	if transport.Fronting.SNI != "front-a.example.com" {
		t.Fatalf("fronting sni = %q, want front-a.example.com", transport.Fronting.SNI)
	}
	if !reflect.DeepEqual(transport.Fronting.Targets, []string{"front-a.example.com", "front-b.example.com"}) {
		t.Fatalf("fronting targets = %#v", transport.Fronting.Targets)
	}
}

func TestParseConfigAPIKeyEnvFallback(t *testing.T) {
	ctx := newTestCommandContext(t, "get", nil)

	cfg, err := parseConfig(ctx, func(name string) string {
		switch name {
		case envval:
			return "new-key"
		case legacyEnvval:
			return "old-key"
		default:
			return ""
		}
	}, func(path string) (string, error) {
		return "/tmp/workdir", nil
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.APIKey != "new-key" {
		t.Fatalf("apikey = %q, want new-key", cfg.APIKey)
	}

	cfg, err = parseConfig(ctx, func(name string) string {
		if name == legacyEnvval {
			return "old-key"
		}
		return ""
	}, func(path string) (string, error) {
		return "/tmp/workdir", nil
	})
	if err != nil {
		t.Fatalf("parseConfig() legacy error = %v", err)
	}
	if cfg.APIKey != "old-key" {
		t.Fatalf("legacy apikey = %q, want old-key", cfg.APIKey)
	}
}

func TestParseGetCommandConfigValidation(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"--progress", "--no-progress"}, wantErr: "please use either '--progress' or '--no-progress'"},
		{args: []string{"--concurrency", "0"}, wantErr: "--concurrency must be greater than 0"},
		{args: []string{"--verbosity", "-1"}, wantErr: "--verbosity must be greater than or equal to 0"},
		{args: []string{"--timeout", "abcx"}, wantErr: "--timeout must be a valid duration"},
		{args: []string{"--roundtrip-timeout", "abcx"}, wantErr: "--roundtrip-timeout must be a valid duration"},
		{args: []string{"--request-delay", "abcx"}, wantErr: "--request-delay must be a valid duration"},
		{args: []string{"--prefer-http2", "--force-http1"}, wantErr: "--prefer-http2 cannot be used with --force-http1"},
		{args: []string{"--share-http2-connection", "--force-http1"}, wantErr: "--share-http2-connection cannot be used with --force-http1"},
		{args: []string{"--fronting-enable"}, wantErr: "--fronting-target is required"},
		{args: []string{"--fronting-enable", "--fronting-target", "https://front.example.com"}, wantErr: "--fronting-target must be a hostname"},
		{args: []string{"--fronting-target", "front.example.com"}, wantErr: "fronting options require --fronting-enable"},
		{args: []string{"--proxy", "ftp://proxy.example.com:21"}, wantErr: "--proxy only supports http:// and socks5:// URLs"},
		{args: []string{"--retry-count", "-1"}, wantErr: "--retry-count must be greater than or equal to 0"},
		{args: []string{"--resolve-to", "example.com"}, wantErr: "--resolve-to must be an IP address"},
		{args: []string{"--utls-profile", "unknown"}, wantErr: "unsupported --utls-profile"},
	}
	for _, tt := range tests {
		ctx := newTestCommandContext(t, "get", tt.args)
		_, err := parseConfig(ctx, func(string) string { return "" }, func(path string) (string, error) {
			return "/tmp/workdir", nil
		})
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Fatalf("parseConfig(%v) error = %v, want substring %q", tt.args, err, tt.wantErr)
		}
	}
}

func TestParseScanCommandValidation(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"--scan-ip-random-count", "-1"}, wantErr: "--scan-ip-random-count must be greater than or equal to 0"},
		{args: []string{"--scan-concurrency", "-1"}, wantErr: "--scan-concurrency must be greater than or equal to 0"},
		{args: []string{"--scan-mode", "invalid"}, wantErr: "unsupported --scan-mode"},
	}
	for _, tt := range tests {
		ctx := newTestCommandContext(t, "scan", tt.args)
		options, err := parseScanCommandOptionsWithEnv(ctx, cliParseEnvironment{
			getenv:        func(string) string { return "" },
			userConfigDir: func() (string, error) { return "/tmp", nil },
			absPath:       filepath.Abs,
		})
		if err == nil {
			_, err = buildScanTransportConfigFromOptions(options.transportOptionFlags)
		}
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Fatalf("scan parse/build (%v) error = %v, want substring %q", tt.args, err, tt.wantErr)
		}
	}
}

func TestReadURLs(t *testing.T) {
	urls, err := readURLs(strings.NewReader("\n# comment\nhttps://example.com/1\nend\nhttps://example.com/2\n"))
	if err != nil {
		t.Fatalf("readURLs() error = %v", err)
	}
	if got, want := len(urls), 2; got != want {
		t.Fatalf("url count = %d, want %d", got, want)
	}
	if urls[0] != "https://example.com/1" || urls[1] != "https://example.com/2" {
		t.Fatalf("unexpected urls = %#v", urls)
	}
}

func TestReadURLList(t *testing.T) {
	t.Run("stdin", func(t *testing.T) {
		urls, err := readURLList("-", strings.NewReader("https://example.com/1\n"))
		if err != nil {
			t.Fatalf("readURLList(stdin) error = %v", err)
		}
		if got, want := len(urls), 1; got != want || urls[0] != "https://example.com/1" {
			t.Fatalf("unexpected urls = %#v", urls)
		}
	})
	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "urls.txt")
		if err := os.WriteFile(path, []byte("https://example.com/1\nhttps://example.com/2\n"), 0666); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		urls, err := readURLList(path, io.NopCloser(strings.NewReader("")))
		if err != nil {
			t.Fatalf("readURLList(file) error = %v", err)
		}
		if got, want := len(urls), 2; got != want {
			t.Fatalf("url count = %d, want %d", got, want)
		}
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return parsed
}

func newTestContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	app := createHelp()
	set := flag.NewFlagSet(app.Name, flag.ContinueOnError)
	for _, flg := range app.Flags {
		flg.Apply(set)
	}
	if err := set.Parse(args); err != nil {
		t.Fatalf("flag parse error: %v", err)
	}
	return cli.NewContext(app, set, nil)
}

func newTestCommandContext(t *testing.T, commandName string, args []string) *cli.Context {
	t.Helper()
	app := createHelp()
	set := flag.NewFlagSet(app.Name, flag.ContinueOnError)
	for _, flg := range app.Flags {
		flg.Apply(set)
	}
	var command *cli.Command
	for i := range app.Commands {
		candidate := &app.Commands[i]
		if candidate.Name == commandName {
			command = candidate
			break
		}
		for _, alias := range candidate.Aliases {
			if alias == commandName {
				command = candidate
				break
			}
		}
		if command != nil {
			break
		}
	}
	if command == nil {
		t.Fatalf("command %q not found", commandName)
	}
	for _, flg := range command.Flags {
		flg.Apply(set)
	}
	if err := set.Parse(args); err != nil {
		t.Fatalf("flag parse error: %v", err)
	}
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = *command
	return ctx
}

func TestParseGetCommandConfigFromYAMLFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gdrivedl.yml")
	configYAML := strings.Join([]string{
		"defaults:",
		"  json: true",
		"transport:",
		"  proxy: http://127.0.0.1:2089",
		"  timeout: 45s",
		"  retry-count: 2",
		"get:",
		"  progress: true",
		"  completion-report: true",
		"  concurrency: 4",
		"  directory: /tmp/from-config",
		"  api-key: config-key",
		"  enable-redownload: true",
		"  mime-type: image/png,application/pdf",
		"  fronting-enable: true",
		"  fronting-target: front-a.example.com,front-b.example.com",
		"  resolve-to: 203.0.113.10,203.0.113.11",
		"  utls-profile: firefox_auto,chrome_auto",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o666); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", configPath, err)
	}

	ctx := newTestCommandContext(t, "get", []string{"--config", configPath, "--url-list", "-", "--retry-count", "5"})
	cfg, err := parseConfig(ctx, func(string) string { return "" }, func(path string) (string, error) {
		return "/tmp/workdir", nil
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.JSONOutput {
		t.Fatal("json output should be loaded from defaults")
	}
	if !cfg.EnableProgress {
		t.Fatal("progress should be loaded from config")
	}
	if !cfg.CompletionReport {
		t.Fatal("completion report should be loaded from config")
	}
	if cfg.MaxConcurrency != 4 {
		t.Fatalf("concurrency = %d, want 4", cfg.MaxConcurrency)
	}
	if cfg.WorkDir != "/tmp/from-config" {
		t.Fatalf("workdir = %q, want /tmp/from-config", cfg.WorkDir)
	}
	if cfg.APIKey != "config-key" {
		t.Fatalf("api-key = %q, want config-key", cfg.APIKey)
	}
	if !cfg.EnableRedownload {
		t.Fatal("enable-redownload should be loaded from config")
	}
	if !reflect.DeepEqual(cfg.InputtedMimeType, []string{"image/png", "application/pdf"}) {
		t.Fatalf("mime-type = %#v", cfg.InputtedMimeType)
	}
	if cfg.Transport.Proxy == nil || cfg.Transport.Proxy.String() != "http://127.0.0.1:2089" {
		t.Fatalf("proxy = %#v, want http://127.0.0.1:2089", cfg.Transport.Proxy)
	}
	if cfg.Transport.Timeout != 45*time.Second {
		t.Fatalf("timeout = %v, want 45s", cfg.Transport.Timeout)
	}
	if cfg.Transport.RetryCount != 5 {
		t.Fatalf("retry count = %d, want CLI override 5", cfg.Transport.RetryCount)
	}
	if !cfg.Transport.Fronting.Enable {
		t.Fatal("fronting should be enabled from config")
	}
	if cfg.Transport.Fronting.Target != "front-a.example.com" {
		t.Fatalf("fronting target = %q, want front-a.example.com", cfg.Transport.Fronting.Target)
	}
	if !reflect.DeepEqual(cfg.Transport.Fronting.Targets, []string{"front-a.example.com", "front-b.example.com"}) {
		t.Fatalf("fronting targets = %#v", cfg.Transport.Fronting.Targets)
	}
	if cfg.Transport.ResolveTo != "203.0.113.10" {
		t.Fatalf("resolve-to = %q, want 203.0.113.10", cfg.Transport.ResolveTo)
	}
	if !reflect.DeepEqual(cfg.Transport.ResolveToAddrs, []string{"203.0.113.10", "203.0.113.11"}) {
		t.Fatalf("resolve-to addrs = %#v", cfg.Transport.ResolveToAddrs)
	}
	if cfg.Transport.UTLSProfileName != "firefox_auto" {
		t.Fatalf("utls profile = %q, want firefox_auto", cfg.Transport.UTLSProfileName)
	}
	if cfg.URLList != "-" {
		t.Fatalf("url-list = %q, want -", cfg.URLList)
	}
}

func TestParseScanCommandConfigFromDefaultXDGPath(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "gdrivedl.yml")
	configYAML := strings.Join([]string{
		"defaults:",
		"  json: true",
		"transport:",
		"  timeout: 30s",
		"  roundtrip-timeout: 8s",
		"scan:",
		"  scan-mode: only-ip",
		"  scan-concurrency: 4",
		"  fronting-enable: true",
		"  fronting-target: scan-a.example.com,scan-b.example.com",
		"  scan-domain-list: domains.txt",
		"  scan-ip-random-count: 7",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o666); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", configPath, err)
	}

	ctx := newTestCommandContext(t, "scan", nil)
	options, err := parseScanCommandOptionsWithEnv(ctx, cliParseEnvironment{
		getenv: func(name string) string {
			if name == "XDG_CONFIG_DIR" {
				return configDir
			}
			return ""
		},
		userConfigDir: func() (string, error) { return "/tmp/unused", nil },
		absPath:       filepath.Abs,
	})
	if err != nil {
		t.Fatalf("parseScanCommandOptionsWithEnv() error = %v", err)
	}
	if !options.JSONOutput {
		t.Fatal("json output should be loaded from defaults")
	}
	if options.ScanMode != scanModeOnlyIP {
		t.Fatalf("scan-mode = %q, want %q", options.ScanMode, scanModeOnlyIP)
	}
	if options.ScanConcurrency != 4 {
		t.Fatalf("scan-concurrency = %d, want 4", options.ScanConcurrency)
	}
	if options.ScanDomainList != "domains.txt" {
		t.Fatalf("scan-domain-list = %q, want domains.txt", options.ScanDomainList)
	}
	if options.ScanIPRandomCount != 7 {
		t.Fatalf("scan-ip-random-count = %d, want 7", options.ScanIPRandomCount)
	}
	transport, err := buildTransportConfigFromOptions(options.transportOptionFlags)
	if err != nil {
		t.Fatalf("buildTransportConfigFromOptions() error = %v", err)
	}
	if transport.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v, want 30s", transport.Timeout)
	}
	if transport.RoundTripTimeout != 8*time.Second {
		t.Fatalf("roundtrip timeout = %v, want 8s", transport.RoundTripTimeout)
	}
	if !transport.Fronting.Enable {
		t.Fatal("fronting should be enabled from config")
	}
	if !reflect.DeepEqual(transport.Fronting.Targets, []string{"scan-a.example.com", "scan-b.example.com"}) {
		t.Fatalf("fronting targets = %#v", transport.Fronting.Targets)
	}
}

func TestParseCommandConfigCanBeDisabled(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "gdrivedl.yml")
	if err := os.WriteFile(configPath, []byte("defaults:\n  json: true\n"), 0o666); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", configPath, err)
	}

	ctx := newTestCommandContext(t, "scan", []string{"--config", ""})
	options, err := parseScanCommandOptionsWithEnv(ctx, cliParseEnvironment{
		getenv: func(name string) string {
			if name == "XDG_CONFIG_DIR" {
				return configDir
			}
			return ""
		},
		userConfigDir: func() (string, error) { return "/tmp/unused", nil },
		absPath:       filepath.Abs,
	})
	if err != nil {
		t.Fatalf("parseScanCommandOptionsWithEnv() error = %v", err)
	}
	if options.JSONOutput {
		t.Fatal("json output should stay disabled when --config '' is used")
	}
}

func TestParseYAMLCLIConfigRejectsUnsupportedGetURLKey(t *testing.T) {
	_, err := parseYAMLCLIConfig(strings.NewReader("get:\n  url: https://drive.google.com/file/d/test/view\n"))
	if err == nil || !strings.Contains(err.Error(), "unsupported get key \"url\"") {
		t.Fatalf("parseYAMLCLIConfig() error = %v, want unsupported get key", err)
	}
}

func TestParseGetCommandConfigNegativeFlagsOverrideConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gdrivedl.yml")
	configYAML := strings.Join([]string{
		"defaults:",
		"  json: true",
		"transport:",
		"  dump-request: true",
		"  fronting-enable: true",
		"  fronting-target: front-a.example.com",
		"  prefer-http2: true",
		"get:",
		"  dry-run: true",
		"  progress: true",
		"  completion-report: true",
		"  exit-report: true",
		"  file-info: true",
		"  enable-redownload: true",
		"  no-top-directory: true",
		"  skip-errors: true",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o666); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", configPath, err)
	}

	ctx := newTestCommandContext(t, "get", []string{
		"--config", configPath,
		"--url-list", "-",
		"--no-json",
		"--no-dry-run",
		"--no-progress",
		"--no-completion-report",
		"--no-exit-report",
		"--no-file-info",
		"--no-enable-redownload",
		"--create-top-directory",
		"--no-skip-errors",
		"--no-dump-request",
		"--no-fronting",
		"--no-prefer-http2",
	})

	cfg, err := parseConfig(ctx, func(string) string { return "" }, func(path string) (string, error) {
		return "/tmp/workdir", nil
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.JSONOutput {
		t.Fatal("json output should be disabled by --no-json")
	}
	if cfg.DryRun {
		t.Fatal("dry run should be disabled by --no-dry-run")
	}
	if cfg.EnableProgress {
		t.Fatal("progress should be disabled by --no-progress")
	}
	if !cfg.Disp {
		t.Fatal("display suppression should be enabled by --no-progress")
	}
	if cfg.CompletionReport {
		t.Fatal("completion report should be disabled by --no-completion-report")
	}
	if cfg.ExitReport {
		t.Fatal("exit report should be disabled by --no-exit-report")
	}
	if cfg.ShowFileInf {
		t.Fatal("file info mode should be disabled by --no-file-info")
	}
	if cfg.EnableRedownload {
		t.Fatal("enable-redownload should be disabled by --no-enable-redownload")
	}
	if cfg.Notcreatetopdirectory {
		t.Fatal("no-top-directory should be disabled by --create-top-directory")
	}
	if cfg.SkipError {
		t.Fatal("skip-errors should be disabled by --no-skip-errors")
	}
	if cfg.Transport.DumpRequest {
		t.Fatal("dump-request should be disabled by --no-dump-request")
	}
	if cfg.Transport.Fronting.Enable {
		t.Fatal("fronting should be disabled by --no-fronting")
	}
	if cfg.Transport.PreferHTTP2 {
		t.Fatal("prefer-http2 should be disabled by --no-prefer-http2")
	}
}

func TestParseLegacyAliasFlagsStillWork(t *testing.T) {
	ctx := newTestCommandContext(t, "get", []string{
		"--fileinf",
		"--apikey", "config-key",
		"--mimetype", "image/png",
		"--resumabledownload", "10m",
		"--notcreatetopdirectory",
		"--skiperror",
		"--NoProgress",
		"--url-list", "-",
	})
	cfg, err := parseConfig(ctx, func(string) string { return "" }, func(path string) (string, error) {
		return "/tmp/workdir", nil
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.ShowFileInf {
		t.Fatal("legacy --fileinf should still enable file info mode")
	}
	if cfg.APIKey != "config-key" {
		t.Fatalf("api key = %q, want config-key", cfg.APIKey)
	}
	if !reflect.DeepEqual(cfg.InputtedMimeType, []string{"image/png"}) {
		t.Fatalf("mime types = %#v", cfg.InputtedMimeType)
	}
	if cfg.Resumabledownload != "10m" {
		t.Fatalf("resumable download = %q, want 10m", cfg.Resumabledownload)
	}
	if !cfg.Notcreatetopdirectory {
		t.Fatal("legacy --notcreatetopdirectory should still work")
	}
	if !cfg.SkipError {
		t.Fatal("legacy --skiperror should still work")
	}
	if !cfg.Disp {
		t.Fatal("legacy --NoProgress should still suppress progress output")
	}
}
