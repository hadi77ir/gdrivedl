package gdrivedl

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestReadHostnames(t *testing.T) {
	hosts, err := readHostnames(strings.NewReader("\n# comment\ngoogle.com,www.google.com\nfonts.gstatic.com\nend\n"))
	if err != nil {
		t.Fatalf("readHostnames() error = %v", err)
	}
	if got, want := strings.Join(hosts, ","), "google.com,www.google.com,fonts.gstatic.com"; got != want {
		t.Fatalf("hosts = %q, want %q", got, want)
	}
}

func TestReadIPSpecs(t *testing.T) {
	specs, err := readIPSpecs(strings.NewReader("\n# comment\n203.0.113.10,203.0.113.0/30\n203.0.113.10\nend\n"))
	if err != nil {
		t.Fatalf("readIPSpecs() error = %v", err)
	}
	if got, want := strings.Join(specs, ","), "203.0.113.10,203.0.113.0/30"; got != want {
		t.Fatalf("specs = %q, want %q", got, want)
	}
}

func TestExpandIPSpecs(t *testing.T) {
	got, err := expandIPSpecs([]string{"203.0.113.0/30", "203.0.113.10"})
	if err != nil {
		t.Fatalf("expandIPSpecs() error = %v", err)
	}
	want := []string{"203.0.113.0", "203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandIPSpecs() = %#v, want %#v", got, want)
	}
}

func TestExpandIPSpecsWithSampling(t *testing.T) {
	got, err := expandIPSpecsWithSampling([]string{"203.0.113.0/29", "203.0.113.10"}, 3)
	if err != nil {
		t.Fatalf("expandIPSpecsWithSampling() error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expandIPSpecsWithSampling() len = %d, want 4", len(got))
	}
	if !containsString(got, "203.0.113.10") {
		t.Fatalf("expandIPSpecsWithSampling() should keep explicit IPs, got %#v", got)
	}
	for _, value := range got {
		if value == "203.0.113.10" {
			continue
		}
		if !mustParseCIDR(t, "203.0.113.0/29").Contains(net.ParseIP(value)) {
			t.Fatalf("sampled IP %q should be inside 203.0.113.0/29", value)
		}
	}
}

func TestExpandIPv4CIDRWithSamplingAllowsLargeRanges(t *testing.T) {
	got, err := expandIPv4CIDRWithSampling(mustParseCIDR(t, "198.51.100.0/8"), 5)
	if err != nil {
		t.Fatalf("expandIPv4CIDRWithSampling() error = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expandIPv4CIDRWithSampling() len = %d, want 5", len(got))
	}
	for _, value := range got {
		if !mustParseCIDR(t, "198.0.0.0/8").Contains(net.ParseIP(value)) {
			t.Fatalf("sampled IP %q should be inside 198.0.0.0/8", value)
		}
	}
}

