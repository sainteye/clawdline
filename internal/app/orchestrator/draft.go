package orchestrator

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Reading the brief a caller wrote, and refusing it by name.
//
// The brief arrives as a **file**, not as a request body: the caller writes
// `<task root>/<task id>/task.json` and then posts three fields. That is the
// Swift app's protocol and it is worth keeping, because the file is the same
// bytes the child will read. A body that the broker turned into a file would
// give the two ends two sources for one contract, and the child's copy would be
// the one nobody validated.
//
// The daemon's task root is its own — `<state dir>/tasks` — rather than
// /tmp/.clawdline, which the Swift broker owns. Two brokers writing one
// directory of task ids is the coexistence rule in plan.md §4 broken in the one
// place it would be hardest to notice.

// Limits, all the Swift app's.
const (
	instructionsLimit = 16 * 1024
	claimsMax         = 32
	claimLength       = 1024
	titleLimit        = work.OutcomeTitleLimit
	kindLimit         = 40
	rootLabelLimit    = 120
	timeoutMin        = 1
	timeoutMax        = 240
	timeoutDefault    = 30
)

// draft is task.json as it is read. Every field is optional at the JSON level
// so that a wrong type is a named refusal rather than a decode failure that
// names the whole file.
type draft struct {
	Protocol       *int             `json:"clawdline_protocol"`
	TaskID         string           `json:"task_id"`
	Kind           string           `json:"kind"`
	Assistant      string           `json:"assistant"`
	PermissionMode string           `json:"permission_mode"`
	ProjectDir     string           `json:"project_dir"`
	Title          string           `json:"title"`
	Instructions   string           `json:"instructions"`
	Claims         *[]string        `json:"claims"`
	Isolation      string           `json:"isolation"`
	IsolationBase  string           `json:"isolation_base"`
	Deliverables   []string         `json:"deliverables"`
	TimeoutMinutes *int             `json:"timeout_minutes"`
	Root           *json.RawMessage `json:"root"`
	Model          string           `json:"model"`
	WorkID         json.RawMessage  `json:"work_id"`

	// Graph is a node's place in a task graph (graphs.go, W6).
	//
	// ReasoningEffort is Codex's `model_reasoning_effort`, raw so that a wrong
	// *type* is refused by name rather than failing the decode of the whole
	// file.
	//
	// The other two are accepted by the Swift broker and not by this one yet.
	// They are read only so that their presence can be refused by name: a
	// broker that silently drops `serialize` starts a task the caller asked to
	// wait. Ignoring a field the protocol documents is a quieter way of doing
	// something else.
	Serialize       json.RawMessage `json:"serialize"`
	Graph           json.RawMessage `json:"graph"`
	AttachSession   json.RawMessage `json:"attach_session"`
	ReasoningEffort json.RawMessage `json:"reasoning_effort"`
}

// admitReasoningEffort reads `reasoning_effort`: Codex's
// `model_reasoning_effort`, one of two names, and only on Codex.
//
// **It is carried now rather than refused by name.** It used to be on the list
// of fields this broker reads only to say no to, and the sentence it said named
// the Swift app as the way to dispatch with it — an app that has since been
// retired, so the alternative was dead. Meanwhile the rest of this daemon had
// already been built around the field: `internal/domain/schedule` validates it
// on a stored template with these same two sentences, `Orchestrator.carry`
// keeps it across a schedule's saves, and the published task row has a
// `reasoning_effort` key. A field a schedule may be saved with and a dispatch
// may not fire with is a trap: the save says yes and every run of it says
// `bad_task`.
//
// The two refusals are the Swift app's own, word for word
// (`OrchestratorDraft.swift`), because a caller fixing one brief at a time
// should meet the same sentence on both brokers.
func admitReasoningEffort(raw json.RawMessage, assistant string) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if assistant != projects.AssistantCodex {
		return "", refuse(http.StatusUnprocessableEntity, "bad_task",
			"reasoning_effort is only valid when assistant is codex")
	}
	var name string
	if json.Unmarshal(raw, &name) != nil || !projects.KnownReasoningEffort(name) {
		return "", refuse(http.StatusUnprocessableEntity, "bad_task",
			"reasoning_effort must be one of: "+strings.Join(projects.ReasoningEfforts, ", "))
	}
	return name, nil
}

