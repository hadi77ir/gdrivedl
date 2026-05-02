package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"github.com/urfave/cli"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	googletransport "google.golang.org/api/transport/http"
)

const legacyEnvval = "GOODLS_APIKEY"

var supportedUTLSProfiles = map[string]utls.ClientHelloID{
	"360_auto":           utls.Hello360_Auto,
	"chrome_auto":        utls.HelloChrome_Auto,
	"edge_auto":          utls.HelloEdge_Auto,
	"firefox_auto":       utls.HelloFirefox_Auto,
	"ios_auto":           utls.HelloIOS_Auto,
	"qq_auto":            utls.HelloQQ_Auto,
	"randomized":         utls.HelloRandomized,
	"randomized_alpn":    utls.HelloRandomizedALPN,
	"randomized_no_alpn": utls.HelloRandomizedNoALPN,
	"safari_auto":        utls.HelloSafari_Auto,
}

type config struct {
	APIKey                string
	CompletionReport      bool
	Disp                  bool
	DryRun                bool
	EnableProgress        bool
	Ext                   string
	ExitReport            bool
	Filename              string
	InputtedMimeType      []string
	MaxConcurrency        int
	Notcreatetopdirectory bool
	OverWrite             bool
	Resumabledownload     string
	ShowFileInf           bool
	Skip                  bool
	SkipError             bool
	Transport             transportConfig
	URL                   string
	URLList               string
	WorkDir               string
}

type transportConfig struct {
	DumpRequest     bool
	DumpResponse    bool
	Fronting        frontingConfig
	ForceHTTP1      bool
	PreferHTTP2     bool
	Proxy           *url.URL
	RequestDelay    time.Duration
	ResolveTo       string
	ShareHTTP2Conn  bool
	Timeout         time.Duration
	UTLSProfile     utls.ClientHelloID
	UTLSProfileName string
	Verbosity       int
	sharedState     *transportSharedState
}

type utlsProfileOption struct {
	name string
	id   utls.ClientHelloID
}

type frontingConfig struct {
	Enable bool
	SNI    string
	Target string
}

type transportRequestPlan struct {
	ConnectAddress string
	HostHeader     string
	LogicalHost    string
	ServerName     string
}

type transportSharedState struct {
	delayMu     sync.Mutex
	lastRequest time.Time

	http2Mu    sync.Mutex
	http2Conns map[string][]*sharedHTTP2Conn
}

type sharedHTTP2Conn struct {
	clientConn *http2.ClientConn
	conn       net.Conn
}

