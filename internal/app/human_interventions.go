package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

const (
	humanInterventionTitleLimit    = 120
	humanInterventionTextLimit     = 500
	humanInterventionDetailLimit   = 16 << 10
	humanInterventionDocumentLimit = 2 << 10
	humanInterventionDraftLimit    = 2 << 10
)

var humanInterventionTaskID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type HumanInterventions struct {
	Store *store.Store
	Now   func() time.Time
}

func NewHumanInterventions(st *store.Store) *HumanInterventions {
	return &HumanInterventions{Store: st, Now: time.Now}
}

type CreateHumanIntervention struct {
	SourceConversation string                         `json:"source_conversation"`
	SourceLabel        string                         `json:"-"`
	TargetConversation string                         `json:"target_conversation"`
	TargetSession      string                         `json:"target_session"`
	TargetMachine      string                         `json:"-"`
	Kind               string                         `json:"kind"`
	Title              string                         `json:"title"`
	Summary            string                         `json:"summary"`
	Action             string                         `json:"action"`
	Reason             string                         `json:"reason"`
	Detail             string                         `json:"detail"`
	Options            []work.HumanInterventionOption `json:"options"`
	DocumentURL        string                         `json:"document_url"`
}

func humanInterventionError(code, message string) error {
	return &WorkError{Status: http.StatusUnprocessableEntity, Code: code, Message: message}
}

func humanText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\x7f")
}

// ValidateHumanDocumentURL accepts only the existing Cloud document locator.
// The locator's session is a route row id, not an assistant conversation id.
func ValidateHumanDocumentURL(raw, machine, session string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > humanInterventionDocumentLimit {
		return humanInterventionError("document_url_too_large", "The document URL is too long.")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "app.clawdline.com" || u.EscapedPath() != "/" || u.RawQuery != "" || u.ForceQuery || u.User != nil || u.Opaque != "" {
		return humanInterventionError("document_url_invalid", "Use a canonical Clawdline Cloud document link.")
	}
	fragment := u.EscapedFragment()
	fields, err := url.ParseQuery(fragment)
	if err != nil {
		return humanInterventionError("document_url_invalid", "The document locator is invalid.")
	}
	scope := fields.Get("scope")
	required := []string{"document", "machine", "session", "scope", "path"}
	if scope == "task" {
		required = append(required, "task")
	} else if scope != "project" {
		return humanInterventionError("document_url_invalid", "The document scope is invalid.")
	}
	if len(fields) != len(required) {
		return humanInterventionError("document_url_invalid", "The document locator has extra or missing fields.")
	}
	for _, key := range required {
		if len(fields[key]) != 1 || fields.Get(key) == "" {
			return humanInterventionError("document_url_invalid", "The document locator has extra or missing fields.")
		}
	}
	if fields.Get("document") != "1" || fields.Get("machine") != machine || fields.Get("session") != session || machine == "" || session == "" || machine == "this-mac" {
		return humanInterventionError("document_target_mismatch", "The document must belong to the target Session on its machine.")
	}
	for _, identity := range []string{machine, session} {
		if len(identity) > 128 || !utf8.ValidString(identity) || strings.IndexFunc(identity, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
			return humanInterventionError("document_url_invalid", "The document identity is invalid.")
		}
	}
	if scope == "task" && !humanInterventionTaskID.MatchString(fields.Get("task")) {
		return humanInterventionError("document_url_invalid", "The document task id is invalid.")
	}
	file := fields.Get("path")
	if len(file) > 512 || !utf8.ValidString(file) || strings.HasPrefix(file, "/") || strings.IndexFunc(file, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
		return humanInterventionError("document_url_invalid", "The document path is invalid.")
	}
	parts := strings.Split(file, "/")
	if len(parts) > 6 {
		return humanInterventionError("document_url_invalid", "The document path is invalid.")
	}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") {
			return humanInterventionError("document_url_invalid", "The document path is invalid.")
		}
	}
	switch strings.ToLower(path.Ext(file)) {
	case ".md", ".markdown", ".txt":
	default:
		return humanInterventionError("document_url_invalid", "The document must be a supported text file.")
	}
	return nil
}

