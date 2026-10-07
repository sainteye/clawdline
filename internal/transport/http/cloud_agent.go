package http

// `POST /v1/cloud/pairing/agent` — carry a browser's pairing offer to another
// machine through an assistant on this one.
//
// The Cloud tab in the Mac app shows a machine it is not paired with, and the
// person asks this Mac to finish the pairing there. This Mac usually reaches
// that machine already (an ssh alias, a cloud provider's session manager), so
// the offer goes over that channel — the person's own — and never through
// Cloud, which is the rule the whole pairing design keeps: Cloud carries
// ciphertext, and the offer is the half of a key exchange it must not see.
//
// **This machine's own token only**, like every pairing route here: the answer
// starts an assistant that runs a command on another machine. The shell asks
// the person in a native dialog before it calls; this route is not reachable by
// a paired phone, a Cloud viewer or a page.
//
// The page supplies three values and no prose. Each is checked before it is
// used: the offer is decoded exactly as `/v1/cloud/pairing/offer` decodes it,
// the machine id must be the Cloud's own shape, and the name is cut to one
// short line and then placed JSON-quoted. The instructions are the fixed
// template in `orchestrator.PairingAgentInstructions`. The offer is never
// logged.

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// cloudMachineID is the shape the Cloud gives a machine id, and the only one
// this route puts into a brief.
var cloudMachineID = regexp.MustCompile(`^mac_[A-Za-z0-9_-]{1,200}$`)

// pairAgentDirect is the answer when the machine named is this one: there is
// nothing to reach, so the offer is completed here, as `/offer` would.
type pairAgentDirect struct {
	Mode string `json:"mode"`
	cloudtransport.PairingState
}

func (s *Server) cloudPairingAgentRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A pairing is handed to this machine's assistant with POST.")
		return
	}
	pairing, ok := s.cloudPairing(w)
	if !ok {
		return
	}
	var body struct {
		Offer       string `json:"offer"`
		MachineID   string `json:"machine_id"`
		MachineName string `json:"machine_name"`
	}
	// The same eight KiB the offer route reads, for the same reason.
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That request body is not readable JSON.")
		return
	}
	offer := strings.TrimSpace(body.Offer)
	if offer == "" || len(offer) > 4096 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "A pairing code is 1 to 4096 characters.")
		return
	}
	if _, err := pairing.CheckOffer(offer); err != nil {
		writeRawAuthRefusal(w, http.StatusBadRequest, "bad_offer",
			"That is not a pairing code a browser on this account is showing now: "+err.Error())
		return
	}
	machineID := strings.TrimSpace(body.MachineID)
	if !cloudMachineID.MatchString(machineID) {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_machine", "machine_id is not a Clawdline Cloud machine id.")
		return
	}
	name := pairAgentMachineName(body.MachineName)
	if name == "" {
		name = machineID
	}

	if machineID == pairing.MachineID {
		state, err := pairing.Complete(r.Context(), offer)
		if err != nil {
			writeCloudPairingError(w, err)
			return
		}
		writeJSON(w, pairAgentDirect{Mode: "direct", PairingState: state})
		return
	}

	checked, _ := pairing.CheckOffer(offer)
	projectDir, err := orchestrator.PairingAgentDir(s.cfg.Dir)
	if err != nil {
		log.Printf("cloud: could not make the folder a pairing hand-off starts in: %v", err)
		writeAuthRefusal(w, http.StatusInternalServerError, "no_folder",
			"This machine could not make the folder the assistant starts in.")
		return
	}
	run := orchestrator.PairingAgentRun{
		TaskID:      orchestrator.NewUUID(),
		Assistant:   pairAgentAssistant(),
		ProjectDir:  projectDir,
		MachineID:   machineID,
		MachineName: name,
		Offer:       offer,
	}
	trust, trustReason := pairAgentTrust(run.Assistant, projectDir)
	dispatch := s.pairAgent
	if dispatch == nil {
		if s.broker == nil {
			writeAuthRefusal(w, http.StatusServiceUnavailable, "broker_unavailable",
				"This daemon cannot start an assistant task.")
			return
		}
		dispatch = s.broker.DispatchPairingAgent
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	out, err := dispatch(ctx, run)
	if err != nil {
		log.Printf("cloud: handing the pairing with %s to an assistant was refused: %v", machineID, err)
		writeBrokerError(w, err)
		return
	}
	log.Printf("cloud: handed the pairing with %s to %s task %s", machineID, run.Assistant, out.Record.ID)
	answer := map[string]any{"mode": "agent", "task_id": out.Record.ID, "trust": trust, "expires_at": checked.ExpiresAt}
	if trustReason != "" {
		answer["trust_reason"] = trustReason
	}
	writeJSON(w, answer)
}

