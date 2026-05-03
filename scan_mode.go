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
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

const (
	scanProbeURL          = "https://gstatic.com/generate_204"
	scanRemoteDNSURL      = "https://dns.google/resolve"
	scanMaxCIDRHostCount  = 65536
	scanRemoteDNSFronting = "google.com"
)

type scanMode string

const (
	scanModeFull        scanMode = "full"
	scanModeOnlyIP      scanMode = "only-ip"
	scanModeOnlyDomains scanMode = "only-domains"
)

type connectivityScanOptions struct {
	Mode            scanMode
	Concurrency     int
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

type scanProbeFunc func(context.Context, transportConfig, *downloadRuntime) error
type scanResolveFunc func(context.Context, string, *downloadRuntime) ([]string, error)
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

func defaultScanResolver(ctx context.Context, host string, _ *downloadRuntime) ([]string, error) {
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
	return func(ctx context.Context, host string, runtime *downloadRuntime) ([]string, error) {
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
		req = withRequestTrace(req, runtime, nil)
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

func defaultScanProbe(ctx context.Context, cfg transportConfig, runtime *downloadRuntime) error {
	probeCfg := cfg
	probeCfg.Scan = false
	probeCfg.disableUTLSFallback = true
	_, err := testConnectivityWithTransport(ctx, probeCfg, runtime, scanProbeURL)
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
	return expandIPSpecsWithSamplingContext(context.Background(), specs, cidrSampleCount)
}

func expandIPSpecsWithSamplingContext(ctx context.Context, specs []string, cidrSampleCount int) ([]string, error) {
	var out []string
	for _, spec := range dedupeStrings(specs) {
		if err := checkScanContext(ctx); err != nil {
			return nil, err
		}
		expanded, err := expandIPSpecWithSamplingContext(ctx, spec, cidrSampleCount)
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
	return expandIPSpecWithSamplingContext(context.Background(), spec, cidrSampleCount)
}

func expandIPSpecWithSamplingContext(ctx context.Context, spec string, cidrSampleCount int) ([]string, error) {
	if ip := net.ParseIP(spec); ip != nil {
		return []string{ip.String()}, nil
	}
	_, ipnet, err := net.ParseCIDR(spec)
	if err != nil {
		return nil, fmt.Errorf("scan IP list values must be an IP address or CIDR range")
	}
	return expandIPv4CIDRWithSamplingContext(ctx, ipnet, cidrSampleCount)
}

func expandIPv4CIDR(ipnet *net.IPNet) ([]string, error) {
	return expandIPv4CIDRWithSampling(ipnet, 0)
}

func expandIPv4CIDRWithSampling(ipnet *net.IPNet, cidrSampleCount int) ([]string, error) {
	return expandIPv4CIDRWithSamplingContext(context.Background(), ipnet, cidrSampleCount)
}

func expandIPv4CIDRWithSamplingContext(ctx context.Context, ipnet *net.IPNet, cidrSampleCount int) ([]string, error) {
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
			return expandIPv4RangeContext(ctx, base, hostCount)
		}
		return sampleIPv4Range(base, hostCount, cidrSampleCount), nil
	}
	if hostCount > scanMaxCIDRHostCount {
		return nil, fmt.Errorf("scan IP range %q expands to %d addresses; split it into smaller CIDRs", ipnet.String(), hostCount)
	}
	return expandIPv4RangeContext(ctx, base, hostCount)
}

func expandIPv4Range(base net.IP, hostCount uint64) []string {
	out, _ := expandIPv4RangeContext(context.Background(), base, hostCount)
	return out
}

func expandIPv4RangeContext(ctx context.Context, base net.IP, hostCount uint64) ([]string, error) {
	start := binary.BigEndian.Uint32(base)
	out := make([]string, 0, int(hostCount))
	for offset := uint64(0); offset < hostCount; offset++ {
		if offset%1024 == 0 {
			if err := checkScanContext(ctx); err != nil {
				return nil, err
			}
		}
		ipValue := start + uint32(offset)
		ipBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(ipBytes, ipValue)
		out = append(out, net.IP(ipBytes).String())
	}
	return out, nil
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

func runConnectivityScan(ctx context.Context, base transportConfig, options connectivityScanOptions, runtime *downloadRuntime, deps scanDependencies) (report scanReport, scanErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deps = deps.withDefaults(base)
	concurrency := options.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	mode := options.Mode
	if mode == "" {
		mode = scanModeFull
	}
	report = scanReport{ProbeURL: scanProbeURL, Mode: string(mode)}
	observer := newScanObserver(runtime, mode, base.Verbosity)
	defer func() {
		observer.finish(scanErr)
	}()
	observer.beginPhase("setup")
	observer.update("loading defaults", "loading scan candidates")
	if err := checkScanContext(ctx); err != nil {
		return report, err
	}
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
	observer.addTotal(int64(len(profiles)))
	observer.beginPhase("direct_probe")
	directWorked := make([]bool, len(profiles))
	if err := runScanParallel(ctx, concurrency, len(profiles), func(index int) error {
		if err := checkScanContext(ctx); err != nil {
			return err
		}
		profile := profiles[index]
		cfg := base
		cfg.Scan = false
		cfg.disableUTLSFallback = true
		cfg.Fronting = frontingConfig{}
		cfg.ResolveTo = ""
		cfg.ResolveToAddrs = nil
		cfg.UTLSProfileName = profile.name
		cfg.UTLSProfile = profile.id
		cfg.UTLSProfiles = []utlsProfileOption{profile}
		observer.update("probing", fmt.Sprintf("direct profile=%s", profile.name))
		probeErr := deps.probe(ctx, cfg, runtime)
		if probeErr == nil {
			directWorked[index] = true
		} else if isCancellationError(probeErr) {
			return probeErr
		}
		observer.complete()
		return nil
	}); err != nil {
		return report, err
	}
	for index, worked := range directWorked {
		if !worked {
			continue
		}
		profile := profiles[index]
		report.DirectProfiles = append(report.DirectProfiles, profile.name)
		report.UTLSProfiles = append(report.UTLSProfiles, profile.name)
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
		observer.addTotal(int64(len(domains) * 2))
		observer.beginPhase("local_dns")
		localDNSResults := make([][]string, len(domains))
		if err := runScanParallel(ctx, concurrency, len(domains), func(index int) error {
			if err := checkScanContext(ctx); err != nil {
				return err
			}
			domain := domains[index]
			observer.update("resolving", fmt.Sprintf("local dns host=%s", domain))
			ips, resolveErr := deps.resolveLocal(ctx, domain, runtime)
			if isCancellationError(resolveErr) {
				return resolveErr
			}
			if resolveErr == nil {
				localDNSResults[index] = dedupeStrings(ips)
			}
			observer.complete()
			return nil
		}); err != nil {
			return report, err
		}
		for index, domain := range domains {
			result := getTarget(domain)
			result.LocalDNSIPs = localDNSResults[index]
			dialSources = append(dialSources, scanIPSourceResult{Source: "local_dns", Target: domain, CandidateIPs: result.LocalDNSIPs})
		}
		observer.beginPhase("remote_dns")
		remoteDNSResults := make([][]string, len(domains))
		if err := runScanParallel(ctx, concurrency, len(domains), func(index int) error {
			if err := checkScanContext(ctx); err != nil {
				return err
			}
			domain := domains[index]
			observer.update("resolving", fmt.Sprintf("remote dns host=%s", domain))
			ips, resolveErr := deps.resolveRemote(ctx, domain, runtime)
			if isCancellationError(resolveErr) {
				return resolveErr
			}
			if resolveErr == nil {
				remoteDNSResults[index] = dedupeStrings(ips)
			}
			observer.complete()
			return nil
		}); err != nil {
			return report, err
		}
		for index, domain := range domains {
			result := getTarget(domain)
			result.RemoteDNSIPs = remoteDNSResults[index]
			dialSources = append(dialSources, scanIPSourceResult{Source: "remote_dns", Target: domain, CandidateIPs: result.RemoteDNSIPs})
		}
		if len(options.ResolveToAddrs) > 0 {
			dialSources = append(dialSources, scanIPSourceResult{Source: "resolve_to", CandidateIPs: dedupeStrings(options.ResolveToAddrs)})
		}
		observer.addTotal(1)
		observer.beginPhase("expand_ip_ranges")
		observer.update("expanding", "expanding IP ranges")
		rangeIPs, err := expandIPSpecsWithSamplingContext(ctx, scanCandidateIPSpecs(defaultIPSpecs, options.ExtraIPSpecs), base.ScanIPRandomCount)
		if isCancellationError(err) {
			return report, err
		}
		if err != nil {
			return report, err
		}
		observer.complete()
		if len(rangeIPs) > 0 {
			dialSources = append(dialSources, scanIPSourceResult{Source: "ip_ranges", CandidateIPs: rangeIPs})
		}
	case scanModeOnlyDomains:
		explicitFrontingTargets := dedupeStrings(options.FrontingTargets)
		if len(options.ResolveToAddrs) == 0 && len(explicitFrontingTargets) == 0 {
			err := fmt.Errorf("--resolve-to or --fronting-target is required when --scan-mode only-domains is used")
			return report, err
		}
		dialSources = append(dialSources, scanIPSourceResult{Source: "resolve_to", CandidateIPs: dedupeStrings(options.ResolveToAddrs)})
		if len(explicitFrontingTargets) > 0 {
			observer.addTotal(int64(len(explicitFrontingTargets)))
			observer.beginPhase("fronting_target_dns")
			frontingDNSResults := make([][]string, len(explicitFrontingTargets))
			if err := runScanParallel(ctx, concurrency, len(explicitFrontingTargets), func(index int) error {
				if err := checkScanContext(ctx); err != nil {
					return err
				}
				target := explicitFrontingTargets[index]
				observer.update("resolving", fmt.Sprintf("fronting target dns host=%s", target))
				ips, resolveErr := deps.resolveLocal(ctx, target, runtime)
				if isCancellationError(resolveErr) {
					return resolveErr
				}
				if resolveErr == nil {
					frontingDNSResults[index] = dedupeStrings(ips)
				}
				observer.complete()
				return nil
			}); err != nil {
				return report, err
			}
			for index, target := range explicitFrontingTargets {
				result := getTarget(target)
				result.LocalDNSIPs = dedupeStrings(append(result.LocalDNSIPs, frontingDNSResults[index]...))
				dialSources = append(dialSources, scanIPSourceResult{Source: "fronting_target_dns", Target: target, CandidateIPs: frontingDNSResults[index]})
			}
		}
	default:
		err := fmt.Errorf("unsupported scan mode %q", mode)
		return report, err
	}
	dialResults := make([]scanIPSourceResult, 0, len(dialSources))
	var dialAccessible []string
	uniqueDialIPs := make([]string, 0)
	uniqueDialIndex := map[string]int{}
	for index := range dialSources {
		dialSources[index].CandidateIPs = dedupeStrings(dialSources[index].CandidateIPs)
		for _, ip := range dialSources[index].CandidateIPs {
			if _, exists := uniqueDialIndex[ip]; exists {
				continue
			}
			uniqueDialIndex[ip] = len(uniqueDialIPs)
			uniqueDialIPs = append(uniqueDialIPs, ip)
		}
	}
	observer.addTotal(int64(len(uniqueDialIPs)))
	observer.beginPhase("dial")
	dialWorked := make([]bool, len(uniqueDialIPs))
	if err := runScanParallel(ctx, concurrency, len(uniqueDialIPs), func(index int) error {
		if err := checkScanContext(ctx); err != nil {
			return err
		}
		ip := uniqueDialIPs[index]
		observer.update("dialing", fmt.Sprintf("ip=%s", ip))
		dialWorked[index] = deps.dial(ctx, base, ip) == nil
		observer.complete()
		return nil
	}); err != nil {
		return report, err
	}
	for _, source := range dialSources {
		if len(source.CandidateIPs) == 0 {
			continue
		}
		for _, ip := range source.CandidateIPs {
			if dialWorked[uniqueDialIndex[ip]] {
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
		totalFrontingProbes := len(domains) * len(report.DialAccessibleIPs) * len(snis) * len(profiles)
		observer.addTotal(int64(totalFrontingProbes))
		observer.beginPhase("fronting_probe")
		type frontingSuccess struct {
			domain      string
			resolveToIP string
			sni         string
			profile     string
		}
		frontingSuccesses := make([]frontingSuccess, 0)
		var frontingMu sync.Mutex
		if err := runScanParallel(ctx, concurrency, totalFrontingProbes, func(index int) error {
			if err := checkScanContext(ctx); err != nil {
				return err
			}
			profileCount := len(profiles)
			sniCount := len(snis)
			ipCount := len(report.DialAccessibleIPs)
			domainIndex := index / (ipCount * sniCount * profileCount)
			remaining := index % (ipCount * sniCount * profileCount)
			ipIndex := remaining / (sniCount * profileCount)
			remaining = remaining % (sniCount * profileCount)
			sniIndex := remaining / profileCount
			profileIndex := remaining % profileCount

			domain := domains[domainIndex]
			ip := report.DialAccessibleIPs[ipIndex]
			sni := snis[sniIndex]
			profile := profiles[profileIndex]

			cfg := base
			cfg.Scan = false
			cfg.disableUTLSFallback = true
			cfg.Fronting = frontingConfig{Enable: true, Target: domain, Targets: []string{domain}, SNI: sni, explicitSNI: true}
			cfg.ResolveTo = ip
			cfg.ResolveToAddrs = []string{ip}
			cfg.UTLSProfileName = profile.name
			cfg.UTLSProfile = profile.id
			cfg.UTLSProfiles = []utlsProfileOption{profile}
			observer.update("probing", fmt.Sprintf("target=%s sni=%s ip=%s profile=%s", domain, sni, ip, profile.name))
			probeErr := deps.probe(ctx, cfg, runtime)
			if probeErr != nil {
				if isCancellationError(probeErr) {
					return probeErr
				}
				observer.complete()
				return nil
			}
			frontingMu.Lock()
			frontingSuccesses = append(frontingSuccesses, frontingSuccess{domain: domain, resolveToIP: ip, sni: sni, profile: profile.name})
			frontingMu.Unlock()
			observer.complete()
			return nil
		}); err != nil {
			return report, err
		}
		sort.Slice(frontingSuccesses, func(i, j int) bool {
			left := frontingSuccesses[i]
			right := frontingSuccesses[j]
			if left.domain != right.domain {
				return left.domain < right.domain
			}
			if left.resolveToIP != right.resolveToIP {
				return left.resolveToIP < right.resolveToIP
			}
			if left.sni != right.sni {
				return left.sni < right.sni
			}
			return left.profile < right.profile
		})
		for _, success := range frontingSuccesses {
			result := getTarget(success.domain)
			result.Profiles = append(result.Profiles, success.profile)
			result.SNIs = append(result.SNIs, success.sni)
			result.ResolveToIPs = append(result.ResolveToIPs, success.resolveToIP)
			report.UTLSProfiles = append(report.UTLSProfiles, success.profile)
			report.FrontingSNIs = append(report.FrontingSNIs, success.sni)
			report.ResolveToAddrs = append(report.ResolveToAddrs, success.resolveToIP)
		}
		for _, domain := range domains {
			result := getTarget(domain)
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
		err := fmt.Errorf("scan found no viable direct, IP, or fronted routes")
		return report, err
	}
	return report, nil
}

func runScanParallel(ctx context.Context, concurrency, count int, fn func(int) error) error {
	if count == 0 {
		return checkScanContext(ctx)
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > count {
		concurrency = count
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		errMu.Unlock()
	}
	worker := func() {
		defer wg.Done()
		for {
			select {
			case <-workerCtx.Done():
				return
			case index, ok := <-jobs:
				if !ok {
					return
				}
				if err := fn(index); err != nil {
					recordErr(err)
					return
				}
			}
		}
	}
	for workerIndex := 0; workerIndex < concurrency; workerIndex++ {
		wg.Add(1)
		go worker()
	}
loop:
	for index := 0; index < count; index++ {
		select {
		case <-workerCtx.Done():
			break loop
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()
	errMu.Lock()
	defer errMu.Unlock()
	if firstErr != nil {
		return firstErr
	}
	return checkScanContext(ctx)
}

func checkScanContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
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
