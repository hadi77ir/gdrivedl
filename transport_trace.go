package gdrivedl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
)

type requestTraceContextKey struct{}

type requestTrace struct {
	runtime *downloadRuntime
	task    *downloadTask
}

func withRequestTrace(req *http.Request, runtime *downloadRuntime, task *downloadTask) *http.Request {
	if req == nil || (runtime == nil && task == nil) {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), requestTraceContextKey{}, &requestTrace{runtime: runtime, task: task}))
}

func requestTraceFromContext(ctx context.Context) *requestTrace {
	trace, _ := ctx.Value(requestTraceContextKey{}).(*requestTrace)
	return trace
}

func (cfg transportConfig) emitTrace(req *http.Request, message string) {
	trace := requestTraceFromContext(req.Context())
	label := req.URL.String()
	if trace != nil && trace.task != nil {
		snapshot := trace.task.snapshot()
		label = firstNonEmpty(snapshot.Name, snapshot.Source, label)
	}
	line := fmt.Sprintf("[http] %s | %s", label, message)
	if trace != nil && trace.runtime != nil {
		trace.runtime.printf("%s\n", line)
		return
	}
	fmt.Printf("%s\n", line)
}

func (cfg transportConfig) setTaskState(req *http.Request, state string) {
	trace := requestTraceFromContext(req.Context())
	if trace != nil && trace.task != nil {
		trace.task.SetState(state)
	}
}

func (cfg transportConfig) logStage(req *http.Request, state string, format string, args ...interface{}) {
	cfg.setTaskState(req, state)
	if cfg.Verbosity <= 0 {
		return
	}
	extra := strings.TrimSpace(fmt.Sprintf(format, args...))
	message := fmt.Sprintf("state=%s method=%s url=%s", state, req.Method, req.URL.String())
	if extra != "" {
		message += " | " + extra
	}
	cfg.emitTrace(req, message)
}

func (cfg transportConfig) logDetail(req *http.Request, minVerbosity int, format string, args ...interface{}) {
	if cfg.Verbosity < minVerbosity {
		return
	}
	cfg.emitTrace(req, fmt.Sprintf(format, args...))
}

func (cfg transportConfig) dumpRequest(req *http.Request) {
	if !cfg.DumpRequest {
		return
	}
	dump, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		cfg.emitTrace(req, fmt.Sprintf("request dump error: %v", err))
		return
	}
	cfg.emitTrace(req, "request dump:\n"+string(dump))
}

func (cfg transportConfig) dumpResponse(req *http.Request, res *http.Response) {
	if !cfg.DumpResponse || res == nil {
		return
	}
	dump, err := httputil.DumpResponse(res, false)
	if err != nil {
		cfg.emitTrace(req, fmt.Sprintf("response dump error: %v", err))
		return
	}
	cfg.emitTrace(req, "response dump:\n"+string(dump))
}
