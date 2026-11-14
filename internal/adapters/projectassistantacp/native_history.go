package projectassistantacp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const nativeHistoryPolicy = "claude-owned-history.v1"

var nativeSessionUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type nativeHistory struct{ Root, Config, Owners, Identity string }

// An execution generation is deliberately absent: a safe new process must see
// the same native history. A changed privacy/instruction snapshot gets another
// directory, so the old opaque context is not readable by the new process.
func historyForRequest(state string, q app.OpenRequest) (nativeHistory, error) {
	f := q.Binding.Fence
	if err := pa.CheckFence(f, f); err != nil {
		return nativeHistory{}, err
	}
	if f.Key != q.Bot.Key || f.Project != q.Bot.Project || f.ConversationID != q.Bot.ConversationID || q.Binding.Agent != q.Bot.Agent || q.Binding.Revoked || q.Binding.InstructionRevision != q.Bot.Instructions.Revision || q.Binding.MemoryRevision < 0 || q.Binding.ForgottenThrough < 0 {
		return nativeHistory{}, refusal(pa.ScopeDenied, "native_history_scope")
	}
	scope, _ := json.Marshal(struct {
		Key     pa.BotKey
		Project pa.ProjectScope
	}{f.Key, f.Project})
	scopeSum := sha256.Sum256(scope)
	// Memory that only gained or changed entries keeps the same history, as
	// in Codync; forgetting or new instructions give it another directory.
	epoch, _ := json.Marshal(struct {
		Policy, Conversation    string
		Forgotten, Instructions int64
	}{nativeHistoryPolicy, f.ConversationID, q.Binding.ForgottenThrough, q.Binding.InstructionRevision})
	sum := sha256.Sum256(epoch)
	identity := hex.EncodeToString(scopeSum[:]) + "/" + hex.EncodeToString(sum[:])
	root := filepath.Join(state, "project-assistants", "native", filepath.FromSlash(identity))
	return nativeHistory{Root: root, Config: filepath.Join(root, "config"), Owners: filepath.Join(root, "owners"), Identity: identity}, nil
}

// Reject links in every feature-owned component. No native ID or directory is
// taken from an SDK history listing, prompt, display name or user conversation.
func privateDirectory(state, path string) error {
	rel, e := filepath.Rel(state, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return refusal(pa.ScopeDenied, "native_directory")
	}
	current := state
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if e = os.Mkdir(current, 0700); e != nil && !os.IsExist(e) {
			return refusal(pa.Invalid, "native_directory")
		}
		info, e := os.Lstat(current)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return refusal(pa.ScopeDenied, "native_directory_link")
		}
	}
	return nil
}

func (h nativeHistory) verifyID(id string) error {
	if !nativeSessionUUID.MatchString(id) {
		return refusal(pa.ScopeDenied, "native_session_id")
	}
	info, e := os.Lstat(filepath.Join(h.Owners, id+".json"))
	if e != nil || !info.Mode().IsRegular() {
		return refusal(pa.ResumeUnsupported, "native_history_owner")
	}
	data, e := os.ReadFile(filepath.Join(h.Owners, id+".json"))
	if e != nil {
		return refusal(pa.ResumeUnsupported, "native_history_owner")
	}
	var record struct{ Policy, Identity, SessionID string }
	if json.Unmarshal(data, &record) != nil || record.Policy != nativeHistoryPolicy || record.Identity != h.Identity || record.SessionID != id {
		return refusal(pa.ScopeDenied, "native_history_owner")
	}
	return nil
}
func (h nativeHistory) recordID(id string) error {
	if !nativeSessionUUID.MatchString(id) {
		return refusal(pa.Invalid, "native_session_id")
	}
	data, _ := json.Marshal(struct{ Policy, Identity, SessionID string }{nativeHistoryPolicy, h.Identity, id})
	f, e := os.OpenFile(filepath.Join(h.Owners, id+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(e) {
		return h.verifyID(id)
	}
	if e != nil {
		return refusal(pa.Invalid, "native_history_owner")
	}
	_, e = f.Write(append(data, '\n'))
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return refusal(pa.Invalid, "native_history_owner")
	}
	return nil
}
