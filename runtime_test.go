package gdrivedl

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDownloadTaskStateAndTrackedReader(t *testing.T) {
	runtime := newDownloadRuntime(false, false)
	task := runtime.newTask("file.txt", "src")
	task.SetTotal(1024)
	task.MarkStarted()
	task.AddDownloaded(128)
	task.SetDownloaded(512)
	task.MarkCompleted()

	snapshot := task.snapshot()
	if snapshot.Name != "file.txt" {
		t.Fatalf("task name = %q, want file.txt", snapshot.Name)
	}
	if snapshot.Total != 1024 {
		t.Fatalf("task total = %d, want 1024", snapshot.Total)
	}
	if snapshot.Downloaded != 512 {
		t.Fatalf("task downloaded = %d, want 512", snapshot.Downloaded)
	}
	if snapshot.Status != taskCompleted {
		t.Fatalf("task status = %q, want %q", snapshot.Status, taskCompleted)
	}

	failing := runtime.newTask("failed.txt", "src")
	failing.MarkFailed(errors.New("boom"))
	if failing.snapshot().Status != taskFailed {
		t.Fatalf("failed task status = %q, want %q", failing.snapshot().Status, taskFailed)
	}

	skipped := runtime.newTask("skipped.txt", "src")
	skipped.MarkSkipped("existing file")
	skipped.MarkFailed(errors.New("ignored"))
	if skipped.snapshot().Status != taskSkipped {
		t.Fatalf("skipped task status = %q, want %q", skipped.snapshot().Status, taskSkipped)
	}

	trackedTask := runtime.newTask("tracked.bin", "src")
	reader := &trackedReadCloser{ReadCloser: io.NopCloser(strings.NewReader("hello")), task: trackedTask}
	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("tracked reader error = %v", err)
	}
	if n != 5 {
		t.Fatalf("tracked reader bytes = %d, want 5", n)
	}
	if trackedTask.snapshot().Downloaded != 5 {
		t.Fatalf("tracked task downloaded = %d, want 5", trackedTask.snapshot().Downloaded)
	}
}

func TestRuntimeFormattingHelpers(t *testing.T) {
	if got := formatBytes(0); got != "0 B" {
		t.Fatalf("formatBytes(0) = %q", got)
	}
	if got := formatBytes(1536); got != "1.5 KiB" {
		t.Fatalf("formatBytes(1536) = %q", got)
	}
	if got := formatETA(90 * time.Second); got != "01:30" {
		t.Fatalf("formatETA(90s) = %q", got)
	}
	if got := formatETA((time.Hour + 2*time.Minute + 3*time.Second)); got != "01:02:03" {
		t.Fatalf("formatETA(1h2m3s) = %q", got)
	}
	if got := truncateLabel("abcdefgh", 5); got != "ab..." {
		t.Fatalf("truncateLabel() = %q", got)
	}
	if got := firstNonEmpty("", " ", "value", "other"); got != "value" {
		t.Fatalf("firstNonEmpty() = %q", got)
	}
	if got := formatProgress(50, 100, taskDownloading); got != "50.0% (50 B/100 B)" {
		t.Fatalf("formatProgress known total = %q", got)
	}
	if got := formatProgress(10, 0, taskCompleted); got != "100.0% (10 B)" {
		t.Fatalf("formatProgress completed = %q", got)
	}
	if got := taskPercent(taskSnapshot{Downloaded: 250, Total: 100, Status: taskDownloading}); got != 100 {
		t.Fatalf("taskPercent capped = %v", got)
	}
}

func TestRuntimeProgressLine(t *testing.T) {
	runtime := newDownloadRuntime(false, false)
	runtime.startedAt = time.Now().Add(-10 * time.Second)
	runtime.lastTick = time.Now().Add(-2 * time.Second)
	runtime.lastBytes = 1024

	line := runtime.progressLine([]taskSnapshot{
		{
			Name:       "alpha.iso",
			State:      "dialing",
			Status:     taskDownloading,
			Downloaded: 2048,
			Total:      4096,
			UpdatedAt:  time.Now(),
		},
		{
			Name:       "beta.zip",
			Status:     taskCompleted,
			Downloaded: 1024,
			Total:      1024,
			UpdatedAt:  time.Now().Add(-time.Second),
		},
	})

	for _, want := range []string{"Current: alpha.iso", "State: dialing", "Total:", "Speed:", "ETA:"} {
		if !strings.Contains(line, want) {
			t.Fatalf("progress line %q missing %q", line, want)
		}
	}
}

func TestRuntimeStructuredEventsAndJSON(t *testing.T) {
	var events []Event
	runtime := newObservedDownloadRuntime(false, true, true, nil, func(event Event) {
		events = append(events, event)
	})
	task := runtime.newTask("file.txt", "src")
	task.SetTotal(5)

	output := captureStdout(t, func() {
		runtime.start()
		runtime.log("status", "[status] hello\n", map[string]any{"message": "hello"})
		task.MarkStarted()
		task.SetDownloaded(5)
		task.MarkCompleted()
		runtime.finish()
	})

	if len(events) == 0 {
		t.Fatal("expected structured events")
	}
	if events[0].Type == "" || events[0].Name == "" || events[0].Timestamp.IsZero() {
		t.Fatalf("invalid first event: %#v", events[0])
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("expected JSON output lines")
	}
	var event Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if event.Type == "" || event.Name == "" || event.Timestamp.IsZero() {
		t.Fatalf("unexpected event = %#v", event)
	}
	if !strings.Contains(output, "\"type\":") {
		t.Fatalf("json output = %q", output)
	}
}

func TestRuntimeJSONForcesExitReportEvents(t *testing.T) {
	var events []Event
	runtime := newObservedDownloadRuntime(false, false, true, nil, func(event Event) {
		events = append(events, event)
	})
	task := runtime.newTask("file.txt", "src")
	task.SetTotal(5)
	task.MarkStarted()
	task.SetDownloaded(5)
	task.MarkCompleted()

	_ = captureStdout(t, func() {
		runtime.start()
		runtime.finish()
	})

	for _, event := range events {
		if event.Type == "report" && event.Name == "exit_item" {
			return
		}
	}
	t.Fatalf("expected exit_item report event, got %#v", events)
}
