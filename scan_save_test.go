package gdrivedl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveScanReportYAMLNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan-results.yml")
	report := scanReport{
		Mode:              string(scanModeFull),
		DirectProfiles:    []string{"firefox_auto"},
		DialAccessibleIPs: []string{"203.0.113.10", "203.0.113.11"},
		FrontingTargets:   []string{"google.com", "www.google.com"},
		FrontingSNIs:      []string{"www.google.com", "google.com"},
		ResolveToAddrs:    []string{"203.0.113.10"},
		UTLSProfiles:      []string{"firefox_auto", "chrome_auto"},
	}
	if err := saveScanReportYAML(path, report); err != nil {
		t.Fatalf("saveScanReportYAML() error = %v", err)
	}
	values := mustLoadSavedYAMLConfig(t, path)
	if values.Transport.FrontingEnable == nil || *values.Transport.FrontingEnable {
		t.Fatalf("transport.fronting-enable = %#v, want false", values.Transport.FrontingEnable)
	}
	if values.Transport.FrontingTarget != nil {
		t.Fatalf("transport.fronting-target = %#v, want nil", values.Transport.FrontingTarget)
	}
	if values.Transport.FrontingSNI != nil {
		t.Fatalf("transport.fronting-sni = %#v, want nil", values.Transport.FrontingSNI)
	}
	if values.Transport.ResolveTo != nil {
		t.Fatalf("transport.resolve-to = %#v, want nil", values.Transport.ResolveTo)
	}
	if values.Transport.UTLSProfile == nil || *values.Transport.UTLSProfile != "firefox_auto" {
		t.Fatalf("transport.utls-profile = %#v, want firefox_auto", values.Transport.UTLSProfile)
	}
	if values.Scan.FrontingTarget == nil || *values.Scan.FrontingTarget != "google.com,www.google.com" {
		t.Fatalf("scan.fronting-target = %#v", values.Scan.FrontingTarget)
	}
	if values.Scan.FrontingSNI == nil || *values.Scan.FrontingSNI != "www.google.com,google.com" {
		t.Fatalf("scan.fronting-sni = %#v", values.Scan.FrontingSNI)
	}
	if values.Scan.ResolveTo == nil || *values.Scan.ResolveTo != "203.0.113.10" {
		t.Fatalf("scan.resolve-to = %#v", values.Scan.ResolveTo)
	}
	if values.Scan.UTLSProfile == nil || *values.Scan.UTLSProfile != "firefox_auto,chrome_auto" {
		t.Fatalf("scan.utls-profile = %#v", values.Scan.UTLSProfile)
	}
}

func TestSaveScanReportYAMLMergesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gdrivedl.yml")
	existing := "defaults:\n  json: true\n\ntransport:\n  proxy: http://127.0.0.1:2089\n\nget:\n  progress: true\n"
	if err := os.WriteFile(path, []byte(existing), 0o666); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	report := scanReport{
		Mode:              string(scanModeOnlyDomains),
		DialAccessibleIPs: []string{"203.0.113.10", "203.0.113.11"},
		FrontingTargets:   []string{"google.com", "www.google.com"},
		FrontingSNIs:      []string{"www.google.com", "google.com"},
		ResolveToAddrs:    []string{"203.0.113.10", "203.0.113.11"},
		UTLSProfiles:      []string{"firefox_auto", "chrome_auto"},
	}
	if err := saveScanReportYAML(path, report); err != nil {
		t.Fatalf("saveScanReportYAML() error = %v", err)
	}
	values := mustLoadSavedYAMLConfig(t, path)
	if values.Defaults.JSONOutput == nil || !*values.Defaults.JSONOutput {
		t.Fatalf("defaults.json = %#v, want true", values.Defaults.JSONOutput)
	}
	if values.Get.EnableProgress == nil || !*values.Get.EnableProgress {
		t.Fatalf("get.progress = %#v, want true", values.Get.EnableProgress)
	}
	if values.Transport.Proxy == nil || *values.Transport.Proxy != "http://127.0.0.1:2089" {
		t.Fatalf("transport.proxy = %#v", values.Transport.Proxy)
	}
	if values.Transport.FrontingEnable == nil || !*values.Transport.FrontingEnable {
		t.Fatalf("transport.fronting-enable = %#v, want true", values.Transport.FrontingEnable)
	}
	if values.Transport.FrontingTarget == nil || *values.Transport.FrontingTarget != "google.com,www.google.com" {
		t.Fatalf("transport.fronting-target = %#v", values.Transport.FrontingTarget)
	}
	if values.Transport.FrontingSNI == nil || *values.Transport.FrontingSNI != "www.google.com" {
		t.Fatalf("transport.fronting-sni = %#v", values.Transport.FrontingSNI)
	}
	if values.Transport.ResolveTo == nil || *values.Transport.ResolveTo != "203.0.113.10,203.0.113.11" {
		t.Fatalf("transport.resolve-to = %#v", values.Transport.ResolveTo)
	}
	if values.Transport.UTLSProfile == nil || *values.Transport.UTLSProfile != "firefox_auto,chrome_auto" {
		t.Fatalf("transport.utls-profile = %#v", values.Transport.UTLSProfile)
	}
	if values.Scan.ResolveTo == nil || *values.Scan.ResolveTo != "203.0.113.10,203.0.113.11" {
		t.Fatalf("scan.resolve-to = %#v", values.Scan.ResolveTo)
	}
	if values.Scan.FrontingSNI == nil || *values.Scan.FrontingSNI != "www.google.com,google.com" {
		t.Fatalf("scan.fronting-sni = %#v", values.Scan.FrontingSNI)
	}
}

func TestSaveScanReportYAMLOnlyIPUsesDialAccessibleIPs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.yml")
	report := scanReport{
		Mode:              string(scanModeOnlyIP),
		DialAccessibleIPs: []string{"198.51.100.20", "198.51.100.21"},
		UTLSProfiles:      []string{"chrome_auto"},
	}
	if err := saveScanReportYAML(path, report); err != nil {
		t.Fatalf("saveScanReportYAML() error = %v", err)
	}
	values := mustLoadSavedYAMLConfig(t, path)
	if values.Scan.ResolveTo == nil || *values.Scan.ResolveTo != "198.51.100.20,198.51.100.21" {
		t.Fatalf("scan.resolve-to = %#v", values.Scan.ResolveTo)
	}
	if values.Transport.ResolveTo != nil {
		t.Fatalf("transport.resolve-to = %#v, want nil", values.Transport.ResolveTo)
	}
	if values.Transport.FrontingEnable == nil || *values.Transport.FrontingEnable {
		t.Fatalf("transport.fronting-enable = %#v, want false", values.Transport.FrontingEnable)
	}
	if values.Transport.UTLSProfile == nil || *values.Transport.UTLSProfile != "chrome_auto" {
		t.Fatalf("transport.utls-profile = %#v", values.Transport.UTLSProfile)
	}
}

func mustLoadSavedYAMLConfig(t *testing.T, path string) yamlCLIConfig {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", path, err)
	}
	defer file.Close()
	values, err := parseYAMLCLIConfig(file)
	if err != nil {
		t.Fatalf("parseYAMLCLIConfig(%s) error = %v", path, err)
	}
	return values
}
