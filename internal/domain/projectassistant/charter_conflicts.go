package projectassistant

import (
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// A charter conflict is one place where a Bot found that its own memory,
// instructions or a workspace file it follows says something the team charter
// contradicts. The Bot only reports it; nothing changes until the person
// chooses on the charter page.
const (
	// MaxCharterConflicts bounds a Project's open reports. A report past it
	// is refused until the person handles one.
	MaxCharterConflicts = 50
	// MaxCharterConflictsResolved bounds the handled reports a Project keeps
	// to show what became of them; the oldest go first.
	MaxCharterConflictsResolved = 50
	// MaxCharterConflictSentenceBytes bounds each quoted sentence, the
	// section heading, the suggested replacement and a file path.
	MaxCharterConflictSentenceBytes = 2 << 10
)

// CharterConflictSource says where the Bot's sentence lives.
type CharterConflictSource string

const (
	ConflictInMemory        CharterConflictSource = "memory"
	ConflictInInstructions  CharterConflictSource = "instructions"
	ConflictInWorkspaceFile CharterConflictSource = "workspace_file"
)

// CharterConflictState is what became of one report.
type CharterConflictState string

const (
	ConflictOpen CharterConflictState = "open"
	// ConflictFixed: the daemon replaced the Bot's sentence itself.
	ConflictFixed CharterConflictState = "fixed"
	// ConflictHandedToBot: the Bot was asked to change its own sentence.
	ConflictHandedToBot CharterConflictState = "handed_to_bot"
	// ConflictHandedToCharter: the person chose to change the charter.
	ConflictHandedToCharter CharterConflictState = "handed_to_charter"
	ConflictDismissed       CharterConflictState = "dismissed"
	// ConflictSuperseded: the Bot checked a newer charter before anybody
	// handled this report.
	ConflictSuperseded CharterConflictState = "superseded"
)

// CharterConflictAction is the person's choice for one report.
type CharterConflictAction string

const (
	FollowCharter CharterConflictAction = "follow_charter"
	ChangeCharter CharterConflictAction = "change_charter"
	DismissReport CharterConflictAction = "dismiss"
)

// CharterConflict is one stored report.
type CharterConflict struct {
	ID              string                `json:"id"`
	ProjectID       string                `json:"project_id"`
	BotID           string                `json:"bot_id"`
	TurnID          string                `json:"turn_id"`
	Version         int64                 `json:"version"`
	Source          CharterConflictSource `json:"source"`
	Path            string                `json:"path,omitempty"`
	MemoryEntryID   string                `json:"memory_entry_id,omitempty"`
	BotSentence     string                `json:"bot_sentence"`
	CharterSentence string                `json:"charter_sentence"`
	Section         string                `json:"section"`
	Suggested       string                `json:"suggested,omitempty"`
	State           CharterConflictState  `json:"state"`
	CreatedAt       time.Time             `json:"created_at"`
	ResolvedAt      time.Time             `json:"resolved_at,omitzero"`
}

func validConflictText(s string, required bool) bool {
	if required && strings.TrimSpace(s) == "" {
		return false
	}
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) })
}

