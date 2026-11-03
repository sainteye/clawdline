package http

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devstack"
	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/adapters/projectlinks"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

// projectSetup is the Projects page's static configuration reading. It stays
// deliberately cheaper and safer than the Links sheet: no git subprocess, no
// port probe, no repository command and no network request are made while the
// Start sheet reads its places.
func projectSetup(cwd, repo string, iconSource icon.Source) *contract.ProjectSetup {
	home, _ := os.UserHomeDir()
	statusDir := filepath.Join(home, ".claude", "statusline-cache")

	out := &contract.ProjectSetup{
		Icon:           contract.ProjectIconSetup(iconSource),
		Deploy:         contract.ProjectDeploySetupMissing,
		DeployActivity: contract.ProjectDeployActivityUnknown,
		Servers:        contract.ProjectServerSetupMissing,
		Sync:           contract.ProjectSyncSetupMissing,
	}
	if repo != "" {
		out.Sync = contract.ProjectSyncSetupReady
	}
	if !strings.HasPrefix(repo, "github.com/") {
		if repo != "" {
			out.Deploy = contract.ProjectDeploySetupNotApplicable
		}
	} else {
		key := strings.TrimPrefix(repo, "github.com/")
		key = strings.Replace(key, "/", "-", 1)
		status := projectlinks.ReadStatus(statusDir, cwd, key, float64(time.Now().UnixNano())/1e9)
		switch {
		case status.Deploy != nil:
			out.Deploy = contract.ProjectDeploySetupReady
			switch status.Deploy.State {
			case "running":
				out.DeployActivity = contract.ProjectDeployActivityRunning
			case "ok":
				out.DeployActivity = contract.ProjectDeployActivitySucceeded
			case "fail":
				out.DeployActivity = contract.ProjectDeployActivityFailed
			}
		case status.DeployQuiet == nil:
			out.Deploy = contract.ProjectDeploySetupMissing
		case status.DeployQuiet.Kind == projectlinks.DeployQuietStateNotDrawn:
			out.Deploy = contract.ProjectDeploySetupReady
			out.DeployActivity = contract.ProjectDeployActivityIdle
		case status.DeployQuiet.Kind == projectlinks.DeployQuietNoFile:
			out.Deploy = contract.ProjectDeploySetupMissing
		default:
			out.Deploy = contract.ProjectDeploySetupAttention
		}
	}

	out.Servers, out.ServerCount = serverSetup(cwd, home)
	out.Unify, out.UnifyCount = unifySetup(cwd)
	return out
}

// serverSetup is the static reading of the nearest .devstack.json: whether
// it is readable and how many servers it declares. Nothing it names is run
// and no port is probed.
func serverSetup(cwd, home string) (contract.ProjectServerSetup, int64) {
	spec, state := devstack.Inspect(cwd, home)
	switch state {
	case devstack.InspectionReady:
		if n := int64(len(spec.Declared)); n > 0 {
			return contract.ProjectServerSetupReady, n
		}
		return contract.ProjectServerSetupEmpty, 0
	case devstack.InspectionUnreadable:
		return contract.ProjectServerSetupAttention, 0
	}
	return contract.ProjectServerSetupMissing, 0
}

// devStackSetup is serverSetup for a Session's info card, which offers to
// start a declared stack or to set one up. The card's button asks the
// Session's own assistant; this daemon still runs none of the file's commands.
func devStackSetup(cwd string) contract.ProjectServerSetup {
	home, _ := os.UserHomeDir()
	state, _ := serverSetup(cwd, home)
	return state
}

// unifySetup is the readiness card's unify row: the same Plan the unify route
// answers with (docs/project-files.md, Unify), which reads the repository
// root and writes nothing. It is computed inline: Plan is bounded by
// projectfiles.MaxScanEntries per skill tree, and on this repository it added
// too little to the places answer to be worth a lazy read (the measurement is
// in docs/project-files.md). A place that cannot be planned is unknown,
// never unified.
func unifySetup(cwd string) (contract.ProjectUnifyStatus, int64) {
	plan, err := projectfiles.Plan(cwd)
	if err != nil {
		return contract.ProjectUnifyStatusUnknown, 0
	}
	if plan.Status != contract.ProjectUnifyStatusDrifting {
		return plan.Status, 0
	}
	return plan.Status, int64(len(plan.Actions) + len(plan.Conflicts))
}
