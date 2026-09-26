package orchestrator

// Handing a Cloud pairing to an assistant on this machine.
//
// A browser signed in to Clawdline Cloud pairs with a machine by showing an
// offer that has to reach that machine by some route other than Cloud: the
// offer is the browser's half of the key exchange, and Cloud only ever carries
// ciphertext. For another machine the route used to be a person copying a
// command and pasting it there. This machine often already reaches that one —
// an ssh alias, a cloud provider's session manager — so the Mac app can hand
// the same command to an assistant here instead, which runs it over that
// access. The offer still travels over the person's own channel, never through
// Cloud.
//
// The task is **detached** and it is admitted through the same door every
// detached task is (Dispatch with Detached set): the same brief checks, the
// same capacity, the same record. What differs is who writes the brief. It is
// written here, from a fixed template, and the caller supplies three values
// that it has already validated — an offer that decodes, a machine id of the
// Cloud's own shape, and a name reduced to one short line. Nothing else a page
// sent reaches the instructions.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// PairingAgentTimeoutMinutes is how long the assistant has. Reaching a host
// and running one command takes a minute; the rest is room for a slow login.
const PairingAgentTimeoutMinutes = 15

// PairingAgentRun is one hand-off: which machine, how it is named, and the
// offer to carry there. Every field has been validated by the caller.
type PairingAgentRun struct {
	TaskID      string
	Assistant   string
	ProjectDir  string
	MachineID   string
	MachineName string
	Offer       string
}

// PairingAgentTitle is the task's title: the result, naming the machine. A
// name that the title rules refuse (a colon, say) falls back to a title that
// does not carry it; the instructions still name the machine.
func PairingAgentTitle(machineName string) string {
	title := truncate("Cloud browser paired with "+machineName, work.OutcomeTitleLimit)
	if work.OutcomeTitleRefusal(title) != "" {
		return "Cloud browser paired with another machine"
	}
	return title
}

// PairingAgentInstructions is the whole brief the assistant reads.
//
// The machine name is placed JSON-quoted, so whatever it holds reads as a
// quoted string and never as a sentence of the brief. The offer is canonical
// base64url — the caller decoded it — so single quotes hold it for any shell.
func PairingAgentInstructions(machineID, machineName, offer string) string {
	quoted, _ := json.Marshal(machineName)
	return fmt.Sprintf(pairingAgentTemplate, quoted, machineID, offer)
}

const pairingAgentTemplate = `# Pair a Clawdline Cloud browser with another machine

From Clawdline Cloud, the person asked for the machine named %[1]s
(Cloud machine id ` + "`%[2]s`" + `) to be paired with their browser, and confirmed it on
this computer. Nobody is watching this tab: do the steps below and nothing else.

1. Reach that machine with the access this computer already has — an ssh host alias, a
   cloud provider's session manager (for example ` + "`aws ssm start-session`" + ` or
   ` + "`aws ssm send-command`" + `), whichever this computer's own configuration (~/.ssh/config,
   known hosts, the cloud CLI's profiles) shows reaches a machine of that name or id.
   Do not install anything, create credentials, or change any configuration to get there.

2. On that machine, as the user that runs Clawdline there, run exactly this one command:

       clawdline cloud pair -offer '%[3]s'

   If ` + "`clawdline`" + ` is not on that machine's PATH, find the binary first — ` + "`command -v clawdline`" + `,
   then ~/.local/bin/clawdline and /usr/local/bin/clawdline, then the ExecStart= line of
   ` + "`systemctl --user cat clawdline`" + ` or ` + "`systemctl cat clawdline`" + ` — and run the same
   command with that full path. Change nothing else on that machine, and do not run the
   command on this computer.

3. Report the ` + "`browser`" + ` and ` + "`machine`" + ` fingerprint lines the command prints, word for
   word, so the person can compare them with what the browser shows.

If the machine cannot be reached, or the command fails, say so with what it printed and
stop. Do not try other ways in that would change anything.

The code in that command is a one-time pairing secret. Do not write it anywhere — a file,
a note, a log, a message — except into that one command.
`

// DispatchPairingAgent writes the brief for one hand-off and admits it as a
// detached task.
//
// The brief is the detached door's own shape: `root.session_id: null` and
// `root.poll_only: true`, `claims: []` because it writes nothing on this
// machine, and `permission_mode: full` because nobody is at the tab to answer
// a prompt — the native confirmation the person gave before this was called
// is the consent.
//
// The inventory receipt a caller's dispatch carries is read here, by the
// broker itself, when the project directory is a repository at all: this
// caller did not read an inventory, and a home directory under version
// control must not turn the hand-off into a `stale_inventory` refusal.
func (b *Broker) DispatchPairingAgent(ctx context.Context, run PairingAgentRun) (Dispatched, error) {
	if !IsTaskID(run.TaskID) {
		return Dispatched{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"task_id must be a lowercase UUID.")
	}
	if strings.TrimSpace(run.Offer) == "" || strings.TrimSpace(run.MachineID) == "" {
		return Dispatched{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"A pairing hand-off names the machine and carries the offer.")
	}
	brief := map[string]any{
		"clawdline_protocol": Protocol,
		"task_id":            run.TaskID,
		"kind":               "cloud-pairing",
		"assistant":          run.Assistant,
		"permission_mode":    "full",
		"project_dir":        run.ProjectDir,
		"title":              PairingAgentTitle(run.MachineName),
		"instructions":       PairingAgentInstructions(run.MachineID, run.MachineName, run.Offer),
		"claims":             []string{},
		"timeout_minutes":    PairingAgentTimeoutMinutes,
		"root":               map[string]any{"session_id": nil, "poll_only": true, "label": "Cloud pairing"},
	}
	if err := b.writeBriefOnce(run.TaskID, brief); err != nil {
		return Dispatched{}, refuse(http.StatusInternalServerError, "internal",
			"Could not write the pairing task file.")
	}
	req := DispatchRequest{TaskID: run.TaskID, Secret: NewSecret(), Detached: true}
	if inv, err := b.ReadInventory(ctx, run.ProjectDir, nil); err == nil {
		req.Generation, req.Offered = inv.Generation, true
	}
	return b.Dispatch(ctx, req)
}
