package gdrivedl

import (
	"fmt"
	"net/http"
	"strings"

	utls "github.com/refraction-networking/utls"
)

type transportAttempt struct {
	frontingEnabled bool
	frontingTarget  string
	profileName     string
	profileID       utls.ClientHelloID
	resolveTo       string
}

func (cfg transportConfig) withScanAttempt(attempt transportAttempt) transportConfig {
	clone := cfg
	clone.Scan = false
	clone.disableUTLSFallback = true
	clone.Fronting.Enable = attempt.frontingEnabled
	clone.Fronting.Target = attempt.frontingTarget
	clone.Fronting.Targets = nil
	clone.ResolveTo = attempt.resolveTo
	clone.ResolveToAddrs = nil
	clone.UTLSProfileName = attempt.profileName
	clone.UTLSProfile = attempt.profileID
	clone.UTLSProfiles = []utlsProfileOption{{name: attempt.profileName, id: attempt.profileID}}
	return clone
}

func (cfg transportConfig) frontingServerName(target string) string {
	if cfg.Fronting.explicitSNI {
		return cfg.Fronting.SNI
	}
	if target != "" {
		return target
	}
	return cfg.Fronting.SNI
}

func (cfg transportConfig) frontingTargets() []string {
	if len(cfg.Fronting.Targets) > 0 {
		return append([]string(nil), cfg.Fronting.Targets...)
	}
	if cfg.Fronting.Target != "" {
		return []string{cfg.Fronting.Target}
	}
	return nil
}

func (cfg transportConfig) resolveTargets() []string {
	if len(cfg.ResolveToAddrs) > 0 {
		return append([]string(nil), cfg.ResolveToAddrs...)
	}
	if cfg.ResolveTo != "" {
		return []string{cfg.ResolveTo}
	}
	return nil
}

func rotateStrings(values []string, start int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for index := 0; index < len(values); index++ {
		out = append(out, values[(start+index)%len(values)])
	}
	return out
}

func (cfg transportConfig) frontingTargetsInRoundRobinOrder() []string {
	targets := cfg.frontingTargets()
	if len(targets) <= 1 {
		return targets
	}
	state := cfg.state()
	state.roundRobinMu.Lock()
	start := state.frontingTargetCursor % len(targets)
	state.frontingTargetCursor = (state.frontingTargetCursor + 1) % len(targets)
	state.roundRobinMu.Unlock()
	return rotateStrings(targets, start)
}

func (cfg transportConfig) resolveTargetsInRoundRobinOrder() []string {
	targets := cfg.resolveTargets()
	if len(targets) <= 1 {
		return targets
	}
	state := cfg.state()
	state.roundRobinMu.Lock()
	start := state.resolveToCursor % len(targets)
	state.resolveToCursor = (state.resolveToCursor + 1) % len(targets)
	state.roundRobinMu.Unlock()
	return rotateStrings(targets, start)
}

func (cfg transportConfig) selectedFrontingTarget() string {
	targets := cfg.frontingTargetsInRoundRobinOrder()
	if len(targets) == 0 {
		return ""
	}
	return targets[0]
}

func (cfg transportConfig) selectedResolveTo() string {
	targets := cfg.resolveTargetsInRoundRobinOrder()
	if len(targets) == 0 {
		return ""
	}
	return targets[0]
}

func (cfg transportConfig) orderedScanProfileNames() []string {
	ordered := []string{
		"chrome_auto",
		"firefox_auto",
		"edge_auto",
		"safari_auto",
		"ios_auto",
		"360_auto",
		"qq_auto",
		"randomized_alpn",
		"randomized_no_alpn",
		"randomized",
	}
	configured := cfg.configuredUTLSProfiles()
	if len(configured) == 0 {
		return ordered
	}
	result := make([]string, 0, len(ordered))
	seen := map[string]struct{}{}
	for _, profile := range configured {
		if _, ok := seen[profile.name]; ok {
			continue
		}
		seen[profile.name] = struct{}{}
		result = append(result, profile.name)
	}
	for _, name := range ordered {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func (cfg transportConfig) scanAlternativeUTLSProfiles() []utlsProfileOption {
	ordered := cfg.orderedScanProfileNames()
	profiles := make([]utlsProfileOption, 0, len(ordered))
	for _, name := range ordered {
		if name == cfg.UTLSProfileName {
			continue
		}
		if id, ok := supportedUTLSProfiles[name]; ok {
			profiles = append(profiles, utlsProfileOption{name: name, id: id})
		}
	}
	return profiles
}

func (cfg transportConfig) scanTransportAttempts(*http.Request) []transportAttempt {
	resolveTargets := cfg.resolveTargetsInRoundRobinOrder()
	if len(resolveTargets) == 0 {
		resolveTargets = []string{""}
	}
	altProfiles := cfg.scanAlternativeUTLSProfiles()
	configured := cfg.configuredUTLSProfiles()
	base := configured[0]
	attempts := make([]transportAttempt, 0, len(resolveTargets)*(1+len(altProfiles)))
	for _, resolveTo := range resolveTargets {
		attempts = append(attempts, transportAttempt{profileName: base.name, profileID: base.id, resolveTo: resolveTo})
	}
	for _, profile := range altProfiles {
		for _, resolveTo := range resolveTargets {
			attempts = append(attempts, transportAttempt{profileName: profile.name, profileID: profile.id, resolveTo: resolveTo})
		}
	}
	if !cfg.Fronting.Enable {
		return attempts
	}
	for _, target := range cfg.frontingTargetsInRoundRobinOrder() {
		for _, resolveTo := range resolveTargets {
			attempts = append(attempts, transportAttempt{frontingEnabled: true, frontingTarget: target, profileName: base.name, profileID: base.id, resolveTo: resolveTo})
		}
		for _, profile := range altProfiles {
			for _, resolveTo := range resolveTargets {
				attempts = append(attempts, transportAttempt{frontingEnabled: true, frontingTarget: target, profileName: profile.name, profileID: profile.id, resolveTo: resolveTo})
			}
		}
	}
	return attempts
}

func (attempt transportAttempt) description() string {
	parts := []string{fmt.Sprintf("profile=%s", attempt.profileName)}
	if attempt.frontingEnabled {
		parts = append(parts, fmt.Sprintf("mode=fronting target=%s", attempt.frontingTarget))
	} else {
		parts = append(parts, "mode=direct")
	}
	if attempt.resolveTo != "" {
		parts = append(parts, fmt.Sprintf("resolve-to=%s", attempt.resolveTo))
	}
	return strings.Join(parts, " ")
}

func (rt *smartRoundTripper) roundTripScanned(req *http.Request) (*http.Response, error) {
	attempts := rt.config.scanTransportAttempts(req)
	var lastErr error
	for index, attempt := range attempts {
		attemptRT := &smartRoundTripper{base: rt.base, config: rt.config.withScanAttempt(attempt)}
		attemptReq := req.Clone(req.Context())
		attemptRT.config.logDetail(attemptReq, 1, "scan attempt=%d %s", index+1, attempt.description())
		res, err := attemptRT.roundTripOnce(attemptReq)
		if err == nil {
			attemptRT.config.emitTrace(attemptReq, "scan selected: "+attempt.description())
			return res, nil
		}
		lastErr = err
		attemptRT.config.logDetail(attemptReq, 1, "scan attempt failed: %s error=%v", attempt.description(), err)
	}
	if lastErr != nil {
		rt.config.emitTrace(req, fmt.Sprintf("scan exhausted %d attempts: %v", len(attempts), lastErr))
	}
	return nil, lastErr
}
