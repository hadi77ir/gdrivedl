package gdrivedl

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type downloadTaskStatus string

const (
	taskPending     downloadTaskStatus = "pending"
	taskDownloading downloadTaskStatus = "downloading"
	taskCompleted   downloadTaskStatus = "completed"
	taskSkipped     downloadTaskStatus = "skipped"
	taskFailed      downloadTaskStatus = "failed"
)

type downloadRuntime struct {
	showProgress bool
	exitReport   bool
	startedAt    time.Time
	observer     func(ProgressSnapshot)

	mu          sync.RWMutex
	tasks       []*downloadTask
	printMu     sync.Mutex
	stopCh      chan struct{}
	doneCh      chan struct{}
	lastLineLen int
	lastBytes   int64
	lastTick    time.Time
	speed       float64
}

type downloadTask struct {
	runtime *downloadRuntime

	mu          sync.Mutex
	name        string
	source      string
	state       string
	total       int64
	downloaded  int64
	status      downloadTaskStatus
	detail      string
	createdAt   time.Time
	startedAt   time.Time
	updatedAt   time.Time
	completedAt time.Time
}

type trackedReadCloser struct {
	io.ReadCloser
	task *downloadTask
}

type taskSnapshot struct {
	Name       string
	Source     string
	State      string
	Status     downloadTaskStatus
	Detail     string
	Total      int64
	Downloaded int64
	UpdatedAt  time.Time
}

func newDownloadRuntime(showProgress, exitReport bool) *downloadRuntime {
	return newObservedDownloadRuntime(showProgress, exitReport, nil)
}

func newObservedDownloadRuntime(showProgress, exitReport bool, observer func(ProgressSnapshot)) *downloadRuntime {
	now := time.Now()
	return &downloadRuntime{
		showProgress: showProgress,
		exitReport:   exitReport,
		startedAt:    now,
		observer:     observer,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		lastTick:     now,
	}
}

func (r *downloadRuntime) start() {
	if r == nil || (!r.showProgress && r.observer == nil) {
		return
	}
	go r.renderLoop()
}

func (r *downloadRuntime) finish() {
	if r == nil {
		return
	}
	if r.showProgress || r.observer != nil {
		close(r.stopCh)
		<-r.doneCh
	}
	r.emitProgress()
	if r.exitReport {
		r.printExitReport()
	}
}

func (r *downloadRuntime) renderLoop() {
	defer close(r.doneCh)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.renderProgress()
			r.emitProgress()
		case <-r.stopCh:
			r.clearProgressLine()
			return
		}
	}
}

func (r *downloadRuntime) newTask(name, source string) *downloadTask {
	if r == nil {
		return nil
	}
	now := time.Now()
	task := &downloadTask{
		runtime:   r,
		name:      firstNonEmpty(name, source, "(unknown)"),
		source:    source,
		state:     "queued",
		status:    taskPending,
		createdAt: now,
		updatedAt: now,
	}
	r.mu.Lock()
	r.tasks = append(r.tasks, task)
	r.mu.Unlock()
	r.emitProgress()
	return task
}

func (r *downloadRuntime) emitProgress() {
	if r == nil || r.observer == nil {
		return
	}
	r.observer(r.progressSnapshot())
}

func (r *downloadRuntime) progressSnapshot() ProgressSnapshot {
	tasks := r.snapshotTasks()
	snapshot := ProgressSnapshot{Tasks: make([]TaskSnapshot, 0, len(tasks))}
	for _, task := range tasks {
		snapshot.Tasks = append(snapshot.Tasks, TaskSnapshot{
			Name:       task.Name,
			Source:     task.Source,
			State:      task.State,
			Status:     string(task.Status),
			Detail:     task.Detail,
			Total:      task.Total,
			Downloaded: task.Downloaded,
			UpdatedAt:  task.UpdatedAt,
		})
		snapshot.TotalDownloaded += task.Downloaded
		if task.Total > 0 {
			snapshot.KnownDownloaded += minInt64(task.Downloaded, task.Total)
			snapshot.KnownTotal += task.Total
		}
	}
	if len(tasks) > 0 {
		snapshot.SummaryLine = r.progressLine(tasks)
		snapshot.SpeedBytesPerSecond = r.speed
	}
	return snapshot
}

