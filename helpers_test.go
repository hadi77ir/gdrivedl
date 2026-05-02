package gdrivedl

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	drive "google.golang.org/api/drive/v3"
)

func TestMainHelperFunctions(t *testing.T) {
	t.Run("clone", func(t *testing.T) {
		p := &para{InputtedMimeType: []string{"a", "b"}, Task: &downloadTask{}, Filename: "file"}
		clone := p.clone()
		clone.InputtedMimeType[0] = "changed"
		if p.InputtedMimeType[0] != "a" {
			t.Fatal("clone should copy InputtedMimeType slice")
		}
		if clone.Task != nil {
			t.Fatal("clone should reset task")
		}
	})

	t.Run("shouldUseResumableFlow", func(t *testing.T) {
		p := &para{APIKey: "key", Resumabledownload: "10m", Kind: "file"}
		if !p.shouldUseResumableFlow() {
			t.Fatal("resumable flow should be enabled")
		}
		p.Kind = "document"
		if p.shouldUseResumableFlow() {
			t.Fatal("resumable flow should be disabled for non-file kind")
		}
	})

	t.Run("getFilename from header", func(t *testing.T) {
		p := &para{}
		res := &http.Response{
			Header: http.Header{"Content-Disposition": []string{"attachment; filename=\"report.txt\""}},
			Body:   io.NopCloser(strings.NewReader("unused")),
		}
		if err := p.getFilename(res); err != nil {
			t.Fatalf("getFilename() error = %v", err)
		}
		if p.Filename != "report.txt" {
			t.Fatalf("filename = %q, want report.txt", p.Filename)
		}
	})

	t.Run("getFilename from html", func(t *testing.T) {
		p := &para{ID: "abc123"}
		res := &http.Response{
			Header: http.Header{},
			Body:   io.NopCloser(strings.NewReader("<span class=\"uc-name-size\"><a href=\"#\">archive.zip</a></span>")),
		}
		if err := p.getFilename(res); err != nil {
			t.Fatalf("getFilename() error = %v", err)
		}
		if p.Filename != "archive.zip" {
			t.Fatalf("filename = %q, want archive.zip", p.Filename)
		}
	})

	t.Run("getURLFromHTML", func(t *testing.T) {
		p := &para{}
		res := &http.Response{Body: io.NopCloser(strings.NewReader("<html><body>" +
			"<form id=\"download-form\" action=\"https://example.com/download\">" +
			"<input type=\"hidden\" name=\"confirm\" value=\"t\">" +
			"<input type=\"hidden\" name=\"id\" value=\"abc123\">" +
			"</form></body></html>"))}
		if err := p.getURLFromHTML(res); err != nil {
			t.Fatalf("getURLFromHTML() error = %v", err)
		}
		if !strings.Contains(p.URLForLargeFile, "https://example.com/download") || !strings.Contains(p.URLForLargeFile, "confirm=t") || !strings.Contains(p.URLForLargeFile, "id=abc123") {
			t.Fatalf("URLForLargeFile = %q", p.URLForLargeFile)
		}
	})

	t.Run("saveFile completion report", func(t *testing.T) {
		dir := t.TempDir()
		res := &http.Response{
			Header: http.Header{"Content-Disposition": []string{"attachment; filename=\"report.txt\""}, "Content-Type": []string{"text/plain"}},
			Body:   io.NopCloser(strings.NewReader("hello")),
		}
		quiet := &para{Disp: true, WorkDir: dir, Kind: "file"}
		output := captureStdout(t, func() {
			if err := quiet.saveFile(res); err != nil {
				t.Fatalf("saveFile() error = %v", err)
			}
		})
		if output != "" {
			t.Fatalf("saveFile() default output = %q, want empty", output)
		}

		res = &http.Response{
			Header: http.Header{"Content-Disposition": []string{"attachment; filename=\"report2.txt\""}, "Content-Type": []string{"text/plain"}},
			Body:   io.NopCloser(strings.NewReader("world")),
		}
		reporting := &para{CompletionReport: true, Disp: true, WorkDir: dir, Kind: "file"}
		output = captureStdout(t, func() {
			if err := reporting.saveFile(res); err != nil {
				t.Fatalf("saveFile() completion report error = %v", err)
			}
		})
		for _, want := range []string{"Completed: report2.txt", "Type: file", "MimeType: text/plain", "FileSize: 5"} {
			if !strings.Contains(output, want) {
				t.Fatalf("saveFile() output %q missing %q", output, want)
			}
		}

		res = &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			ContentLength: 5,
			Header:        http.Header{"Content-Disposition": []string{"attachment; filename=\"report3.txt\""}, "Content-Type": []string{"text/plain"}},
			Body:          io.NopCloser(strings.NewReader("again")),
		}
		task := &downloadTask{status: taskPending, updatedAt: time.Now()}
		dryRun := &para{DryRun: true, Disp: true, WorkDir: dir, Kind: "file", Task: task}
		output = captureStdout(t, func() {
			if err := dryRun.saveFile(res); err != nil {
				t.Fatalf("saveFile() dry run error = %v", err)
			}
		})
		if output != "" {
			t.Fatalf("dry-run saveFile() output = %q, want empty", output)
		}
		if _, err := os.Stat(filepath.Join(dir, "report3.txt")); !os.IsNotExist(err) {
			t.Fatalf("dry-run should not create a file, stat err = %v", err)
		}
		snapshot := task.snapshot()
		if snapshot.Status != taskCompleted || snapshot.Detail != "dry run" {
			t.Fatalf("dry-run task snapshot = %#v", snapshot)
		}
	})

	t.Run("detectGoogleLoginRequirement by request URL", func(t *testing.T) {
		p := &para{}
		res := &http.Response{
			Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:   io.NopCloser(strings.NewReader("<html><body>Sign in</body></html>")),
			Request: &http.Request{
				URL: mustParseURL(t, "https://accounts.google.com/ServiceLogin?continue=https://drive.google.com/"),
			},
		}
		err := p.detectGoogleLoginRequirement(res)
		if err == nil || !strings.Contains(err.Error(), "Google login/sign-in is required") {
			t.Fatalf("detectGoogleLoginRequirement() error = %v", err)
		}
	})

	t.Run("detectGoogleLoginRequirement by response body", func(t *testing.T) {
		p := &para{}
		body := "<html><head><title>Sign in - Google Accounts</title></head><body><form action=\"https://accounts.google.com/ServiceLogin\"><input id=\"identifierId\"></form></body></html>"
		res := &http.Response{
			Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:   io.NopCloser(strings.NewReader(body)),
			Request: &http.Request{
				URL: mustParseURL(t, "https://drive.google.com/uc?export=download&id=abc123"),
			},
		}
		err := p.detectGoogleLoginRequirement(res)
		if err == nil || !strings.Contains(err.Error(), "public links") {
			t.Fatalf("detectGoogleLoginRequirement() error = %v", err)
		}
		readBack, readErr := io.ReadAll(res.Body)
		if readErr != nil {
			t.Fatalf("ReadAll() error = %v", readErr)
		}
		if string(readBack) != body {
			t.Fatalf("body was not restored: %q", string(readBack))
		}
	})

	t.Run("detectGoogleLoginRequirement ignores public confirmation page", func(t *testing.T) {
		p := &para{}
		res := &http.Response{
			Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body: io.NopCloser(strings.NewReader("<html><body>" +
				"<form id=\"download-form\" action=\"https://example.com/download\">" +
				"<input type=\"hidden\" name=\"confirm\" value=\"t\">" +
				"</form></body></html>")),
			Request: &http.Request{
				URL: mustParseURL(t, "https://drive.google.com/uc?export=download&id=abc123"),
			},
		}
		if err := p.detectGoogleLoginRequirement(res); err != nil {
			t.Fatalf("detectGoogleLoginRequirement() error = %v", err)
		}
		if err := p.getURLFromHTML(res); err != nil {
			t.Fatalf("getURLFromHTML() after detection error = %v", err)
		}
	})

	t.Run("checkURL", func(t *testing.T) {
		cases := []struct {
			name    string
			url     string
			ext     string
			wantURL string
			wantErr string
		}{
			{
				name:    "shared file",
				url:     "https://drive.google.com/file/d/abc123/view?usp=sharing",
				wantURL: anyurl + "&id=abc123",
			},
			{
				name:    "document ms",
				url:     "https://docs.google.com/document/d/doc123/edit",
				ext:     "ms",
				wantURL: docutl + "document/d/doc123/export?format=docx",
			},
			{
				name:    "uc url",
				url:     "https://drive.google.com/uc?export=download&id=xyz789",
				wantURL: anyurl + "&id=xyz789",
			},
			{
				name:    "bad url",
				url:     "https://example.com/nope",
				wantErr: "URL is wrong",
			},
		}
		for _, tc := range cases {
			p := &para{Ext: tc.ext}
			err := p.checkURL(tc.url)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("%s error = %v, want substring %q", tc.name, err, tc.wantErr)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s error = %v", tc.name, err)
			}
			if p.URL != tc.wantURL {
				t.Fatalf("%s URL = %q, want %q", tc.name, p.URL, tc.wantURL)
			}
		}
	})
}

