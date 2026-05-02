package gdrivedl

import (
	"flag"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli"
)

func TestParseConfigTransportOptions(t *testing.T) {
	ctx := newTestContext(t, []string{
		"--completion-report",
		"--dry-run",
		"--progress",
		"--exit-report",
		"--concurrency", "3",
		"--timeout", "45s",
		"--request-delay", "1500ms",
		"--verbosity", "2",
		"--dump-request",
		"--dump-response",
		"--prefer-http2",
		"--share-http2-connection",
		"--fronting-enable",
		"--fronting-target", "front.example.com",
		"--resolve-to", "203.0.113.10",
		"--proxy", "socks5://127.0.0.1:1080",
		"--utls-profile", "firefox_auto",
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
	if cfg.Transport.RequestDelay != 1500*time.Millisecond {
		t.Fatalf("request delay = %v, want 1500ms", cfg.Transport.RequestDelay)
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
	if cfg.Transport.UTLSProfileName != "firefox_auto" {
		t.Fatalf("utls profile = %q, want firefox_auto", cfg.Transport.UTLSProfileName)
	}
	if !cfg.Transport.Fronting.Enable {
		t.Fatal("fronting should be enabled")
	}
	if cfg.Transport.Fronting.Target != "front.example.com" {
		t.Fatalf("fronting target = %q", cfg.Transport.Fronting.Target)
	}
	if cfg.Transport.Fronting.SNI != "front.example.com" {
		t.Fatalf("fronting sni = %q, want front.example.com", cfg.Transport.Fronting.SNI)
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
	if plan.ServerName != "front.example.com" {
		t.Fatalf("server name = %q, want front.example.com", plan.ServerName)
	}
}

func TestParseConfigAPIKeyEnvFallback(t *testing.T) {
	ctx := newTestContext(t, nil)

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

func TestParseConfigValidation(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"--progress", "--NoProgress"}, wantErr: "please use either '--progress' or '--NoProgress'"},
		{args: []string{"--concurrency", "0"}, wantErr: "--concurrency must be greater than 0"},
		{args: []string{"--verbosity", "-1"}, wantErr: "--verbosity must be greater than or equal to 0"},
		{args: []string{"--timeout", "abcx"}, wantErr: "--timeout must be a valid duration"},
		{args: []string{"--request-delay", "abcx"}, wantErr: "--request-delay must be a valid duration"},
		{args: []string{"--prefer-http2", "--force-http1"}, wantErr: "--prefer-http2 cannot be used with --force-http1"},
		{args: []string{"--share-http2-connection", "--force-http1"}, wantErr: "--share-http2-connection cannot be used with --force-http1"},
		{args: []string{"--fronting-enable"}, wantErr: "--fronting-target is required"},
		{args: []string{"--fronting-enable", "--fronting-target", "https://front.example.com"}, wantErr: "--fronting-target must be a hostname"},
		{args: []string{"--fronting-target", "front.example.com"}, wantErr: "fronting options require --fronting-enable"},
		{args: []string{"--proxy", "ftp://proxy.example.com:21"}, wantErr: "--proxy only supports http:// and socks5:// URLs"},
		{args: []string{"--resolve-to", "example.com"}, wantErr: "--resolve-to must be an IP address"},
		{args: []string{"--utls-profile", "unknown"}, wantErr: "unsupported --utls-profile"},
	}
	for _, tt := range tests {
		ctx := newTestContext(t, tt.args)
		_, err := parseConfig(ctx, func(string) string { return "" }, func(path string) (string, error) {
			return "/tmp/workdir", nil
		})
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Fatalf("parseConfig(%v) error = %v, want substring %q", tt.args, err, tt.wantErr)
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
