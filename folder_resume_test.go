package gdrivedl

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	drive "google.golang.org/api/drive/v3"
)

func TestDownloadFolderFileWithResumeStrategySkipsCompletedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alpha.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runtime := newDownloadRuntime(false, false)
	task := runtime.newTask("alpha.bin", "file-id")
	p := &para{WorkDir: dir, APIKey: "key", Resumabledownload: "10m", Task: task}
	file := &drive.File{Id: "file-id", Name: "alpha.bin", Size: 5}
	resumeCalled := false
	directCalled := false

	err := p.downloadFolderFileWithResumeStrategy(file,
		func(*para, *drive.File) error {
			resumeCalled = true
			return nil
		},
		func(*para, *drive.File) error {
			directCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("downloadFolderFileWithResumeStrategy() error = %v", err)
	}
	if resumeCalled || directCalled {
		t.Fatalf("resumeCalled=%v directCalled=%v, want both false", resumeCalled, directCalled)
	}
	snapshot := task.snapshot()
	if snapshot.Status != taskCompleted {
		t.Fatalf("task status = %q, want %q", snapshot.Status, taskCompleted)
	}
	if snapshot.Detail != "already complete" {
		t.Fatalf("task detail = %q, want already complete", snapshot.Detail)
	}
	if snapshot.Total != 5 || snapshot.Downloaded != 5 {
		t.Fatalf("task progress = %d/%d, want 5/5", snapshot.Downloaded, snapshot.Total)
	}
}

func TestDownloadFolderFileWithResumeStrategyPreservesPartialBeforeFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alpha.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	p := &para{WorkDir: dir, APIKey: "key", Resumabledownload: "10m", Disp: true}
	file := &drive.File{Id: "file-id", Name: "alpha.bin", Size: 10}
	var directCalled bool

	err := p.downloadFolderFileWithResumeStrategy(file,
		func(*para, *drive.File) error {
			return fmt.Errorf("%w: unsupported", errResumableFullRedownloadRequired)
		},
		func(p *para, file *drive.File) error {
			directCalled = true
			return os.WriteFile(filepath.Join(p.WorkDir, file.Name), []byte("abcdefghij"), 0o666)
		},
	)
	if err != nil {
		t.Fatalf("downloadFolderFileWithResumeStrategy() error = %v", err)
	}
	if !directCalled {
		t.Fatal("direct downloader was not called")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "abcdefghij" {
		t.Fatalf("final content = %q, want abcdefghij", string(content))
	}
	backups, err := filepath.Glob(path + ".gdrivedl-partial.*.bak*")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("unexpected leftover backups = %#v", backups)
	}
}

func TestDownloadFolderFileWithResumeStrategyKeepsBackupOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alpha.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	p := &para{WorkDir: dir, APIKey: "key", Resumabledownload: "10m", Disp: true}
	file := &drive.File{Id: "file-id", Name: "alpha.bin", Size: 10}

	err := p.downloadFolderFileWithResumeStrategy(file,
		func(*para, *drive.File) error {
			return fmt.Errorf("%w: unsupported", errResumableFullRedownloadRequired)
		},
		func(*para, *drive.File) error {
			return errors.New("full download failed")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "partial file preserved at") {
		t.Fatalf("downloadFolderFileWithResumeStrategy() error = %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("final path stat error = %v, want not-exist", statErr)
	}
	backups, globErr := filepath.Glob(path + ".gdrivedl-partial.*.bak*")
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	if len(backups) != 1 {
		t.Fatalf("backup count = %d, want 1 (%#v)", len(backups), backups)
	}
	content, readErr := os.ReadFile(backups[0])
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(content) != "abc" {
		t.Fatalf("backup content = %q, want abc", string(content))
	}
}

func TestResDownloadFileByAPIKeyRejectsIgnoredRange(t *testing.T) {
	v := &valResumableDownload{
		para: para{
			APIKey: "key",
			Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					Status:        "200 OK",
					StatusCode:    http.StatusOK,
					Header:        http.Header{"Content-Length": []string{"4"}},
					Body:          io.NopCloser(strings.NewReader("full")),
					ContentLength: 4,
					Request:       req,
				}, nil
			})},
		},
		dlParams: dlParams{
			DownloadFile: &drive.File{Id: "file-id", Name: "alpha.bin", Size: 100},
			Range:        "bytes=50-99",
			Start:        50,
			End:          99,
		},
	}

	res, err := v.resDownloadFileByAPIKey()
	if res != nil {
		t.Fatalf("response = %#v, want nil", res)
	}
	if !errors.Is(err, errResumableFullRedownloadRequired) {
		t.Fatalf("error = %v, want resumable fallback error", err)
	}
}
