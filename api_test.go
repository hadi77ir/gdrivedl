package gdrivedl

import (
	"reflect"
	"testing"
)

func TestRequestNewTransportConfigSupportsScanAndLists(t *testing.T) {
	cfg, err := (Request{
		FrontingEnable: true,
		FrontingTarget: "front-a.example.com,front-b.example.com",
		ResolveTo:      "203.0.113.10,203.0.113.11",
		Scan:           true,
		UTLSProfile:    "firefox_auto,chrome_auto",
	}).newTransportConfig()
	if err != nil {
		t.Fatalf("newTransportConfig() error = %v", err)
	}
	if !cfg.Scan {
		t.Fatal("scan should be enabled")
	}
	if cfg.ResolveTo != "203.0.113.10" {
		t.Fatalf("ResolveTo = %q, want 203.0.113.10", cfg.ResolveTo)
	}
	if !reflect.DeepEqual(cfg.ResolveToAddrs, []string{"203.0.113.10", "203.0.113.11"}) {
		t.Fatalf("ResolveToAddrs = %#v", cfg.ResolveToAddrs)
	}
	if !cfg.Fronting.Enable {
		t.Fatal("fronting should be enabled")
	}
	if cfg.Fronting.Target != "front-a.example.com" {
		t.Fatalf("Fronting.Target = %q, want front-a.example.com", cfg.Fronting.Target)
	}
	if cfg.Fronting.SNI != "front-a.example.com" {
		t.Fatalf("Fronting.SNI = %q, want front-a.example.com", cfg.Fronting.SNI)
	}
	if !reflect.DeepEqual(cfg.Fronting.Targets, []string{"front-a.example.com", "front-b.example.com"}) {
		t.Fatalf("Fronting.Targets = %#v", cfg.Fronting.Targets)
	}
	if cfg.UTLSProfileName != "firefox_auto" {
		t.Fatalf("UTLSProfileName = %q, want firefox_auto", cfg.UTLSProfileName)
	}
	if got := len(cfg.UTLSProfiles); got != 2 {
		t.Fatalf("UTLSProfiles len = %d, want 2", got)
	}
	if cfg.RetryCount != 0 {
		t.Fatalf("RetryCount = %d, want 0", cfg.RetryCount)
	}
}

func TestRequestNewTransportConfigSupportsRetryCount(t *testing.T) {
	cfg, err := (Request{RetryCount: 3}).newTransportConfig()
	if err != nil {
		t.Fatalf("newTransportConfig() error = %v", err)
	}
	if cfg.RetryCount != 3 {
		t.Fatalf("RetryCount = %d, want 3", cfg.RetryCount)
	}
}

func TestRequestNewTransportConfigRejectsNegativeRetryCount(t *testing.T) {
	_, err := (Request{RetryCount: -1}).newTransportConfig()
	if err == nil || err.Error() != "RetryCount must be greater than or equal to 0" {
		t.Fatalf("newTransportConfig() error = %v", err)
	}
}

func TestRequestNewTransportConfigRejectsFrontingFieldsWithoutEnable(t *testing.T) {
	_, err := (Request{FrontingTarget: "front.example.com"}).newTransportConfig()
	if err == nil || err.Error() != "FrontingTarget and FrontingSNI require FrontingEnable" {
		t.Fatalf("newTransportConfig() error = %v", err)
	}
}

func TestRequestNewParaCreatesRuntimeForEvents(t *testing.T) {
	p, _, err := (Request{
		URL:           "https://drive.google.com/file/d/abc/view",
		JSONOutput:    true,
		EventObserver: func(Event) {},
	}).newPara(nil, nil)
	if err != nil {
		t.Fatalf("newPara() error = %v", err)
	}
	if p.Runtime == nil {
		t.Fatal("runtime should be created for structured events")
	}
}