func parseConfig(c *cli.Context, getenv func(string) string, absPath func(string) (string, error)) (*config, error) {
	workdir := c.String("directory")
	var err error
	if workdir == "" {
		workdir, err = absPath(".")
		if err != nil {
			return nil, err
		}
	}
	transport, err := parseTransportConfig(c)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(c.String("apikey"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(getenv(envval))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(getenv(legacyEnvval))
	}
	return &config{
		APIKey:                apiKey,
		CompletionReport:      c.Bool("completion-report"),
		Disp:                  c.Bool("NoProgress"),
		DryRun:                c.Bool("dry-run"),
		EnableProgress:        c.Bool("progress"),
		Ext:                   c.String("extension"),
		ExitReport:            c.Bool("exit-report"),
		Filename:              c.String("filename"),
		InputtedMimeType:      splitCommaSeparated(c.StringSlice("mimetype")),
		MaxConcurrency:        c.Int("concurrency"),
		Notcreatetopdirectory: c.Bool("notcreatetopdirectory"),
		OverWrite:             c.Bool("overwrite"),
		Resumabledownload:     c.String("resumabledownload"),
		ShowFileInf:           c.Bool("fileinf"),
		Skip:                  c.Bool("skip"),
		SkipError:             c.Bool("skiperror"),
		Transport:             transport,
		URL:                   c.String("url"),
		URLList:               c.String("url-list"),
		WorkDir:               workdir,
	}, nil
}

func (cfg *config) toPara() *para {
	return &para{
		APIKey:                cfg.APIKey,
		CompletionReport:      cfg.CompletionReport,
		Disp:                  cfg.Disp,
		DlFolder:              false,
		DownloadBytes:         -1,
		DryRun:                cfg.DryRun,
		EnableProgress:        cfg.EnableProgress,
		Ext:                   cfg.Ext,
		ExitReport:            cfg.ExitReport,
		Filename:              cfg.Filename,
		InputtedMimeType:      cfg.InputtedMimeType,
		MaxConcurrency:        cfg.MaxConcurrency,
		Notcreatetopdirectory: cfg.Notcreatetopdirectory,
		OverWrite:             cfg.OverWrite,
		Resumabledownload:     cfg.Resumabledownload,
		ShowFileInf:           cfg.ShowFileInf,
		Skip:                  cfg.Skip,
		SkipError:             cfg.SkipError,
		TransportConfig:       cfg.Transport,
		WorkDir:               cfg.WorkDir,
	}
}

func parseTransportConfig(c *cli.Context) (transportConfig, error) {
	if c.Bool("progress") && c.Bool("NoProgress") {
		return transportConfig{}, fmt.Errorf("please use either '--progress' or '--NoProgress'")
	}
	if c.Int("concurrency") < 1 {
		return transportConfig{}, fmt.Errorf("--concurrency must be greater than 0")
	}
	preferHTTP2 := c.Bool("prefer-http2")
	forceHTTP1 := c.Bool("force-http1")
	shareHTTP2Conn := c.Bool("share-http2-connection")
	if preferHTTP2 && forceHTTP1 {
		return transportConfig{}, fmt.Errorf("--prefer-http2 cannot be used with --force-http1")
	}
	if shareHTTP2Conn && forceHTTP1 {
		return transportConfig{}, fmt.Errorf("--share-http2-connection cannot be used with --force-http1")
	}
	proxyURL, err := parseProxyURL(c.String("proxy"))
	if err != nil {
		return transportConfig{}, err
	}
	fronting, err := parseFrontingConfig(c)
	if err != nil {
		return transportConfig{}, err
	}
	resolveTo, err := parseResolveTo(c.String("resolve-to"))
	if err != nil {
		return transportConfig{}, err
	}
	profileName, profileID, err := parseUTLSProfile(c.String("utls-profile"))
	if err != nil {
		return transportConfig{}, err
	}
	timeout, err := parseTimeout(c.String("timeout"))
	if err != nil {
		return transportConfig{}, err
	}
	requestDelay, err := parseFlexibleDuration(c.String("request-delay"), "--request-delay")
	if err != nil {
		return transportConfig{}, err
	}
	verbosity, err := parseVerbosity(c.Int("verbosity"))
	if err != nil {
		return transportConfig{}, err
	}
	return transportConfig{
		DumpRequest:     c.Bool("dump-request"),
		DumpResponse:    c.Bool("dump-response"),
		Fronting:        fronting,
		ForceHTTP1:      forceHTTP1,
		PreferHTTP2:     preferHTTP2,
		Proxy:           proxyURL,
		RequestDelay:    requestDelay,
		ResolveTo:       resolveTo,
		ShareHTTP2Conn:  shareHTTP2Conn,
		Timeout:         timeout,
		UTLSProfile:     profileID,
		UTLSProfileName: profileName,
		Verbosity:       verbosity,
		sharedState:     newTransportSharedState(),
	}, nil
}

func parseFrontingConfig(c *cli.Context) (frontingConfig, error) {
	enabled := c.Bool("fronting-enable")
	target := strings.TrimSpace(c.String("fronting-target"))
	sni := strings.TrimSpace(c.String("fronting-sni"))
	if !enabled {
		if target != "" || sni != "" {
			return frontingConfig{}, fmt.Errorf("fronting options require --fronting-enable")
		}
		return frontingConfig{}, nil
	}
	if target == "" {
		return frontingConfig{}, fmt.Errorf("--fronting-target is required when --fronting-enable is set")
	}
	var err error
	target, err = parseHostnameValue(target, "--fronting-target")
	if err != nil {
		return frontingConfig{}, err
	}
	if sni == "" {
		sni = target
	} else {
		sni, err = parseHostnameValue(sni, "--fronting-sni")
		if err != nil {
			return frontingConfig{}, err
		}
	}
	return frontingConfig{Enable: true, SNI: sni, Target: target}, nil
}

func splitCommaSeparated(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func parseHostnameValue(value, flagName string) (string, error) {
	parsed, err := url.Parse("//" + value)
	if err != nil || parsed.Hostname() == "" || parsed.Host != value || parsed.Port() != "" {
		return "", fmt.Errorf("%s must be a hostname", flagName)
	}
	return strings.ToLower(parsed.Hostname()), nil
}

func parseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("--proxy must include a host")
	}
	switch parsed.Scheme {
	case "http", "socks5":
		return parsed, nil
	default:
		return nil, fmt.Errorf("--proxy only supports http:// and socks5:// URLs")
	}
}