func TestConfigHelperFunctions(t *testing.T) {
	if got := splitCommaSeparated([]string{" a,b ", "c", "", "d, e"}); strings.Join(got, ",") != "a,b,c,d,e" {
		t.Fatalf("splitCommaSeparated() = %#v", got)
	}
	if got := normalizeUTLSProfileName("Firefox-Auto"); got != "firefox_auto" {
		t.Fatalf("normalizeUTLSProfileName() = %q", got)
	}
	if got, err := parseTimeout("60"); err != nil || got != time.Minute {
		t.Fatalf("parseTimeout(60) = (%v, %v)", got, err)
	}
	if _, err := parseTimeout("-1s"); err == nil {
		t.Fatal("parseTimeout() should reject negative durations")
	}
	if got, err := parseFlexibleDuration("1500ms", "--request-delay"); err != nil || got != 1500*time.Millisecond {
		t.Fatalf("parseFlexibleDuration(1500ms) = (%v, %v)", got, err)
	}
	if _, err := parseFlexibleDuration("bad", "--request-delay"); err == nil {
		t.Fatal("parseFlexibleDuration() should reject invalid durations")
	}
	if got, err := parseVerbosity(2); err != nil || got != 2 {
		t.Fatalf("parseVerbosity(2) = (%d, %v)", got, err)
	}
	if _, err := parseVerbosity(-1); err == nil {
		t.Fatal("parseVerbosity() should reject negative values")
	}
	names := supportedUTLSProfileNames()
	if len(names) == 0 || names[0] > names[len(names)-1] {
		t.Fatalf("supportedUTLSProfileNames() = %#v", names)
	}
	if got, err := parseHostnameValue("Example.COM", "--host"); err != nil || got != "example.com" {
		t.Fatalf("parseHostnameValue() = (%q, %v)", got, err)
	}
	if _, err := parseHostnameValue("https://example.com", "--host"); err == nil {
		t.Fatal("parseHostnameValue() should reject scheme")
	}
	if parsed, err := parseProxyURL("http://127.0.0.1:2089"); err != nil || parsed.Host != "127.0.0.1:2089" {
		t.Fatalf("parseProxyURL() = (%v, %v)", parsed, err)
	}
	if parsed, err := parseProxyURL(""); err != nil || parsed != nil {
		t.Fatalf("parseProxyURL(empty) = (%v, %v)", parsed, err)
	}
	if got, err := parseResolveTo("2001:db8::1"); err != nil || got != "2001:db8::1" {
		t.Fatalf("parseResolveTo() = (%q, %v)", got, err)
	}
	if got := requestHost(&http.Request{Host: "override.example", URL: mustParseURL(t, "https://example.com")}); got != "override.example" {
		t.Fatalf("requestHost() = %q", got)
	}
	if got := requestPort(transportRequestPlan{ConnectAddress: "example.com:443"}, "80"); got != "443" {
		t.Fatalf("requestPort() = %q", got)
	}

	transport := transportConfig{Fronting: frontingConfig{Enable: true, Target: "front.example.com", SNI: "front.example.com"}}
	plan := transport.buildRequestPlan(&http.Request{URL: mustParseURL(t, "https://logical.example/path")})
	if plan.ConnectAddress != "front.example.com:443" {
		t.Fatalf("buildRequestPlan connect = %q", plan.ConnectAddress)
	}
	if !transport.rewritesNetworkDestination(plan) {
		t.Fatal("rewritesNetworkDestination() should be true")
	}

	transport = transportConfig{PreferHTTP2: true}
	if !transport.shouldPreferHTTP2() {
		t.Fatal("shouldPreferHTTP2() should be true")
	}
	if got := transport.alpnProtocols(); len(got) != 2 || got[0] != "h2" || got[1] != "http/1.1" {
		t.Fatalf("alpnProtocols() = %#v", got)
	}

	transport = transportConfig{ShareHTTP2Conn: true}
	if !transport.shouldShareHTTP2Conn() || !transport.shouldPreferHTTP2() {
		t.Fatal("ShareHTTP2Conn should imply HTTP/2 preference")
	}

	transport = transportConfig{Fronting: frontingConfig{Enable: true}}
	if got := transport.alpnProtocols(); len(got) != 2 || got[0] != "h2" || got[1] != "http/1.1" {
		t.Fatalf("fronting ALPN = %#v", got)
	}

	transport = transportConfig{UTLSProfileName: "chrome_auto", UTLSProfile: supportedUTLSProfiles["chrome_auto"]}
	profiles := transport.utlsHandshakeProfiles()
	if len(profiles) < 4 || profiles[0].name != "chrome_auto" || profiles[1].name != "firefox_auto" {
		t.Fatalf("utlsHandshakeProfiles() = %#v", profiles)
	}

	transport = transportConfig{Fronting: frontingConfig{Enable: true}, UTLSProfileName: "chrome_auto", UTLSProfile: supportedUTLSProfiles["chrome_auto"]}
	profiles = transport.utlsHandshakeProfiles()
	if len(profiles) < 4 || profiles[0].name != "firefox_auto" || profiles[1].name != "edge_auto" || profiles[len(profiles)-1].name != "chrome_auto" {
		t.Fatalf("fronting utlsHandshakeProfiles() = %#v", profiles)
	}

	transport = transportConfig{PreferHTTP2: true, ForceHTTP1: true}
	if transport.shouldPreferHTTP2() {
		t.Fatal("ForceHTTP1 should disable HTTP/2 preference")
	}
	if got := transport.alpnProtocols(); len(got) != 1 || got[0] != "http/1.1" {
		t.Fatalf("forced HTTP/1.1 ALPN = %#v", got)
	}
}

