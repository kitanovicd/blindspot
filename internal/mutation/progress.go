package mutation

import (
	"fmt"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
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
			fmt.Sprintf("Progress  %d/%d completed", completed, total),
			fmt.Sprintf("%d caught", s.Caught),
			fmt.Sprintf("%d survived", s.Survived),
		}
		if s.Invalid > 0 {
			parts = append(parts, fmt.Sprintf("%d invalid", s.Invalid))
		}
		if unresolved > 0 {
			parts = append(parts, fmt.Sprintf("%d unresolved", unresolved))
		}
		r.logf("%s · %s elapsed", strings.Join(parts, " · "), time.Since(started).Round(time.Second))
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
