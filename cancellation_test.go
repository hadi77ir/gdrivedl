package gdrivedl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type cancelingReadCloser struct {
	payload []byte
	read    bool
}

func (r *cancelingReadCloser) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	n := copy(p, r.payload)
	return n, context.Canceled
}

func (r *cancelingReadCloser) Close() error {
	return nil
}

func TestFinishTaskWithErrorCancellationMarksCancelled(t *testing.T) {
	runtime := newDownloadRuntime(false, false)
	task := runtime.newTask("file.bin", "src")
	finishTaskWithError(task, context.Canceled)
	if got := task.snapshot().Status; got != taskCancelled {
		t.Fatalf("task status = %q, want %q", got, taskCancelled)
	}
	if got := task.snapshot().Detail; got != "cancelled" {
		t.Fatalf("task detail = %q, want cancelled", got)
	}

	other := runtime.newTask("other.bin", "src")
	finishTaskWithError(other, errors.New("boom"))
	if got := other.snapshot().Status; got != taskFailed {
		t.Fatalf("task status = %q, want %q", got, taskFailed)
	}
}

func TestSaveFileCancellationKeepsPartialData(t *testing.T) {
	dir := t.TempDir()
	res := &http.Response{
		Header: http.Header{
			"Content-Disposition": []string{"attachment; filename=\"partial.bin\""},
			"Content-Type":        []string{"application/octet-stream"},
		},
		Body: &cancelingReadCloser{payload: []byte("abc")},
	}
	p := &para{Disp: true, WorkDir: dir, Kind: "file"}
	err := p.saveFile(res)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("saveFile() error = %v, want context.Canceled", err)
	}
	content, readErr := os.ReadFile(filepath.Join(dir, "partial.bin"))
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(content) != "abc" {
		t.Fatalf("partial content = %q, want abc", string(content))
	}
}

func TestCommandContextUsesAppMetadata(t *testing.T) {
	cliCtx := newTestContext(t, nil)
	want, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cliCtx.App.Metadata == nil {
		cliCtx.App.Metadata = map[string]interface{}{}
	}
	cliCtx.App.Metadata[appContextMetadataKey] = want
	if got := commandContext(cliCtx); got != want {
		t.Fatalf("commandContext() = %#v, want %#v", got, want)
	}
}
