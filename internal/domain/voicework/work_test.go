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

func TestCancelledAdmissionCannotStartAndPausedExecutionCanStopHonestly(t *testing.T) {
	s := New("work")
	input(t, &s, "a", "Send later")
	apply(t, &s, "a", add("send", "send_message_to_session", "one"))
	_ = s.Edit("send", 1, "confirm", 3)
	c := s.Claim("runner", 4)
	_ = s.Edit("send", 1, "pause", 5)
	if err := s.BeginEffect(c.ID, "runner", c.ActionKey, 6); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := s.Finish(c.ID, "runner", c.ActionKey, "", "voice_work_stopped", false, 7); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[0].Status != "paused" {
		t.Fatal(s.Tasks[0])
	}
}
func TestContinuationUsesOriginalInputAndOnlyReleasesAfterAllReads(t *testing.T) {
	s := New("work")
	input(t, &s, "a", "Start the project after looking it up")
	if err := s.ApplyPlan(0, []string{"a"}, []Change{add("projects", "list_projects", "")}, "Looking up projects", []string{"projects"}, 2); err != nil {
		t.Fatal(err)
	}
	if len(s.Pending()) != 0 {
		t.Fatal("discovery not awaited")
	}
	c := s.Claim("runner", 3)
	_ = s.Finish(c.ID, "runner", c.ActionKey, `{"projects":[{"id":"p"}]}`, "", false, 4)
	if p := s.ReadyPending(); len(p) != 1 || p[0].Text != "Start the project after looking it up" || p[0].PlanRounds != 1 {
		t.Fatal(p)
	}
	if err := s.ApplyPlan(s.PlanVersion, []string{"a"}, []Change{add("start", "start_session", "")}, "Confirm this start", nil, 5); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[1].Status != "waiting_confirmation" || len(s.Pending()) != 0 {
		t.Fatal(s)
	}
}
func TestCancelledDiscoveryDoesNotResurrectTheGoal(t *testing.T) {
	s := New("work")
	input(t, &s, "a", "Look up a project then start it")
	_ = s.ApplyPlan(0, []string{"a"}, []Change{add("projects", "list_projects", "")}, "", []string{"projects"}, 2)
	c := s.Claim("runner", 3)
	_ = s.Edit(c.ID, 1, "cancel", 4)
	_ = s.Finish(c.ID, "runner", c.ActionKey, "", "voice_work_stopped", false, 5)
	if len(s.Pending()) != 0 || len(s.Inputs[0].WaitFor) != 0 {
		t.Fatal(s)
	}
}
func TestAnUnknownNavigationEffectCannotBeResumedAsARead(t *testing.T) {
	s := New("work")
	s.Tasks = []Task{{ID: "open", Version: 1, Tool: "get_devstack_links", Args: map[string]any{"open": true}, Status: "outcome_unknown"}}
	if err := s.Edit("open", 1, "resume", 1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
func TestAnotherCallCannotStealCallSettingsAndRetriesGetNewAttemptKeys(t *testing.T) {
	s := New("work")
	_, _ = s.AddInput(Input{ID: "a", Text: "Speak slower", Call: "phone", Ready: true})
	apply(t, &s, "a", add("speed", "set_speaking_speed", ""))
	_ = s.Edit("speed", 1, "confirm", 3)
	if s.ClaimFor("desktop", "", []string{"set_speaking_speed"}, 4) != nil {
		t.Fatal("other browser claimed live-call work")
	}
	c := s.ClaimFor("phone", "phone", []string{"set_speaking_speed"}, 4)
	if c == nil {
		t.Fatal("capable call cannot claim")
	}
	_ = s.Finish(c.ID, "phone", c.ActionKey, "", "known refusal", false, 5)
	_ = s.Edit(c.ID, 1, "resume", 6)
	next := s.ClaimFor("phone", "phone", []string{"set_speaking_speed"}, 7)
	if next == nil || next.ActionKey == c.ActionKey {
		t.Fatal("retry reused a terminal receipt")
	}
}

func TestProgressPersistsEveryPreparedMemberAndStaleAttemptsAreFenced(t *testing.T) {
	s := New("work")
	input(t, &s, "a", "Read the group")
	apply(t, &s, "a", add("read", "get_sessions_summary", ""))
	c := s.Claim("browser", 3)
	for _, result := range []string{"first", "second"} {
		if err := s.Progress(c.ID, "browser", c.ActionKey, result, 4); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Finish(c.ID, "browser", c.ActionKey, "All returned", "", false, 5)
	if len(s.Tasks[0].Progress) != 2 || s.Tasks[0].Progress[0].Value != "first" || s.Tasks[0].ResultRevision == 0 {
		t.Fatal(s.Tasks[0])
	}
}

func TestPausedAndUnknownDiscoveryWaitForExplicitResumeBeforeContinuing(t *testing.T) {
	for _, stop := range []string{"pause", "unknown"} {
		t.Run(stop, func(t *testing.T) {
			s := New("work")
			input(t, &s, "a", "Look up then start the project")
			_ = s.ApplyPlan(0, []string{"a"}, []Change{add("lookup", "list_projects", "")}, "", []string{"lookup"}, 2)
			c := s.Claim("runner", 3)
			if stop == "pause" {
				_ = s.Edit(c.ID, 1, "pause", 4)
				_ = s.Finish(c.ID, "runner", c.ActionKey, "", "voice_work_stopped", false, 5)
			} else {
				_ = s.Finish(c.ID, "runner", c.ActionKey, "", "lost connection", true, 5)
			}
			if len(s.Pending()) != 0 || len(s.Inputs[0].WaitFor) != 1 {
				t.Fatal("goal was resumed or forgotten", s)
			}
			_ = s.Edit(c.ID, 1, "resume", 6)
			retry := s.Claim("runner", 7)
			if retry == nil {
				t.Fatal("retry not claimable")
			}
			_ = s.Finish(retry.ID, "runner", retry.ActionKey, `{"id":"exact-project"}`, "", false, 8)
			if len(s.ReadyPending()) != 1 {
				t.Fatal("explicitly recovered discovery did not continue", s)
			}
		})
	}
}
func TestAmendingAFailedTaskDoesNotReplayOldEvidence(t *testing.T) {
	s := New("work")
	input(t, &s, "a", "Read progress")
	apply(t, &s, "a", add("read", "get_session_summary", "one"))
	c := s.Claim("runner", 3)
	_ = s.Progress(c.ID, "runner", c.ActionKey, "old evidence", 4)
	_ = s.Finish(c.ID, "runner", c.ActionKey, "old result", "known refusal", false, 5)
	input(t, &s, "b", "Use another Session")
	change := add("read", "get_session_summary", "two")
	change.Op = "amend"
	apply(t, &s, "b", change)
	if s.Tasks[0].ResultRevision != 0 || len(s.Tasks[0].Progress) != 0 || s.Tasks[0].Result != "" {
		t.Fatal("old evidence survived a changed spec", s.Tasks[0])
	}
}

func TestLostAdmissionAcknowledgementCannotTurnCancellationIntoARetryableFailure(t *testing.T) {
	s := New("work")
	input(t, &s, "a", "Send the approved message")
	apply(t, &s, "a", add("send", "send_message_to_session", "one"))
	_ = s.Edit("send", 1, "confirm", 3)
	c := s.Claim("runner", 4)
	_ = s.BeginEffect(c.ID, "runner", c.ActionKey, 5)
	_ = s.Edit(c.ID, 1, "cancel", 6)
	// The executor was interrupted before it observed the admission response.
	_ = s.Finish(c.ID, "runner", c.ActionKey, "", "voice_work_stopped", false, 7)
	if s.Tasks[0].Status != "outcome_unknown" {
		t.Fatal(s.Tasks[0])
	}
	if err := s.Edit(c.ID, 1, "resume", 8); !errors.Is(err, ErrConflict) {
		t.Fatal("uncertain effect became retryable", err)
	}
}

func TestOpeningADevStackStaysWithTheOriginatingCall(t *testing.T) {
	s := New("work")
	_, _ = s.AddInput(Input{ID: "a", Text: "Open this project's links", Call: "phone", Ready: true})
	c := add("links", "get_devstack_links", "")
	c.Args["open"] = true
	apply(t, &s, "a", c)
	_ = s.Edit("links", 1, "confirm", 3)
	if s.ClaimFor("desktop", "", []string{"get_devstack_links"}, 4) != nil {
		t.Fatal("navigation ran on another browser")
	}
	if s.ClaimFor("phone", "phone", []string{"get_devstack_links"}, 4) == nil {
		t.Fatal("originating call could not execute navigation")
	}
}

// Asked to rename mismatched Sessions, the planner's work list must accept a
// smart rename, and as a write it runs only after the person confirms it.
func TestASmartRenameIsWorkThatWaitsForConfirmation(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "Rename the working Sessions whose names do not match their work")
	apply(t, &s, "a", Change{Op: "add", ID: "rename", Text: "Smart-name two Sessions", Tool: "smart_rename_sessions",
		Args: map[string]any{"sessions": []any{"Fix bug", "New Session"}}})
	if s.Tasks[0].Status != "waiting_confirmation" || s.Claim("executor", 3) != nil {
		t.Fatalf("an unconfirmed rename ran: %+v", s.Tasks[0])
	}
	if err := s.Edit("rename", 1, "confirm", 3); err != nil {
		t.Fatal(err)
	}
	if claimed := s.Claim("executor", 4); claimed == nil || claimed.ID != "rename" {
		t.Fatalf("a confirmed rename did not run: %+v", claimed)
	}
}

// "What happened since" is a read: the work list runs it without asking.
func TestSessionUpdatesAreAReadThatRunsWithoutConfirmation(t *testing.T) {
	s := New("assistant")
	input(t, &s, "a", "What happened in this Session since you last told me?")
	apply(t, &s, "a", add("updates", "get_session_updates", "%2"))
	if s.Tasks[0].Status != "queued" {
		t.Fatalf("a read waits for confirmation: %+v", s.Tasks[0])
	}
}

func TestConversationInputWaitsForRoutingAndKeepsItsFocus(t *testing.T) {
	s := New("workspace")
	focus := &Focus{ID: "fleet:machine-a:%257", Label: "Review", Machine: "machine-a", Conversation: "conversation-a"}
	_, err := s.AddInput(Input{ID: "utterance", Call: "call", Text: "Summarize this Session", Deferred: true, Focus: focus})
	if err != nil {
		t.Fatal(err)
	}
	focus.ID = "changed"
	if len(s.ReadyPending()) != 0 || s.Inputs[0].Focus.ID != "fleet:machine-a:%257" {
		t.Fatal(s)
	}
	if err = s.Answer("utterance", "another-call"); err == nil {
		t.Fatal("wrong call resolved input")
	}
	if err = s.Answer("utterance", "call"); err != nil {
		t.Fatal(err)
	}
	if len(s.Pending()) != 0 || !s.Inputs[0].Planned {
		t.Fatal(s)
	}
}

func TestFailedInputDoesNotRetryOrBlockNewPlanning(t *testing.T) {
	s := New("workspace")
	input(t, &s, "failed", "Read A")
	s.FailPlanning([]string{"failed"}, "voice_work_invalid_plan", "validate")
	input(t, &s, "new", "Read B")
	ready := s.ReadyPending()
	if len(ready) != 1 || ready[0].ID != "new" || s.Inputs[0].PlanningError == "" {
		t.Fatal(s)
	}
	if err := s.RetryPlanning([]string{"failed"}); err != nil {
		t.Fatal(err)
	}
	if len(s.ReadyPending()) != 2 {
		t.Fatal(s)
	}
}

func TestLegacyPlanningFailureIsIsolatedBeforeNewInput(t *testing.T) {
	s := New("workspace")
	input(t, &s, "old", "Read A")
	s.PlannerError = "voice_work_planning_failed"
	input(t, &s, "new", "Read B")
	if s.Inputs[0].PlanningError == "" || len(s.ReadyPending()) != 1 || s.ReadyPending()[0].ID != "new" {
		t.Fatal(s)
	}
}

func TestFailedInstructionsStillFenceWritesUntilExplicitlyDismissed(t *testing.T) {
	s := New("work")
	input(t, &s, "send-input", "Send A")
	apply(t, &s, "send-input", add("send", "send_message_to_session", "A"))
	if err := s.Edit("send", 1, "confirm", 3); err != nil {
		t.Fatal(err)
	}
	input(t, &s, "correction", "Wait, cancel that")
	s.FailPlanning([]string{"correction"}, "voice_work_invalid_plan", "validate")
	if s.Claim("runner", 4) != nil {
		t.Fatal("failed cancellation silently released a write")
	}
	input(t, &s, "read-input", "Read B")
	apply(t, &s, "read-input", add("read", "get_session_summary", "B"))
	read := s.Claim("runner", 5)
	if read == nil || read.ID != "read" {
		t.Fatal("unrelated reads were blocked", read)
	}
	_ = s.Finish("read", "runner", read.ActionKey, "Read B", "", false, 6)
	if err := s.Dismiss("correction"); err != nil {
		t.Fatal(err)
	}
	if !s.Inputs[1].Cancelled || s.Inputs[1].Text != "Wait, cancel that" {
		t.Fatal("dismiss erased the original words")
	}
	if next := s.Claim("runner", 7); next == nil || next.ID != "send" {
		t.Fatal(next)
	}
}

func TestAHungUpCallLeavesNothingForTheNextCallToRoute(t *testing.T) {
	s := New("work")
	_, _ = s.AddInput(Input{ID: "chat", Call: "call", Text: "What is the summary?", Deferred: true})
	if len(s.Pending()) != 1 {
		t.Fatal(s)
	}
	if err := s.EndCall("call"); err != nil {
		t.Fatal(err)
	}
	if !s.Inputs[0].Planned || !s.Inputs[0].Cancelled || len(s.Pending()) != 0 {
		t.Fatal(s)
	}
	if err := s.Ready([]string{"chat"}); err == nil {
		t.Fatal("a later call routed an utterance from a call that hung up")
	}
	if s.Inputs[0].Text != "What is the summary?" {
		t.Fatal("ending the call erased the original words")
	}
}

func TestMessagePreparationAndSpokenConsentBindTheExactSavedRequest(t *testing.T) {
	s := New("workspace")
	_, _ = s.AddInput(Input{ID: "request", Call: "call", Text: "Tell the Session to inspect the delay", Deferred: true})
	target := Focus{ID: "recipient", Label: "Recipient", Conversation: "conversation", Machine: "machine"}
	if err := s.PrepareMessage("send", "call", "request", target, "Please inspect the delay.", 2); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[0].Status != "waiting_confirmation" || s.Tasks[0].ApprovedVersion != 0 {
		t.Fatal(s)
	}
	if s.Claim("runner", 3) != nil {
		t.Fatal("preparation sent a message")
	}
	_, _ = s.AddInput(Input{ID: "not-consent", Call: "call", Text: "Can you send it?", Deferred: true})
	if err := s.ConfirmMessage("send", 1, "call", "not-consent", 4); err == nil {
		t.Fatal("a question approved the action")
	}
	_, _ = s.AddInput(Input{ID: "consent", Call: "call", Text: "可以，送出。", Deferred: true})
	if err := s.ConfirmMessage("send", 2, "call", "consent", 5); err == nil {
		t.Fatal("stale version approved")
	}
	if err := s.ConfirmMessage("send", 1, "foreign", "consent", 5); err == nil {
		t.Fatal("foreign call approved")
	}
	if err := s.ConfirmMessage("send", 1, "call", "consent", 5); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[0].ApprovedVersion != 1 || s.Tasks[0].Status != "queued" {
		t.Fatal(s.Tasks[0])
	}
	if err := s.ConfirmMessage("send", 1, "call", "consent", 6); err == nil {
		t.Fatal("consent reused")
	}
	if s.Tasks[0].Args["message"] != "Please inspect the delay." {
		t.Fatal(s.Tasks[0])
	}
}

func TestNewSpokenConsentDoesNotReleaseAnOlderWriteOrOverrideALaterCancellation(t *testing.T) {
	s := New("workspace")
	_, _ = s.AddInput(Input{ID: "old", Text: "Old send", Ready: true})
	_ = s.Apply(s.PlanVersion, []string{"old"}, []Change{{Op: "add", ID: "old-send", Text: "Old send", Tool: "send_message_to_session", Args: map[string]any{"message": "old"}}}, "", 1)
	_ = s.Edit("old-send", 1, "confirm", 2)
	_, _ = s.AddInput(Input{ID: "cancel-old", Text: "Cancel the earlier send", Ready: true})
	s.FailPlanning([]string{"cancel-old"}, "voice_work_timeout", "provider")
	_, _ = s.AddInput(Input{ID: "new", Call: "call", Text: "Send a separate message", Deferred: true})
	if err := s.PrepareMessage("new-send", "call", "new", Focus{ID: "recipient", Label: "Recipient"}, "New message", 3); err != nil {
		t.Fatal(err)
	}
	_, _ = s.AddInput(Input{ID: "consent", Call: "call", Text: "可以，送出", Deferred: true})
	if err := s.ConfirmMessage("new-send", 1, "call", "consent", 4); err != nil {
		t.Fatal(err)
	}
	next := s.Claim("runner", 5)
	if next == nil || next.ID != "new-send" {
		t.Fatal(next)
	}
	_, _ = s.AddInput(Input{ID: "cancel-new", Text: "Stop the new send"})
	if err := s.BeginEffect(next.ID, "runner", next.ActionKey, 6); err == nil {
		t.Fatal("later cancellation lost the race")
	}
	if s.Inputs[1].Planned {
		t.Fatal("earlier unresolved request was erased")
	}
}

func TestGoalCompletionPreservesTheActualSendReceipt(t *testing.T) {
	s := New("workspace")
	_, _ = s.AddInput(Input{ID: "source", Text: "Send", Ready: true})
	if err := s.Apply(s.PlanVersion, []string{"source"}, []Change{{Op: "add", ID: "send", Text: "Send", Tool: "send_message_to_session", Args: map[string]any{}}}, "", 1); err != nil {
		t.Fatal(err)
	}
	_ = s.Edit("send", 1, "confirm", 2)
	task := s.Claim("runner", 3)
	_ = s.BeginEffect(task.ID, "runner", task.ActionKey, 4)
	receipt := `{"success":true,"sent_to":"Recipient","receipt":{"request":"key","id":"recipient","action":"typed"}}`
	if err := s.Finish(task.ID, "runner", task.ActionKey, receipt, "", false, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Edit(task.ID, 1, "complete", 6); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[0].MessageReceipt != receipt || s.Tasks[0].DeliveredAt != 5 {
		t.Fatal("manual goal completion erased receipt", s.Tasks[0])
	}
}

// Calls on 2026-11-13 answered an exact read-back with 「可以」, "Okay" and
// 「我確認」; every one was refused for not being 「可以，送出」 and nothing sent.
func TestAPlainYesAfterTheRequestConfirmsOnlyAsTheNewestUtterance(t *testing.T) {
	prepared := func() State {
		s := New("workspace")
		_, _ = s.AddInput(Input{ID: "early-yes", Call: "call", Text: "好", Deferred: true})
		_, _ = s.AddInput(Input{ID: "request", Call: "call", Text: "Tell the Session to inspect the delay", Deferred: true})
		if err := s.PrepareMessage("send", "call", "request", Focus{ID: "r", Label: "R", Conversation: "c", Machine: "m"}, "Please inspect the delay.", 2); err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, yes := range []string{"可以", "Okay", "我確認", "好，送出吧"} {
		s := prepared()
		_, _ = s.AddInput(Input{ID: "yes", Call: "call", Text: yes, Deferred: true})
		if err := s.ConfirmMessage("send", 1, "call", "yes", 5); err != nil {
			t.Fatalf("%q: %v", yes, err)
		}
	}
	s := prepared()
	if err := s.ConfirmMessage("send", 1, "call", "early-yes", 5); err == nil {
		t.Fatal("a yes said before the request approved it")
	}
	_, _ = s.AddInput(Input{ID: "yes", Call: "call", Text: "可以", Deferred: true})
	_, _ = s.AddInput(Input{ID: "later", Call: "call", Text: "先等一下", Deferred: true})
	if err := s.ConfirmMessage("send", 1, "call", "yes", 5); err == nil {
		t.Fatal("a yes followed by another utterance approved it")
	}
	for _, no := range []string{"不要", "先不要送", "可以改成別的嗎"} {
		if PlainConsent(no) {
			t.Fatalf("%q read as consent", no)
		}
	}
}

// 2026-11-13: a request left unexecuted in one call was taken up by the next
// call and sent to the Session that call was placed from. Work does not cross
// calls: hanging up ends what the call left open, except what was confirmed.
func TestHangingUpClearsTheCallsOpenWorkButKeepsWhatWasConfirmed(t *testing.T) {
	s := New("workspace")
	_, _ = s.AddInput(Input{ID: "said", Call: "call", Text: "Tell it to stop", Deferred: true})
	_, _ = s.AddInput(Input{ID: "asked", Call: "call", Text: "Rename it", Deferred: false, Ready: true})
	_, _ = s.AddInput(Input{ID: "other-call", Call: "other", Text: "Keep me", Deferred: true})
	s.Tasks = []Task{
		{ID: "proposal", Tool: "send_message_to_session", Sources: []string{"said"}, Version: 1, Status: "waiting_confirmation"},
		{ID: "approved", Tool: "send_message_to_session", Sources: []string{"said"}, Version: 1, ApprovedVersion: 1, Status: "queued"},
		{ID: "read", Tool: "get_session_summary", Sources: []string{"asked"}, Version: 1, ApprovedVersion: 1, Status: "queued"},
		{ID: "sending", Tool: "send_message_to_session", Sources: []string{"said"}, Version: 1, ApprovedVersion: 1, Status: "running"},
		{ID: "elsewhere", Tool: "send_message_to_session", Sources: []string{"other-call"}, Version: 1, Status: "waiting_confirmation"},
	}
	if err := s.EndCall("call"); err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, task := range s.Tasks {
		status[task.ID] = task.Status
	}
	want := map[string]string{"proposal": "cancelled", "approved": "queued", "read": "cancelled", "sending": "running", "elsewhere": "waiting_confirmation"}
	for id, w := range want {
		if status[id] != w {
			t.Errorf("%s: %s, want %s", id, status[id], w)
		}
	}
	for _, in := range s.Inputs {
		open := !in.Planned
		if in.Call == "call" && open {
			t.Errorf("%s is still open after hanging up", in.ID)
		}
		if in.Call == "other" && !open {
			t.Errorf("another call's %s was closed", in.ID)
		}
	}
}
