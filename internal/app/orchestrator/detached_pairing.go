package orchestrator

// Handing a Cloud pairing to an assistant on this machine.
//
// A browser signed in to Clawdline Cloud shows an offer for the target
// machine. A paired helper receives that offer in an encrypted Cloud command,
// then uses access it already has (an ssh alias or a cloud provider's session
// manager) to run the pairing command on the target. The relay sees only
// ciphertext; the helper's assistant receives the offer in a fixed brief.
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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// PairingAgentKind is the kind every hand-off's task has, and the only kind
// the card's progress route answers for.
const PairingAgentKind = "cloud-pairing"

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
   ` + "`systemctl --user cat clawdline-next`" + ` or ` + "`systemctl cat clawdline-next`" + ` — and run the same
   command with that full path. Change nothing else on that machine, and do not run the
   command on this computer.

3. Report the ` + "`browser`" + ` and ` + "`machine`" + ` fingerprint lines the command prints, word for
   word. In result.json, also include this structured field, copying only fingerprints
   actually printed by the successful command on the target over the existing access:

       "pairing_comparison": {"machine_id": "%[2]s", "viewer_fingerprint": "<browser line>", "machine_fingerprint": "<machine line>"}

   Never derive the machine fingerprint from Cloud, the browser, or the offer. Do not
   invent, complete, or substitute values. If either line is missing, omit the field;
   the browser will keep manual comparison. Do not operate the browser yourself.

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
// caller did not read an inventory, and a state directory that happens to sit
// inside a repository must not turn the hand-off into a `stale_inventory`
// refusal.
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
		"kind":               PairingAgentKind,
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

// PairingAgentDirName is the folder under this machine's Clawdline state
// directory that a pairing hand-off's assistant starts in.
const PairingAgentDirName = "pairing-agent"

// PairingAgentDir makes, or makes private again, the one folder a pairing
// hand-off's assistant starts in: `<state>/pairing-agent`, mode 0700, and
// answers its absolute path.
//
// It used to be the home directory. That was far wider than a task that reads
// the machine's own ssh and cloud configuration by path and writes nothing
// here, and it was a folder Claude Code asks about before it draws a composer:
// the person who used it for real had to find the new tab and press "Yes, I
// trust this folder" before anything moved. A folder of Clawdline's own, empty
// and used for nothing else, is one the daemon can record as trusted without
// that answer reaching anything of the person's (the caller does, through
// projects.TrustClaudeProject; Codex is answered per launch by trustArgs).
func PairingAgentDir(stateDir string) (string, error) {
	if !filepath.IsAbs(stateDir) {
		abs, err := filepath.Abs(stateDir)
		if err != nil {
			return "", err
		}
		stateDir = abs
	}
	dir := filepath.Join(stateDir, PairingAgentDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	if info.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// PairingAgentProgress is what the Cloud card may know about a hand-off while
// it runs: a word for where the task is, and the task's own sentence once it
// ended. Never the brief, which holds the offer.
type PairingAgentProgress struct {
	// State is one of starting, dialog, working, done, failed.
	State        string                  `json:"state"`
	Summary      string                  `json:"summary,omitempty"`
	FailedReason string                  `json:"failed_reason,omitempty"`
	Comparison   *PairingAgentComparison `json:"comparison,omitempty"`
}

// PairingAgentComparison is the assistant's observation carried by the trusted helper,
// bound by the helper to the exact offer. It is not an independent remote-execution audit.
type PairingAgentComparison struct {
	MachineID          string `json:"machine_id"`
	OfferSHA256        string `json:"offer_sha256"`
	ViewerFingerprint  string `json:"viewer_fingerprint"`
	MachineFingerprint string `json:"machine_fingerprint"`
}

var pairingFingerprint = regexp.MustCompile(`^[A-Z2-7]{4}(-[A-Z2-7]{4}){3}$`)

func pairingAgentComparison(r Record) *PairingAgentComparison {
	if r.Kind != PairingAgentKind || r.Result == nil {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.Result.PairingComparison, &fields) != nil || len(fields) != 3 {
		return nil
	}
	var c PairingAgentComparison
	if json.Unmarshal(fields["machine_id"], &c.MachineID) != nil ||
		json.Unmarshal(fields["viewer_fingerprint"], &c.ViewerFingerprint) != nil ||
		json.Unmarshal(fields["machine_fingerprint"], &c.MachineFingerprint) != nil ||
		!pairingFingerprint.MatchString(c.ViewerFingerprint) || !pairingFingerprint.MatchString(c.MachineFingerprint) {
		return nil
	}
	// The caller validated this ID before writing the fixed brief.
	if !strings.Contains(r.Instructions, "(Cloud machine id `"+c.MachineID+"`)") || c.MachineID == "" {
		return nil
	}
	m := pairingOfferInBrief.FindStringSubmatch(r.Instructions)
	if m == nil || m[1] == "" {
		return nil
	}
	c.OfferSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(m[1])))
	return &c
}

// Pairing hand-off progress words.
const (
	PairingAgentStarting = "starting"
	PairingAgentDialog   = "dialog"
	PairingAgentWorking  = "working"
	PairingAgentDone     = "done"
	PairingAgentFailed   = "failed"
)

// pairingOfferInBrief finds the offer in a hand-off's instructions, so that a
// summary that repeats it can have it taken out.
var pairingOfferInBrief = regexp.MustCompile(`-offer '([^']+)'`)

// PairingAgentProgressOf maps a hand-off's record onto the card's words.
//
// The child's summary is its own words and it was told never to write the
// offer; it is read here as if it might have, and the offer is cut out of it
// before it leaves, as is anything past 600 characters.
func PairingAgentProgressOf(r Record) PairingAgentProgress {
	scrub := func(s string) string {
		s = strings.TrimSpace(s)
		if m := pairingOfferInBrief.FindStringSubmatch(r.Instructions); m != nil && m[1] != "" {
			s = strings.ReplaceAll(s, m[1], "…")
		}
		return truncate(s, 600)
	}
	switch r.State {
	case StateSuccess:
		summary := ""
		if r.Result != nil {
			summary = scrub(r.Result.Summary)
		}
		return PairingAgentProgress{State: PairingAgentDone, Summary: summary, Comparison: pairingAgentComparison(r)}
	case StateFailure, StateTimeout, StateCancelled, StateSpawnFailed:
		reason := ""
		switch {
		case r.Result != nil && strings.TrimSpace(r.Result.Summary) != "":
			reason = r.Result.Summary
		case strings.TrimSpace(r.Verdict) != "":
			reason = r.Verdict
		case strings.TrimSpace(r.SpawnError) != "":
			reason = r.SpawnError
		default:
			reason = string(r.State)
		}
		return PairingAgentProgress{State: PairingAgentFailed, FailedReason: scrub(reason)}
	}
	if !r.AwaitingDialogSince.IsZero() {
		return PairingAgentProgress{State: PairingAgentDialog}
	}
	if !r.AcceptedAt.IsZero() {
		return PairingAgentProgress{State: PairingAgentWorking}
	}
	return PairingAgentProgress{State: PairingAgentStarting}
}