func parseResolveTo(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return "", fmt.Errorf("--resolve-to must be an IP address")
	}
	return ip.String(), nil
}

func parseUTLSProfile(raw string) (string, utls.ClientHelloID, error) {
	if strings.TrimSpace(raw) == "" {
		return "chrome_auto", supportedUTLSProfiles["chrome_auto"], nil
	}
	profileName := normalizeUTLSProfileName(raw)
	profileID, ok := supportedUTLSProfiles[profileName]
	if !ok {
		return "", utls.ClientHelloID{}, fmt.Errorf("unsupported --utls-profile %q (supported: %s)", raw, strings.Join(supportedUTLSProfileNames(), ", "))
	}
	return profileName, profileID, nil
}

func parseTimeout(raw string) (time.Duration, error) {
	return parseFlexibleDuration(raw, "--timeout")
}

func parseFlexibleDuration(raw, flagName string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if duration, err := time.ParseDuration(raw); err == nil {
		if duration < 0 {
			return 0, fmt.Errorf("%s must be greater than or equal to 0", flagName)
		}
		return duration, nil
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		duration, err := time.ParseDuration(raw + "s")
		if err != nil {
			return 0, err
		}
		return duration, nil
	}
	return 0, fmt.Errorf("%s must be a valid duration (examples: 30s, 2m, 1h)", flagName)
}

func parseVerbosity(value int) (int, error) {
	if value < 0 {
		return 0, fmt.Errorf("--verbosity must be greater than or equal to 0")
	}
	return value, nil
}

func normalizeUTLSProfileName(raw string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", "_"))
}

func supportedUTLSProfileNames() []string {
	names := make([]string, 0, len(supportedUTLSProfiles))
	for name := range supportedUTLSProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func newTransportSharedState() *transportSharedState {
	return &transportSharedState{http2Conns: map[string][]*sharedHTTP2Conn{}}
}

func (cfg frontingConfig) Match(string) bool {
	return cfg.Enable
}

func (cfg transportConfig) shouldPreferHTTP2() bool {
	if cfg.ForceHTTP1 {
		return false
	}
	return cfg.PreferHTTP2 || cfg.ShareHTTP2Conn
}

func (cfg transportConfig) shouldShareHTTP2Conn() bool {
	return !cfg.ForceHTTP1 && cfg.ShareHTTP2Conn
}

func (cfg transportConfig) alpnProtocols() []string {
	if cfg.shouldPreferHTTP2() || (cfg.Fronting.Enable && !cfg.ForceHTTP1) {
		return []string{http2.NextProtoTLS, "http/1.1"}
	}
	return []string{"http/1.1"}
}

func (cfg transportConfig) state() *transportSharedState {
	if cfg.sharedState != nil {
		return cfg.sharedState
	}
	return newTransportSharedState()
}

func (cfg transportConfig) utlsHandshakeProfiles() []utlsProfileOption {
	profiles := []utlsProfileOption{{name: cfg.UTLSProfileName, id: cfg.UTLSProfile}}
	if cfg.UTLSProfileName != "chrome_auto" {
		return profiles
	}
	if cfg.Fronting.Enable {
		profiles = []utlsProfileOption{}
		for _, name := range []string{"firefox_auto", "edge_auto", "randomized_alpn", "chrome_auto"} {
			profiles = append(profiles, utlsProfileOption{name: name, id: supportedUTLSProfiles[name]})
		}
		return profiles
	}
	for _, name := range []string{"firefox_auto", "edge_auto", "randomized_alpn"} {
		profiles = append(profiles, utlsProfileOption{name: name, id: supportedUTLSProfiles[name]})
	}
	return profiles
}

func (cfg transportConfig) newHTTPClient(jar http.CookieJar) (*http.Client, error) {
	rt, err := cfg.newRoundTripper()
	if err != nil {
		return nil, err
	}
	return &http.Client{Jar: jar, Timeout: cfg.Timeout, Transport: rt}, nil
}

func (cfg transportConfig) newGoogleAPIClient(ctx context.Context, apiKey string) (*http.Client, error) {
	rt, err := cfg.newRoundTripper()
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		rt, err = googletransport.NewTransport(ctx, rt, option.WithAPIKey(apiKey))
		if err != nil {
			return nil, err
		}
	}
	return &http.Client{Timeout: cfg.Timeout, Transport: rt}, nil
}

