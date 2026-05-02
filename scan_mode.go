package gdrivedl

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	scanProbeURL          = "https://gstatic.com/generate_204"
	scanRemoteDNSURL      = "https://dns.google/resolve"
	scanDomainAssetPath   = "assets/google-subdomains.txt"
	scanIPAssetPath       = "assets/google-ips.txt"
	scanMaxCIDRHostCount  = 65536
	scanRemoteDNSFronting = "google.com"
)

var embeddedScanDomains = []string{
	"gstatic.com",
	"www.gstatic.com",
	"google.com",
	"www.google.com",
	"ssl.gstatic.com",
	"fonts.gstatic.com",
	"accounts.google.com",
	"clients6.google.com",
	"ogs.google.com",
	"play.google.com",
	"support.google.com",
}

type scanMode string

const (
	scanModeFull        scanMode = "full"
	scanModeOnlyIP      scanMode = "only-ip"
	scanModeOnlyDomains scanMode = "only-domains"
)

type connectivityScanOptions struct {
	Mode            scanMode
	ExtraDomains    []string
	ExtraIPSpecs    []string
	FrontingSNIs    []string
	FrontingTargets []string
	ResolveToAddrs  []string
}

type scanTargetResult struct {
	Target       string
	LocalDNSIPs  []string
	RemoteDNSIPs []string
	Profiles     []string
	SNIs         []string
	ResolveToIPs []string
}

type scanIPSourceResult struct {
	Source        string
	Target        string
	CandidateIPs  []string
	AccessibleIPs []string
}

type scanReport struct {
	ProbeURL          string
	Mode              string
	DirectProfiles    []string
	DialAccessibleIPs []string
	FrontingSNIs      []string
	FrontingTargets   []string
	ResolveToAddrs    []string
	UTLSProfiles      []string
	DialSources       []scanIPSourceResult
	Targets           []scanTargetResult
}

type scanProbeFunc func(context.Context, transportConfig) error
type scanResolveFunc func(context.Context, string) ([]string, error)
type scanDialFunc func(context.Context, transportConfig, string) error
type scanListFunc func() ([]string, error)

type scanDependencies struct {
	loadDefaultDomains scanListFunc
	loadDefaultIPSpecs scanListFunc
	resolveLocal       scanResolveFunc
	resolveRemote      scanResolveFunc
	probe              scanProbeFunc
	dial               scanDialFunc
}

type remoteDNSResolveResponse struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func (deps scanDependencies) withDefaults(base transportConfig) scanDependencies {
	if deps.loadDefaultDomains == nil {
		deps.loadDefaultDomains = defaultScanDomainLoader
	}
	if deps.loadDefaultIPSpecs == nil {
		deps.loadDefaultIPSpecs = defaultScanIPSpecLoader
	}
	if deps.resolveLocal == nil {
		deps.resolveLocal = defaultScanResolver
	}
	if deps.resolveRemote == nil {
		deps.resolveRemote = defaultFrontedDNSResolver(base)
	}
	if deps.probe == nil {
		deps.probe = defaultScanProbe
	}
	if deps.dial == nil {
		deps.dial = defaultScanDial
	}
	return deps
}

func readHostnameList(source string, stdin io.Reader) ([]string, error) {
	if source == "-" {
		return readHostnames(stdin)
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readHostnames(file)
}

func readHostnames(reader io.Reader) ([]string, error) {
	var hosts []string
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "", line == "end", strings.HasPrefix(line, "#"):
			continue
		}
		for _, part := range splitCommaSeparated([]string{line}) {
			host, err := parseHostnameValue(part, "scan domain")
			if err != nil {
				return nil, err
			}
			hosts = append(hosts, host)
		}
	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	return dedupeStrings(hosts), nil
}