func TestRunConnectivityScan(t *testing.T) {
	base := transportConfig{UTLSProfiles: []utlsProfileOption{{name: "chrome_auto", id: supportedUTLSProfiles["chrome_auto"]}}}
	frontingResolveAttempts := map[string]struct{}{}
	deps := scanDependencies{
		loadDefaultDomains: func() ([]string, error) {
			return []string{"scan.example.com"}, nil
		},
		loadDefaultIPSpecs: func() ([]string, error) {
			return []string{"198.51.100.20/31"}, nil
		},
		resolveLocal: func(context.Context, string, *downloadRuntime) ([]string, error) {
			return []string{"203.0.113.10"}, nil
		},
		resolveRemote: func(context.Context, string, *downloadRuntime) ([]string, error) {
			return []string{"203.0.113.11"}, nil
		},
		dial: func(_ context.Context, _ transportConfig, ip string) error {
			switch ip {
			case "203.0.113.11", "198.51.100.20":
				return nil
			default:
				return errors.New("dial fail")
			}
		},
		probe: func(_ context.Context, cfg transportConfig, _ *downloadRuntime) error {
			if !cfg.Fronting.Enable {
				if cfg.UTLSProfileName == "firefox_auto" {
					return nil
				}
				return errors.New("direct fail")
			}
			frontingResolveAttempts[cfg.ResolveTo] = struct{}{}
			if cfg.Fronting.Target == "scan.example.com" && cfg.ResolveTo == "203.0.113.11" && (cfg.UTLSProfileName == "chrome_auto" || cfg.UTLSProfileName == "firefox_auto") {
				return nil
			}
			return errors.New("fronting fail")
		},
	}
	report, err := runConnectivityScan(context.Background(), base, connectivityScanOptions{Mode: scanModeFull}, nil, deps)
	if err != nil {
		t.Fatalf("runConnectivityScan() error = %v", err)
	}
	if report.Mode != string(scanModeFull) {
		t.Fatalf("Mode = %q", report.Mode)
	}
	if got := strings.Join(report.DirectProfiles, ","); got != "firefox_auto" {
		t.Fatalf("DirectProfiles = %q", got)
	}
	if got := strings.Join(report.FrontingSNIs, ","); got != "scan.example.com" {
		t.Fatalf("FrontingSNIs = %q", got)
	}
	if got := strings.Join(report.FrontingTargets, ","); got != "scan.example.com" {
		t.Fatalf("FrontingTargets = %q", got)
	}
	if got := strings.Join(report.ResolveToAddrs, ","); got != "203.0.113.11" {
		t.Fatalf("ResolveToAddrs = %q", got)
	}
	if got := strings.Join(report.DialAccessibleIPs, ","); got != "203.0.113.11,198.51.100.20" {
		t.Fatalf("DialAccessibleIPs = %q", got)
	}
	profiles := append([]string(nil), report.UTLSProfiles...)
	sort.Strings(profiles)
	if got := strings.Join(profiles, ","); got != "chrome_auto,firefox_auto" {
		t.Fatalf("UTLSProfiles = %q", got)
	}
	if len(report.DialSources) != 3 {
		t.Fatalf("DialSources len = %d", len(report.DialSources))
	}
	if len(report.Targets) != 1 {
		t.Fatalf("Targets len = %d", len(report.Targets))
	}
	if report.Targets[0].Target != "scan.example.com" {
		t.Fatalf("Target = %q", report.Targets[0].Target)
	}
	if !reflect.DeepEqual(report.Targets[0].LocalDNSIPs, []string{"203.0.113.10"}) {
		t.Fatalf("LocalDNSIPs = %#v", report.Targets[0].LocalDNSIPs)
	}
	if !reflect.DeepEqual(report.Targets[0].RemoteDNSIPs, []string{"203.0.113.11"}) {
		t.Fatalf("RemoteDNSIPs = %#v", report.Targets[0].RemoteDNSIPs)
	}
	if !reflect.DeepEqual(report.Targets[0].SNIs, []string{"scan.example.com"}) {
		t.Fatalf("SNIs = %#v", report.Targets[0].SNIs)
	}
	if !reflect.DeepEqual(report.Targets[0].ResolveToIPs, []string{"203.0.113.11"}) {
		t.Fatalf("ResolveToIPs = %#v", report.Targets[0].ResolveToIPs)
	}
	resolveAttempts := make([]string, 0, len(frontingResolveAttempts))
	for ip := range frontingResolveAttempts {
		resolveAttempts = append(resolveAttempts, ip)
	}
	sort.Strings(resolveAttempts)
	if !reflect.DeepEqual(resolveAttempts, []string{"198.51.100.20", "203.0.113.11"}) {
		t.Fatalf("fronting resolve attempts = %#v", resolveAttempts)
	}
}

func TestRunConnectivityScanSamplesCIDRInputs(t *testing.T) {
	base := transportConfig{
		ScanIPRandomCount: 1,
		UTLSProfiles:      []utlsProfileOption{{name: "chrome_auto", id: supportedUTLSProfiles["chrome_auto"]}},
	}
	deps := scanDependencies{
		loadDefaultDomains: func() ([]string, error) { return []string{"scan.example.com"}, nil },
		loadDefaultIPSpecs: func() ([]string, error) { return []string{"198.51.100.0/30"}, nil },
		resolveLocal:       func(context.Context, string, *downloadRuntime) ([]string, error) { return nil, nil },
		resolveRemote:      func(context.Context, string, *downloadRuntime) ([]string, error) { return nil, nil },
		dial:               func(context.Context, transportConfig, string) error { return nil },
		probe: func(_ context.Context, cfg transportConfig, _ *downloadRuntime) error {
			if !cfg.Fronting.Enable {
				return nil
			}
			return errors.New("fronting fail")
		},
	}
	report, err := runConnectivityScan(context.Background(), base, connectivityScanOptions{Mode: scanModeFull}, nil, deps)
	if err != nil {
		t.Fatalf("runConnectivityScan() error = %v", err)
	}
	if len(report.DialSources) != 1 {
		t.Fatalf("DialSources len = %d, want 1", len(report.DialSources))
	}
	if report.DialSources[0].Source != "ip_ranges" {
		t.Fatalf("DialSources[0].Source = %q, want ip_ranges", report.DialSources[0].Source)
	}
	if len(report.DialSources[0].CandidateIPs) != 1 {
		t.Fatalf("CandidateIPs len = %d, want 1", len(report.DialSources[0].CandidateIPs))
	}
	if len(report.DialSources[0].AccessibleIPs) != 1 {
		t.Fatalf("AccessibleIPs len = %d, want 1", len(report.DialSources[0].AccessibleIPs))
	}
	if !mustParseCIDR(t, "198.51.100.0/30").Contains(net.ParseIP(report.DialSources[0].CandidateIPs[0])) {
		t.Fatalf("candidate IP %q should be inside 198.51.100.0/30", report.DialSources[0].CandidateIPs[0])
	}
}

