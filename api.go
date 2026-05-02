package gdrivedl

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const Version = "2.1.0"

type Request struct {
	URL                   string
	URLs                  []string
	APIKey                string
	CompletionReport      bool
	DryRun                bool
	EventObserver         EventObserver
	Extension             string
	ExitReport            bool
	Filename              string
	JSONOutput            bool
	MimeTypes             []string
	NotCreateTopDirectory bool
	Overwrite             bool
	ResumableDownload     string
	ShowFileInfo          bool
	Skip                  bool
	SkipError             bool
	WorkDir               string

	Concurrency          int
	DumpRequest          bool
	DumpResponse         bool
	FrontingEnable       bool
	FrontingSNI          string
	FrontingTarget       string
	ForceHTTP1           bool
	PreferHTTP2          bool
	Proxy                string
	RetryCount           int
	RequestDelay         time.Duration
	ResolveTo            string
	Scan                 bool
	ShareHTTP2Conn       bool
	ShowTerminalProgress bool
	Timeout              time.Duration
	UTLSProfile          string
	Verbosity            int
}

type TaskSnapshot struct {
	Name       string
	Source     string
	State      string
	Status     string
	Detail     string
	Total      int64
	Downloaded int64
	UpdatedAt  time.Time
}

type ProgressSnapshot struct {
	Tasks               []TaskSnapshot
	SummaryLine         string
	TotalDownloaded     int64
	KnownDownloaded     int64
	KnownTotal          int64
	SpeedBytesPerSecond float64
}

type ProgressObserver func(ProgressSnapshot)

type ConnectivityReport struct {
	ProbeURL        string
	Status          string
	StatusCode      int
	Protocol        string
	Proxy           string
	RetryCount      int
	FrontingEnabled bool
	FrontingTargets []string
	FrontingSNI     string
	ResolveToAddrs  []string
	UTLSProfiles    []string
}

func Download(ctx context.Context, req Request) error {
	return DownloadWithObserver(ctx, req, nil)
}

func TestConnectivity(ctx context.Context, req Request) (ConnectivityReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	transport, err := req.newTransportConfig()
	if err != nil {
		return ConnectivityReport{}, err
	}
	runtime := (*downloadRuntime)(nil)
	if req.JSONOutput || req.EventObserver != nil {
		runtime = newObservedDownloadRuntime(false, false, req.JSONOutput, nil, req.EventObserver)
	}
	return testConnectivityWithTransport(ctx, transport, runtime, scanProbeURL)
}

func DownloadWithObserver(ctx context.Context, req Request, observer ProgressObserver) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p, urls, err := req.newPara(ctx, observer)
	if err != nil {
		return err
	}
	if p.Runtime != nil {
		p.Runtime.start()
		defer p.Runtime.finish()
	}
	if len(urls) > 0 {
		return downloadURLList(p, urls)
	}
	return p.download(req.URL)
}

func (req Request) newPara(ctx context.Context, observer ProgressObserver) (*para, []string, error) {
	if req.URL != "" && len(req.URLs) > 0 {
		return nil, nil, fmt.Errorf("please use either Request.URL or Request.URLs")
	}
	if req.URL == "" && len(req.URLs) == 0 {
		return nil, nil, fmt.Errorf("a URL is required")
	}
	workDir := req.WorkDir
	if workDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		workDir = cwd
	}
	transport, err := req.newTransportConfig()
	if err != nil {
		return nil, nil, err
	}
	concurrency := req.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	p := &para{
		APIKey:                req.APIKey,
		CompletionReport:      req.CompletionReport,
		Context:               ctx,
		Disp:                  !req.ShowTerminalProgress,
		DryRun:                req.DryRun,
		EnableProgress:        req.ShowTerminalProgress,
		Ext:                   req.Extension,
		Filename:              req.Filename,
		InputtedMimeType:      append([]string(nil), req.MimeTypes...),
		JSONOutput:            req.JSONOutput,
		MaxConcurrency:        concurrency,
		Notcreatetopdirectory: req.NotCreateTopDirectory,
		OverWrite:             req.Overwrite,
		Resumabledownload:     req.ResumableDownload,
		Scan:                  req.Scan,
		ShowFileInf:           req.ShowFileInfo,
		Skip:                  req.Skip,
		SkipError:             req.SkipError,
		TransportConfig:       transport,
		URL:                   req.URL,
		WorkDir:               workDir,
		ExitReport:            req.ExitReport,
	}
	if observer != nil || req.ShowTerminalProgress || req.ExitReport || concurrency > 1 || req.EventObserver != nil || req.JSONOutput {
		p.Runtime = newObservedDownloadRuntime(req.ShowTerminalProgress, req.ExitReport, req.JSONOutput, observer, req.EventObserver)
	}
	return p, append([]string(nil), req.URLs...), nil
}

