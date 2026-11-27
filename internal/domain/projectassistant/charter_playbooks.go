package projectassistant

import (
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A playbook is a named Markdown document that belongs to a Project's team
// charter and is assigned to some of the Project's Bots. It is part of the
// charter document: every playbook change is a new charter version, and each
// Bot hears only the playbooks assigned to it, inside its charter block.
const (
	// MaxCharterPlaybookTextBytes bounds one playbook's text.
	MaxCharterPlaybookTextBytes = 16 << 10
	// MaxCharterPlaybooks bounds the playbooks one charter version holds.
	MaxCharterPlaybooks = 20
	// MaxCharterPlaybookTitleBytes bounds a playbook's one-line title.
	MaxCharterPlaybookTitleBytes = 120
	// MaxCharterPlaybookBots bounds the Bots one playbook is assigned to.
	MaxCharterPlaybookBots = 64
	// MaxCharterDocumentBytes bounds one version's charter text plus the text
	// of all its playbooks, which is the most any Bot's prompt can carry.
	MaxCharterDocumentBytes = 128 << 10
	// maxCharterPlaybookIDBytes bounds a daemon-issued playbook identifier.
	maxCharterPlaybookIDBytes = 64
)

// CharterPlaybook is one playbook of a charter version. ID is issued by the
// daemon and stays the same across versions.
type CharterPlaybook struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Text   string   `json:"text"`
	BotIDs []string `json:"bot_ids"`
}

// ConflictInPlaybook is a sentence in one of the Bot's charter playbooks;
// the report's path is the playbook id.
const ConflictInPlaybook CharterConflictSource = "playbook"

// Assigned says whether the playbook is assigned to bot.
func (p CharterPlaybook) Assigned(bot string) bool { return slices.Contains(p.BotIDs, bot) }

// PlaybooksFor lists, in the version's order, the playbooks assigned to bot.
func (v CharterVersion) PlaybooksFor(bot string) []CharterPlaybook {
	out := []CharterPlaybook{}
	for _, p := range v.Playbooks {
		if p.Assigned(bot) {
			out = append(out, p)
		}
	}
	return out
}

// Playbook finds one playbook of the version by id.
func (v CharterVersion) Playbook(id string) (CharterPlaybook, bool) {
	for _, p := range v.Playbooks {
		if p.ID == id {
			return p, true
		}
	}
	return CharterPlaybook{}, false
}

// DeliveryVersion is the newest version at which what bot hears of this
// charter changed: its text, or the playbooks assigned to it. Version 0 is no
// charter. A Bot whose delivery did not move does not hear the charter again.
func (v CharterVersion) DeliveryVersion(bot string) int64 {
	if v.Version <= 0 || v.Author == CharterByRemoval {
		return 0
	}
	if d, ok := v.Deliveries[bot]; ok {
		return d
	}
	if v.TextVersion > 0 {
		return v.TextVersion
	}
	return v.Version
}

// DeliveredText is the charter text bot hears: the charter text followed by
// the playbooks assigned to it, after the rule that orders them.
func (v CharterVersion) DeliveredText(bot string) string {
	return v.Text + renderPlaybooks(v.PlaybooksFor(bot))
}

const playbookRule = "The playbooks below belong to this charter and are assigned to you. Order of precedence: the charter, then these playbooks, then your own instructions, then your memory. 以下 playbook 屬於這份章程並指派給你。優先順序：章程，其次是這些 playbook，再來是你自己的指示，最後是記憶。"

func renderPlaybooks(playbooks []CharterPlaybook) string {
	if len(playbooks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n" + playbookRule + "\n")
	for _, p := range playbooks {
		b.WriteString("<playbook id=\"" + xmlAttr(p.ID) + "\" title=\"" + xmlAttr(p.Title) + "\">\n" + p.Text + "\n</playbook>\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func xmlAttr(s string) string { return strings.ReplaceAll(xmlText(s), `"`, "&quot;") }

// NextCharterDeliveries sets next's TextVersion and Deliveries from the
// version before it. prior is the newest version row (a removal included,
// or the zero version when there is none). A changed text, or a charter
// after none, moves every Bot's delivery to next. Otherwise a Bot moves only
// when the playbooks it hears differ, so assigning, editing or unassigning a
// playbook re-delivers to the Bots it concerns and to nobody else.
func NextCharterDeliveries(prior CharterVersion, next *CharterVersion) {
	next.Deliveries = map[string]int64{}
	if prior.Version <= 0 || prior.Author == CharterByRemoval || next.Author == CharterByRemoval || prior.Text != next.Text {
		next.TextVersion = next.Version
		return
	}
	next.TextVersion = prior.TextVersion
	if next.TextVersion <= 0 {
		next.TextVersion = prior.Version
	}
	maps.Copy(next.Deliveries, prior.Deliveries)
	bots := map[string]bool{}
	for _, p := range append(slices.Clone(prior.Playbooks), next.Playbooks...) {
		for _, bot := range p.BotIDs {
			bots[bot] = true
		}
	}
	for bot := range bots {
		if renderPlaybooks(prior.PlaybooksFor(bot)) != renderPlaybooks(next.PlaybooksFor(bot)) {
			next.Deliveries[bot] = next.Version
		}
	}
}

func validOneLine(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// ValidateCharterPlaybook checks one playbook as the person wrote it. The id
// is empty for a new one.
func ValidateCharterPlaybook(p CharterPlaybook) error {
	if p.ID != "" && (!strings.HasPrefix(p.ID, "pb-") || len(p.ID) > maxCharterPlaybookIDBytes || !validOneLine(p.ID) || strings.ContainsAny(p.ID, `"<>& `)) {
		return fail(Invalid, "playbook_id")
	}
	if strings.TrimSpace(p.Title) == "" || !validOneLine(p.Title) {
		return fail(Invalid, "playbook_title")
	}
	if len(p.Title) > MaxCharterPlaybookTitleBytes {
		return fail(CapacityFull, "charter_playbook_title")
	}
	if !utf8.ValidString(p.Text) {
		return fail(Invalid, "playbook_text")
	}
	if len(p.Text) > MaxCharterPlaybookTextBytes {
		return fail(CapacityFull, "charter_playbook_text")
	}
	if len(p.BotIDs) > MaxCharterPlaybookBots {
		return fail(CapacityFull, "charter_playbook_bots")
	}
	seen := map[string]bool{}
	for _, bot := range p.BotIDs {
		if bot == "" || len(bot) > MaxTowerFloorProjectBytes || !validOneLine(bot) || seen[bot] {
			return fail(Invalid, "bot_ids")
		}
		seen[bot] = true
	}
	return nil
}

// ValidateCharterDocument checks one whole version: its text, its playbooks
// and the size of everything a Bot could hear.
func ValidateCharterDocument(text string, playbooks []CharterPlaybook) error {
	if len(playbooks) > MaxCharterPlaybooks {
		return fail(CapacityFull, "charter_playbooks")
	}
	total := len(text)
	ids := map[string]bool{}
	for _, p := range playbooks {
		if p.ID == "" || ids[p.ID] {
			return fail(Invalid, "playbook_id")
		}
		ids[p.ID] = true
		if err := ValidateCharterPlaybook(p); err != nil {
			return err
		}
		total += len(p.Text)
	}
	if total > MaxCharterDocumentBytes {
		return fail(CapacityFull, "charter_document")
	}
	return nil
}
