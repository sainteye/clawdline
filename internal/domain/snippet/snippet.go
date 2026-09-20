// Package snippet is the text somebody wrote once and presses instead of
// typing again.
//
// One record is one snippet. It belongs either to every project (`global`) or
// to one (`project`, with the path of that project beside it), and inside its
// own group it keeps the order the person put it in.
//
// This package decides what a snippet may be and what a refusal is called; it
// holds no state, opens nothing and resolves no path. The store owns the rows
// and the transaction (internal/adapters/store), and the routes own the door,
// the audit line and the project a session is standing in
// (internal/transport/http).
//
// The rules are the Swift app's (`Sources/Snippets.swift`), including the
// refusal codes, because the console that reads them is the same console: a
// page that branched on `snippet_limit_reached` over there must find it here.
package snippet

import (
	"sort"
	"strings"
)

// Scope is whether a snippet belongs to every project or to one.
type Scope string

const (
	Global  Scope = "global"
	Project Scope = "project"
)

// KnownScope reports whether s is one of the two.
func KnownScope(s Scope) bool { return s == Global || s == Project }

// The bounds, **counted in UTF-8 bytes**.
//
// Bytes and not characters, because bytes are what every downstream pays: the
// row in the store, the audit line, and the answer each read writes out. Four
// thousand ZWJ emoji are four thousand characters and about a hundred
// kilobytes, so a bound that counted characters would bound nothing anybody
// pays for.
//
// The title's 200 is the Swift store's: sixty bytes is sixty Latin letters and
// twenty Han characters, so the bound that reads as a generous title in one
// language is a fifth of a sentence in another. The body stays at 4,000 —
// about 1,333 Han characters, longer than anything this sheet is for.
// `project` is a path, so 1,024 is this platform's `PATH_MAX`: it cannot
// refuse a path that could exist.
//
// These three refuse one oversized field at the door and nothing accumulates
// behind them, which is why they are admission bounds on the capacity
// register's baseline rather than rows of their own. What does accumulate —
// how many snippets there are — is `snippets.total` and `snippets.scope`.
const (
	MaxTitleBytes   = 200
	MaxBodyBytes    = 4_000
	MaxProjectBytes = 1_024
	// MaxPosition bounds the one field that is read back and then armed:
	// `position` has 100 added to it by the next create in its group, so a
	// hand-written 9223372036854775807 would trap the next write. The rule
	// assigns (offset+1)*100 for at most a hundred snippets, so a billion is
	// four orders of magnitude past anything this store writes.
	MaxPosition = 1_000_000_000
	// Step is the gap between two neighbours' positions.
	Step = 100
)

// Record is one snippet, as it is stored and as it goes on the wire.
//
// Project is empty for a global snippet, and the wire leaves the key out
// rather than sending null: the Swift store refuses a body carrying
// `project: null` beside `scope: "global"`, and a console written against that
// refusal must not be handed the shape it refuses.
type Record struct {
	ID        string
	Title     string
	Body      string
	Scope     Scope
	Project   string
	Position  int64
	CreatedAt int64
	UpdatedAt int64
}

// Refusal is one answer that is not a snippet: its status, the code a client
// may branch on, a sentence for a person reading a log, and the numbers the
// code alone cannot carry.
type Refusal struct {
	Status  int
	Code    string
	Message string
	Extra   map[string]int64
}

// The codes. They are the Swift app's spelling, and they are the only part of
// a refusal anything may branch on.
const (
	CodeMalformed     = "malformed_snippet"
	CodeTooLong       = "snippet_too_long"
	CodeScopeMismatch = "snippet_scope_mismatch"
	CodeNotFound      = "snippet_not_found"
	CodeLimitReached  = "snippet_limit_reached"
)

func refuse(status int, code, message string, extra map[string]int64) *Refusal {
	return &Refusal{Status: status, Code: code, Message: message, Extra: extra}
}

// Fields are the four keys a writer may name, and which of them it named.
//
// Presence is kept apart from the value because for three of them the two mean
// different things: no `title` key leaves the title alone, while `title: ""`
// is a title being emptied, which is refused. And `project` present at all
// beside `scope: "global"` is a refusal even when its value is empty.
type Fields struct {
	Title      string
	HasTitle   bool
	Body       string
	HasBody    bool
	Scope      Scope
	HasScope   bool
	Project    string
	HasProject bool
}

// writable is the exact set of keys a create or a save may carry. An unknown
// key is refused rather than ignored: a client sending `postition` has a
// defect, and accepting the body silently is how it ships.
var writable = map[string]bool{"title": true, "body": true, "scope": true, "project": true}

