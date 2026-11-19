// Package sessionboard defines the wire and storage contract for a Project's
// asynchronous discussion between native Sessions.
package sessionboard

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxBoards      = 32
	MaxMembers     = 32
	MaxPosts       = 512 // retained posts per board
	MaxPageRows    = 24
	MaxName        = 120
	MaxText        = 4000 // bytes, including UTF-8 encoding
	MaxKey         = 128
	MaxArchiveDays = 30
)

var (
	ErrInvalid     = errors.New("session_board_invalid")
	ErrMissing     = errors.New("session_board_not_found")
	ErrClosed      = errors.New("session_board_closed")
	ErrDenied      = errors.New("session_board_denied")
	ErrFull        = errors.New("session_board_full")
	ErrReplay      = errors.New("session_board_key_reused")
	ErrPostExpired = errors.New("session_board_post_expired")
)

// Presence describes the current Session list. It is computed for the roster
// response and is never stored as membership or attached to a historical post.
type Presence string

const (
	Listed    Presence = "listed"
	NotListed Presence = "not_listed"
)

type Actor struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Persona  string   `json:"persona,omitempty"`
	Presence Presence `json:"presence,omitempty"`
}

// SameMembers compares two rosters in store order by their stable actor IDs.
func SameMembers(a, b []Actor) bool {
	return slices.EqualFunc(a, b, func(left, right Actor) bool { return left.ID == right.ID })
}

type Board struct {
	ID         string  `json:"id"`
	Project    string  `json:"project"`
	Name       string  `json:"name"`
	Purpose    string  `json:"purpose"`
	Lead       string  `json:"lead"`
	Summary    string  `json:"summary"`
	ArchivedAt int64   `json:"archived_at,omitempty"`
	Latest     int64   `json:"latest"`
	Members    []Actor `json:"members,omitempty"`
}

type Post struct {
	Seq     int64  `json:"seq"`
	Author  Actor  `json:"author"`
	Text    string `json:"text"`
	ReplyTo int64  `json:"reply_to,omitempty"`
	At      int64  `json:"at"`
}

type Page struct {
	Board Board  `json:"board"`
	Posts []Post `json:"posts"`
	More  bool   `json:"more"`
}

type Inbox struct {
	Posts []Post `json:"posts"`
	More  bool   `json:"more"`
}

type Command struct {
	Action  string   `json:"action"`
	Board   string   `json:"board,omitempty"`
	Source  string   `json:"source,omitempty"`
	Name    string   `json:"name,omitempty"`
	Text    string   `json:"text,omitempty"`
	Target  string   `json:"target,omitempty"`
	Members []string `json:"members,omitempty"`
	ReplyTo int64    `json:"reply_to,omitempty"`
}

type Result struct {
	ID  string `json:"id"`
	Seq int64  `json:"seq,omitempty"`
}

func ValidText(s string, maximum int) bool {
	return len(s) <= maximum && utf8.ValidString(s) && !strings.ContainsRune(s, 0) && strings.TrimSpace(s) != ""
}