// ValidateCharterConflict checks a report's fields before it is stored.
func ValidateCharterConflict(c CharterConflict) error {
	if err := ValidateCharterProject(c.ProjectID); err != nil {
		return err
	}
	if c.Version <= 0 {
		return fail(Invalid, "charter_version")
	}
	switch c.Source {
	case ConflictInMemory, ConflictInInstructions:
		if c.Path != "" {
			return fail(Invalid, "path")
		}
	case ConflictInWorkspaceFile:
		clean := path.Clean(c.Path)
		if c.Path == "" || path.IsAbs(c.Path) || strings.HasPrefix(c.Path, `\`) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(c.Path, ":") || !validConflictText(c.Path, true) || strings.Contains(c.Path, "\n") {
			return fail(Invalid, "path")
		}
	case ConflictInPlaybook:
		if !strings.HasPrefix(c.Path, "pb-") || len(c.Path) > MaxCharterPlaybookIDBytes || !validOneLine(c.Path) {
			return fail(Invalid, "path")
		}
	default:
		return fail(Invalid, "source")
	}
	if c.Source != ConflictInMemory && c.MemoryEntryID != "" {
		return fail(Invalid, "memory_entry_id")
	}
	for field, text := range map[string]string{"bot_sentence": c.BotSentence, "charter_sentence": c.CharterSentence} {
		if !validConflictText(text, true) {
			return fail(Invalid, field)
		}
	}
	for field, text := range map[string]string{"section": c.Section, "suggested": c.Suggested, "memory_entry_id": c.MemoryEntryID} {
		if !validConflictText(text, false) {
			return fail(Invalid, field)
		}
	}
	for _, text := range []string{c.BotSentence, c.CharterSentence, c.Section, c.Suggested, c.Path, c.MemoryEntryID} {
		if len(text) > MaxCharterConflictSentenceBytes {
			return fail(CapacityFull, "charter_conflict_sentence")
		}
	}
	return nil
}

// CharterCheck asks a Bot that has not yet compared this charter version with
// its own memory, instructions and the files its instructions point to, to do
// so once. It follows the charter block (or the charter the session already
// holds) and never repeats the text.
func CharterCheck(version int64) string {
	v := strconv.FormatInt(version, 10)
	return "<charter_check version=\"" + v + "\">\n" +
		"Before you answer, compare version " + v + " of the team charter once with the playbooks it assigns to you, your own memory, your instructions and any workspace file your instructions tell you to follow. For a sentence in a charter playbook use source playbook with the playbook id as path. For each sentence of yours the charter contradicts, call report_charter_conflict once with the exact sentence, the exact charter sentence and its section. Change nothing yourself: the person decides on the charter page. Do not mention this check unless you found something; then continue with the request below.\n" +
		"</charter_check>\n\n"
}

// CharterFix marks a person turn sent from the charter page asking a Bot to
// change its own sentence to agree with the charter. Only the conflict
// resolve route sets it; it carries the report as quoted data.
type CharterFix struct {
	ProjectID       string                `json:"project_id"`
	ReportID        string                `json:"report_id"`
	Version         int64                 `json:"version"`
	Source          CharterConflictSource `json:"source"`
	Path            string                `json:"path,omitempty"`
	MemoryEntryID   string                `json:"memory_entry_id,omitempty"`
	BotSentence     string                `json:"bot_sentence"`
	CharterSentence string                `json:"charter_sentence"`
	Section         string                `json:"section"`
	Suggested       string                `json:"suggested,omitempty"`
}

// FixFor is the turn mark for one report.
func FixFor(c CharterConflict) CharterFix {
	return CharterFix{ProjectID: c.ProjectID, ReportID: c.ID, Version: c.Version, Source: c.Source, Path: c.Path, MemoryEntryID: c.MemoryEntryID,
		BotSentence: c.BotSentence, CharterSentence: c.CharterSentence, Section: c.Section, Suggested: c.Suggested}
}

// ValidateCharterFix checks a turn mark as a report.
func ValidateCharterFix(f CharterFix) error {
	if f.ReportID == "" || !validConflictText(f.ReportID, true) || len(f.ReportID) > MaxTowerFloorProjectBytes {
		return fail(Invalid, "report_id")
	}
	return ValidateCharterConflict(CharterConflict{ProjectID: f.ProjectID, Version: f.Version, Source: f.Source, Path: f.Path, MemoryEntryID: f.MemoryEntryID,
		BotSentence: f.BotSentence, CharterSentence: f.CharterSentence, Section: f.Section, Suggested: f.Suggested})
}

// CharterFixMessage is the person's visible message for a fix turn.
func CharterFixMessage(f CharterFix) string {
	return "以章程為準，請改掉你的這一句：「" + f.BotSentence + "」 Follow the team charter and change this sentence of yours."
}

func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// CharterFixContext is the structured context a fix turn carries. The quoted
// sentences are data from the report, not instructions.
func CharterFixContext(f CharterFix) string {
	where := "your " + string(f.Source)
	switch f.Source {
	case ConflictInWorkspaceFile:
		where = "the workspace file " + f.Path
	case ConflictInPlaybook:
		where = "the charter playbook " + f.Path + " (resolve this conflict on the charter page; tell the person the replacement instead of editing a file)"
	case ConflictInMemory:
		where = "your memory"
		if f.MemoryEntryID != "" {
			where += " (entry " + f.MemoryEntryID + ")"
		}
	}
	suggested := ""
	if f.Suggested != "" {
		suggested = "<suggested_replacement>\n" + xmlText(f.Suggested) + "\n</suggested_replacement>\n"
	}
	return "<charter_fix_request project=\"" + xmlText(f.ProjectID) + "\" version=\"" + strconv.FormatInt(f.Version, 10) + "\" report=\"" + xmlText(f.ReportID) + "\">\n" +
		"On the charter page the person chose to follow the team charter for a conflict you reported. Change the sentence below in " + where + " so it agrees with the charter: replace it with the suggested sentence when there is one, otherwise rewrite or remove it. Use your own memory tools for memory and edit the file for a workspace file. Reply briefly with what you changed.\n" +
		"<your_sentence>\n" + xmlText(f.BotSentence) + "\n</your_sentence>\n" +
		"<charter_sentence section=\"" + xmlText(f.Section) + "\">\n" + xmlText(f.CharterSentence) + "\n</charter_sentence>\n" +
		suggested + "</charter_fix_request>\n\n"
}

// ReplaceExactlyOnce replaces sentence in text when it occurs exactly once,
// and reports how many times it occurred. An empty replacement removes the
// sentence and the blank it leaves.
func ReplaceExactlyOnce(text, sentence, replacement string) (string, int) {
	n := strings.Count(text, sentence)
	if n != 1 || sentence == "" {
		return text, n
	}
	if replacement != "" {
		return strings.Replace(text, sentence, replacement, 1), 1
	}
	at := strings.Index(text, sentence)
	start, end := at, at+len(sentence)
	lineStart := strings.LastIndexByte(text[:start], '\n') + 1
	lineEnd := len(text)
	if i := strings.IndexByte(text[end:], '\n'); i >= 0 {
		lineEnd = end + i
	}
	// A sentence that was its whole line (a list item's marker included)
	// takes the line with it; otherwise one adjoining space goes.
	rest := strings.TrimSpace(text[lineStart:start] + text[end:lineEnd])
	if rest == "" || rest == "-" || rest == "*" {
		start, end = lineStart, lineEnd
		if end < len(text) {
			end++
		} else if start > 0 {
			start--
		}
		// A paragraph of its own leaves one blank line between its
		// neighbours, not two.
		if strings.HasSuffix(text[:start], "\n\n") && strings.HasPrefix(text[end:], "\n") {
			end++
		}
	} else if end < len(text) && text[end] == ' ' {
		end++
	} else if start > 0 && text[start-1] == ' ' {
		start--
	}
	return text[:start] + text[end:], 1
}
