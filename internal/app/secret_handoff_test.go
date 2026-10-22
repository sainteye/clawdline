package app

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSecretHandoffIsScopedSingleUseAndExpires(t *testing.T) {
	h := NewSecretHandoffs()
	now := time.Unix(100, 0)
	h.Now = func() time.Time { return now }
	value := []byte("credential fixture")
	if err := h.Put("note", "session-a", value); err != nil {
		t.Fatal(err)
	}
	value[0] = 'X'
	if _, err := h.Take("note", "session-b"); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("wrong Session: %v", err)
	}
	claimed, err := h.Take("note", "session-a")
	if err != nil || string(claimed) != "credential fixture" {
		t.Fatalf("claim: %q %v", claimed, err)
	}
	if _, err := h.Take("note", "session-a"); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("second claim: %v", err)
	}
	if err := h.Put("note", "session-a", []byte("again")); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("used note accepted: %v", err)
	}
	if err := h.Put("later", "session-a", []byte("short lived")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Duration(MaxSecretHandoffSeconds) * time.Second)
	if _, err := h.Take("later", "session-a"); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("expired claim: %v", err)
	}
}

func TestSecretHandoffLimitsAndReplacement(t *testing.T) {
	h := NewSecretHandoffs()
	if err := h.Put("empty", "session", nil); !errors.Is(err, ErrSecretEmpty) {
		t.Fatalf("empty: %v", err)
	}
	if err := h.Put("large", "session", []byte(strings.Repeat("x", MaxSecretHandoffBytes+1))); !errors.Is(err, ErrSecretTooLarge) {
		t.Fatalf("large: %v", err)
	}
	if err := h.Put("replace", "session", []byte("first")); err != nil {
		t.Fatal(err)
	}
	old := h.values["replace"].value
	if err := h.Put("replace", "session", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if string(old) != "\x00\x00\x00\x00\x00" {
		t.Fatal("replaced value was not wiped")
	}
	for i := 0; i < MaxSecretHandoffPending-1; i++ {
		if err := h.Put(string(rune(0x100+i)), "session", []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Put("overflow", "session", []byte("x")); !errors.Is(err, ErrSecretFull) {
		t.Fatalf("overflow: %v", err)
	}
}

func TestSecretHandoffCommitFailureDoesNotPublishOrReplaceValue(t *testing.T) {
	h := NewSecretHandoffs()
	failed := errors.New("commit failed")
	for _, existing := range []bool{false, true} {
		if existing {
			if err := h.Put("note", "session", []byte("accepted fixture")); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.PutAndCommit("note", "session", []byte("rejected fixture"), func() error { return failed }); !errors.Is(err, failed) {
			t.Fatalf("commit failure: %v", err)
		}
		if h.Pending("note", "session") != existing {
			t.Fatal("failed commit changed availability")
		}
		if existing {
			value, err := h.Take("note", "session")
			if err != nil || string(value) != "accepted fixture" {
				t.Fatalf("accepted value replaced: %q %v", value, err)
			}
		}
	}
}
