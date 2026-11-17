package taskresource

import (
	"testing"
	"time"
)

func TestCleanupRequiresIndependentPositiveEvidence(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	r := Resource{ID: "synthetic-resource", Class: Scratch, Manifest: "sealed"}
	f := Facts{ScopeAllowed: true, PathOwned: true, IdentityMatches: true, OwnerComplete: true,
		CompletedAt: now.Add(-25 * time.Hour).Unix(), OwnerPresence: "absent", UsePresence: "absent", OutputsSaved: true, ManifestMatches: true}
	p := DefaultPolicy()
	if got := Evaluate(r, p, f, now); got.Outcome != "eligible" {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		name, reason string
		change       func(*Resource, *Facts)
	}{
		{"artifact", "protected_class", func(r *Resource, _ *Facts) { r.Class = Artifact }},
		{"evidence", "protected_class", func(r *Resource, _ *Facts) { r.Class = Evidence }},
		{"removed lifecycle", "already_removed", func(r *Resource, _ *Facts) { r.LastOutcome = "removed" }},
		{"future lifecycle", "lifecycle_unknown", func(r *Resource, _ *Facts) { r.LastOutcome = "future_hold" }},
		{"pin", "pinned", func(r *Resource, _ *Facts) { r.Pinned = true }},
		{"scope", "scope_not_allowed", func(_ *Resource, f *Facts) { f.ScopeAllowed = false }},
		{"foreign", "path_not_owned", func(_ *Resource, f *Facts) { f.PathOwned = false }},
		{"reused directory", "identity_changed", func(_ *Resource, f *Facts) { f.IdentityMatches = false }},
		{"unfinished", "completion_missing", func(_ *Resource, f *Facts) { f.OwnerComplete = false }},
		{"missing completion time", "completion_missing", func(_ *Resource, f *Facts) { f.CompletedAt = 0 }},
		{"owner unknown", "owner_unknown", func(_ *Resource, f *Facts) { f.OwnerPresence = "unknown" }},
		{"owner live", "owner_present", func(_ *Resource, f *Facts) { f.OwnerPresence = "present" }},
		{"reader unknown", "use_unknown", func(_ *Resource, f *Facts) { f.UsePresence = "" }},
		{"reader live", "use_present", func(_ *Resource, f *Facts) { f.UsePresence = "present" }},
		{"no publication", "outputs_unsealed", func(_ *Resource, f *Facts) { f.OutputsSaved = false }},
		{"no manifest", "outputs_unsealed", func(r *Resource, _ *Facts) { r.Manifest = "" }},
		{"changed output", "outputs_changed", func(_ *Resource, f *Facts) { f.ManifestMatches = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr, ff := r, f
			tc.change(&rr, &ff)
			if got := Evaluate(rr, p, ff, now); got.Outcome != "kept" || got.Reason != tc.reason {
				t.Fatal(got)
			}
		})
	}
}

func TestEveryTerminalTaskKeepsScratchForOneDayAndLogsForAWeek(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	f := Facts{ScopeAllowed: true, PathOwned: true, IdentityMatches: true, OwnerComplete: true,
		OwnerPresence: "absent", UsePresence: "absent", OutputsSaved: true, ManifestMatches: true}
	for _, class := range []Class{Scratch, Cache, Log} {
		duration := 24 * time.Hour
		if class == Log {
			duration = 7 * 24 * time.Hour
		}
		for _, delta := range []time.Duration{-time.Second, 0, time.Second} {
			f.CompletedAt = now.Add(-duration - delta).Unix()
			got := Evaluate(Resource{ID: "synthetic", Class: class, Manifest: "sealed"}, DefaultPolicy(), f, now)
			if (got.Outcome == "eligible") != (delta >= 0) {
				t.Fatalf("%s at %v: %+v", class, delta, got)
			}
		}
	}
}

func TestStoragePressureRefusesNewWorkWithoutRelaxingProtection(t *testing.T) {
	p := DefaultPolicy()
	u := Usage{Known: true, LargestCache: p.CacheBytes, CacheTotal: p.CacheTotal, ScratchTotal: p.ScratchTotal, Available: p.DiskReserve}
	if reason := Admission(p, u); reason != "" {
		t.Fatal(reason)
	}
	for _, tc := range []struct {
		reason string
		change func(*Usage)
	}{
		{"storage_unknown", func(u *Usage) { u.Known = false }},
		{"disk_reserve_unavailable", func(u *Usage) { u.Available-- }},
		{"cache_over_budget", func(u *Usage) { u.LargestCache++ }},
		{"cache_total_over_budget", func(u *Usage) { u.CacheTotal++ }},
		{"scratch_over_budget", func(u *Usage) { u.ScratchTotal++ }},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			v := u
			tc.change(&v)
			if got := Admission(p, v); got != tc.reason {
				t.Fatal(got)
			}
		})
	}
}