func (req Request) newTransportConfig() (transportConfig, error) {
	proxyURL, err := parseProxyURL(req.Proxy)
	if err != nil {
		return transportConfig{}, err
	}
	profiles, err := parseUTLSProfileList(req.UTLSProfile)
	if err != nil {
		return transportConfig{}, err
	}
	profileName := profiles[0].name
	profileID := profiles[0].id
	resolveToAddrs, err := parseResolveToList(req.ResolveTo)
	if err != nil {
		return transportConfig{}, err
	}
	resolveTo := ""
	if len(resolveToAddrs) > 0 {
		resolveTo = resolveToAddrs[0]
	}
	fronting := frontingConfig{}
	if req.FrontingEnable {
		targets := splitCommaSeparated([]string{req.FrontingTarget})
		if len(targets) == 0 {
			return transportConfig{}, fmt.Errorf("FrontingTarget is required when FrontingEnable is true")
		}
		normalizedTargets := make([]string, 0, len(targets))
		for _, rawTarget := range targets {
			target, err := parseHostnameValue(rawTarget, "FrontingTarget")
			if err != nil {
				return transportConfig{}, err
			}
			normalizedTargets = append(normalizedTargets, target)
		}
		target := normalizedTargets[0]
		sni := req.FrontingSNI
		explicitSNI := strings.TrimSpace(sni) != ""
		if sni == "" {
			sni = target
		} else {
			sni, err = parseHostnameValue(sni, "FrontingSNI")
			if err != nil {
				return transportConfig{}, err
			}
		}
		fronting = frontingConfig{Enable: true, Target: target, Targets: normalizedTargets, SNI: sni, explicitSNI: explicitSNI}
	} else if strings.TrimSpace(req.FrontingTarget) != "" || strings.TrimSpace(req.FrontingSNI) != "" {
		return transportConfig{}, fmt.Errorf("FrontingTarget and FrontingSNI require FrontingEnable")
	}
	if req.Timeout < 0 {
		return transportConfig{}, fmt.Errorf("Timeout must be greater than or equal to 0")
	}
	if req.RetryCount < 0 {
		return transportConfig{}, fmt.Errorf("RetryCount must be greater than or equal to 0")
	}
	if req.RequestDelay < 0 {
		return transportConfig{}, fmt.Errorf("RequestDelay must be greater than or equal to 0")
	}
	if req.Verbosity < 0 {
		return transportConfig{}, fmt.Errorf("Verbosity must be greater than or equal to 0")
	}
	if req.PreferHTTP2 && req.ForceHTTP1 {
		return transportConfig{}, fmt.Errorf("PreferHTTP2 cannot be used with ForceHTTP1")
	}
	if req.ShareHTTP2Conn && req.ForceHTTP1 {
		return transportConfig{}, fmt.Errorf("ShareHTTP2Conn cannot be used with ForceHTTP1")
	}
	return transportConfig{
		DumpRequest:     req.DumpRequest,
		DumpResponse:    req.DumpResponse,
		Fronting:        fronting,
		ForceHTTP1:      req.ForceHTTP1,
		PreferHTTP2:     req.PreferHTTP2,
		Proxy:           proxyURL,
		RetryCount:      req.RetryCount,
		RequestDelay:    req.RequestDelay,
		ResolveTo:       resolveTo,
		ResolveToAddrs:  resolveToAddrs,
		Scan:            req.Scan,
		ShareHTTP2Conn:  req.ShareHTTP2Conn,
		Timeout:         req.Timeout,
		UTLSProfile:     profileID,
		UTLSProfileName: profileName,
		UTLSProfiles:    profiles,
		Verbosity:       req.Verbosity,
		sharedState:     newTransportSharedState(),
	}, nil
}

func (p *para) requestContext() context.Context {
	if p != nil && p.Context != nil {
		return p.Context
	}
	return context.Background()
}

func (p *para) contextErr() error {
	return p.requestContext().Err()
}
