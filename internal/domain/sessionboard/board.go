// Package sessionboard defines the wire and storage contract for a Project's
// asynchronous discussion between native Sessions.
package sessionboard

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	MaxBoards      = 32
	MaxMembers     = 24
	MaxPosts       = 512
	MaxPageRows    = 24
	MaxName        = 120
	MaxText        = 4000 // bytes, including UTF-8 encoding
	MaxKey         = 128
	MaxArchiveDays = 30
)

var (
	ErrInvalid = errors.New("session_board_invalid")
	ErrMissing = errors.New("session_board_not_found")
	ErrClosed  = errors.New("session_board_closed")
	ErrDenied  = errors.New("session_board_denied")
	ErrFull    = errors.New("session_board_full")
	ErrReplay  = errors.New("session_board_key_reused")
)

type Actor struct {
	ID    string `json:"id"`
	Label string `json:"label"`
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

type Command struct {
	Action  string `json:"action"`
	Board   string `json:"board,omitempty"`
	Name    string `json:"name,omitempty"`
	Text    string `json:"text,omitempty"`
	Target  string `json:"target,omitempty"`
	ReplyTo int64  `json:"reply_to,omitempty"`
}

type Result struct {
	ID  string `json:"id"`
	Seq int64  `json:"seq,omitempty"`
}

func ValidText(s string, maximum int) bool {
	return len(s) <= maximum && utf8.ValidString(s) && !strings.ContainsRune(s, 0) && strings.TrimSpace(s) != ""
}