func readIPSpecList(source string, stdin io.Reader) ([]string, error) {
	if source == "-" {
		return readIPSpecs(stdin)
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readIPSpecs(file)
}

func readIPSpecs(reader io.Reader) ([]string, error) {
	var specs []string
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "", line == "end", strings.HasPrefix(line, "#"):
			continue
		}
		for _, part := range splitCommaSeparated([]string{line}) {
			spec, err := normalizeIPSpec(part)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	return dedupeStrings(specs), nil
}

func normalizeIPSpec(value string) (string, error) {
	if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
		return ip.String(), nil
	}
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("scan IP list values must be an IP address or CIDR range")
	}
	return ipnet.String(), nil
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func readOptionalHostnameAsset(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	return readHostnames(file)
}

func readOptionalIPAsset(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	return readIPSpecs(file)
}

func defaultScanDomainLoader() ([]string, error) {
	hosts, err := readOptionalHostnameAsset(scanDomainAssetPath)
	if err != nil {
		return nil, err
	}
	if len(hosts) > 0 {
		return hosts, nil
	}
	return append([]string(nil), embeddedScanDomains...), nil
}

func defaultScanIPSpecLoader() ([]string, error) {
	return readOptionalIPAsset(scanIPAssetPath)
}

func defaultScanResolver(ctx context.Context, host string) ([]string, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP == nil {
			continue
		}
		out = append(out, addr.IP.String())
	}
	return dedupeStrings(out), nil
}

func defaultFrontedDNSResolver(base transportConfig) scanResolveFunc {
	profiles := scanProfileCandidates(base)
	return func(ctx context.Context, host string) ([]string, error) {
		cfg := base
		cfg.Scan = false
		cfg.disableUTLSFallback = false
		cfg.Fronting = frontingConfig{
			Enable:      true,
			Target:      scanRemoteDNSFronting,
			Targets:     []string{scanRemoteDNSFronting},
			SNI:         scanRemoteDNSFronting,
			explicitSNI: true,
		}
		cfg.ResolveTo = ""
		cfg.ResolveToAddrs = nil
		cfg.sharedState = newTransportSharedState()
		if len(profiles) > 0 {
			cfg.UTLSProfiles = append([]utlsProfileOption(nil), profiles...)
			cfg.UTLSProfileName = profiles[0].name
			cfg.UTLSProfile = profiles[0].id
		}
		client, err := cfg.newHTTPClient(nil)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, scanRemoteDNSURL, nil)
		if err != nil {
			return nil, err
		}
		query := req.URL.Query()
		query.Set("name", host)
		query.Set("type", "A")
		req.URL.RawQuery = query.Encode()
		req.Header.Set("Accept", "application/dns-json")
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, res.Body)
			return nil, fmt.Errorf("remote DNS probe failed: %s", res.Status)
		}
		var payload remoteDNSResolveResponse
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			return nil, err
		}
		var out []string
		for _, answer := range payload.Answer {
			if answer.Type != 1 {
				continue
			}
			ip := net.ParseIP(strings.TrimSpace(answer.Data))
			if ip == nil {
				continue
			}
			out = append(out, ip.String())
		}
		return dedupeStrings(out), nil
	}
}

func defaultScanProbe(ctx context.Context, cfg transportConfig) error {
	probeCfg := cfg
	probeCfg.Scan = false
	probeCfg.disableUTLSFallback = true
	_, err := testConnectivityWithTransport(ctx, probeCfg, nil, scanProbeURL)
	return err
}

func defaultScanDial(ctx context.Context, cfg transportConfig, ip string) error {
	targetAddress := net.JoinHostPort(ip, "443")
	if cfg.Proxy == nil {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", targetAddress)
		if err != nil {
			return err
		}
		return conn.Close()
	}
	switch cfg.Proxy.Scheme {
	case "http":
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", cfg.Proxy.Host)
		if err != nil {
			return err
		}
		defer conn.Close()
		return establishHTTPProxyTunnel(conn, targetAddress, cfg.Proxy)
	case "socks5":
		dialer, err := proxy.FromURL(cfg.Proxy, proxy.Direct)
		if err != nil {
			return err
		}
		var conn net.Conn
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			conn, err = contextDialer.DialContext(ctx, "tcp", targetAddress)
		} else {
			conn, err = dialer.Dial("tcp", targetAddress)
		}
		if err != nil {
			return err
		}
		return conn.Close()
	default:
		return fmt.Errorf("unsupported proxy scheme: %s", cfg.Proxy.Scheme)
	}
}