func (cfg transportConfig) newRoundTripper() (http.RoundTripper, error) {
	if cfg.sharedState == nil {
		cfg.sharedState = newTransportSharedState()
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	if err := cfg.applyProxy(base); err != nil {
		return nil, err
	}
	base.ForceAttemptHTTP2 = false
	return &smartRoundTripper{base: base, config: cfg}, nil
}

func (cfg transportConfig) applyProxy(transport *http.Transport) error {
	if cfg.Proxy == nil {
		return nil
	}
	switch cfg.Proxy.Scheme {
	case "http":
		transport.Proxy = http.ProxyURL(cfg.Proxy)
		return nil
	case "socks5":
		dialer, err := proxy.FromURL(cfg.Proxy, proxy.Direct)
		if err != nil {
			return err
		}
		transport.Proxy = nil
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			transport.DialContext = contextDialer.DialContext
			return nil
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
		return nil
	default:
		return fmt.Errorf("unsupported proxy scheme: %s", cfg.Proxy.Scheme)
	}
}

func (cfg transportConfig) buildRequestPlan(req *http.Request) transportRequestPlan {
	logicalHost := req.URL.Hostname()
	serverName := logicalHost
	dialHost := logicalHost
	if cfg.Fronting.Match(logicalHost) {
		dialHost = cfg.Fronting.Target
		serverName = cfg.Fronting.SNI
	}
	if cfg.ResolveTo != "" {
		dialHost = cfg.ResolveTo
	}
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return transportRequestPlan{
		ConnectAddress: net.JoinHostPort(dialHost, port),
		HostHeader:     requestHost(req),
		LogicalHost:    logicalHost,
		ServerName:     serverName,
	}
}

func (cfg transportConfig) rewritesNetworkDestination(plan transportRequestPlan) bool {
	return cfg.ResolveTo != "" || plan.ServerName != plan.LogicalHost || plan.ConnectAddress != net.JoinHostPort(plan.LogicalHost, requestPort(plan, ""))
}

func requestPort(plan transportRequestPlan, fallback string) string {
	_, port, err := net.SplitHostPort(plan.ConnectAddress)
	if err != nil {
		return fallback
	}
	return port
}

type smartRoundTripper struct {
	base   *http.Transport
	config transportConfig
}

func (rt *smartRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := rt.waitRequestDelay(req.Context(), req); err != nil {
		return nil, err
	}
	plan := rt.config.buildRequestPlan(req)
	if req.URL.Scheme != "https" {
		clone := req.Clone(req.Context())
		if rt.config.rewritesNetworkDestination(plan) {
			clone.Host = plan.HostHeader
			clone.URL.Host = plan.ConnectAddress
		}
		rt.config.dumpRequest(clone)
		rt.config.logStage(clone, "sending request", "scheme=http")
		res, err := rt.base.RoundTrip(clone)
		if err != nil {
			rt.config.logStage(clone, "request failed", "error=%v", err)
			return nil, err
		}
		rt.config.logStage(clone, "response headers received", "scheme=http content-length=%d", res.ContentLength)
		rt.config.logDetail(clone, 1, "http status=%s", res.Status)
		rt.config.dumpResponse(clone, res)
		return res, nil
	}
	return rt.roundTripHTTPS(req, plan)
}

func (rt *smartRoundTripper) roundTripHTTPS(req *http.Request, plan transportRequestPlan) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Host = plan.HostHeader
	if rt.config.shouldShareHTTP2Conn() {
		if key, entry, ok := rt.getSharedHTTP2Conn(plan); ok {
			rt.config.logDetail(clone, 1, "reusing shared http2 connection")
			return rt.roundTripHTTP2(clone, nil, entry, key)
		}
	}
	conn, protocol, err := rt.dialTLSConnection(req.Context(), plan, clone)
	if err != nil {
		rt.config.logStage(clone, "connection failed", "error=%v", err)
		return nil, err
	}
	if protocol == http2.NextProtoTLS {
		if rt.config.shouldShareHTTP2Conn() {
			key := rt.sharedHTTP2Key(plan)
			entry, err := rt.newSharedHTTP2Conn(conn)
			if err != nil {
				conn.Close()
				rt.config.logStage(clone, "connection failed", "protocol=h2 error=%v", err)
				return nil, err
			}
			rt.storeSharedHTTP2Conn(key, entry)
			return rt.roundTripHTTP2(clone, nil, entry, key)
		}
		return rt.roundTripHTTP2(clone, conn, nil, "")
	}
	clone.Close = true
	rt.config.dumpRequest(clone)
	rt.config.logStage(clone, "sending request", "protocol=http/1.1")
	if err := clone.Write(conn); err != nil {
		conn.Close()
		rt.config.logStage(clone, "request failed", "error=%v", err)
		return nil, err
	}
	rt.config.logStage(clone, "waiting for response", "protocol=http/1.1")
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, clone)
	if err != nil {
		conn.Close()
		rt.config.logStage(clone, "response failed", "error=%v", err)
		return nil, err
	}
	rt.config.logStage(clone, "response headers received", "protocol=http/1.1 content-length=%d", res.ContentLength)
	rt.config.logDetail(clone, 1, "http status=%s", res.Status)
	rt.config.dumpResponse(clone, res)
	res.Body = &transportBody{ReadCloser: res.Body, conn: conn}
	return res, nil
}

