// Package opsdocs is the Ops documents list's rules that need no disk and no
// network: ids, the bounds, the names a new Markdown file may take, and how a
// link's type is read from its URL alone (docs/ops-documents.md).
package opsdocs

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"
)

// Folder is where a new Markdown file is created, under the Project's
// canonical root.
const Folder = "clawdline-ops"

// The bounds. Each is registered in internal/domain/capacity.
const (
	EntriesPerProjectLimit     = 500
	CollectionsPerProjectLimit = 64
	TitleBytesLimit            = 200
	CollectionNameBytesLimit   = 100
	MarkdownNameBytesLimit     = 255
	URLBytesLimit              = 2048
	NoteBytesLimit             = 1000
	ReadIDsLimit               = 16
)

// Kinds, title sources, statuses and to-do states, as the wire spells them.
const (
	KindFile = "file"
	KindLink = "link"

	TitleUser  = "user"
	TitleAgent = "agent"
	TitleFile  = "file"

	StatusReady       = "ready"
	StatusResolving   = "resolving"
	StatusNeedsAccess = "needs_access"
	StatusMissing     = "missing"
	StatusUnreadable  = "unreadable"

	TypeMarkdown = "markdown"
	TypeText     = "text"
	TypeFile     = "file"
	TypeWeb      = "web"
)

var (
	ErrBadURL  = errors.New("an Ops link is an http or https URL")
	ErrBadName = errors.New("a Markdown file name has no slash, no leading dot and no control characters")
)

const (
	entryPrefix      = "ope_"
	collectionPrefix = "opc_"
)

func newID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// NewEntryID and NewCollectionID mint "ope_"/"opc_" and 24 lowercase hex.
func NewEntryID() string      { return newID(entryPrefix) }
func NewCollectionID() string { return newID(collectionPrefix) }

func validID(prefix, id string) bool {
	rest, ok := strings.CutPrefix(id, prefix)
	if !ok || len(rest) != 24 {
		return false
	}
	for _, c := range rest {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func ValidEntryID(id string) bool      { return validID(entryPrefix, id) }
func ValidCollectionID(id string) bool { return validID(collectionPrefix, id) }

//go:embed link_types.json
var linkTypesJSON []byte

type linkRule struct {
	Host       string `json:"host"`
	HostSuffix string `json:"host_suffix"`
	PathPrefix string `json:"path_prefix"`
	Type       string `json:"type"`
}

var linkTable = func() struct {
	Rules    []linkRule `json:"rules"`
	Fallback string     `json:"fallback"`
} {
	var t struct {
		Rules    []linkRule `json:"rules"`
		Fallback string     `json:"fallback"`
	}
	if err := json.Unmarshal(linkTypesJSON, &t); err != nil || t.Fallback == "" {
		panic("opsdocs: link_types.json is unreadable")
	}
	return t
}()

// ClassifyURL returns the cleaned URL and its type, from the URL alone.
func ClassifyURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > URLBytesLimit || !utf8.ValidString(raw) || strings.ContainsAny(raw, " \t\r\n\x00") {
		return "", "", ErrBadURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", "", ErrBadURL
	}
	host := strings.ToLower(u.Hostname())
	for _, r := range linkTable.Rules {
		if r.Host != "" && host != r.Host {
			continue
		}
		if r.HostSuffix != "" && !strings.HasSuffix(host, r.HostSuffix) {
			continue
		}
		if r.PathPrefix != "" && !strings.HasPrefix(u.Path, r.PathPrefix) {
			continue
		}
		return raw, r.Type, nil
	}
	return raw, linkTable.Fallback, nil
}

// FileType is a file's type from its name and, when it was readable as
// UTF-8 text, that fact.
func FileType(name string, text bool) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown":
		return TypeMarkdown
	}
	if text {
		return TypeText
	}
	return TypeFile
}

// FileTitle is a file's default title: its name without the extension.
func FileTitle(rel string) string {
	base := path.Base(rel)
	if ext := path.Ext(base); ext != "" && ext != base {
		base = strings.TrimSuffix(base, ext)
	}
	return Clip(base, TitleBytesLimit)
}

// MarkdownFileName is the file a new Markdown entry named name is created
// as: name plus ".md". The name carries no extension of its own.
func MarkdownFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !plainName(name) {
		return "", ErrBadName
	}
	file := name + ".md"
	if len(file) > MarkdownNameBytesLimit {
		return "", ErrBadName
	}
	return file, nil
}

// RenameTarget checks a rename's new file name: one path segment.
func RenameTarget(name string) error {
	if name == "" || len(name) > MarkdownNameBytesLimit || !plainName(name) {
		return ErrBadName
	}
	return nil
}

func plainName(name string) bool {
	if !utf8.ValidString(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\:") ||
		strings.EqualFold(name, ".git") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Clip cuts s to at most n bytes on a rune boundary.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// CleanText is a one-line title or name: trimmed, valid UTF-8, no control
// characters, at most limit bytes. ok is false when it is empty or longer.
func CleanText(s string, limit int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > limit || !utf8.ValidString(s) {
		return "", false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return s, true
}

// ResolveTodoText is the to-do a Session is sent to read one link. It is in
// English because an agent reads it, and is far below the to-do limit.
func ResolveTodoText(entryID, rawURL, todoID string) string {
	return "Read this Ops document link and tell Clawdline what it is.\n\n" +
		"URL: " + rawURL + "\n\n" +
		"Read it with whatever connector you have (Google Drive, Notion, web fetch). " +
		"Do not paste the document's content anywhere: not in chat, not in a file, not in a to-do.\n\n" +
		"Then run exactly one of:\n" +
		"  clawdline ops resolve " + entryID + " --todo " + todoID + " --title \"<the document's real title>\"\n" +
		"  clawdline ops resolve " + entryID + " --todo " + todoID + " --needs-access --reason \"<what the person must do>\"\n\n" +
		"If the answer is resolve_stale, stop: the person asked another Session or removed the link."
}

// Entry is one stored Ops record. Status is the stored one; a file's is
// checked on disk whenever it is read.
type Entry struct {
	ID, ProjectKey, Kind, Title, TitleSource, Collection string
	Position                                             int64
	Path, URL, Type, Status, AccessNote                  string
	ResolveTerminal, ResolveTodo                         string
	CreatedAt, UpdatedAt                                 int64
}

// Collection is one stored group of entries.
type Collection struct {
	ID, ProjectKey, Name string
	Position             int64
}

// PendingRename is a rename recorded before the disk was touched; the
// daemon settles it when it starts.
type PendingRename struct {
	EntryID, ProjectKey, From, To string
}
