package voicework

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// MessageConfirmation accepts an explicit send instruction, never a model's
// confirmed flag. The stored utterance is the source of consent; a bare yes is
// PlainConsent, which ConfirmMessage accepts only in its place.
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

// plainYes is a whole utterance of agreement, the way a person answers a
// question in conversation. The browser mirrors it (`plainConsent`).
var plainYes = regexp.MustCompile(`^(?:(?:嗯|恩|對|是|好|好的|好啊|好喔|可以|可以啊|沒問題|没问题|確認|确认|我確認|我确认|確定|确定|送出|送吧|傳吧|传吧|發吧|发吧|請送出|请送出|送出去|就這樣|就这样|ok|okay|yes|yeah|yep|sure|sendit|goahead)(?:吧|喔|哦|啊|呀|的)?)+$`)

// PlainConsent accepts a bare yes. It approves a message only as the newest
// utterance of the call, said after the request (ConfirmMessage), and the
// browser only offers it right after the exact recipient and message were
// played back. On 2026-11-13 「可以」「Okay」「我確認」 were all refused.
func PlainConsent(text string) bool {
	return plainYes.MatchString(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text))
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
	index, request, newest := -1, -1, -1
	for i, in := range s.Inputs {
		if in.Call != call {
			continue
		}
		newest = i
		for _, source := range task.Sources {
			if in.ID == source {
				request = i
			}
		}
	}
	for i, in := range s.Inputs {
		if in.ID != consent || in.Call != call || !in.Deferred || in.Planned {
			continue
		}
		if MessageConfirmation(in.Text) || PlainConsent(in.Text) && i > request && i == newest {
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