func (rt *smartRoundTripper) roundTripHTTP2(req *http.Request, conn net.Conn, sharedEntry *sharedHTTP2Conn, sharedKey string) (*http.Response, error) {
	clientConn := (*http2.ClientConn)(nil)
	if sharedEntry != nil {
		clientConn = sharedEntry.clientConn
	} else {
		transport := &http2.Transport{}
		var err error
		clientConn, err = transport.NewClientConn(conn)
		if err != nil {
			conn.Close()
			rt.config.logStage(req, "connection failed", "protocol=h2 error=%v", err)
			return nil, err
		}
	}
	rt.config.dumpRequest(req)
	rt.config.logStage(req, "sending request", "protocol=h2")
	rt.config.logStage(req, "waiting for response", "protocol=h2")
	res, err := clientConn.RoundTrip(req)
	if err != nil {
		if sharedEntry != nil {
			rt.pruneSharedHTTP2Conn(sharedKey, sharedEntry)
		} else {
			clientConn.Close()
		}
		rt.config.logStage(req, "response failed", "protocol=h2 error=%v", err)
		return nil, err
	}
	rt.config.logStage(req, "response headers received", "protocol=h2 content-length=%d", res.ContentLength)
	rt.config.logDetail(req, 1, "http status=%s", res.Status)
	rt.config.dumpResponse(req, res)
	if sharedEntry == nil {
		res.Body = &http2TransportBody{ReadCloser: res.Body, clientConn: clientConn}
	}
	return res, nil
}

