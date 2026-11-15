package projectassistant

import (
	"strings"
	"testing"
	"time"
)

func TestRoutineIntervalValidationAndAnchoredAdvance(t *testing.T) {
	due := time.Unix(1000, 0)
	base := Routine{ID: "interval", Key: BotKey{MachineID: "machine", BotID: "bot"}, Project: ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: RepositoryProject}, ConversationID: "chat", Name: "Routine", Instruction: "Return a result", IntervalSeconds: 60, TimeoutSeconds: 3600, Enabled: true, NextDue: due, Version: 1}
	if e := ValidateRoutine(base); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Routine){func(r *Routine) { r.IntervalSeconds = 0 }, func(r *Routine) { r.IntervalSeconds = MaxRoutineIntervalSeconds + 1 }, func(r *Routine) { r.TimeoutSeconds = 0 }, func(r *Routine) { r.TimeoutSeconds = MaxRoutineTimeoutSeconds + 1 }, func(r *Routine) { r.Name = strings.Repeat("x", MaxRoutineNameBytes+1) }, func(r *Routine) { r.Instruction = strings.Repeat("x", MaxRoutineInstructionBytes+1) }, func(r *Routine) { r.Project.MachineID = "foreign" }} {
		r := base
		change(&r)
		if ValidateRoutine(r) == nil {
			t.Fatal("invalid routine accepted", r.ID)
		}
	}
	for _, s := range []int64{0, 60, 600, 1000} {
		now := due.Add(time.Duration(s) * time.Second)
		next := AdvanceRoutine(base, now)
		if !next.After(now) || next.Sub(due)%(60*time.Second) != 0 || next.Sub(now) > 60*time.Second {
			t.Fatal("lost anchored cadence", now, next)
		}
	}
	edge := base
	edge.Name = strings.Repeat("x", MaxRoutineNameBytes)
	edge.Instruction = strings.Repeat("x", MaxRoutineInstructionBytes)
	edge.IntervalSeconds = MaxRoutineIntervalSeconds
	edge.TimeoutSeconds = MaxRoutineTimeoutSeconds
	if e := ValidateRoutine(edge); e != nil {
		t.Fatal(e)
	}
}