func scanCandidateDomains(defaultDomains, frontingTargets, extra []string) []string {
	combined := append([]string{}, defaultDomains...)
	combined = append(combined, frontingTargets...)
	combined = append(combined, extra...)
	return dedupeStrings(combined)
}

func scanCandidateSNIs(domains, explicitSNIs []string) []string {
	combined := append([]string{}, domains...)
	combined = append(combined, explicitSNIs...)
	return dedupeStrings(combined)
}

func scanCandidateIPSpecs(defaultSpecs, extra []string) []string {
	combined := append([]string{}, defaultSpecs...)
	combined = append(combined, extra...)
	return dedupeStrings(combined)
}

func scanProfileCandidates(cfg transportConfig) []utlsProfileOption {
	ordered := cfg.orderedScanProfileNames()
	profiles := make([]utlsProfileOption, 0, len(ordered))
	for _, name := range ordered {
		if id, ok := supportedUTLSProfiles[name]; ok {
			profiles = append(profiles, utlsProfileOption{name: name, id: id})
		}
	}
	return profiles
}

func expandIPSpecs(specs []string) ([]string, error) {
	return expandIPSpecsWithSampling(specs, 0)
}

func expandIPSpecsWithSampling(specs []string, cidrSampleCount int) ([]string, error) {
	var out []string
	for _, spec := range dedupeStrings(specs) {
		expanded, err := expandIPSpecWithSampling(spec, cidrSampleCount)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded...)
	}
	return dedupeStrings(out), nil
}

func expandIPSpec(spec string) ([]string, error) {
	return expandIPSpecWithSampling(spec, 0)
}

func expandIPSpecWithSampling(spec string, cidrSampleCount int) ([]string, error) {
	if ip := net.ParseIP(spec); ip != nil {
		return []string{ip.String()}, nil
	}
	_, ipnet, err := net.ParseCIDR(spec)
	if err != nil {
		return nil, fmt.Errorf("scan IP list values must be an IP address or CIDR range")
	}
	return expandIPv4CIDRWithSampling(ipnet, cidrSampleCount)
}

func expandIPv4CIDR(ipnet *net.IPNet) ([]string, error) {
	return expandIPv4CIDRWithSampling(ipnet, 0)
}

func expandIPv4CIDRWithSampling(ipnet *net.IPNet, cidrSampleCount int) ([]string, error) {
	base := ipnet.IP.To4()
	if base == nil {
		return nil, fmt.Errorf("scan IP ranges only support IPv4 CIDR values")
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("scan IP ranges only support IPv4 CIDR values")
	}
	hostCount := uint64(1) << uint(32-ones)
	if cidrSampleCount > 0 {
		if uint64(cidrSampleCount) >= hostCount {
			if hostCount > scanMaxCIDRHostCount {
				return nil, fmt.Errorf("scan IP range %q contains %d addresses; lower --scan-ip-random-count or split it into smaller CIDRs", ipnet.String(), hostCount)
			}
			return expandIPv4Range(base, hostCount), nil
		}
		return sampleIPv4Range(base, hostCount, cidrSampleCount), nil
	}
	if hostCount > scanMaxCIDRHostCount {
		return nil, fmt.Errorf("scan IP range %q expands to %d addresses; split it into smaller CIDRs", ipnet.String(), hostCount)
	}
	return expandIPv4Range(base, hostCount), nil
}

func expandIPv4Range(base net.IP, hostCount uint64) []string {
	start := binary.BigEndian.Uint32(base)
	out := make([]string, 0, int(hostCount))
	for offset := uint64(0); offset < hostCount; offset++ {
		ipValue := start + uint32(offset)
		ipBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(ipBytes, ipValue)
		out = append(out, net.IP(ipBytes).String())
	}
	return out
}