func (rt *smartRoundTripper) dialTLSConnection(ctx context.Context, plan transportRequestPlan, req *http.Request) (net.Conn, string, error) {
	profiles := rt.config.utlsHandshakeProfiles()
	var lastErr error
	for index, profile := range profiles {
		conn, err := rt.dialProxyAwareTCP(ctx, plan.ConnectAddress, req)
		if err != nil {
			return nil, "", err
		}
		if rt.config.Proxy != nil && rt.config.Proxy.Scheme == "http" {
			rt.config.logStage(req, "proxy connect", "target=%s", plan.ConnectAddress)
			if err := establishHTTPProxyTunnel(conn, plan.ConnectAddress, rt.config.Proxy); err != nil {
				conn.Close()
				rt.config.logStage(req, "proxy connect failed", "error=%v", err)
				return nil, "", err
			}
		}
		rt.config.logStage(req, "tls handshake", "server-name=%s profile=%s", plan.ServerName, profile.name)
		uconn := utls.UClient(conn, &utls.Config{
			ServerName: plan.ServerName,
			NextProtos: rt.config.alpnProtocols(),
		}, profile.id)
		if err := uconn.HandshakeContext(ctx); err != nil {
			conn.Close()
			lastErr = err
			rt.config.logStage(req, "tls handshake failed", "profile=%s error=%v", profile.name, err)
			if index+1 < len(profiles) {
				rt.config.logDetail(req, 1, "retrying tls handshake with profile=%s", profiles[index+1].name)
				continue
			}
			return nil, "", err
		}
		if profile.name != rt.config.UTLSProfileName {
			rt.config.logDetail(req, 1, "tls handshake fallback profile=%s", profile.name)
		}
		rt.config.logDetail(req, 2, "tls negotiated protocol=%q", uconn.ConnectionState().NegotiatedProtocol)
		return uconn, uconn.ConnectionState().NegotiatedProtocol, nil
	}
	return nil, "", lastErr
}

func (rt *smartRoundTripper) dialProxyAwareTCP(ctx context.Context, targetAddress string, req *http.Request) (net.Conn, error) {
	if rt.config.Proxy == nil {
		return rt.dialAddress(ctx, targetAddress, req)
	}
	switch rt.config.Proxy.Scheme {
	case "http":
		return rt.dialAddress(ctx, rt.config.Proxy.Host, req)
	case "socks5":
		dialer, err := proxy.FromURL(rt.config.Proxy, proxy.Direct)
		if err != nil {
			return nil, err
		}
		rt.config.logStage(req, "dialing", "proxy=socks5 target=%s", targetAddress)
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			return contextDialer.DialContext(ctx, "tcp", targetAddress)
		}
		return dialer.Dial("tcp", targetAddress)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", rt.config.Proxy.Scheme)
	}
}

func (rt *smartRoundTripper) dialAddress(ctx context.Context, address string, req *http.Request) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	dialAddress := address
	if net.ParseIP(host) == nil {
		rt.config.logStage(req, "resolving", "address=%s", address)
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			rt.config.logStage(req, "resolve failed", "address=%s error=%v", address, err)
			return nil, err
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("no resolved addresses for %s", host)
		}
		dialAddress = net.JoinHostPort(addrs[0].IP.String(), port)
		rt.config.logDetail(req, 2, "resolved %s -> %s", address, dialAddress)
	}
	rt.config.logStage(req, "dialing", "address=%s", dialAddress)
	return (&net.Dialer{}).DialContext(ctx, "tcp", dialAddress)
}