func TestRunConnectivityScanOnlyIPIncludesResolveTo(t *testing.T) {
	base := transportConfig{UTLSProfiles: []utlsProfileOption{{name: "chrome_auto", id: supportedUTLSProfiles["chrome_auto"]}}}
	deps := scanDependencies{
		loadDefaultDomains: func() ([]string, error) { return nil, nil },
		loadDefaultIPSpecs: func() ([]string, error) { return nil, nil },
		resolveLocal:       func(context.Context, string, *downloadRuntime) ([]string, error) { return nil, nil },
		resolveRemote:      func(context.Context, string, *downloadRuntime) ([]string, error) { return nil, nil },
		dial: func(_ context.Context, _ transportConfig, ip string) error {
			if ip == "203.0.113.99" {
				return nil
			}
			return errors.New("dial fail")
		},
		probe: func(context.Context, transportConfig, *downloadRuntime) error { return errors.New("direct fail") },
	}
	report, err := runConnectivityScan(context.Background(), base, connectivityScanOptions{
		Mode:           scanModeOnlyIP,
		ResolveToAddrs: []string{"203.0.113.99"},
	}, nil, deps)
	if err != nil {
		t.Fatalf("runConnectivityScan() error = %v", err)
	}
	if got := strings.Join(report.DialAccessibleIPs, ","); got != "203.0.113.99" {
		t.Fatalf("DialAccessibleIPs = %q", got)
	}
	if len(report.DialSources) != 1 || report.DialSources[0].Source != "resolve_to" {
		t.Fatalf("DialSources = %#v", report.DialSources)
	}
	if len(report.Targets) != 0 {
		t.Fatalf("Targets len = %d, want 0", len(report.Targets))
	}
	if len(report.FrontingTargets) != 0 {
		t.Fatalf("FrontingTargets = %#v, want none", report.FrontingTargets)
	}
}

func TestRunConnectivityScanOnlyDomainsIncludesFrontingSNI(t *testing.T) {
	base := transportConfig{UTLSProfiles: []utlsProfileOption{{name: "chrome_auto", id: supportedUTLSProfiles["chrome_auto"]}}}
	localResolveCalls := 0
	remoteResolveCalls := 0
	deps := scanDependencies{
		loadDefaultDomains: func() ([]string, error) { return nil, nil },
		loadDefaultIPSpecs: func() ([]string, error) { return nil, nil },
		resolveLocal: func(context.Context, string, *downloadRuntime) ([]string, error) {
			localResolveCalls++
			return nil, nil
		},
		resolveRemote: func(context.Context, string, *downloadRuntime) ([]string, error) {
			remoteResolveCalls++
			return nil, nil
		},
		dial: func(_ context.Context, _ transportConfig, ip string) error {
			if ip == "203.0.113.42" {
				return nil
			}
			return errors.New("dial fail")
		},
		probe: func(_ context.Context, cfg transportConfig, _ *downloadRuntime) error {
			if cfg.Fronting.Target == "target.example.com" && cfg.Fronting.SNI == "sni.example.com" && cfg.ResolveTo == "203.0.113.42" {
				return nil
			}
			return errors.New("fronting fail")
		},
	}
	report, err := runConnectivityScan(context.Background(), base, connectivityScanOptions{
		Mode:            scanModeOnlyDomains,
		FrontingTargets: []string{"target.example.com"},
		FrontingSNIs:    []string{"sni.example.com"},
		ResolveToAddrs:  []string{"203.0.113.42"},
	}, nil, deps)
	if err != nil {
		t.Fatalf("runConnectivityScan() error = %v", err)
	}
	if localResolveCalls != 0 || remoteResolveCalls != 0 {
		t.Fatalf("DNS resolvers should not be used in only-domains mode: local=%d remote=%d", localResolveCalls, remoteResolveCalls)
	}
	if got := strings.Join(report.FrontingTargets, ","); got != "target.example.com" {
		t.Fatalf("FrontingTargets = %q", got)
	}
	if got := strings.Join(report.FrontingSNIs, ","); got != "sni.example.com" {
		t.Fatalf("FrontingSNIs = %q", got)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("Targets len = %d, want 1", len(report.Targets))
	}
	if !reflect.DeepEqual(report.Targets[0].SNIs, []string{"sni.example.com"}) {
		t.Fatalf("SNIs = %#v", report.Targets[0].SNIs)
	}
	if !reflect.DeepEqual(report.Targets[0].ResolveToIPs, []string{"203.0.113.42"}) {
		t.Fatalf("ResolveToIPs = %#v", report.Targets[0].ResolveToIPs)
	}
}

