package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

// Finish is the supervisor, `clawdline update finish`, run from the old
// release outside the service's job. It is safe to run again at any point:
// each run reads pending.json, continues the phase recorded there, and
// writes down what it decided before it acts on it. A nil answer means the
// update is settled (healthy, rolled back, or nothing pending), so the
// supervisor job exits 0 and is not started again; an error asks the service
// manager to start it again.
func (e Env) Finish(ctx context.Context) error {
	p, ok, err := e.ReadPending()
	if err != nil {
		// Without from and to there is nothing safe to switch: keep the file
		// for a person and stop asking to be restarted.
		_ = os.Rename(e.pendingPath(), e.pendingPath()+".unreadable")
		_ = e.record(contract.UpdateApplyStateFailed, "", "", &contract.UpdateApplyError{Code: CodeStateUnreadable, Detail: err.Error()}, "")
		return nil
	}
	svc, serr := install.ReadServiceFile(e.StateDir)
	if !ok {
		if serr == nil {
			e.endSupervisor(ctx, svc)
		}
		return nil
	}
	if serr != nil {
		// No service to restart: go back to the release that was running,
		// which needs no restart because nothing switched it yet, or will
		// be started by whoever starts the daemon next.
		_ = e.Layout.SwitchCurrent(p.From)
		return e.settleRolledBack(p, &contract.UpdateApplyError{Code: CodeNoService, Detail: serr.Error()})
	}
	p.SupervisorRuns++
	if p.Phase == phaseSwitch && p.SupervisorRuns > SupervisorRunsLimit {
		p.Phase = phaseRollback
		p.Reason = &contract.UpdateApplyError{Code: CodeSupervisorGaveUp,
			Detail: fmt.Sprintf("the supervisor was started %d times without settling the update", p.SupervisorRuns-1)}
	}
	if err := e.writePending(p); err != nil {
		return err
	}
	if p.Phase == phaseSwitch {
		_ = e.record(contract.UpdateApplyStateRestarting, p.From, p.To, nil, p.StagedApp)
		why := e.trySwitch(ctx, svc, p)
		if why == nil {
			// The boot guard may have given the update up while this waited;
			// what it recorded stands.
			if _, still, _ := e.ReadPending(); !still {
				e.endSupervisor(ctx, svc)
				return nil
			}
			if err := e.clearPending(); err != nil {
				return err
			}
			staged, aerr := e.swapApp(ctx, p.StagedApp, p.From)
			var appErr *contract.UpdateApplyError
			if aerr != nil {
				appErr = &contract.UpdateApplyError{Code: "app_swap_deferred", Detail: aerr.Error()}
			}
			_ = e.record(contract.UpdateApplyStateHealthy, p.From, p.To, appErr, staged)
			_ = e.pruneReleases(p.To, staged)
			e.endSupervisor(ctx, svc)
			return nil
		}
		p.Phase, p.Reason = phaseRollback, why
		if err := e.writePending(p); err != nil {
			return err
		}
	}
	return e.rollback(ctx, svc, p)
}

// trySwitch points current at the new release, restarts the service and
// waits for it. nil means healthy.
func (e Env) trySwitch(ctx context.Context, svc install.ServiceFile, p Pending) *contract.UpdateApplyError {
	if err := e.Layout.SwitchCurrent(p.To); err != nil {
		return &contract.UpdateApplyError{Code: CodeSwitchFailed, Detail: err.Error()}
	}
	if err := e.restartService(ctx, svc); err != nil {
		return &contract.UpdateApplyError{Code: CodeSupervisorFailed, Detail: "restarting the service: " + err.Error()}
	}
	if err := e.waitHealthy(ctx, svc, p.ToCommit); err != nil {
		return codeOf(err, CodeHealthTimeout)
	}
	return nil
}

// rollback switches back to the old release, asks its next start to restore
// the snapshot when the new release's store change was not additive,
// restarts it and records rolled_back.
func (e Env) rollback(ctx context.Context, svc install.ServiceFile, p Pending) error {
	if err := e.Layout.SwitchCurrent(p.From); err != nil {
		if p.SupervisorRuns > 2*SupervisorRunsLimit {
			// The old release cannot be made current again: say so and stop.
			_ = e.clearPending()
			_ = e.record(contract.UpdateApplyStateFailed, p.From, p.To, &contract.UpdateApplyError{Code: CodeSwitchFailed,
				Detail: "could not switch back to " + p.From + ": " + err.Error()}, "")
			e.endSupervisor(ctx, svc)
			return nil
		}
		return err
	}
	if p.NonAdditive && p.Snapshot != "" {
		if err := writeJSON(e.restorePath(), restoreMarker{From: p.From, To: p.To, Snapshot: p.Snapshot}); err != nil {
			return err
		}
	}
	why := p.Reason
	if why == nil {
		why = &contract.UpdateApplyError{Code: CodeHealthTimeout, Detail: "the new release did not come up"}
	}
	if err := e.restartService(ctx, svc); err != nil {
		why = &contract.UpdateApplyError{Code: why.Code, Detail: why.Detail + "; restarting the previous release failed too: " + err.Error()}
	} else if err := e.waitHealthy(ctx, svc, p.FromCommit); err != nil {
		why = &contract.UpdateApplyError{Code: why.Code, Detail: why.Detail + "; the previous release did not answer either: " + err.Error()}
	}
	err := e.settleRolledBack(p, why)
	e.endSupervisor(ctx, svc)
	return err
}