func (r *downloadRuntime) printf(format string, args ...interface{}) {
	if r == nil {
		fmt.Printf(format, args...)
		return
	}
	r.printMu.Lock()
	defer r.printMu.Unlock()
	r.clearProgressLineLocked()
	fmt.Printf(format, args...)
}

func (r *downloadRuntime) renderProgress() {
	if r == nil || !r.showProgress {
		return
	}
	tasks := r.snapshotTasks()
	if len(tasks) == 0 {
		return
	}
	line := r.progressLine(tasks)
	if line == "" {
		return
	}
	r.printMu.Lock()
	defer r.printMu.Unlock()
	padding := ""
	if len(line) < r.lastLineLen {
		padding = strings.Repeat(" ", r.lastLineLen-len(line))
	}
	fmt.Printf("\r%s%s", line, padding)
	r.lastLineLen = len(line)
}

func (r *downloadRuntime) clearProgressLine() {
	if r == nil {
		return
	}
	r.printMu.Lock()
	defer r.printMu.Unlock()
	r.clearProgressLineLocked()
}

func (r *downloadRuntime) clearProgressLineLocked() {
	if !r.showProgress || r.lastLineLen == 0 {
		return
	}
	fmt.Printf("\r%s\r", strings.Repeat(" ", r.lastLineLen))
	r.lastLineLen = 0
}

func (r *downloadRuntime) snapshotTasks() []taskSnapshot {
	r.mu.RLock()
	tasks := append([]*downloadTask(nil), r.tasks...)
	r.mu.RUnlock()

	out := make([]taskSnapshot, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		out = append(out, task.snapshot())
	}
	return out
}

func (r *downloadRuntime) progressLine(tasks []taskSnapshot) string {
	var (
		current         *taskSnapshot
		knownDownloaded int64
		knownTotal      int64
		allDownloaded   int64
		remainingKnown  int64
	)
	for i := range tasks {
		task := tasks[i]
		allDownloaded += task.Downloaded
		if task.Total > 0 {
			knownDownloaded += minInt64(task.Downloaded, task.Total)
			knownTotal += task.Total
			if task.Status == taskPending || task.Status == taskDownloading {
				remaining := task.Total - minInt64(task.Downloaded, task.Total)
				if remaining > 0 {
					remainingKnown += remaining
				}
			}
		}
		if task.Status == taskPending || task.Status == taskDownloading {
			if current == nil || task.UpdatedAt.After(current.UpdatedAt) {
				current = &task
			}
		}
	}
	if current == nil {
		current = &tasks[len(tasks)-1]
	}

	now := time.Now()
	elapsed := now.Sub(r.startedAt)
	if elapsed <= 0 {
		elapsed = time.Second
	}
	deltaTime := now.Sub(r.lastTick)
	if deltaTime > 0 {
		r.speed = float64(allDownloaded-r.lastBytes) / deltaTime.Seconds()
		r.lastBytes = allDownloaded
		r.lastTick = now
	}
	if r.speed <= 0 {
		r.speed = float64(allDownloaded) / elapsed.Seconds()
	}

	currentLabel := truncateLabel(firstNonEmpty(current.Name, current.Source, "(unknown)"), 36)
	currentState := firstNonEmpty(current.State, string(current.Status))
	currentProgress := formatProgress(current.Downloaded, current.Total, current.Status)
	totalProgress := formatProgress(knownDownloaded, knownTotal, taskDownloading)
	if knownTotal == 0 {
		totalProgress = formatBytes(allDownloaded)
	} else if knownDownloaded < allDownloaded {
		totalProgress += "+"
	}
	eta := "--"
	if r.speed > 0 && remainingKnown > 0 {
		eta = formatETA(time.Duration(float64(remainingKnown)/r.speed) * time.Second)
	}
	return fmt.Sprintf("Current: %s | State: %s | Progress: %s | Total: %s | Speed: %s/s | ETA: %s", currentLabel, currentState, currentProgress, totalProgress, formatBytes(int64(r.speed)), eta)
}

func (r *downloadRuntime) printExitReport() {
	tasks := r.snapshotTasks()
	if len(tasks) == 0 {
		return
	}
	nameWidth := len("FILE")
	for _, task := range tasks {
		if length := len(firstNonEmpty(task.Name, task.Source, "(unknown)")); length > nameWidth {
			nameWidth = length
		}
	}
	r.printMu.Lock()
	defer r.printMu.Unlock()
	r.clearProgressLineLocked()
	fmt.Printf("Exit report:\n")
	fmt.Printf("%-*s  %-9s  %s\n", nameWidth, "FILE", "PERCENT", "STATUS")
	for _, task := range tasks {
		status := string(task.Status)
		if task.Detail != "" {
			status += " (" + task.Detail + ")"
		}
		fmt.Printf("%-*s  %8.1f%%  %s\n", nameWidth, firstNonEmpty(task.Name, task.Source, "(unknown)"), taskPercent(task), status)
	}
}