// Whether the assistant's first screen will be a composer.
const (
	// pairAgentTrustRecorded: Claude Code has the folder as trusted, now or
	// from before.
	pairAgentTrustRecorded = "recorded"
	// pairAgentTrustNotRecorded: it could not be written, so the new tab may
	// be asking; the card says so.
	pairAgentTrustNotRecorded = "not_recorded"
	// pairAgentTrustPerLaunch: Codex, answered on its command line for this
	// one run (orchestrator trustArgs), with nothing written.
	pairAgentTrustPerLaunch = "per_launch"
)

// pairAgentTrust records the hand-off's own folder as one Claude Code trusts.
//
// The person pressed Start in the native sheet that names the machine and the
// command, which is the answer to the question Claude Code would otherwise ask
// in a tab nobody is looking at. It is the folder PairingAgentDir made — never
// the home directory, never a project, never a worktree. A failure does not
// stop the hand-off; it is said, with why.
func pairAgentTrust(assistant, dir string) (string, string) {
	if assistant != projects.AssistantClaude {
		return pairAgentTrustPerLaunch, ""
	}
	config, err := projects.ClaudeConfigPath()
	if err == nil {
		_, err = projects.TrustClaudeProject(config, dir)
	}
	if err != nil {
		log.Printf("cloud: the pairing hand-off's folder was not recorded as trusted by Claude Code: %v", err)
		return pairAgentTrustNotRecorded, err.Error()
	}
	return pairAgentTrustRecorded, ""
}

// `GET /v1/cloud/pairing/agent/<task_id>` — where a hand-off is, for the card.
//
// The shell polls this after a hand-off and passes each change to the Cloud
// page, so the card can say the AI is starting, working, done, or why it
// stopped. This machine's own token only, like the POST; it answers tasks of
// kind cloud-pairing and nothing else, so it is not a way to read other work;
// and it answers the state and the task's own sentence, never the brief.
func (s *Server) cloudPairingAgentTaskRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A pairing hand-off is read with GET.")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/cloud/pairing/agent/")
	if !orchestrator.IsTaskID(id) {
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No pairing hand-off has that id.")
		return
	}
	read := s.pairAgentTask
	if read == nil {
		if s.broker == nil {
			writeAuthRefusal(w, http.StatusServiceUnavailable, "broker_unavailable",
				"This daemon has no assistant tasks to read.")
			return
		}
		read = func(ctx context.Context, id string) (orchestrator.Record, error) {
			rec, _, err := s.broker.Record(ctx, id)
			return rec, err
		}
	}
	rec, err := read(r.Context(), id)
	if err != nil || rec.Kind != orchestrator.PairingAgentKind {
		// Another kind of task reads as no task at all.
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No pairing hand-off has that id.")
		return
	}
	writeJSON(w, orchestrator.PairingAgentProgressOf(rec))
}

// pairAgentMachineName is a machine's name reduced to what may sit in a brief:
// one line, no control or format characters, runs of space collapsed, at most
// 64 characters. It is quoted with JSON where it is placed; this only keeps it
// short and on one line.
func pairAgentMachineName(raw string) string {
	out := make([]rune, 0, 64)
	space := false
	for _, r := range raw {
		if len(out) >= 64 {
			break
		}
		if unicode.Is(unicode.Cf, r) {
			// Zero-width and direction marks change how a name reads
			// without being any of it.
			continue
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			space = len(out) > 0
			continue
		}
		if space {
			out = append(out, ' ')
			space = false
			if len(out) >= 64 {
				break
			}
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

// pairAgentAssistant is this machine's default assistant: the first one
// installed, in the order the assistants route answers, which is the one the
// console's own forms pick when nothing else was chosen. Claude when neither
// reads as installed, so the dispatch refuses by name rather than here.
func pairAgentAssistant() string {
	now := time.Now()
	for _, a := range limits.Assistants {
		if quotaReader().Quota(a, now).Installed {
			return a
		}
	}
	return limits.Assistants[0]
}