type draftRoot struct {
	SessionID  *string `json:"session_id"`
	Assistant  *string `json:"assistant"`
	ProjectDir string  `json:"project_dir"`
	Label      string  `json:"label"`
	PollOnly   *bool   `json:"poll_only"`
}

// ReadDraft reads and validates the brief for one task id.
func (b *Broker) ReadDraft(id string) (Record, error) {
	return b.readDraft(id, false)
}

// readDraft is ReadDraft for either door: a brief a caller wrote, or one the
// broker wrote itself from a stored schedule's template (scheduled.go).
func (b *Broker) readDraft(id string, scheduled bool) (Record, error) {
	return b.readDraftAs(id, scheduled, false)
}

// readDraftAs is readDraft for the detached route too: a detached brief is
// admitted only with `root.session_id: null` and `root.poll_only: true`.
func (b *Broker) readDraftAs(id string, scheduled, detached bool) (Record, error) {
	path := filepath.Join(b.Tasks.Path(id), "task.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return Record{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"No readable task.json under "+b.Tasks.Path(id)+"/.")
	}
	var d draft
	if err := json.Unmarshal(body, &d); err != nil {
		return Record{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"No readable task.json under "+b.Tasks.Path(id)+"/.")
	}
	return b.admit(id, d, scheduled, detached)
}

// admit turns a parsed brief into a record, or names the first thing wrong
// with it. The order is the Swift app's, because a caller fixing one refusal at
// a time should meet them in the same sequence on both brokers.
//
// A scheduled brief differs in exactly the two places the Swift app's
// `dispatch(taskID:secret:schedule:)` differs, and nowhere else: it has no
// root — the broker wrote `root.session_id: null` itself, and nobody is waiting
// on the tab — and its claims may be absent, because a stored template carries
// a body no caller is holding to correct (`claimsRequirementRefusal`'s
// `writtenForThisDispatch`). Absent stays absent: the record says "not
// declared" and the dispatch warns, rather than reading it as "writes nothing".
// Every other field meets the same checks a caller's brief meets, so a template
// asking for something this broker does not do is refused by name.
func (b *Broker) admit(id string, d draft, scheduled, detached bool) (Record, error) {
	bad := func(msg string) (Record, error) {
		return Record{}, refuse(http.StatusUnprocessableEntity, "bad_task", msg)
	}
	if d.Protocol == nil || *d.Protocol != Protocol {
		return bad("clawdline_protocol must be 1")
	}
	if d.TaskID != id {
		return bad("task_id must be a lowercase UUID and match the dispatch")
	}
	if d.Assistant != "claude" && d.Assistant != "codex" {
		return bad("assistant must be claude or codex")
	}
	if !usableDir(d.ProjectDir) {
		return bad("project_dir must be an absolute path to a directory")
	}
	instructions := strings.TrimSpace(d.Instructions)
	if instructions == "" || len(d.Instructions) > instructionsLimit {
		return bad("instructions must be non-empty and at most 16 KiB")
	}
	var claims []string
	switch {
	case d.Claims != nil:
		admitted, err := admitClaims(*d.Claims)
		if err != nil {
			return bad(err.Error())
		}
		claims = admitted
	case !scheduled:
		return Record{}, refuse(http.StatusUnprocessableEntity, "claims_required", claimsRequiredMessage)
	}
	isolation := d.Isolation
	if isolation == "" {
		isolation = IsolationNone
	}
	if isolation != IsolationNone && isolation != IsolationWorktree {
		return bad("isolation must be one of: none, worktree")
	}
	permission := d.PermissionMode
	if permission == "" {
		permission = "full"
	}
	switch permission {
	case "ask", "edits", "full":
	default:
		return bad("permission_mode must be one of: ask, edits, full")
	}
	timeout := timeoutDefault
	if d.TimeoutMinutes != nil {
		timeout = *d.TimeoutMinutes
	}
	if timeout < timeoutMin || timeout > timeoutMax {
		return bad("timeout_minutes must be 1…240")
	}

	var root *RootRef
	switch {
	case scheduled:
	case detached:
		if err := admitDetached(d.Root); err != nil {
			return Record{}, err
		}
	default:
		admitted, err := admitRoot(d.Root)
		if err != nil {
			return Record{}, err
		}
		root = admitted
	}
	for name, raw := range map[string]json.RawMessage{
		"serialize": d.Serialize, "attach_session": d.AttachSession,
	} {
		if len(raw) > 0 && string(raw) != "null" {
			return bad(name + " is not supported by this broker yet; dispatch without it")
		}
	}
	model := strings.TrimSpace(d.Model)
	if model != "" && !modelName(model) {
		return bad("model must be a model name: lower-case letters, digits, . _ -, at most 64 characters")
	}
	effort, err := admitReasoningEffort(d.ReasoningEffort, d.Assistant)
	if err != nil {
		return Record{}, err
	}
	workID, err := admitWorkID(d.WorkID)
	if err != nil {
		return Record{}, err
	}
	graph, err := admitGraph(d.Graph)
	if err != nil {
		return Record{}, err
	}

	kind := truncate(strings.TrimSpace(d.Kind), kindLimit)
	if kind == "" {
		kind = "custom"
	}
	title := strings.TrimSpace(d.Title)
	if reason := work.OutcomeTitleRefusal(title); reason != "" {
		return bad("title: " + reason)
	}

	return Record{
		Protocol:        Protocol,
		ID:              id,
		Kind:            kind,
		Assistant:       d.Assistant,
		PermissionMode:  permission,
		Claims:          claims,
		Isolation:       isolation,
		ProjectDir:      filepath.Clean(d.ProjectDir),
		Title:           title,
		Instructions:    d.Instructions,
		Deliverables:    d.Deliverables,
		TimeoutMinutes:  timeout,
		Root:            root,
		Model:           model,
		ReasoningEffort: effort,
		WorkID:          workID,
		Graph:           graph,
		State:           StateQueued,
	}, nil
}

