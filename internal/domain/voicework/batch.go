package voicework

import (
	"slices"
	"strings"
	"unicode"
)

const DefaultAutoContinue = 3
const MaxAutoContinue = 8

// TaskRef binds permission to a specification, never just its display name.
type TaskRef struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}
type AutoBatch struct {
	Epoch    int64     `json:"epoch"`
	Limit    int       `json:"limit"`
	Selected []TaskRef `json:"selected"`
	Closed   bool      `json:"closed"`
}
type BatchOffer struct {
	Epoch int64     `json:"epoch"`
	Limit int       `json:"limit"`
	Tasks []TaskRef `json:"tasks"`
}

func autoLimit(limit int) int {
	if limit < 1 || limit > MaxAutoContinue {
		return DefaultAutoContinue
	}
	return limit
}
func ref(t Task) TaskRef { return TaskRef{t.ID, t.Version} }
func (s State) autoAllowed(t Task) bool {
	if !ReadTask(t) || t.ApprovedVersion == t.Version {
		return true
	}
	b := s.AutoBatch
	return b == nil || slices.Contains(b.Selected, ref(t)) || !b.Closed && len(b.Selected) < b.Limit
}
func (s *State) reserveAuto(t Task, limit int) {
	if !ReadTask(t) || t.ApprovedVersion == t.Version {
		return
	}
	if s.AutoBatch == nil {
		s.AutoBatch = &AutoBatch{Epoch: 1, Limit: autoLimit(limit), Selected: []TaskRef{}}
	}
	if !slices.Contains(s.AutoBatch.Selected, ref(t)) {
		s.AutoBatch.Selected = append(s.AutoBatch.Selected, ref(t))
	}
}

// Continuation offers are read-only projections. A full batch must settle before
// the next exact selection can be approved. Failed/cancelled work is not success.
func (s State) Continuation(limit int) *BatchOffer {
	b := s.AutoBatch
	if b == nil || !b.Closed && len(b.Selected) < b.Limit {
		return nil
	}
	for _, r := range b.Selected {
		t := s.task(r.ID)
		if t != nil && t.Version == r.Version && (t.Status == "queued" || t.Status == "running" || t.Status == "cancel_requested") {
			return nil
		}
	}
	candidates := []Task{}
	for _, t := range s.Tasks {
		if t.Status != "queued" || !ReadTask(t) || t.ApprovedVersion == t.Version || slices.Contains(b.Selected, ref(t)) {
			continue
		}
		ready := true
		for _, id := range t.DependsOn {
			d := s.task(id)
			if d == nil || d.Status != "done" {
				ready = false
				break
			}
		}
		if ready {
			candidates = append(candidates, t)
		}
	}
	slices.SortStableFunc(candidates, func(a, b Task) int {
		if a.Priority > b.Priority {
			return -1
		}
		if a.Priority < b.Priority {
			return 1
		}
		return 0
	})
	if len(candidates) == 0 {
		return nil
	}
	limit = autoLimit(limit)
	offer := &BatchOffer{Epoch: b.Epoch, Limit: limit, Tasks: []TaskRef{}}
	for _, t := range candidates {
		offer.Tasks = append(offer.Tasks, ref(t))
		if len(offer.Tasks) == limit {
			break
		}
	}
	return offer
}

// Continue accepts a reviewed set, not a blank cheque for later-added tasks.
// A spoken action also consumes a saved explicit utterance from this call.
func (s *State) Continue(offer BatchOffer, limit int, call, input string) error {
	expected := s.Continuation(limit)
	if expected == nil || offer.Epoch != expected.Epoch || offer.Limit != expected.Limit || !slices.Equal(offer.Tasks, expected.Tasks) {
		return ErrConflict
	}
	consent := -1
	if call != "" || input != "" {
		if call == "" || input == "" {
			return ErrInvalid
		}
		for i, in := range s.Inputs {
			if in.ID == input && in.Call == call && !in.Planned && in.Deferred && in.PlanningError == "" && ContinuationConfirmation(in.Text) {
				consent = i
				break
			}
		}
		if consent < 0 {
			return ErrConflict
		}
	}
	if consent >= 0 {
		s.Inputs[consent].Planned = true
	}
	s.AutoBatch = &AutoBatch{Epoch: offer.Epoch + 1, Limit: offer.Limit, Selected: slices.Clone(offer.Tasks), Closed: true}
	s.Revision++
	return nil
}
func ContinuationConfirmation(text string) bool {
	return slices.Contains([]string{"可以繼續", "繼續下一批", "確認下一批", "可以继续", "继续下一批", "yescontinue", "continuethenextbatch"}, normalizedConfirmation(text))
}

func normalizedConfirmation(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
}