func sampleIPv4Range(base net.IP, hostCount uint64, sampleCount int) []string {
	count := uint64(sampleCount)
	if count > hostCount {
		count = hostCount
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	selected := make(map[uint64]struct{}, int(count))
	for offset := hostCount - count; offset < hostCount; offset++ {
		candidate := uint64(rng.Int63n(int64(offset + 1)))
		if _, exists := selected[candidate]; exists {
			selected[offset] = struct{}{}
			continue
		}
		selected[candidate] = struct{}{}
	}
	offsets := make([]uint64, 0, len(selected))
	for offset := range selected {
		offsets = append(offsets, offset)
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	start := binary.BigEndian.Uint32(base)
	out := make([]string, 0, len(offsets))
	for _, offset := range offsets {
		ipValue := start + uint32(offset)
		ipBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(ipBytes, ipValue)
		out = append(out, net.IP(ipBytes).String())
	}
	return out
}

func runConnectivityScan(ctx context.Context, base transportConfig, options connectivityScanOptions, deps scanDependencies) (scanReport, error) {
	deps = deps.withDefaults(base)
	mode := options.Mode
	if mode == "" {
		mode = scanModeFull
	}
	report := scanReport{ProbeURL: scanProbeURL, Mode: string(mode)}
	defaultDomains, err := deps.loadDefaultDomains()
	if err != nil {
		return report, err
	}
	defaultIPSpecs, err := deps.loadDefaultIPSpecs()
	if err != nil {
		return report, err
	}
	profiles := scanProfileCandidates(base)
	domains := scanCandidateDomains(defaultDomains, options.FrontingTargets, options.ExtraDomains)
	snis := scanCandidateSNIs(domains, options.FrontingSNIs)
	for _, profile := range profiles {
		cfg := base
		cfg.Scan = false
		cfg.disableUTLSFallback = true
		cfg.Fronting = frontingConfig{}
		cfg.ResolveTo = ""
		cfg.ResolveToAddrs = nil
		cfg.UTLSProfileName = profile.name
		cfg.UTLSProfile = profile.id
		cfg.UTLSProfiles = []utlsProfileOption{profile}
		if err := deps.probe(ctx, cfg); err == nil {
			report.DirectProfiles = append(report.DirectProfiles, profile.name)
			report.UTLSProfiles = append(report.UTLSProfiles, profile.name)
		}
	}
	targetResults := map[string]*scanTargetResult{}
	getTarget := func(domain string) *scanTargetResult {
		if result, ok := targetResults[domain]; ok {
			return result
		}
		result := &scanTargetResult{Target: domain}
		targetResults[domain] = result
		return result
	}
	dialSources := make([]scanIPSourceResult, 0, len(domains)*2+2)
	switch mode {
	case scanModeFull, scanModeOnlyIP:
		for _, domain := range domains {
			result := getTarget(domain)
			ips, err := deps.resolveLocal(ctx, domain)
			if err == nil {
				result.LocalDNSIPs = dedupeStrings(ips)
			}
			dialSources = append(dialSources, scanIPSourceResult{Source: "local_dns", Target: domain, CandidateIPs: result.LocalDNSIPs})
		}
		for _, domain := range domains {
			result := getTarget(domain)
			ips, err := deps.resolveRemote(ctx, domain)
			if err == nil {
				result.RemoteDNSIPs = dedupeStrings(ips)
			}
			dialSources = append(dialSources, scanIPSourceResult{Source: "remote_dns", Target: domain, CandidateIPs: result.RemoteDNSIPs})
		}
		if len(options.ResolveToAddrs) > 0 {
			dialSources = append(dialSources, scanIPSourceResult{Source: "resolve_to", CandidateIPs: dedupeStrings(options.ResolveToAddrs)})
		}
		rangeIPs, err := expandIPSpecsWithSampling(scanCandidateIPSpecs(defaultIPSpecs, options.ExtraIPSpecs), base.ScanIPRandomCount)
		if err != nil {
			return report, err
		}
		if len(rangeIPs) > 0 {
			dialSources = append(dialSources, scanIPSourceResult{Source: "ip_ranges", CandidateIPs: rangeIPs})
		}
	case scanModeOnlyDomains:
		if len(options.ResolveToAddrs) == 0 {
			return report, fmt.Errorf("--resolve-to is required when --scan-mode only-domains is used")
		}
		dialSources = append(dialSources, scanIPSourceResult{Source: "resolve_to", CandidateIPs: dedupeStrings(options.ResolveToAddrs)})
	default:
		return report, fmt.Errorf("unsupported scan mode %q", mode)
	}
	dialResults := make([]scanIPSourceResult, 0, len(dialSources))
	dialCache := map[string]bool{}
	var dialAccessible []string
	for _, source := range dialSources {
		source.CandidateIPs = dedupeStrings(source.CandidateIPs)
		if len(source.CandidateIPs) == 0 {
			continue
		}
		for _, ip := range source.CandidateIPs {
			worked, seen := dialCache[ip]
			if !seen {
				worked = deps.dial(ctx, base, ip) == nil
				dialCache[ip] = worked
			}
			if worked {
				source.AccessibleIPs = append(source.AccessibleIPs, ip)
				dialAccessible = append(dialAccessible, ip)
			}
		}
		source.AccessibleIPs = dedupeStrings(source.AccessibleIPs)
		dialResults = append(dialResults, source)
	}
	report.DialSources = dialResults
	report.DialAccessibleIPs = dedupeStrings(dialAccessible)
	if mode != scanModeOnlyIP {
		for _, domain := range domains {
			result := getTarget(domain)
			for _, ip := range report.DialAccessibleIPs {
				ipWorked := false
				for _, sni := range snis {
					for _, profile := range profiles {
						cfg := base
						cfg.Scan = false
						cfg.disableUTLSFallback = true
						cfg.Fronting = frontingConfig{Enable: true, Target: domain, Targets: []string{domain}, SNI: sni, explicitSNI: true}
						cfg.ResolveTo = ip
						cfg.ResolveToAddrs = []string{ip}
						cfg.UTLSProfileName = profile.name
						cfg.UTLSProfile = profile.id
						cfg.UTLSProfiles = []utlsProfileOption{profile}
						if err := deps.probe(ctx, cfg); err != nil {
							continue
						}
						result.Profiles = append(result.Profiles, profile.name)
						result.SNIs = append(result.SNIs, sni)
						report.UTLSProfiles = append(report.UTLSProfiles, profile.name)
						report.FrontingSNIs = append(report.FrontingSNIs, sni)
						ipWorked = true
					}
				}
				if ipWorked {
					result.ResolveToIPs = append(result.ResolveToIPs, ip)
					report.ResolveToAddrs = append(report.ResolveToAddrs, ip)
				}
			}
			result.LocalDNSIPs = dedupeStrings(result.LocalDNSIPs)
			result.RemoteDNSIPs = dedupeStrings(result.RemoteDNSIPs)
			result.Profiles = dedupeStrings(result.Profiles)
			result.SNIs = dedupeStrings(result.SNIs)
			result.ResolveToIPs = dedupeStrings(result.ResolveToIPs)
			if len(result.Profiles) > 0 && len(result.ResolveToIPs) > 0 {
				report.Targets = append(report.Targets, *result)
				report.FrontingTargets = append(report.FrontingTargets, domain)
			}
		}
	}
	report.DirectProfiles = dedupeStrings(report.DirectProfiles)
	report.FrontingSNIs = dedupeStrings(report.FrontingSNIs)
	report.FrontingTargets = dedupeStrings(report.FrontingTargets)
	report.ResolveToAddrs = dedupeStrings(report.ResolveToAddrs)
	report.UTLSProfiles = dedupeStrings(report.UTLSProfiles)
	if len(report.DirectProfiles) == 0 && len(report.DialAccessibleIPs) == 0 && len(report.FrontingTargets) == 0 {
		return report, fmt.Errorf("scan found no viable direct, IP, or fronted routes")
	}
	return report, nil
}

func testConnectivityWithTransport(ctx context.Context, cfg transportConfig, runtime *downloadRuntime, probeURL string) (ConnectivityReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCfg := cfg
	probeCfg.Scan = false
	client, err := probeCfg.newHTTPClient(nil)
	if err != nil {
		return ConnectivityReport{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return ConnectivityReport{}, err
	}
	req = withRequestTrace(req, runtime, nil)
	res, err := client.Do(req)
	if err != nil {
		report := newConnectivityReport(probeCfg, probeURL)
		if runtime != nil {
			runtime.report("connectivity", report.failureFields(err))
		}
		return report, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	report := newConnectivityReport(probeCfg, probeURL)
	report.Status = res.Status
	report.StatusCode = res.StatusCode
	report.Protocol = res.Proto
	if runtime != nil {
		runtime.report("connectivity", report.fields())
	}
	if res.StatusCode != http.StatusNoContent {
		return report, fmt.Errorf("unexpected probe status: %s", res.Status)
	}
	return report, nil
}

func newConnectivityReport(cfg transportConfig, probeURL string) ConnectivityReport {
	profiles := cfg.configuredUTLSProfiles()
	profileNames := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		profileNames = append(profileNames, profile.name)
	}
	proxy := ""
	if cfg.Proxy != nil {
		proxy = cfg.Proxy.String()
	}
	return ConnectivityReport{
		ProbeURL:        probeURL,
		Proxy:           proxy,
		RetryCount:      cfg.RetryCount,
		FrontingEnabled: cfg.Fronting.Enable,
		FrontingTargets: append([]string(nil), cfg.frontingTargets()...),
		FrontingSNI:     cfg.Fronting.SNI,
		ResolveToAddrs:  append([]string(nil), cfg.resolveTargets()...),
		UTLSProfiles:    profileNames,
	}
}

func (report ConnectivityReport) print(w io.Writer) {
	fmt.Fprintf(w, "Connectivity Probe: %s\n", report.ProbeURL)
	fmt.Fprintf(w, "HTTP Status: %s\n", firstNonEmpty(report.Status, "(not reached)"))
	fmt.Fprintf(w, "HTTP Protocol: %s\n", firstNonEmpty(report.Protocol, "(unknown)"))
	fmt.Fprintf(w, "Proxy: %s\n", firstNonEmpty(report.Proxy, "(none)"))
	fmt.Fprintf(w, "Retry Count: %d\n", report.RetryCount)
	fmt.Fprintf(w, "Fronting Enabled: %t\n", report.FrontingEnabled)
	fmt.Fprintf(w, "Fronting Targets: %s\n", joinOrNone(report.FrontingTargets))
	fmt.Fprintf(w, "Fronting SNI: %s\n", firstNonEmpty(report.FrontingSNI, "(none)"))
	fmt.Fprintf(w, "Resolve-To IPs: %s\n", joinOrNone(report.ResolveToAddrs))
	fmt.Fprintf(w, "uTLS Profiles: %s\n", joinOrNone(report.UTLSProfiles))
}

func (report ConnectivityReport) fields() map[string]any {
	return map[string]any{
		"probe_url":        report.ProbeURL,
		"status":           report.Status,
		"status_code":      report.StatusCode,
		"protocol":         report.Protocol,
		"proxy":            report.Proxy,
		"retry_count":      report.RetryCount,
		"fronting_enabled": report.FrontingEnabled,
		"fronting_targets": report.FrontingTargets,
		"fronting_sni":     report.FrontingSNI,
		"resolve_to_addrs": report.ResolveToAddrs,
		"utls_profiles":    report.UTLSProfiles,
	}
}

func (report ConnectivityReport) failureFields(err error) map[string]any {
	fields := report.fields()
	fields["error"] = err.Error()
	return fields
}

func (report scanReport) print(w io.Writer) {
	fmt.Fprintf(w, "Scan Probe: %s\n", report.ProbeURL)
	fmt.Fprintf(w, "Scan Mode: %s\n", firstNonEmpty(report.Mode, string(scanModeFull)))
	fmt.Fprintf(w, "Direct uTLS Profiles: %s\n", joinOrNone(report.DirectProfiles))
	fmt.Fprintf(w, "Dial-Accessible IPs: %s\n", joinOrNone(report.DialAccessibleIPs))
	fmt.Fprintf(w, "Fronting Targets: %s\n", joinOrNone(report.FrontingTargets))
	fmt.Fprintf(w, "Fronting SNIs: %s\n", joinOrNone(report.FrontingSNIs))
	fmt.Fprintf(w, "Resolve-To IPs: %s\n", joinOrNone(report.ResolveToAddrs))
	fmt.Fprintf(w, "Reusable uTLS Profiles: %s\n", joinOrNone(report.UTLSProfiles))
	for _, source := range report.DialSources {
		if source.Target != "" {
			fmt.Fprintf(w, "Dial Source: %s | Target: %s | Candidate IPs: %s | Accessible IPs: %s\n", source.Source, source.Target, joinOrNone(source.CandidateIPs), joinOrNone(source.AccessibleIPs))
			continue
		}
		fmt.Fprintf(w, "Dial Source: %s | Candidate IPs: %s | Accessible IPs: %s\n", source.Source, joinOrNone(source.CandidateIPs), joinOrNone(source.AccessibleIPs))
	}
	for _, target := range report.Targets {
		fmt.Fprintf(w, "Target: %s | Local DNS IPs: %s | Remote DNS IPs: %s | Profiles: %s | SNIs: %s | Resolve-To IPs: %s\n", target.Target, joinOrNone(target.LocalDNSIPs), joinOrNone(target.RemoteDNSIPs), joinOrNone(target.Profiles), joinOrNone(target.SNIs), joinOrNone(target.ResolveToIPs))
	}
}

func (report scanReport) fields() map[string]any {
	return map[string]any{
		"probe_url":           report.ProbeURL,
		"scan_mode":           report.Mode,
		"direct_profiles":     report.DirectProfiles,
		"dial_accessible_ips": report.DialAccessibleIPs,
		"fronting_snis":       report.FrontingSNIs,
		"fronting_targets":    report.FrontingTargets,
		"resolve_to_addrs":    report.ResolveToAddrs,
		"utls_profiles":       report.UTLSProfiles,
		"dial_sources":        report.DialSources,
		"targets":             report.Targets,
	}
}

func (report scanReport) apply(cfg transportConfig) transportConfig {
	clone := cfg
	clone.Scan = false
	selectedProfiles := report.UTLSProfiles
	if len(report.DirectProfiles) > 0 {
		selectedProfiles = report.DirectProfiles
		clone.Fronting = frontingConfig{}
		clone.ResolveTo = ""
		clone.ResolveToAddrs = nil
	} else if len(report.FrontingTargets) > 0 && len(report.ResolveToAddrs) > 0 {
		clone.Fronting.Enable = true
		clone.Fronting.Target = report.FrontingTargets[0]
		clone.Fronting.Targets = append([]string(nil), report.FrontingTargets...)
		if len(report.FrontingSNIs) > 0 {
			clone.Fronting.SNI = report.FrontingSNIs[0]
			clone.Fronting.explicitSNI = true
		} else if !clone.Fronting.explicitSNI {
			clone.Fronting.SNI = clone.Fronting.Target
		}
		clone.ResolveTo = report.ResolveToAddrs[0]
		clone.ResolveToAddrs = append([]string(nil), report.ResolveToAddrs...)
	}
	if len(selectedProfiles) > 0 {
		profiles := make([]utlsProfileOption, 0, len(selectedProfiles))
		for _, name := range selectedProfiles {
			if id, ok := supportedUTLSProfiles[name]; ok {
				profiles = append(profiles, utlsProfileOption{name: name, id: id})
			}
		}
		if len(profiles) > 0 {
			clone.UTLSProfiles = profiles
			clone.UTLSProfileName = profiles[0].name
			clone.UTLSProfile = profiles[0].id
		}
	}
	return clone
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ",")
}
