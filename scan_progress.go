package gdrivedl

import (
	"fmt"
	"time"
)

type scanObserver struct {
	runtime   *downloadRuntime
	verbosity int
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
	observer.total += count
	observer.emitProgress()
}

func (observer *scanObserver) beginPhase(phase string) {
	if observer == nil {
		return
	}
	if observer.phase == phase {
		return
	}
	observer.phase = phase
	observer.logf(0, "phase=%s progress=%d/%d", phase, observer.completed, observer.total)
	observer.emitProgress()
}

func (observer *scanObserver) update(state, current string) {
	if observer == nil {
		return
	}
	observer.state = state
	observer.current = current
	observer.logf(1, "phase=%s state=%s current=%s progress=%d/%d", observer.phase, observer.state, observer.current, observer.completed, observer.total)
	observer.emitProgress()
}

func (observer *scanObserver) complete() {
	if observer == nil {
		return
	}
	observer.completed++
	observer.emitProgress()
}

func (observer *scanObserver) finish(err error) {
	if observer == nil {
		return
	}
	switch {
	case err == nil:
		observer.state = "completed"
		observer.logf(0, "state=completed progress=%d/%d elapsed=%s", observer.completed, observer.total, time.Since(observer.startedAt).Round(time.Millisecond))
	case isCancellationError(err):
		observer.state = "cancelled"
		observer.current = err.Error()
		observer.logf(0, "state=cancelled progress=%d/%d elapsed=%s", observer.completed, observer.total, time.Since(observer.startedAt).Round(time.Millisecond))
	default:
		observer.state = "failed"
		observer.current = err.Error()
		observer.logf(0, "state=failed error=%q progress=%d/%d elapsed=%s", err.Error(), observer.completed, observer.total, time.Since(observer.startedAt).Round(time.Millisecond))
	}
	observer.emitProgress()
}

func (observer *scanObserver) emitProgress() {
	if !observer.hasStructuredOutput() {
		return
	}
	percent := 0.0
	if observer.total > 0 {
		percent = (float64(observer.completed) / float64(observer.total)) * 100
		if percent > 100 {
			percent = 100
		}
	}
	observer.runtime.emitEvent("progress", "scan", map[string]any{
		"mode":            string(observer.mode),
		"phase":           observer.phase,
		"state":           observer.state,
		"current":         observer.current,
		"completed_steps": observer.completed,
		"total_steps":     observer.total,
		"percent":         percent,
		"elapsed":         time.Since(observer.startedAt).Round(time.Millisecond).String(),
	})
}

func (observer *scanObserver) logf(minVerbosity int, format string, args ...interface{}) {
	if observer == nil {
		return
	}
	effectiveVerbosity := observer.verbosity - 1
	if effectiveVerbosity < minVerbosity && !observer.hasStructuredOutput() {
		return
	}
	line := fmt.Sprintf("[scan] %s", fmt.Sprintf(format, args...))
	fields := map[string]any{
		"mode":            string(observer.mode),
		"phase":           observer.phase,
		"state":           observer.state,
		"current":         observer.current,
		"completed_steps": observer.completed,
		"total_steps":     observer.total,
		"message":         line,
	}
	if observer.runtime != nil {
		observer.runtime.log("scan", line+"\n", fields)
		return
	}
	fmt.Println(line)
}
