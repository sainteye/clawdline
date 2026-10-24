package antigravitysession

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// nativeMode accepts only an unambiguous native select whose current value is
// offered. Unknown or malformed mode metadata does not affect ordinary chat.
func nativeMode(raw json.RawMessage, wanted string) (string, error) {
	var options []json.RawMessage
	if json.Unmarshal(raw, &options) != nil {
		return "", refusal("permission_unavailable")
	}
	current, found := "", false
	for _, rawOption := range options {
		var key struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(rawOption, &key) != nil || key.ID != "mode" {
			continue
		}
		if found {
			return "", refusal("permission_unavailable")
		}
		found = true
		var option struct {
			Type    string `json:"type"`
			Current string `json:"currentValue"`
			Options []struct {
				Value string `json:"value"`
			} `json:"options"`
		}
		if json.Unmarshal(rawOption, &option) != nil || option.Type != "select" {
			return "", refusal("permission_unavailable")
		}
		seen := map[string]bool{}
		for _, v := range option.Options {
			if v.Value == "" || strings.ContainsAny(v.Value, " \t\r\n") || seen[v.Value] {
				return "", refusal("permission_unavailable")
			}
			seen[v.Value] = true
		}
		if !seen[wanted] || !seen[option.Current] {
			return "", refusal("permission_unavailable")
		}
		if option.Current != "default" && option.Current != "auto_edit" && option.Current != "yolo" {
			return "", refusal("permission_unavailable")
		}
		current = option.Current
	}
	if !found {
		return "", refusal("permission_unavailable")
	}
	return current, nil
}

// ConfigureDispatch performs metadata-only native selections under one lease.
// Required ACKs fence each subsequent selection; no prompt or history is written.
func (h *Host) ConfigureDispatch(ctx context.Context, row session.Session, model, effort, permission string) error {
	mode := map[string]string{"ask": "default", "edits": "auto_edit", "full": "yolo"}[permission]
	if mode == "" {
		return refusal("unsupported_permission")
	}
	c, err := h.lookup(row)
	if err != nil {
		return err
	}
	if _, err = h.Resume(ctx, row.CWD, row.ConversationID); err != nil {
		return err
	}
	client, done, err := h.beginMetadata(c)
	if err != nil {
		return err
	}
	defer done()
	check := func(id, value string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		c.mu.Lock()
		defer c.mu.Unlock()
		if h.closed || h.ctx.Err() != nil || c.hidden || c.client != client {
			return refusal("closed")
		}
		if id == "mode" {
			_, err := nativeMode(client.ConfigOptions(), value)
			return err
		}
		current, err := nativeMode(client.ConfigOptions(), mode)
		if err != nil {
			return err
		}
		if current != mode {
			return refusal("permission_unavailable")
		}
		if id == "" {
			return nil
		}
		if id == "model" {
			if c.structuredConfig {
				for _, m := range c.models {
					if m.ID == value {
						return nil
					}
				}
			}
			return refusal("model_not_offered")
		}
		for _, e := range c.efforts {
			if e == value {
				return nil
			}
		}
		return refusal("effort_not_offered")
	}
	for _, selection := range []struct{ id, value string }{{"mode", mode}, {"model", model}, {"thinking_level", effort}} {
		if selection.value == "" {
			continue
		}
		id, value := selection.id, selection.value
		if err := check(id, value); err != nil {
			return err
		}
		var selectionErr error
		err := client.SetConfigOptionChecked(ctx, id, value, func() error { return check(id, value) }, func(raw json.RawMessage) (applyErr error) {
			// Return the typed dispatch refusal to this caller after the wire ACK;
			// missing mode support must not terminate an ordinary conversation.
			defer func() { selectionErr = applyErr; applyErr = nil }()
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.client != client || c.hidden {
				return refusal("closed")
			}
			{
				var result struct {
					Options json.RawMessage `json:"configOptions"`
				}
				if json.Unmarshal(raw, &result) != nil {
					return refusal("permission_unavailable")
				}
				current, err := nativeMode(result.Options, mode)
				if err != nil {
					return err
				}
				if current != mode {
					return refusal("permission_unavailable")
				}
			}
			if err := c.readMetadata(raw, false); err != nil {
				return err
			}
			if (id == "model" && c.model != value) || (id == "thinking_level" && c.effort != value) {
				return refusal("config_not_acknowledged")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if selectionErr != nil {
			return selectionErr
		}
	}
	return check("", "")
}
