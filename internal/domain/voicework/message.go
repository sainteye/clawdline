package voicework

import (
	"encoding/json"
	"strings"
	"unicode"
)

// MessageConfirmation accepts an explicit send instruction, not a bare yes or
// a model's confirmed flag. The stored utterance is the source of consent.
func MessageConfirmation(text string) bool {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
	switch normalized {
	case "可以送出", "確定送出", "確認送出", "好送出", "可以发送", "确认发送", "確認發送", "yessendit", "confirmandsend", "sendit":
		return true
	}
	return false
}

// PrepareMessage records a single known action without a second planning model.
// It never approves or executes it, and cannot resurrect an answered request.
func (s *State) PrepareMessage(id, call, source string, target Focus, message string, now int64) error {
	if !ValidText(id) || !ValidText(call) || !ValidText(target.ID) || !ValidText(target.Label) || !ValidText(message) {
		return ErrInvalid
	}
	for _, field := range []string{target.Machine, target.Conversation} {
		if field != "" && !ValidText(field) {
			return ErrInvalid
		}
	}
	found := false
	for _, in := range s.Inputs {
		if in.ID == source && in.Call == call && in.Deferred && !in.Planned && in.PlanningError == "" {
			found = true
		}
	}
	if !found {
		return ErrConflict
	}
	// The store callback is transactional; validate every fallible field before Ready.
	raw, _ := json.Marshal(s)
	var next State
	_ = json.Unmarshal(raw, &next)
	version := next.PlanVersion
	if err := next.Ready([]string{source}); err != nil {
		return err
	}
	args := map[string]any{"session_id": target.ID, "session_label": target.Label, "message": message, "_voice_plain_message": true, "_voice_target": map[string]any{"id": target.ID, "label": target.Label, "conversation": target.Conversation, "machine": target.Machine}}
	if err := next.ApplyPlan(version, []string{source}, []Change{{Op: "add", ID: id, Text: message, Tool: "send_message_to_session", Args: args, DependsOn: []string{}}}, "", nil, now); err != nil {
		return err
	}
	*s = next
	return nil
}

// ConfirmMessage binds one saved explicit utterance to the current task version.
// Call identity, task identity and consent are checked before changing anything.
func (s *State) ConfirmMessage(id string, version int64, call, consent string, now int64) error {
	task := s.task(id)
	if task == nil || task.Tool != "send_message_to_session" || task.Version != version || task.Status != "waiting_confirmation" {
		return ErrConflict
	}
	belongs := false
	for _, source := range task.Sources {
		for _, in := range s.Inputs {
			if in.ID == source && in.Call == call {
				belongs = true
			}
		}
	}
	if !belongs {
		return ErrConflict
	}
	index := -1
	for i, in := range s.Inputs {
		if in.ID == consent && in.Call == call && in.Deferred && !in.Planned && MessageConfirmation(in.Text) {
			index = i
		}
	}
	if index < 0 {
		return ErrConflict
	}
	if err := s.Edit(id, version, "confirm", now); err != nil {
		return err
	}
	task.ConfirmedInput = consent
	s.Inputs[index].Planned = true
	s.Revision++
	return nil
}

// A new exact spoken consent authorizes only this message over earlier inbox
// entries. Older approved writes stay fenced; every later instruction wins.
func (s State) writeBlocked(task Task) bool {
	boundary := -1
	if task.Tool == "send_message_to_session" && task.ApprovedVersion == task.Version && task.ConfirmedInput != "" {
		for i, in := range s.Inputs {
			if in.ID == task.ConfirmedInput && in.Planned && MessageConfirmation(in.Text) {
				for _, source := range task.Sources {
					for _, original := range s.Inputs {
						if original.ID == source && original.Call == in.Call && in.Call != "" {
							boundary = i
						}
					}
				}
			}
		}
	}
	for i, in := range s.Inputs {
		if !in.Planned && (boundary < 0 || i > boundary) {
			return true
		}
	}
	return false
}