func TestRunConnectivityScanCancelled(t *testing.T) {
	base := transportConfig{UTLSProfiles: []utlsProfileOption{{name: "chrome_auto", id: supportedUTLSProfiles["chrome_auto"]}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := scanDependencies{
		loadDefaultDomains: func() ([]string, error) { return []string{"scan.example.com"}, nil },
		loadDefaultIPSpecs: func() ([]string, error) { return nil, nil },
		resolveLocal: func(_ context.Context, _ string, _ *downloadRuntime) ([]string, error) {
			cancel()
			return nil, context.Canceled
		},
		resolveRemote: func(context.Context, string, *downloadRuntime) ([]string, error) { return nil, nil },
		probe:         func(context.Context, transportConfig, *downloadRuntime) error { return nil },
		dial:          func(context.Context, transportConfig, string) error { return nil },
	}

	_, err := runConnectivityScan(ctx, base, connectivityScanOptions{Mode: scanModeFull}, nil, deps)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runConnectivityScan() error = %v, want context canceled", err)
	}
}

func TestRunConnectivityScanEmitsProgressAndLogEvents(t *testing.T) {
	base := transportConfig{UTLSProfiles: []utlsProfileOption{{name: "chrome_auto", id: supportedUTLSProfiles["chrome_auto"]}}}
	var events []Event
	runtime := newObservedDownloadRuntime(false, false, true, nil, func(event Event) {
		events = append(events, event)
	})
	deps := scanDependencies{
		loadDefaultDomains: func() ([]string, error) { return nil, nil },
		loadDefaultIPSpecs: func() ([]string, error) { return nil, nil },
		resolveLocal:       func(context.Context, string, *downloadRuntime) ([]string, error) { return nil, nil },
		resolveRemote:      func(context.Context, string, *downloadRuntime) ([]string, error) { return nil, nil },
		probe:              func(context.Context, transportConfig, *downloadRuntime) error { return nil },
		dial:               func(context.Context, transportConfig, string) error { return nil },
	}

	output := captureStdout(t, func() {
		_, err := runConnectivityScan(context.Background(), base, connectivityScanOptions{
			Mode:           scanModeOnlyIP,
			ResolveToAddrs: []string{"203.0.113.42"},
		}, runtime, deps)
		if err != nil {
			t.Fatalf("runConnectivityScan() error = %v", err)
		}
	})
	if !strings.Contains(output, "\"name\":\"scan\"") {
		t.Fatalf("expected JSON scan events in output, got %q", output)
	}
	var sawProgress bool
	var sawLog bool
	var sawCompleted bool
	for _, event := range events {
		switch {
		case event.Type == "progress" && event.Name == "scan":
			sawProgress = true
			if state, _ := event.Fields["state"].(string); state == "completed" {
				sawCompleted = true
			}
		case event.Type == "log" && event.Name == "scan":
			sawLog = true
		}
	}
	if !sawProgress {
		t.Fatalf("expected scan progress events, got %#v", events)
	}
	if !sawLog {
		t.Fatalf("expected scan log events, got %#v", events)
	}
	if !sawCompleted {
		t.Fatalf("expected completed scan progress event, got %#v", events)
	}
}

func mustParseCIDR(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	_, ipnet, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatalf("ParseCIDR(%q) error = %v", raw, err)
	}
	return ipnet
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