// ParseFields reads a request body into Fields.
//
// **Every byte bound is measured here, on the value exactly as it arrived.** A
// path normaliser truncates at PATH_MAX and says nothing about it, so a bound
// taken after normalisation can never fire: 2,005 bytes come back as 1,024,
// pass a 1,024 bound, and the snippet is then filed under a path the client
// never sent. Normalisation is applied afterwards, by Normalized.
func ParseFields(body map[string]any) (Fields, *Refusal) {
	var f Fields
	for key, value := range body {
		if !writable[key] {
			return f, refuse(400, CodeMalformed,
				"A snippet contains missing or unknown fields.", nil)
		}
		text, ok := value.(string)
		if !ok {
			return f, refuse(400, CodeMalformed,
				"A snippet's title, body, scope and project are text.", nil)
		}
		switch key {
		case "title":
			f.Title, f.HasTitle = text, true
		case "body":
			f.Body, f.HasBody = text, true
		case "scope":
			f.Scope, f.HasScope = Scope(text), true
		case "project":
			f.Project, f.HasProject = text, true
		}
	}
	if len(f.Title) > MaxTitleBytes || len(f.Body) > MaxBodyBytes || len(f.Project) > MaxProjectBytes {
		return f, refuse(400, CodeTooLong,
			"A snippet title may contain 200 bytes of UTF-8, its body 4000, and its project path 1024.",
			map[string]int64{
				"title_count":   int64(len(f.Title)),
				"body_count":    int64(len(f.Body)),
				"project_count": int64(len(f.Project)),
			})
	}
	return f, nil
}

// ScopeAgrees is the one agreement a scope and a project path have to reach,
// asked in both directions: a project snippet names one project, and a global
// snippet does not carry the key at all.
//
// **Not "carries no path" — carries no key.** `project: ""` beside
// `scope: "global"` is refused, and so is `project: null`, because a client
// that assembles its next request by copying a record and overwriting two
// fields sends exactly that, and accepting it would file a global snippet
// against a path nobody chose. An empty path beside `scope: "project"` is the
// same refusal as no key, because an empty path is not a project.
func ScopeAgrees(f Fields) bool {
	if f.Scope == Project {
		return f.HasProject && f.Project != ""
	}
	return !f.HasProject
}

// Normalized returns f with its project path put through one spelling.
//
// The bound on that path was already taken by ParseFields, on the value as it
// arrived, which is the only place it can be taken. `spell` is the caller's —
// the routes know about symbolic links and this package does not.
func (f Fields) Normalized(spell func(string) string) Fields {
	if !f.HasProject || spell == nil {
		return f
	}
	f.Project = spell(f.Project)
	return f
}

// New is a snippet being made: title, body and scope are all required, and
// `project` is required by a project scope and refused by a global one.
func New(id string, f Fields, position, now int64) (Record, *Refusal) {
	if !f.HasTitle || !f.HasBody || !f.HasScope {
		return Record{}, refuse(400, CodeMalformed,
			"A snippet needs a title, a body and a scope.", nil)
	}
	return build(id, f, position, now, now)
}

// Patch is a snippet being saved: whichever fields the body named, over the
// ones the record already has.
//
// An empty body is refused rather than treated as "nothing changed": the
// caller that has nothing to send has a sheet to close, and a request that
// says nothing is a client defect worth naming.
func Patch(current Record, f Fields, now int64) (Record, *Refusal) {
	if !f.HasTitle && !f.HasBody && !f.HasScope && !f.HasProject {
		return Record{}, refuse(400, CodeMalformed,
			"A snippet change contains no fields or an unknown field.", nil)
	}
	merged := f
	if !f.HasTitle {
		merged.Title, merged.HasTitle = current.Title, true
	}
	if !f.HasBody {
		merged.Body, merged.HasBody = current.Body, true
	}
	if !f.HasScope {
		merged.Scope, merged.HasScope = current.Scope, true
	}
	// A scope that stays `project` keeps the project it was already filed
	// under, so a save that only changes the title need not repeat the path.
	if !f.HasProject && merged.Scope == Project && current.Project != "" {
		merged.Project, merged.HasProject = current.Project, true
	}
	return build(current.ID, merged, current.Position, current.CreatedAt, now)
}

// Moved reports whether next belongs in a different group from current, which
// is what decides whether its position is reassigned: a snippet that changes
// scope or project lands at the end of the group it arrives in.
func Moved(current, next Record) bool {
	return current.Scope != next.Scope || current.Project != next.Project
}