func (e Env) settleRolledBack(p Pending, why *contract.UpdateApplyError) error {
	_ = e.markFailed(p.To)
	if err := e.clearPending(); err != nil {
		return err
	}
	return e.record(contract.UpdateApplyStateRolledBack, p.From, p.To, why, "")
}

// BootGuard runs first in `serve`, before the port and the store. It puts a
// snapshot back when a rollback asked for one, and counts the new release's
// starts while its update is pending. It answers exit when this process must
// stop with a non-zero status so the service manager starts the release
// `current` now names, which is the old one again.
func (e Env) BootGuard(exe string) (exit bool, note string, err error) {
	name, kind := Running(e.Layout, exe)
	if kind != install.KindRelease {
		return false, "", nil
	}
	var marker restoreMarker
	if rerr := readJSON(e.restorePath(), &marker); rerr == nil && marker.From == name {
		if err := e.restore(marker.Snapshot); err != nil {
			return false, "", fmt.Errorf("restoring the store snapshot %s: %w", marker.Snapshot, err)
		}
		os.Remove(e.restorePath())
		note = "restored the store from " + marker.Snapshot + " after " + marker.To + " rolled back"
	} else if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return false, "", rerr
	}
	p, ok, err := e.ReadPending()
	if err != nil || !ok || name != p.To {
		return false, note, err
	}
	var why *contract.UpdateApplyError
	switch {
	case p.Phase == phaseRollback:
		why = p.Reason
	case e.now().After(p.Deadline):
		why = &contract.UpdateApplyError{Code: CodeBootGuard, Detail: fmt.Sprintf("%s started after its update's deadline %s", p.To, p.Deadline.Format("2006-01-02T15:04:05Z"))}
	case p.Attempts >= BootAttemptsLimit:
		why = &contract.UpdateApplyError{Code: CodeBootGuard, Detail: fmt.Sprintf("%s started %d times without its update settling", p.To, p.Attempts)}
	}
	if why == nil {
		p.Attempts++
		return false, note, e.writePending(p)
	}
	if err := e.Layout.SwitchCurrent(p.From); err != nil {
		return false, note, fmt.Errorf("switching back to %s: %w", p.From, err)
	}
	if p.NonAdditive && p.Snapshot != "" {
		if err := writeJSON(e.restorePath(), restoreMarker{From: p.From, To: p.To, Snapshot: p.Snapshot}); err != nil {
			return true, note, err
		}
	}
	if err := e.settleRolledBack(p, why); err != nil {
		return true, note, err
	}
	return true, strings.TrimPrefix(note+"; ", "; ") + "rolled back to " + p.From + ": " + why.Detail, nil
}

// pruneReleases keeps current and the PreviousReleasesKeptLimit newest other
// releases. A commit-named source deploy is never removed, nor is anything
// still staged; unpacking leftovers and staged downloads are.
func (e Env) pruneReleases(current, staged string) error {
	entries, err := os.ReadDir(e.Layout.Releases)
	if err != nil {
		return err
	}
	var versions []release.Version
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-") {
			os.RemoveAll(e.Layout.ReleaseDir(name))
			continue
		}
		if !ent.IsDir() || name == current {
			continue
		}
		v, err := release.ParseVersion(name)
		if err != nil {
			continue // a source deploy, or something not ours
		}
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return release.Compare(versions[i], versions[j]) > 0 })
	var errs []error
	for i, v := range versions {
		dir := e.Layout.ReleaseDir(v.String())
		if i < PreviousReleasesKeptLimit || (staged != "" && strings.HasPrefix(staged, dir+string(os.PathSeparator))) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
		}
	}
	// Staged downloads are left alone while another update holds the lock.
	if _, err := os.Stat(e.lockPath()); err == nil {
		return errors.Join(errs...)
	}
	if ents, err := os.ReadDir(e.Layout.Staging); err == nil {
		for _, ent := range ents {
			os.RemoveAll(e.Layout.Staging + string(os.PathSeparator) + ent.Name())
		}
	}
	return errors.Join(errs...)
}