func (t *downloadTask) snapshot() taskSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return taskSnapshot{
		Name:       t.name,
		Source:     t.source,
		State:      t.state,
		Status:     t.status,
		Detail:     t.detail,
		Total:      t.total,
		Downloaded: t.downloaded,
		UpdatedAt:  t.updatedAt,
	}
}

func (t *downloadTask) SetName(name string) {
	if t == nil || strings.TrimSpace(name) == "" {
		return
	}
	t.mu.Lock()
	t.name = name
	t.updatedAt = time.Now()
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) SetTotal(total int64) {
	if t == nil || total <= 0 {
		return
	}
	t.mu.Lock()
	if t.total == 0 || total > t.total {
		t.total = total
	}
	t.updatedAt = time.Now()
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) SetState(state string) {
	if t == nil || strings.TrimSpace(state) == "" {
		return
	}
	t.mu.Lock()
	t.state = state
	t.updatedAt = time.Now()
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) SetDetail(detail string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.detail = strings.TrimSpace(detail)
	t.updatedAt = time.Now()
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) MarkStarted() {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.startedAt.IsZero() {
		t.startedAt = now
	}
	if t.status == taskPending {
		t.status = taskDownloading
	}
	if t.state == "" || t.state == "queued" {
		t.state = "starting"
	}
	t.updatedAt = now
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) AddDownloaded(size int64) {
	if t == nil || size <= 0 {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.startedAt.IsZero() {
		t.startedAt = now
	}
	t.status = taskDownloading
	t.state = "downloading"
	t.downloaded += size
	t.updatedAt = now
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) SetDownloaded(size int64) {
	if t == nil || size < 0 {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.startedAt.IsZero() {
		t.startedAt = now
	}
	t.downloaded = size
	if size > 0 && t.status == taskPending {
		t.status = taskDownloading
	}
	if size > 0 && (t.state == "" || t.state == "queued") {
		t.state = "downloading"
	}
	t.updatedAt = now
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) MarkCompleted() {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.startedAt.IsZero() {
		t.startedAt = now
	}
	t.status = taskCompleted
	t.state = "completed"
	t.completedAt = now
	t.updatedAt = now
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) MarkSkipped(detail string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	t.status = taskSkipped
	t.state = "skipped"
	t.detail = detail
	t.completedAt = now
	t.updatedAt = now
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (t *downloadTask) MarkFailed(err error) {
	if t == nil || err == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.status == taskCompleted || t.status == taskSkipped {
		t.mu.Unlock()
		return
	}
	t.status = taskFailed
	t.state = "failed"
	t.detail = err.Error()
	t.completedAt = now
	t.updatedAt = now
	t.mu.Unlock()
	t.runtime.emitProgress()
}

func (r *trackedReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 && r.task != nil {
		r.task.AddDownloaded(int64(n))
	}
	return n, err
}

func taskPercent(task taskSnapshot) float64 {
	if task.Total > 0 {
		return minFloat64(100, (float64(minInt64(task.Downloaded, task.Total))/float64(task.Total))*100)
	}
	if task.Status == taskCompleted {
		return 100
	}
	return 0
}

func formatProgress(downloaded, total int64, status downloadTaskStatus) string {
	if total > 0 {
		return fmt.Sprintf("%.1f%% (%s/%s)", minFloat64(100, (float64(minInt64(downloaded, total))/float64(total))*100), formatBytes(downloaded), formatBytes(total))
	}
	if status == taskCompleted {
		return fmt.Sprintf("100.0%% (%s)", formatBytes(downloaded))
	}
	return formatBytes(downloaded)
}

func formatBytes(size int64) string {
	if size < 0 {
		size = 0
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func formatETA(duration time.Duration) string {
	if duration <= 0 {
		return "00:00"
	}
	seconds := int(duration.Seconds())
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%02d:%02d", minutes, secs)
}

func truncateLabel(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func minFloat64(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
