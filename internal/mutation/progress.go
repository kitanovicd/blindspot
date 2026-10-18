package mutation

import (
	"fmt"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/terminal"
)

// startEvaluationProgress reports aggregate results on a timer, even when no
// mutation has finished. snapshot must safely read the workers' shared state.
// The returned function waits for the reporter to stop and prints a final update.
func (r *Runner) startEvaluationProgress(total int, snapshot func() report.Summary) func() {
	started := time.Now()
	update := func() {
		s := snapshot()
		unresolved := s.Timeout + s.Error
		completed := s.Caught + s.Survived + s.Invalid + unresolved
		parts := []string{
			r.style.Text(terminal.Accent, fmt.Sprintf("Progress  %d/%d completed", completed, total)),
			r.style.Count(terminal.Green, s.Caught, "caught"),
			r.style.Count(terminal.Yellow, s.Survived, "survived"),
		}
		if s.Invalid > 0 {
			parts = append(parts, r.style.Count(terminal.Muted, s.Invalid, "invalid"))
		}
		if unresolved > 0 {
			parts = append(parts, r.style.Count(terminal.Yellow, unresolved, "unresolved"))
		}
		r.logf("  %s · %s", strings.Join(parts, " · "), r.style.Text(terminal.Muted, fmt.Sprintf("%s elapsed", time.Since(started).Round(time.Second))))
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				update()
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		update()
	}
}
