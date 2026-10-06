package install

import (
	"fmt"
	"strconv"
)

// What `setup` finds already on the machine, and whether it may go on. The
// rule is that nothing the person set up some other way — a daemon started in
// a terminal, the app's own daemon, a source deploy — is changed without them
// asking for it.

// Existing is the machine as setup found it.
type Existing struct {
	// Port is the port setup is about to install on, and PortAnswers whether
	// something already listens there.
	Port        int
	PortAnswers bool
	// Service is the state directory's service.json, when there is one.
	Service *ServiceFile
	// ServiceName is the unit or label setup would install, and
	// ServiceInstalled whether its unit or plist file is already there.
	ServiceName      string
	ServiceInstalled bool
	// Current is what `current` points at, and its kind.
	Current     string
	CurrentKind Kind
}

// Refusal is why setup stopped: a sentence and what to do about it.
type Refusal struct {
	Code   string
	Detail string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Detail }

// The codes a Refusal carries.
const (
	CodePortHeld          = "port_held_by_other_daemon"
	CodeSourceDeploy      = "source_deploy_needs_adopt"
	CodeSignature         = "release_not_verified"
	CodeTmuxMissing       = "tmux_missing"
	CodeUnsupported       = "platform_unsupported"
	CodeServiceFailed     = "service_failed"
	CodeHealthFailed      = "health_check_failed"
	CodeTreeMismatch      = "release_tree_mismatch"
	CodeChannelMismatch   = "channel_mismatch"
	CodeNoUserManager     = "no_user_service_manager"
	CodeSessionCheck      = "session_check_failed"
	CodeNotInstalled      = "not_installed"
	CodeCurrentNotRelease = "current_not_a_release"
)

// OursOnPort is whether the daemon answering on the port is this layout's own
// service: service.json names the same service and port, or — for a source
// deploy being adopted — the unit file is already ours.
func (e Existing) OursOnPort(adopt bool) bool {
	if e.Service != nil && e.Service.Name == e.ServiceName && e.Service.Port == e.Port {
		return true
	}
	return adopt && e.ServiceInstalled
}

// Decide is whether setup may install over what it found.
func (e Existing) Decide(adopt bool) error {
	if e.CurrentKind == KindSourceDeploy && !adopt {
		return &Refusal{Code: CodeSourceDeploy, Detail: fmt.Sprintf(
			"this machine already runs a source deploy (current -> releases/%s), which tools/deploy-linux-user.sh "+
				"keeps in step with the hosted console. Setup leaves it as it is. To make it a release install instead "+
				"— it then follows signed releases and stops tracking the hosted BUILD.json — run setup again with --adopt; "+
				"running tools/deploy-linux-user.sh later makes it a source deploy again", e.Current)}
	}
	if e.PortAnswers && !e.OursOnPort(adopt) {
		return &Refusal{Code: CodePortHeld, Detail: fmt.Sprintf(
			"another Clawdline daemon (or some other program) already answers on port %d, and it is not the service "+
				"this installer manages. Two daemons over one state directory would be two writers, so nothing was changed. "+
				"Quit it first — the Clawdline Next app from its menu, or the terminal running `clawdline serve` with Ctrl-C — "+
				"and run the installer again, or install on another port with --port <n>", e.Port)}
	}
	return nil
}

// PortFlag is how a refusal names the port flag in a suggested command.
func PortFlag(port int) string {
	if port == DefaultPort {
		return ""
	}
	return " --port " + strconv.Itoa(port)
}
