package gdrivedl

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestTransportTraceHelpers(t *testing.T) {
	task := &downloadTask{name: "file.bin", status: taskPending}
	req, err := http.NewRequest("GET", "https://example.com/file", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req = withRequestTrace(req, nil, task)
	trace := requestTraceFromContext(req.Context())
	if trace == nil || trace.task != task {
		t.Fatal("request trace was not attached")
	}

	cfg := transportConfig{Verbosity: 1, DumpRequest: true, DumpResponse: true}
	output := captureStdout(t, func() {
		cfg.logStage(req, "resolving", "address=%s", "example.com:443")
		cfg.dumpRequest(req)
		cfg.dumpResponse(req, &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": []string{"text/plain"}},
			Body:          io.NopCloser(strings.NewReader("payload")),
			ContentLength: int64(len("payload")),
			Request:       req,
		})
	})

	if task.snapshot().State != "resolving" {
		t.Fatalf("task state = %q, want resolving", task.snapshot().State)
	}
	for _, want := range []string{"state=resolving", "request dump:", "GET /file HTTP/1.1", "response dump:", "200 OK"} {
		if !strings.Contains(output, want) {
			t.Fatalf("trace output %q missing %q", output, want)
		}
	}
}

func TestTransportConfigNewHTTPClientTimeout(t *testing.T) {
	client, err := (transportConfig{Timeout: 12}).newHTTPClient(nil)
	if err != nil {
		t.Fatalf("newHTTPClient() error = %v", err)
	}
	if client.Timeout != 12 {
		t.Fatalf("client timeout = %v, want 12ns", client.Timeout)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy() error = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return buf.String()
}
