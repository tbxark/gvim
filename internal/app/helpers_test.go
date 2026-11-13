package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui"
)

// buildModel builds the UI model and returns it with the file index of the root. It
// starts no watcher and saves nothing, so it is for the tests that only need the model.
// The caller closes the index.
func buildModel(ctx context.Context, opts Options) (*tui.Model, *finder.Index, error) {
	rt, err := buildRuntime(ctx, opts, false)
	if err != nil {
		return nil, nil, err
	}
	return rt.model, rt.projects.Index(), nil
}

// closeAll runs the checked exit save and reports a failure on stderr, for tests and
// cleanups that do not look at the outcome.
func (m *workspaceManager) closeAll() {
	if err := m.closeAllChecked(workspaceExitTimeout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: workspace save:", err)
	}
}

// testWrites records the capture writes of each manager, for waitWrites.
var (
	testWritesMu sync.Mutex
	testWrites   = map[*workspaceManager][]*saveJob{}
)

func init() {
	captureJobHook = func(m *workspaceManager, job *saveJob) {
		testWritesMu.Lock()
		testWrites[m] = append(testWrites[m], job)
		testWritesMu.Unlock()
	}
}

// waitWrites waits for every outstanding capture write, up to timeout per job. It reports
// the errors (a failed save is a lost capture and must be visible).
func (m *workspaceManager) waitWrites(timeout time.Duration) error {
	testWritesMu.Lock()
	jobs := append([]*saveJob(nil), testWrites[m]...)
	testWritesMu.Unlock()
	var errs []error
	for _, job := range jobs {
		select {
		case <-job.done:
			if job.err != nil {
				errs = append(errs, job.err)
			}
		case <-time.After(timeout):
			errs = append(errs, errors.New("workspace: capture write did not finish"))
		}
	}
	return errors.Join(errs...)
}

// Applied reports the last sequence applied. Seq reports the latest requested sequence; the
// pair lets a test wait for the worker without polling UI state.
func (b *lspBaselineUpdater) Applied() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.applied
}

func (b *lspBaselineUpdater) Seq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}