func (rt *smartRoundTripper) waitRequestDelay(ctx context.Context, req *http.Request) error {
	if rt.config.RequestDelay <= 0 {
		return nil
	}
	state := rt.config.state()
	state.delayMu.Lock()
	defer state.delayMu.Unlock()
	if !state.lastRequest.IsZero() {
		wait := state.lastRequest.Add(rt.config.RequestDelay).Sub(time.Now())
		if wait > 0 {
			rt.config.logStage(req, "request delay", "delay=%s", wait.Round(time.Millisecond))
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	state.lastRequest = time.Now()
	return nil
}

func (rt *smartRoundTripper) sharedHTTP2Key(plan transportRequestPlan) string {
	proxyAddress := ""
	if rt.config.Proxy != nil {
		proxyAddress = rt.config.Proxy.String()
	}
	return strings.Join([]string{plan.ConnectAddress, plan.HostHeader, plan.ServerName, proxyAddress, rt.config.UTLSProfileName}, "\x00")
}

func (rt *smartRoundTripper) getSharedHTTP2Conn(plan transportRequestPlan) (string, *sharedHTTP2Conn, bool) {
	key := rt.sharedHTTP2Key(plan)
	state := rt.config.state()
	state.http2Mu.Lock()
	defer state.http2Mu.Unlock()
	entries := state.http2Conns[key]
	if len(entries) == 0 {
		return "", nil, false
	}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if entry.clientConn.ReserveNewRequest() {
			return key, entry, true
		}
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		connState := entry.clientConn.State()
		if connState.Closed || connState.Closing {
			entry.close()
			continue
		}
		filtered = append(filtered, entry)
	}
	if len(filtered) == 0 {
		delete(state.http2Conns, key)
	} else {
		state.http2Conns[key] = filtered
	}
	return "", nil, false
}

func (rt *smartRoundTripper) newSharedHTTP2Conn(conn net.Conn) (*sharedHTTP2Conn, error) {
	transport := &http2.Transport{}
	clientConn, err := transport.NewClientConn(conn)
	if err != nil {
		return nil, err
	}
	return &sharedHTTP2Conn{clientConn: clientConn, conn: conn}, nil
}

func (rt *smartRoundTripper) storeSharedHTTP2Conn(key string, entry *sharedHTTP2Conn) {
	state := rt.config.state()
	state.http2Mu.Lock()
	state.http2Conns[key] = append(state.http2Conns[key], entry)
	state.http2Mu.Unlock()
}

func (rt *smartRoundTripper) pruneSharedHTTP2Conn(key string, entry *sharedHTTP2Conn) {
	if entry == nil || entry.clientConn.CanTakeNewRequest() {
		return
	}
	state := rt.config.state()
	state.http2Mu.Lock()
	entries := state.http2Conns[key]
	filtered := entries[:0]
	for _, candidate := range entries {
		if candidate != entry {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) == 0 {
		delete(state.http2Conns, key)
	} else {
		state.http2Conns[key] = filtered
	}
	state.http2Mu.Unlock()
	entry.close()
}

func establishHTTPProxyTunnel(conn net.Conn, targetAddress string, proxyURL *url.URL) error {
	headers := []string{
		fmt.Sprintf("CONNECT %s HTTP/1.1", targetAddress),
		fmt.Sprintf("Host: %s", targetAddress),
	}
	if proxyURL != nil && proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + password))
		headers = append(headers, "Proxy-Authorization: Basic "+token)
	}
	if _, err := fmt.Fprintf(conn, "%s\r\n\r\n", strings.Join(headers, "\r\n")); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		if msg := strings.TrimSpace(string(body)); msg != "" {
			return fmt.Errorf("proxy CONNECT failed: %s", msg)
		}
		return fmt.Errorf("proxy CONNECT failed: %s", res.Status)
	}
	return nil
}

type transportBody struct {
	io.ReadCloser
	conn net.Conn
}

func (body *transportBody) Close() error {
	err := body.ReadCloser.Close()
	if closeErr := body.conn.Close(); err == nil {
		err = closeErr
	}
	return err
}

type http2TransportBody struct {
	io.ReadCloser
	clientConn *http2.ClientConn
}

func (body *http2TransportBody) Close() error {
	err := body.ReadCloser.Close()
	if closeErr := body.clientConn.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (entry *sharedHTTP2Conn) close() {
	if entry == nil {
		return
	}
	if entry.clientConn != nil {
		entry.clientConn.Close()
	}
	if entry.conn != nil {
		entry.conn.Close()
	}
}

func requestHost(req *http.Request) string {
	if req.Host != "" {
		return req.Host
	}
	return req.URL.Host
}

func (p *para) newHTTPClient(jar *cookiejar.Jar) (*http.Client, error) {
	return p.TransportConfig.newHTTPClient(jar)
}

func (p *para) newDriveService(ctx context.Context) (*drive.Service, error) {
	client, err := p.TransportConfig.newGoogleAPIClient(ctx, p.APIKey)
	if err != nil {
		return nil, err
	}
	return drive.NewService(ctx, option.WithHTTPClient(client))
}