// admitWorkID reads the work item a dispatch names (D36). Absent or null
// names none, which is allowed: the task is then only its root's to-do. A
// value that is there and is not a work id is refused by name — a broker that
// dropped it would bind the work to nothing while its caller believed
// otherwise.
func admitWorkID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil || !IsTaskID(v) {
		return "", refuse(http.StatusUnprocessableEntity, "bad_task",
			"work_id must be a lowercase UUID naming a work item, or left out")
	}
	return v, nil
}

// modelName is SessionLaunchPolicy.modelName: `[a-z0-9._-]`, 1…64, never
// opening with `-`. No string it admits holds a character a shell reads.
func modelName(v string) bool {
	if v == "" || len(v) > 64 || strings.HasPrefix(v, "-") {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// claimsRequiredMessage is the Swift app's, verbatim: it is the one refusal
// that has to teach the protocol, because a caller that omits claims does not
// know there was a decision to make.
const claimsRequiredMessage = `claims is required: the relative paths under project_dir this task may write. ` +
	`Name the files when you know them, the directories when you do not, or send "claims": [] to declare that ` +
	`this task writes nothing. An isolated task declares the same list — its lease is dropped for the private ` +
	`checkout and the paths are kept as its landing write set. Leave out what a repository guard rewrites for ` +
	`you, such as a ratcheted line count: that belongs to whoever lands the change, not to this task's lease.`

// admitClaims checks the reservation itself.
//
// A claim is a **relative** path under project_dir. An absolute one reserves
// something outside the repository the arbitration is about, and `..` reserves
// a place the caller did not name; neither is refused as a matter of taste —
// both would make an overlap check compare two different things.
func admitClaims(in []string) ([]string, error) {
	if len(in) > claimsMax {
		return nil, errString("claims must hold at most 32 paths")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		if raw == "" || len(raw) > claimLength {
			return nil, errString("each claim must be 1…1024 characters")
		}
		if strings.HasPrefix(raw, "/") {
			return nil, errString("claims are relative to project_dir: " + raw)
		}
		if strings.ContainsRune(raw, 0) {
			return nil, errString("a claim may not contain NUL")
		}
		for _, part := range strings.Split(raw, "/") {
			if part == ".." {
				return nil, errString("a claim may not walk out of project_dir: " + raw)
			}
		}
		if seen[raw] {
			return nil, errString("claims may not repeat a path: " + raw)
		}
		seen[raw] = true
		out = append(out, raw)
	}
	return out, nil
}

// admitRoot resolves the owner half of the brief.
//
// The two refusals below are long because each of them is a fork somebody took
// wrongly: a poll-only body on this route means the caller wanted unattended
// automation and reached for the owned-child route, and a missing session id
// means the child would finish with nobody to tell.
func admitRoot(raw *json.RawMessage) (*RootRef, error) {
	if raw == nil {
		return nil, refuse(http.StatusUnprocessableEntity, "root_session_required", rootRequiredMessage)
	}
	var d draftRoot
	if err := json.Unmarshal(*raw, &d); err != nil {
		return nil, refuse(http.StatusUnprocessableEntity, "bad_task", "root must be an object")
	}
	pollOnly := d.PollOnly != nil && *d.PollOnly
	sessionID := ""
	if d.SessionID != nil {
		sessionID = strings.TrimSpace(*d.SessionID)
	}
	if pollOnly {
		if sessionID != "" {
			return nil, refuse(http.StatusUnprocessableEntity, "bad_task",
				"root.poll_only is only valid when root.session_id is null")
		}
		return nil, refuse(http.StatusUnprocessableEntity, "detached_route_required", detachedRouteMessage)
	}
	if sessionID == "" {
		return nil, refuse(http.StatusUnprocessableEntity, "root_session_required", rootRequiredMessage)
	}
	if d.Assistant == nil || (*d.Assistant != "claude" && *d.Assistant != "codex") {
		return nil, refuse(http.StatusUnprocessableEntity, "root_assistant_required", rootAssistantMessage)
	}
	return &RootRef{
		SessionID:  sessionID,
		Assistant:  *d.Assistant,
		ProjectDir: d.ProjectDir,
		Label:      truncate(d.Label, rootLabelLimit),
	}, nil
}

// admitDetached is the detached route's half of the root check, the Swift
// app's `detached_task_required`: that route runs unattended automation only,
// and a brief naming an owner, or not saying it polls, is an owned child sent
// to the wrong door.
func admitDetached(raw *json.RawMessage) error {
	wrong := refuse(http.StatusUnprocessableEntity, "detached_task_required",
		"POST /v1/orchestrator/detached-tasks runs unattended automation only: task.json must carry "+
			"root.session_id null and root.poll_only true. An owned child goes through POST /v1/orchestrator/tasks.")
	if raw == nil {
		return wrong
	}
	var d draftRoot
	if err := json.Unmarshal(*raw, &d); err != nil {
		return refuse(http.StatusUnprocessableEntity, "bad_task", "root must be an object")
	}
	if d.SessionID != nil && strings.TrimSpace(*d.SessionID) != "" {
		return wrong
	}
	if d.PollOnly == nil || !*d.PollOnly {
		return wrong
	}
	return nil
}

const (
	rootRequiredMessage = "root.session_id is required for API dispatch so the child can be grouped, closed and " +
		"reported back to its owner. Resolve this interactive Root with GET /v1/orchestrator/whoami, then resend " +
		"with its current process-bound conversation id and assistant."
	rootAssistantMessage = "root.assistant is required when root.session_id names an owner; send claude or codex. " +
		"The historical Claude fallback exists only in persisted legacy compatibility readers, not ownership decisions."
	detachedRouteMessage = "POST /v1/orchestrator/tasks creates an owned Child and never accepts root.poll_only. " +
		"Resolve this interactive Root with GET /v1/orchestrator/whoami and resend with root.session_id plus " +
		"root.assistant. Only unattended automation may use poll-only, through POST /v1/orchestrator/detached-tasks."
)

// IsTaskID is the id shape the protocol accepts: 36 characters of lowercase
// hex and hyphens. It is deliberately not a UUID parse — the Swift app accepts
// this alphabet, and an id it minted must not be refused here.
func IsTaskID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for _, r := range v {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || r == '-') {
			return false
		}
	}
	return true
}

// IsTaskSecret is 64 lowercase hex characters, and nothing else.
func IsTaskSecret(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, r := range v {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func usableDir(path string) bool {
	// filepath.IsAbs rather than a leading slash: `C:\work\repo` is an
	// absolute path, and testing the spelling refused every Windows brief
	// with a message saying the path was not absolute when it was.
	if path == "" || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// truncate cuts to a rune count, never to a byte count: cutting UTF-8 by bytes
// produces a replacement character in somebody's title.
func truncate(v string, limit int) string {
	if utf8.RuneCountInString(v) <= limit {
		return v
	}
	runes := []rune(v)
	return string(runes[:limit])
}

type errString string

func (e errString) Error() string { return string(e) }