func TestFolderHelperFunctions(t *testing.T) {
	if got := mime2ext("application/pdf"); got != ".pdf" {
		t.Fatalf("mime2ext() = %q", got)
	}
	if got := defFormat("application/vnd.google-apps.document"); got != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("defFormat() = %q", got)
	}
	if got := extToMime(".TXT"); got != "text/plain" {
		t.Fatalf("extToMime() = %q", got)
	}
	if !containsMimeType([]string{"a", "b"}, "b") || containsMimeType([]string{"a"}, "c") {
		t.Fatal("containsMimeType() mismatch")
	}

	listing := &folderListing{
		FolderTree: folderTree{Names: []string{"root", "root"}},
		FileList: []folderFileList{{Files: []*drive.File{
			{Name: "report", MimeType: "application/vnd.google-apps.document"},
			{Name: "report", MimeType: "application/vnd.google-apps.document"},
		}}},
	}
	(&para{Ext: "pdf"}).dupChkFoldersFiles(listing)
	if listing.FolderTree.Names[1] != "root_2" {
		t.Fatalf("duplicate folder rename = %q", listing.FolderTree.Names[1])
	}
	if listing.FileList[0].Files[1].Name != "report_2.pdf" {
		t.Fatalf("duplicate file rename = %q", listing.FileList[0].Files[1].Name)
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	task := &downloadTask{status: taskPending, updatedAt: time.Now()}
	p := &para{WorkDir: dir, Filename: "existing.txt", Skip: true, Task: task}
	if err := p.makeFileByCondition(&drive.File{Name: "existing.txt"}); err != nil {
		t.Fatalf("makeFileByCondition() error = %v", err)
	}
	if task.snapshot().Status != taskSkipped {
		t.Fatalf("task status = %q, want %q", task.snapshot().Status, taskSkipped)
	}

	newDir := filepath.Join(dir, "child")
	if err := p.makeDirByCondition(newDir); err != nil {
		t.Fatalf("makeDirByCondition() error = %v", err)
	}
	nestedDir := filepath.Join(dir, "parent", "child", "grandchild")
	if err := p.makeDirByCondition(nestedDir); err != nil {
		t.Fatalf("makeDirByCondition(nested) error = %v", err)
	}

	dryRunDir := filepath.Join(dir, "dry-run-child")
	if err := (&para{DryRun: true}).makeDirByCondition(dryRunDir); err != nil {
		t.Fatalf("makeDirByCondition(dry-run) error = %v", err)
	}
	if _, err := os.Stat(dryRunDir); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create directory, stat err = %v", err)
	}
	if !chkFile(newDir) {
		t.Fatalf("directory %q was not created", newDir)
	}
	if !chkFile(nestedDir) {
		t.Fatalf("nested directory %q was not created", nestedDir)
	}
}
