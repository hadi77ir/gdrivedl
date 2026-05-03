package gdrivedl

import (
	"fmt"
	"sync"
	"time"
)

type scanObserver struct {
	runtime   *downloadRuntime
	verbosity int
	mode      scanMode
	mu        sync.Mutex
	phase     string
	state     string
	current   string
	completed int64
	total     int64
	startedAt time.Time
}

type scanObserverSnapshot struct {
	mode      scanMode
	phase     string
	state     string
	current   string
	completed int64
	total     int64
	startedAt time.Time
}

func newScanObserver(runtime *downloadRuntime, mode scanMode, verbosity int) *scanObserver {
	return &scanObserver{
		runtime:   runtime,
		verbosity: verbosity,
		mode:      mode,
		startedAt: time.Now(),
	}
}

func (observer *scanObserver) hasStructuredOutput() bool {
	return observer != nil && observer.runtime != nil && observer.runtime.hasStructuredOutput()
}

func (observer *scanObserver) addTotal(count int64) {
	if observer == nil || count <= 0 {
		return
	}
	observer.mu.Lock()
	observer.total += count
	snapshot := observer.snapshotLocked()
	observer.mu.Unlock()
	observer.emitProgressSnapshot(snapshot)
}

func (observer *scanObserver) beginPhase(phase string) {
	if observer == nil {
		return
	}
	observer.mu.Lock()
	if observer.phase == phase {
		observer.mu.Unlock()
		return
	}
	observer.phase = phase
	snapshot := observer.snapshotLocked()
	observer.mu.Unlock()
	observer.logfSnapshot(0, snapshot, "phase=%s progress=%d/%d", phase, snapshot.completed, snapshot.total)
	observer.emitProgressSnapshot(snapshot)
}

func (observer *scanObserver) update(state, current string) {
	if observer == nil {
		return
	}
	observer.mu.Lock()
	observer.state = state
	observer.current = current
	snapshot := observer.snapshotLocked()
	observer.mu.Unlock()
	observer.logfSnapshot(1, snapshot, "phase=%s state=%s current=%s progress=%d/%d", snapshot.phase, snapshot.state, snapshot.current, snapshot.completed, snapshot.total)
	observer.emitProgressSnapshot(snapshot)
}

func (observer *scanObserver) complete() {
	if observer == nil {
		return
	}
	observer.mu.Lock()
	observer.completed++
	snapshot := observer.snapshotLocked()
	observer.mu.Unlock()
	observer.emitProgressSnapshot(snapshot)
}

func (observer *scanObserver) finish(err error) {
	if observer == nil {
		return
	}
	observer.mu.Lock()
	var message string
	switch {
	case err == nil:
		observer.state = "completed"
		message = "state=completed progress=%d/%d elapsed=%s"
	case isCancellationError(err):
		observer.state = "cancelled"
		observer.current = err.Error()
		message = "state=cancelled progress=%d/%d elapsed=%s"
	default:
		observer.state = "failed"
		observer.current = err.Error()
		message = fmt.Sprintf("state=failed error=%q progress=%%d/%%d elapsed=%%s", err.Error())
	}
	snapshot := observer.snapshotLocked()
	observer.mu.Unlock()
	observer.logfSnapshot(0, snapshot, message, snapshot.completed, snapshot.total, time.Since(snapshot.startedAt).Round(time.Millisecond))
	observer.emitProgressSnapshot(snapshot)
}

func (observer *scanObserver) emitProgressSnapshot(snapshot scanObserverSnapshot) {
	if observer == nil || !observer.hasStructuredOutput() {
		return
	}
	percent := 0.0
	if snapshot.total > 0 {
		percent = (float64(snapshot.completed) / float64(snapshot.total)) * 100
		if percent > 100 {
			percent = 100
		}
	}
	observer.runtime.emitEvent("progress", "scan", map[string]any{
		"mode":            string(snapshot.mode),
		"phase":           snapshot.phase,
		"state":           snapshot.state,
		"current":         snapshot.current,
		"completed_steps": snapshot.completed,
		"total_steps":     snapshot.total,
		"percent":         percent,
		"elapsed":         time.Since(snapshot.startedAt).Round(time.Millisecond).String(),
	})
}

func (observer *scanObserver) logfSnapshot(minVerbosity int, snapshot scanObserverSnapshot, format string, args ...interface{}) {
	if observer == nil {
		return
	}
	effectiveVerbosity := observer.verbosity - 1
	if effectiveVerbosity < minVerbosity && !observer.hasStructuredOutput() {
		return
	}
	line := fmt.Sprintf("[scan] %s", fmt.Sprintf(format, args...))
	fields := map[string]any{
		"mode":            string(snapshot.mode),
		"phase":           snapshot.phase,
		"state":           snapshot.state,
		"current":         snapshot.current,
		"completed_steps": snapshot.completed,
		"total_steps":     snapshot.total,
		"message":         line,
	}
	if observer.runtime != nil {
		observer.runtime.log("scan", line+"\n", fields)
		return
	}
	fmt.Println(line)
}

func (observer *scanObserver) snapshotLocked() scanObserverSnapshot {
	return scanObserverSnapshot{
		mode:      observer.mode,
		phase:     observer.phase,
		state:     observer.state,
		current:   observer.current,
		completed: observer.completed,
		total:     observer.total,
		startedAt: observer.startedAt,
	}
}
