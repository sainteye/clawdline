package projectassistant

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// A team charter is the person's own Markdown for one Project: who does what,
// how work flows, and what must be asked of the person. Every Bot of the
// Project follows it, and where it conflicts with a Bot's instructions or
// memory the charter wins. Each save is a new immutable version; the current
// charter is the highest version.
const (
	// MaxCharterTextBytes bounds one charter version's text.
	MaxCharterTextBytes = 32 << 10
	// MaxCharterVersions bounds the versions one Project keeps. Saving past
	// it removes the oldest version; the current one is never removed.
	MaxCharterVersions = 200
	// MaxCharterProposals bounds a Project's pending lead proposals. A new
	// proposal past it is refused until the person applies or dismisses one.
	MaxCharterProposals = 20
	// MaxCharterReasonBytes bounds a version's reason or a proposal summary.
	MaxCharterReasonBytes = 500
	// MaxCharterBodyBytes bounds one charter write's HTTP body: the text,
	// JSON-escaped, and the fields around it.
	MaxCharterBodyBytes = 256 << 10
	// MaxCharterRequestBytes bounds the person's words in a change request.
	MaxCharterRequestBytes = 4 << 10
	// MaxCharterProjects bounds the charters one machine lists.
	MaxCharterProjects = 512
)

// CharterAuthor says how a version came to be.
type CharterAuthor string

const (
	CharterByPerson       CharterAuthor = "person"
	CharterByLeadProposal CharterAuthor = "lead_proposal"
	CharterByRestore      CharterAuthor = "restore"
	CharterByTemplate     CharterAuthor = "template"
	// CharterByRemoval marks the Project's charter being taken away. Its
	// text is empty; while it is the newest version the Project has no
	// charter and reads as version 0. Its number still counts, so a charter
	// made later gets a version no Bot heard before.
	CharterByRemoval CharterAuthor = "removed"
)

// CharterVersion is one immutable saved charter. Text and Playbooks are
// empty in a history listing and set when one version is read.
type CharterVersion struct {
	Version   int64             `json:"version"`
	Author    CharterAuthor     `json:"author"`
	Reason    string            `json:"reason"`
	CreatedAt time.Time         `json:"created_at"`
	Text      string            `json:"text,omitempty"`
	Playbooks []CharterPlaybook `json:"playbooks,omitempty"`
	// Workflow is the version's team workflow; nil when it has none.
	Workflow *CharterWorkflow `json:"workflow,omitempty"`
	// TextVersion is the version at which the text last changed, and
	// Deliveries the Bots whose playbooks changed after it, each with the
	// version that changed them. Both are set when the version is saved, so
	// a prompt never reads history; see DeliveryVersion.
	TextVersion int64            `json:"-"`
	Deliveries  map[string]int64 `json:"-"`
}

// CharterSummary is one Project's charter in the machine listing.
type CharterSummary struct {
	ProjectID string        `json:"project_id"`
	Version   int64         `json:"version"`
	UpdatedAt time.Time     `json:"updated_at"`
	UpdatedBy CharterAuthor `json:"updated_by"`
	Pending   int           `json:"pending_proposals"`
}

// CharterProposal is a lead Bot's suggested replacement: of the text (Kind
// text, the default), or of the workflow (Kind workflow, Text empty).
// Nothing changes until the person applies it; applying it saves a new
// version with the proposed part and the current version's other parts.
type CharterProposal struct {
	ID          string              `json:"id"`
	ProjectID   string              `json:"project_id"`
	BaseVersion int64               `json:"base_version"`
	Kind        CharterProposalKind `json:"kind"`
	Text        string              `json:"text"`
	Workflow    *CharterWorkflow    `json:"workflow,omitempty"`
	Summary     string              `json:"summary"`
	BotID       string              `json:"bot_id"`
	TurnID      string              `json:"turn_id"`
	CreatedAt   time.Time           `json:"created_at"`
}

// CharterEdit marks a person turn sent from a charter page to the lead Bot.
// Only the charter change-request route sets it; it names the Project and
// the version the person was reading, never any authority of its own.
type CharterEdit struct {
	ProjectID string `json:"project_id"`
	Version   int64  `json:"version"`
	// Target is empty for the charter text, or workflow.
	Target CharterEditTarget `json:"target,omitempty"`
}

