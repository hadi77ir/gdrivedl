package gdrivedl

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportConfigRetriesRetryableHTTPStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := (transportConfig{RetryCount: 1}).newHTTPClient(nil)
	if err != nil {
		t.Fatalf("newHTTPClient() error = %v", err)
	}
	res, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNoContent)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestConnectivityWithTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate_204" {
			t.Fatalf("path = %q, want /generate_204", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	report, err := testConnectivityWithTransport(context.Background(), transportConfig{RetryCount: 2}, nil, server.URL+"/generate_204")
	if err != nil {
		t.Fatalf("testConnectivityWithTransport() error = %v", err)
	}
	if report.ProbeURL != server.URL+"/generate_204" {
		t.Fatalf("ProbeURL = %q", report.ProbeURL)
	}
	if report.StatusCode != http.StatusNoContent {
		t.Fatalf("StatusCode = %d, want %d", report.StatusCode, http.StatusNoContent)
	}
	if report.Status != "204 No Content" {
		t.Fatalf("Status = %q, want 204 No Content", report.Status)
	}
	if report.RetryCount != 2 {
		t.Fatalf("RetryCount = %d, want 2", report.RetryCount)
	}
	if report.Protocol == "" {
		t.Fatal("Protocol should not be empty")
	}
}
