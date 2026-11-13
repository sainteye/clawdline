package projectassistantstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A position is a stateless reference, never a retained snapshot or authority.
// Timeline ordering is descending (anchor, kind, ID COLLATE BINARY).
type viewPosition struct {
	Collection string `json:"c"`
	Anchor     int64  `json:"a,omitempty"`
	Kind       int    `json:"k,omitempty"`
	ID         string `json:"i"`
}

func viewCursorBinding(q app.ReadViewQuery, h app.ReadViewHead, collection string) []byte {
	// Hash the trusted authority instead of putting it in a public token.
	authority := sha256.Sum256([]byte(q.AuthorizationBinding))
	binding := struct {
		Authority [sha256.Size]byte
		Key       pa.BotKey
		Project   pa.ProjectScope
		Chat      string
		Fence     *pa.Fence
	}{Authority: authority, Key: h.Key, Project: h.Project, Chat: h.Conversation.ID}
	if collection != "history" {
		binding.Fence = &h.CurrentFence
	}
	raw, _ := json.Marshal(binding)
	return raw
}

func (s *ViewStore) cursor(q app.ReadViewQuery, h app.ReadViewHead, p viewPosition) (string, *pa.Failure) {
	raw, _ := json.Marshal(p)
	if base64.RawURLEncoding.EncodedLen(len(raw))+1+base64.RawURLEncoding.EncodedLen(sha256.Size) > q.CursorByteLimit {
		return "", &pa.Failure{Code: pa.CapacityFull, Field: "read_view_cursor_bytes"}
	}
	mac := s.cursorMAC(q, h, p.Collection, raw)
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac)
	if len(token) > q.CursorByteLimit {
		return "", &pa.Failure{Code: pa.CapacityFull, Field: "read_view_cursor_bytes"}
	}
	return token, nil
}

func (s *ViewStore) cursorMAC(q app.ReadViewQuery, h app.ReadViewHead, collection string, raw []byte) []byte {
	mac := hmac.New(sha256.New, s.signingKey[:])
	mac.Write([]byte("clawdline/project-assistants/read-view/v1\x00"))
	mac.Write([]byte(s.sourceEpoch))
	mac.Write([]byte{0})
	mac.Write([]byte(collection))
	mac.Write([]byte{0})
	mac.Write(viewCursorBinding(q, h, collection))
	mac.Write([]byte{0})
	mac.Write(raw)
	return mac.Sum(nil)
}

func (s *ViewStore) position(q app.ReadViewQuery, h app.ReadViewHead) (viewPosition, error) {
	var p viewPosition
	bad := func() (viewPosition, error) { return p, failure(pa.CursorExpired, "read_view_cursor") }
	parts := strings.Split(q.Request.Cursor, ".")
	if len(parts) != 2 {
		return bad()
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &p) != nil {
		return bad()
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, s.cursorMAC(q, h, p.Collection, raw)) {
		return bad()
	}
	want := string(q.Request.Mode)
	if q.Request.Mode == pa.ReadViewDetail {
		if p.Collection != "permission" && p.Collection != "trace" {
			return bad()
		}
	} else if p.Collection != want {
		return bad()
	}
	if p.ID == "" || p.Anchor < 0 || p.Kind < 0 || p.Kind > 1 || p.Collection == "history" && p.Anchor == 0 {
		return bad()
	}
	return p, nil
}