// Delivered charter text. The opening block precedes shared memory in a
// fresh session; the update block prefixes the next prompt of a resumed
// session that last heard an older version.
const charterRule = "This is the team charter the person wrote for this Project. Follow it. Where it conflicts with your own instructions or memory, the charter wins. 這是使用者為這個 Project 寫的團隊章程，請遵守；與你自己的指示或記憶衝突時，以章程為準。"

// CharterOpening is the charter block of a fresh session's opening; empty
// when the Project has no charter.
func CharterOpening(version int64, text string) string {
	if version <= 0 {
		return ""
	}
	return "<team_charter version=\"" + strconv.FormatInt(version, 10) + "\">\n" + charterRule + "\n\n" + text + "\n</team_charter>\n"
}

// CharterUpdate replaces whatever charter a resumed session heard earlier.
func CharterUpdate(version int64, text string) string {
	if version <= 0 {
		return "<team_charter_update version=\"0\">This Project no longer has a team charter. Disregard the earlier one. 這個 Project 已沒有團隊章程，請不再依照先前的章程。</team_charter_update>\n\n"
	}
	return "<team_charter_update version=\"" + strconv.FormatInt(version, 10) + "\">\nThe team charter changed. This is all of it now and replaces the earlier charter in this conversation. 團隊章程已更新，以下是完整內容，取代先前的章程。\n" + charterRule + "\n\n" + text + "\n</team_charter_update>\n\n"
}

// CharterEditContext is the structured context a charter change request
// carries to the lead Bot. The person's words are quoted data for the
// proposal, not a change by themselves.
func CharterEditContext(projectID string, version int64, text, words string) string {
	if strings.TrimSpace(text) == "" {
		text = "(empty)"
	}
	return "<charter_edit_request project=\"" + projectID + "\" version=\"" + strconv.FormatInt(version, 10) + "\">\n" +
		"The person asked, from this Project's charter page, to change its team charter. Read the current charter below, then call propose_charter_edit once with this project, base_version " + strconv.FormatInt(version, 10) + ", the whole new text and a one-line summary of what you removed and added. Nothing changes until the person presses Apply. Reply briefly in their language with the sentences you removed and added. 使用者從章程頁要求修改團隊章程：讀完以下目前內容後，呼叫一次 propose_charter_edit，填入完整新內容與一句刪改摘要；使用者按下套用前不會有任何改變。\n" +
		"<current_charter>\n" + text + "\n</current_charter>\n" +
		"<person_request>\n" + words + "\n</person_request>\n</charter_edit_request>\n\n"
}

func validCharterProject(id string) bool {
	return id != "" && len(id) <= MaxTowerFloorProjectBytes && utf8.ValidString(id) && !strings.ContainsFunc(id, unicode.IsControl) && !strings.ContainsAny(id, `"<>`)
}

// ValidateCharterProject checks a Project identifier a charter is keyed by.
func ValidateCharterProject(id string) error {
	if !validCharterProject(id) {
		return fail(Invalid, "project_id")
	}
	return nil
}

// ValidateCharterText checks one version's text and reason.
func ValidateCharterText(text, reason string) error {
	if !utf8.ValidString(text) {
		return fail(Invalid, "text")
	}
	if len(text) > MaxCharterTextBytes {
		return fail(CapacityFull, "charter_text")
	}
	if !utf8.ValidString(reason) || strings.ContainsFunc(reason, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		return fail(Invalid, "reason")
	}
	if len(reason) > MaxCharterReasonBytes {
		return fail(CapacityFull, "charter_reason")
	}
	return nil
}

// ValidateCharterRequest checks the person's words of a change request.
func ValidateCharterRequest(words string) error {
	if strings.TrimSpace(words) == "" || !utf8.ValidString(words) {
		return fail(Invalid, "text")
	}
	if len(words) > MaxCharterRequestBytes {
		return fail(CapacityFull, "charter_request")
	}
	return nil
}
