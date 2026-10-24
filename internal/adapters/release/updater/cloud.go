package updater

import (
	"path/filepath"
	"slices"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/contract"
)

// CloudAdmission captures the executable's installation identity once. In
// particular, a service hard link or current symlink can change underneath a
// process that has not stopped yet; looking Running up again would identify
// that surviving process as the replacement binary.
func (e Env) CloudAdmission(exe string, running Served) func() bool {
	name, kind := Running(e.Layout, exe)
	if filepath.Clean(exe) == filepath.Clean(e.Layout.ServiceBin) && kind != install.KindRelease && kind != install.KindSourceDeploy {
		return func() bool { return false }
	}
	commit, _ := buildOf(e.Layout, name)
	// The stable service hard link may already have been replaced before this
	// function runs. The process's own build identity must agree with disk.
	if (kind == install.KindRelease && running.Version != "" && running.Version != name) ||
		((kind == install.KindRelease || kind == install.KindSourceDeploy) && running.Commit != "" && running.Commit != commit) {
		return func() bool { return false }
	}
	var mu sync.Mutex
	witnessed := false
	target, targetCommit := "", ""
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		if p, pending, err := e.ReadPending(); err != nil || pending {
			witnessed = true
			if err == nil {
				target, targetCommit = p.To, p.ToCommit
			}
			return false
		}
		a, err := e.ReadApply()
		if err != nil || !slices.Contains(contract.UpdateApplyStateValues, a.State) {
			return false
		}
		if witnessed && (a.State != contract.UpdateApplyStateHealthy || a.To != name || target != name || targetCommit == "" || targetCommit != commit) {
			return false
		}
		if kind == install.KindRelease || kind == install.KindSourceDeploy {
			current, currentKind := e.Layout.CurrentRelease()
			if current != name || currentKind != kind {
				return false
			}
			if a.To == name && a.State != contract.UpdateApplyStateHealthy {
				return false
			}
		}
		// Finish removes pending BEFORE recording healthy or rolled_back.
		// The intermediate restarting state must never admit the new binary.
		// An unreadable pending renamed by Finish is also not acceptance.
		if a.Error != nil && a.Error.Code == CodeStateUnreadable {
			return false
		}
		// Recheck after reading the receipt/current, so a newly created pending
		// cannot be mistaken for the previous settled update.
		_, pending, err := e.ReadPending()
		return err == nil && !pending
	}
}