func validateHumanIntervention(c CreateHumanIntervention) error {
	switch c.Kind {
	case "read", "answer", "action", "report":
	default:
		return humanInterventionError("intervention_kind_invalid", "Choose a supported intervention kind.")
	}
	for _, field := range []struct {
		name, value string
		max         int
	}{{"title", c.Title, humanInterventionTitleLimit}, {"summary", c.Summary, humanInterventionTextLimit}, {"action", c.Action, humanInterventionTextLimit}, {"reason", c.Reason, humanInterventionTextLimit}} {
		if !humanText(field.value, field.max) {
			return humanInterventionError("intervention_content_invalid", "A title, summary, concrete human action and reason are required within their size limits.")
		}
	}
	if len(c.Detail) > humanInterventionDetailLimit || !utf8.ValidString(c.Detail) {
		return humanInterventionError("intervention_detail_too_large", "The detail is too large or invalid.")
	}
	if len(c.Options) != 0 && (c.Kind != "answer" || len(c.Options) < 2 || len(c.Options) > 4) {
		return humanInterventionError("intervention_options_invalid", "Answer suggestions need two to four options.")
	}
	for _, o := range c.Options {
		if !humanText(o.Label, humanInterventionTitleLimit) || !humanText(o.Draft, humanInterventionDraftLimit) {
			return humanInterventionError("intervention_options_invalid", "Each suggestion needs a label and editable draft.")
		}
	}
	return ValidateHumanDocumentURL(c.DocumentURL, c.TargetMachine, c.TargetSession)
}

func (h *HumanInterventions) Create(ctx context.Context, c CreateHumanIntervention, k store.ReceiptKey, answer func(work.HumanIntervention) []byte) (work.HumanIntervention, []byte, error) {
	var out work.HumanIntervention
	var body []byte
	if err := validateHumanIntervention(c); err != nil {
		return out, nil, err
	}
	at := h.Now().UTC().Truncate(time.Second)
	out = work.HumanIntervention{ID: newWorkID(), SourceConversation: c.SourceConversation, SourceLabel: c.SourceLabel,
		TargetConversation: c.TargetConversation, TargetSession: c.TargetSession, Kind: c.Kind, Title: strings.TrimSpace(c.Title),
		Summary: strings.TrimSpace(c.Summary), Action: strings.TrimSpace(c.Action), Reason: strings.TrimSpace(c.Reason),
		Detail: c.Detail, Options: c.Options, DocumentURL: c.DocumentURL, CreatedAt: at, Version: 1}
	err := h.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.AddHumanIntervention(out, at); err != nil {
			return err
		}
		if answer != nil {
			body = answer(out)
			return tx.CompleteReceipt(k, store.ReceiptAnswer{Status: http.StatusCreated, Body: body})
		}
		return nil
	})
	return out, body, mapHumanInterventionError(err)
}

func (h *HumanInterventions) List(ctx context.Context, conversation string) ([]work.HumanIntervention, int64, error) {
	return h.Store.HumanInterventions(ctx, conversation)
}

func (h *HumanInterventions) Update(ctx context.Context, id, conversation, action, resolution string, version int64, k store.ReceiptKey, answer func(work.HumanIntervention) []byte) (work.HumanIntervention, []byte, error) {
	var out work.HumanIntervention
	var body []byte
	if action != "read" && action != "resolve" && action != "reopen" {
		return out, nil, humanInterventionError("intervention_action_invalid", "Choose read, resolve or reopen.")
	}
	if action == "resolve" && len(resolution) > humanInterventionTextLimit {
		return out, nil, humanInterventionError("intervention_resolution_too_large", "The resolution is too long.")
	}
	at := h.Now().UTC().Truncate(time.Second)
	err := h.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.HumanIntervention(id)
		if err != nil {
			return err
		}
		if prev.TargetConversation != conversation {
			return store.ErrHumanInterventionNotFound
		}
		if prev.Version != version {
			return store.ErrConflict
		}
		out = prev
		switch action {
		case "read":
			if out.ReadAt == nil {
				out.ReadAt = &at
			}
		case "resolve":
			if out.ReadAt == nil {
				out.ReadAt = &at
			}
			out.ResolvedAt = &at
			out.Resolution = strings.TrimSpace(resolution)
		case "reopen":
			out.ResolvedAt = nil
			out.Resolution = ""
		}
		if err := tx.PutHumanIntervention(prev, out); err != nil {
			return err
		}
		out.Version++
		if answer != nil {
			body = answer(out)
			return tx.CompleteReceipt(k, store.ReceiptAnswer{Status: http.StatusOK, Body: body})
		}
		return nil
	})
	return out, body, mapHumanInterventionError(err)
}

func mapHumanInterventionError(err error) error {
	if err == nil {
		return nil
	}
	var typed *WorkError
	if errors.As(err, &typed) {
		return err
	}
	switch {
	case errors.Is(err, store.ErrHumanInterventionsFull):
		return &WorkError{Status: http.StatusInsufficientStorage, Code: "interventions_full", Message: "The intervention capacity is full; resolve an open request first."}
	case errors.Is(err, store.ErrHumanInterventionNotFound):
		return &WorkError{Status: http.StatusNotFound, Code: "intervention_not_found", Message: "No intervention belongs to that Session."}
	case errors.Is(err, store.ErrConflict):
		return &WorkError{Status: http.StatusConflict, Code: "version_conflict", Message: "The intervention changed; reread it before writing."}
	default:
		return &WorkError{Status: http.StatusServiceUnavailable, Code: "store_unavailable", Message: "The intervention could not be saved."}
	}
}