func build(id string, f Fields, position, createdAt, updatedAt int64) (Record, *Refusal) {
	title, body := strings.TrimSpace(f.Title), f.Body
	if title == "" || strings.TrimSpace(body) == "" {
		return Record{}, refuse(400, CodeMalformed,
			"A snippet title and body cannot be empty.", nil)
	}
	if !KnownScope(f.Scope) {
		return Record{}, refuse(400, CodeMalformed,
			"A snippet's scope is `global` or `project`.", nil)
	}
	if !ScopeAgrees(f) {
		return Record{}, refuse(400, CodeScopeMismatch,
			"Project snippets need one project path; global snippets cannot carry one.", nil)
	}
	if len(title) > MaxTitleBytes || len(body) > MaxBodyBytes || len(f.Project) > MaxProjectBytes {
		return Record{}, refuse(400, CodeTooLong,
			"A snippet title may contain 200 bytes of UTF-8, its body 4000, and its project path 1024.",
			map[string]int64{
				"title_count":   int64(len(title)),
				"body_count":    int64(len(body)),
				"project_count": int64(len(f.Project)),
			})
	}
	project := ""
	if f.Scope == Project {
		project = f.Project
	}
	return Record{
		ID: id, Title: title, Body: body, Scope: f.Scope, Project: project,
		Position: position, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// Limits are how many snippets this machine holds, from the capacity register.
type Limits struct {
	Total int64
	Scope int64
}

// LimitRefusal is the answer when one more snippet would not fit: the whole
// machine's row first, then the group the new one would join.
//
// Nothing is let go to make room. Every snippet is something a person wrote,
// so the register files both rows as evidence and only a person removes one.
func LimitRefusal(total, scope int64, limits Limits) *Refusal {
	if limits.Total > 0 && total >= limits.Total {
		return refuse(409, CodeLimitReached,
			"This machine already holds every snippet it keeps.",
			map[string]int64{"count": total, "limit": limits.Total})
	}
	if limits.Scope > 0 && scope >= limits.Scope {
		return refuse(409, CodeLimitReached,
			"This scope already holds every snippet it keeps.",
			map[string]int64{"count": scope, "limit": limits.Scope})
	}
	return nil
}

// NotFound is the one refusal for a snippet nobody has: a bad id and a missing
// row answer the same thing, so neither tells a caller which ids exist.
func NotFound() *Refusal {
	return refuse(404, CodeNotFound, "No snippet named that exists on this machine.", nil)
}

// Sort puts records in the order the sheet draws them: the order the person
// chose, then — for two that claim the same place — the order they arrived in.
//
// Never the title. A list that re-sorts itself when a snippet is renamed is a
// list nobody can point at.
func Sort(records []Record) {
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt < b.CreatedAt
		}
		return a.ID < b.ID
	})
}

// InScope is the snippets one session sees, in the order the sheet draws them:
// the open project's own first, then the ones that belong to every project.
//
// A project key that is empty is a session whose project could not be
// resolved, which sees the global group and nothing else — a smaller answer,
// not a wrong one.
func InScope(records []Record, projectKey string) []Record {
	mine := make([]Record, 0, len(records))
	global := make([]Record, 0, len(records))
	for _, r := range records {
		switch {
		case r.Scope == Project && projectKey != "" && r.Project == projectKey:
			mine = append(mine, r)
		case r.Scope == Global:
			global = append(global, r)
		}
	}
	Sort(mine)
	Sort(global)
	return append(mine, global...)
}

// Order is a complete new order for one group: exactly the snippets already in
// it, each named once.
//
// The promise is the landing queue's: it may reorder the members of that group
// and may never add or remove one. So an order that is missing a member, names
// a stranger, or repeats an id is refused whole rather than applied halfway.
func Order(current []Record, ids []string) ([]Record, *Refusal) {
	if len(ids) != len(current) {
		return nil, refuse(400, CodeScopeMismatch,
			"That order must contain every snippet in this scope, and no others.", nil)
	}
	byID := make(map[string]Record, len(current))
	for _, r := range current {
		byID[r.ID] = r
	}
	out := make([]Record, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for offset, id := range ids {
		row, ok := byID[id]
		if !ok || seen[id] {
			return nil, refuse(400, CodeScopeMismatch,
				"That order must contain every snippet in this scope, and no others.", nil)
		}
		seen[id] = true
		row.Position = int64(offset+1) * Step
		out = append(out, row)
	}
	return out, nil
}

// NextPosition is where a new member of a group goes: after the last one
// there, and never past MaxPosition.
func NextPosition(highest int64) int64 {
	if highest < 0 {
		highest = 0
	}
	next := highest + Step
	if next > MaxPosition {
		return MaxPosition
	}
	return next
}
