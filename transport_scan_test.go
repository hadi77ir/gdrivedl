package gdrivedl

import (
	"net/http"
	"testing"
)

func TestTransportBuildRequestPlanRoundRobin(t *testing.T) {
	transport := transportConfig{
		Fronting: frontingConfig{
			Enable:  true,
			Target:  "front-a.example.com",
			Targets: []string{"front-a.example.com", "front-b.example.com"},
		},
		ResolveTo:      "203.0.113.10",
		ResolveToAddrs: []string{"203.0.113.10", "203.0.113.11"},
		sharedState:    newTransportSharedState(),
	}
	req := &http.Request{URL: mustParseURL(t, "https://logical.example/path")}

	plan1 := transport.buildRequestPlan(req)
	if plan1.ConnectAddress != "203.0.113.10:443" || plan1.ServerName != "front-a.example.com" {
		t.Fatalf("plan1 = %#v", plan1)
	}
	plan2 := transport.buildRequestPlan(req)
	if plan2.ConnectAddress != "203.0.113.11:443" || plan2.ServerName != "front-b.example.com" {
		t.Fatalf("plan2 = %#v", plan2)
	}
	plan3 := transport.buildRequestPlan(req)
	if plan3.ConnectAddress != "203.0.113.10:443" || plan3.ServerName != "front-a.example.com" {
		t.Fatalf("plan3 = %#v", plan3)
	}
}

func TestTransportScanAttemptsOrder(t *testing.T) {
	transport := transportConfig{
		Fronting: frontingConfig{
			Enable:  true,
			Target:  "front-a.example.com",
			Targets: []string{"front-a.example.com", "front-b.example.com"},
		},
		ResolveTo:       "203.0.113.10",
		ResolveToAddrs:  []string{"203.0.113.10"},
		UTLSProfileName: "chrome_auto",
		UTLSProfile:     supportedUTLSProfiles["chrome_auto"],
		sharedState:     newTransportSharedState(),
	}
	req := &http.Request{URL: mustParseURL(t, "https://logical.example/path")}
	attempts := transport.scanTransportAttempts(req)
	altCount := len(transport.scanAlternativeUTLSProfiles())
	if len(attempts) < (1+altCount)*3 {
		t.Fatalf("unexpected attempt count = %d", len(attempts))
	}
	if attempts[0].frontingEnabled || attempts[0].profileName != "chrome_auto" {
		t.Fatalf("first attempt = %#v", attempts[0])
	}
	if attempts[1].frontingEnabled || attempts[1].profileName == "chrome_auto" {
		t.Fatalf("second attempt = %#v", attempts[1])
	}
	frontingBase := 1 + altCount
	if !attempts[frontingBase].frontingEnabled || attempts[frontingBase].frontingTarget != "front-a.example.com" || attempts[frontingBase].profileName != "chrome_auto" {
		t.Fatalf("fronting base attempt = %#v", attempts[frontingBase])
	}
	secondTargetBase := frontingBase + 1 + altCount
	if !attempts[secondTargetBase].frontingEnabled || attempts[secondTargetBase].frontingTarget != "front-b.example.com" || attempts[secondTargetBase].profileName != "chrome_auto" {
		t.Fatalf("second target base attempt = %#v", attempts[secondTargetBase])
	}
	if attempts[0].resolveTo != "203.0.113.10" || attempts[frontingBase].resolveTo != "203.0.113.10" {
		t.Fatalf("resolve-to assignment mismatch: %#v %#v", attempts[0], attempts[frontingBase])
	}
}
