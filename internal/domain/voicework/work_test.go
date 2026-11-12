package voicework

import (
	"errors"
	"testing"
)

func input(t *testing.T, s *State, id, text string) {
	t.Helper()
	if _, err := s.AddInput(Input{ID: id, Text: text, At: 1, Ready: true}); err != nil {
		t.Fatal(err)
	}
}
func add(id, tool, target string) Change {
	return Change{Op: "add", ID: id, Text: "Inspect the selected work", Tool: tool, Args: map[string]any{"session_id": target}}
}
func apply(t *testing.T, s *State, source string, changes ...Change) {
	t.Helper()
	if err := s.Apply(s.PlanVersion, []string{source}, changes, "Recorded the work.", 2); err != nil {
		t.Fatal(err)
	}
}

func TestNewSpeechDoesNotEraseWorkOrInvalidateAnAlreadyReceivedPrefix(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Inspect one work item")
	version := s.PlanVersion
	input(t, &s, "b", "Also inspect another work item")
	if err := s.Apply(version, []string{"a"}, []Change{add("first", "get_session_summary", "one")}, "", 2); err != nil {
		t.Fatal(err)
	}
	if len(s.Pending()) != 1 || s.Pending()[0].ID != "b" {
		t.Fatalf("pending: %+v", s.Pending())
	}
	apply(t, &s, "b", add("second", "get_session_summary", "two"))
	if len(s.Tasks) != 2 || s.Tasks[0].Status != "queued" {
		t.Fatalf("tasks: %+v", s.Tasks)
	}
	if changed, err := s.AddInput(Input{ID: "b", Text: "Also inspect another work item"}); err != nil || changed {
		t.Fatalf("duplicate: %v %v", changed, err)
	}
	if _, err := s.AddInput(Input{ID: "b", Text: "Different words"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestAStalePlanAndADependencyCycleAreAtomicRefusals(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Inspect two work items")
	a, b := add("first", "get_session_summary", "one"), add("second", "get_session_summary", "two")
	a.DependsOn = []string{"second"}
	b.DependsOn = []string{"first"}
	if err := s.Apply(0, []string{"a"}, []Change{a, b}, "", 2); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if len(s.Tasks) != 0 || len(s.Pending()) != 1 {
		t.Fatalf("partial commit: %+v", s)
	}
	apply(t, &s, "a", add("first", "get_session_summary", "one"))
	if err := s.Apply(0, nil, nil, "", 3); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestReadsCanRunTogetherAndDependentWorkWaitsForItsResult(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Inspect independent work and then its history")
	a, b, c := add("first", "get_session_summary", "one"), add("second", "get_session_summary", "two"), add("third", "get_session_recap", "one")
	c.DependsOn = []string{"first"}
	apply(t, &s, "a", a, b, c)
	one := s.Claim("executor", 3)
	two := s.Claim("executor", 3)
	if one == nil || two == nil || one.ID != "first" || two.ID != "second" || s.Claim("other", 3) != nil {
		t.Fatalf("claims: %+v %+v", one, two)
	}
	if err := s.Finish(one.ID, "executor", one.ActionKey, "Verified progress", "", false, 4); err != nil {
		t.Fatal(err)
	}
	if third := s.Claim("executor", 5); third == nil || third.ID != "third" {
		t.Fatalf("dependent: %+v", third)
	}
}

func TestWritesNeedCurrentConfirmationAndWaitForUnprocessedSpeech(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Send a message")
	apply(t, &s, "a", add("send", "send_message_to_session", "one"))
	if s.Claim("executor", 3) != nil {
		t.Fatal("unconfirmed write ran")
	}
	if err := s.Edit("send", 1, "confirm", 3); err != nil {
		t.Fatal(err)
	}
	input(t, &s, "b", "Wait, change that message")
	if s.Claim("executor", 4) != nil {
		t.Fatal("write overtook new instructions")
	}
	apply(t, &s, "b", Change{Op: "amend", ID: "send", Text: "Send the revised message", Tool: "send_message_to_session", Args: map[string]any{"session_id": "one", "message": "Revised words"}})
	if err := s.Edit("send", 1, "confirm", 5); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if s.Claim("executor", 5) != nil {
		t.Fatal("old approval survived an amendment")
	}
}

func TestLostExecutionsStayUnknownAndNeverRetryAnExternalEffect(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Send a message")
	apply(t, &s, "a", add("send", "send_message_to_session", "one"))
	_ = s.Edit("send", 1, "confirm", 3)
	run := s.Claim("executor", 4)
	if run == nil {
		t.Fatal("no claim")
	}
	if s.Claim("new-executor", 4+LeaseSecondsLimit+1) != nil || s.Tasks[0].Status != "outcome_unknown" {
		t.Fatal("lost effect was retried")
	}
	if err := s.Edit("send", 1, "resume", 100); !errors.Is(err, ErrConflict) {
		t.Fatal("unknown write became retryable", err)
	}
	if err := s.Finish("send", "executor", run.ActionKey, "Late answer", "", false, 100); !errors.Is(err, ErrConflict) {
		t.Fatal("late result rewrote an unknown outcome", err)
	}
}

func TestCancelRequestKeepsTheActualOutcomeAndSendingIsNotFinishingTheGoal(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Ask the work Session to finish its task")
	apply(t, &s, "a", add("send", "send_message_to_session", "one"))
	_ = s.Edit("send", 1, "confirm", 3)
	run := s.Claim("executor", 4)
	_ = s.Edit("send", 1, "cancel", 5)
	if s.Tasks[0].Status != "cancel_requested" {
		t.Fatal("pretended an in-flight action was cancelled")
	}
	if err := s.Finish("send", "executor", run.ActionKey, "Request delivered", "", false, 6); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[0].Status != "waiting_result" {
		t.Fatal("a sent request was reported as a completed goal")
	}
}

func TestAnUnknownToolCannotBecomeExecutableWork(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Inspect the work")
	if err := s.Apply(0, []string{"a"}, []Change{add("bad", "shell", "one")}, "", 2); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestPausingAnUnconfirmedWriteCannotApproveItAndWritesExcludeReads(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Send then read")
	apply(t, &s, "a", add("send", "send_message_to_session", "one"), add("read", "get_session_summary", "two"))
	if err := s.Edit("send", 1, "pause", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Edit("send", 1, "resume", 3); err != nil {
		t.Fatal(err)
	}
	claimed := s.Claim("executor", 4)
	if claimed == nil || claimed.ID != "read" || s.Tasks[0].Status != "waiting_confirmation" {
		t.Fatalf("pause approved a write: %+v", s)
	}
	_ = s.Finish("read", "executor", claimed.ActionKey, "Read", "", false, 5)
	if err := s.Edit("send", 1, "confirm", 6); err != nil {
		t.Fatal(err)
	}
	if c := s.Claim("executor", 7); c == nil || c.ID != "send" {
		t.Fatal(c)
	}
	input(t, &s, "b", "Also inspect another Session")
	apply(t, &s, "b", add("later", "get_session_summary", "three"))
	if s.Claim("other", 8) != nil {
		t.Fatal("read overlapped a write")
	}
}

func TestProgressIsNotCompletionAndOldExecutorsCannotPublishIt(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Read the group")
	apply(t, &s, "a", add("read", "get_sessions_summary", ""))
	c := s.Claim("executor", 3)
	if err := s.Progress(c.ID, "other", c.ActionKey, "Wrong owner", 4); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := s.Progress(c.ID, "executor", c.ActionKey, "One Session is ready", 4); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[0].Status != "running" || s.Tasks[0].Result != "One Session is ready" {
		t.Fatal(s.Tasks[0])
	}
}

func TestNewUtterancesWaitForTheirOwnDirectoryWithoutOvertakingEarlierInput(t *testing.T) {
	s := New("assistant")
	_, _ = s.AddInput(Input{ID: "first", Text: "Inspect the first Session"})
	_, _ = s.AddInput(Input{ID: "second", Text: "Also inspect the new Session"})
	if err := s.Ready([]string{"second"}); err != nil {
		t.Fatal(err)
	}
	if len(s.ReadyPending()) != 0 {
		t.Fatal("a later directory overtook an earlier utterance")
	}
	if err := s.Ready([]string{"first"}); err != nil {
		t.Fatal(err)
	}
	if len(s.ReadyPending()) != 2 {
		t.Fatal(s.ReadyPending())
	}
	_, _ = s.AddInput(Input{ID: "third", Text: "Wait, use another Session"})
	if len(s.ReadyPending()) != 2 {
		t.Fatal("new speech used the earlier directory")
	}
	if err := s.Apply(s.PlanVersion, []string{"third"}, nil, "", 3); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
