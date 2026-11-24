package projectassistant

import (
	"errors"
	"slices"
	"strings"
)

// SettingsConflictPrefix starts the field of a pa_stale_version refusal that
// names settings changed both by this edit and elsewhere, for example
// "settings.instructions,name". Older clients still read the code as stale.
const SettingsConflictPrefix = "settings."

// MaxSettingsRebaseAttempts bounds how often one settings save is rebased on
// a fresh read after other Bot activity won the commit race. At the bound the
// save is refused as pa_stale_version "bot" and the draft is kept.
const MaxSettingsRebaseAttempts = 3

// The editable fields a conflict can name, in the order they are reported.
const (
	ConflictName              = "name"
	ConflictAppearance        = "appearance"
	ConflictInstructions      = "instructions"
	ConflictAgent             = "agent"
	ConflictApproval          = "approval"
	ConflictRuntimeSettings   = "runtime_settings"
	ConflictLoginProfile      = "login_profile"
	ConflictExtensions        = "extensions"
	ConflictFallbacks         = "fallbacks"
	ConflictNotify            = "notify"
	ConflictBackgroundThreads = "background_threads"
	ConflictManageTeam        = "manage_team"
)

// SettingsConflicts returns the fields named by a settings conflict refusal.
func SettingsConflicts(err error) []string {
	if Code(err) != StaleVersion {
		return nil
	}
	var f *Failure
	if !errors.As(err, &f) {
		return nil
	}
	field := f.Field
	if !strings.HasPrefix(field, SettingsConflictPrefix) {
		return nil
	}
	return strings.Split(strings.TrimPrefix(field, SettingsConflictPrefix), ",")
}

func effectiveSteps(steps []FallbackStep, agents []Agent) []FallbackStep {
	if len(steps) > 0 {
		return steps
	}
	out := []FallbackStep{}
	for _, a := range agents {
		if a != "" {
			out = append(out, FallbackStep{Agent: a})
		}
	}
	return out
}

func notifyOn(v string) bool { return v != "off" }

func threadsOf(v int) int {
	if v == 0 {
		return DefaultBackgroundThreads
	}
	return v
}

// RebaseEdit moves an edit made against in.Base onto the current Bot. A field
// the edit left at its base value takes the current value, so a change saved
// elsewhere is never written back; a field the edit changed is kept unless the
// current Bot changed it too, to a different value. Unrelated Bot activity
// (turns, memory, background tasks, ordering) advances the version without
// touching these fields and never conflicts. Without a base the edit is
// returned unchanged and ApplyEdit checks the version as before.
func RebaseEdit(current Bot, in EditBot) (EditBot, error) {
	base := in.Base
	if base == nil {
		return in, nil
	}
	var conflicts []string
	// decide reports whether the edit changed the field, recording a conflict
	// when another writer changed it as well, to a different value.
	decide := func(field string, edited, changedElsewhere, agrees bool) bool {
		if edited && changedElsewhere && !agrees && !in.OverwriteConflicts {
			conflicts = append(conflicts, field)
		}
		return edited
	}
	steps := func(b Bot) []FallbackStep { return effectiveSteps(b.FallbackSteps.Steps(), b.FallbackAgents.Agents()) }
	baseSteps := effectiveSteps(base.FallbackSteps, base.FallbackAgents)

	if !decide(ConflictName, in.Name != base.Name, current.Name != base.Name, current.Name == in.Name) {
		in.Name = current.Name
	}
	if !decide(ConflictAppearance, in.Appearance != base.Appearance, current.Appearance != base.Appearance, current.Appearance == in.Appearance) {
		in.Appearance = current.Appearance
	}
	if !decide(ConflictInstructions, in.Instructions != base.Instructions, current.Instructions.Text != base.Instructions, current.Instructions.Text == in.Instructions) {
		in.Instructions = current.Instructions.Text
	}
	if in.Agent != "" && !decide(ConflictAgent, in.Agent != base.Agent, current.Agent != base.Agent, current.Agent == in.Agent) {
		in.Agent = ""
	}
	if in.PermissionPolicy != "" {
		edited := in.PermissionPolicy != base.PermissionPolicy || in.Mode != base.Mode
		elsewhere := current.PermissionPolicy != base.PermissionPolicy || current.Mode != base.Mode
		if !decide(ConflictApproval, edited, elsewhere, current.PermissionPolicy == in.PermissionPolicy && current.Mode == in.Mode) {
			in.PermissionPolicy, in.Mode = "", ""
		}
	}
	if in.RuntimeSettings != nil && !decide(ConflictRuntimeSettings, !SameRuntimeSettings(*in.RuntimeSettings, base.RuntimeSettings), !SameRuntimeSettings(current.RuntimeSettings, base.RuntimeSettings), SameRuntimeSettings(current.RuntimeSettings, *in.RuntimeSettings)) {
		in.RuntimeSettings = nil
	}
	if in.LoginProfile != nil && !decide(ConflictLoginProfile, *in.LoginProfile != base.LoginProfile, current.LoginProfile != base.LoginProfile, current.LoginProfile == *in.LoginProfile) {
		in.LoginProfile = nil
	}
	if in.Extensions != nil && !decide(ConflictExtensions, !SameExtensions(*in.Extensions, base.Extensions), !SameExtensions(current.Extensions, base.Extensions), SameExtensions(current.Extensions, *in.Extensions)) {
		in.Extensions = nil
	}
	if in.FallbackSteps != nil || in.FallbackAgents != nil {
		var mine []FallbackStep
		if in.FallbackSteps != nil && len(*in.FallbackSteps) > 0 {
			mine = *in.FallbackSteps
		} else if in.FallbackAgents != nil {
			mine = effectiveSteps(nil, *in.FallbackAgents)
		} else {
			mine = []FallbackStep{}
		}
		now := steps(current)
		if !decide(ConflictFallbacks, !slices.Equal(mine, baseSteps), !slices.Equal(now, baseSteps), slices.Equal(now, mine)) {
			in.FallbackSteps, in.FallbackAgents = nil, nil
		}
	}
	if in.Notify != "" && !decide(ConflictNotify, notifyOn(in.Notify) != notifyOn(base.Notify), notifyOn(current.Notify) != notifyOn(base.Notify), notifyOn(current.Notify) == notifyOn(in.Notify)) {
		in.Notify = ""
	}
	if in.BackgroundThreads != 0 && !decide(ConflictBackgroundThreads, in.BackgroundThreads != threadsOf(base.BackgroundThreads), current.BackgroundConcurrency() != threadsOf(base.BackgroundThreads), current.BackgroundConcurrency() == in.BackgroundThreads) {
		in.BackgroundThreads = 0
	}
	if in.ManageTeam != nil && !decide(ConflictManageTeam, *in.ManageTeam != base.ManageTeam, current.ManageTeam != base.ManageTeam, current.ManageTeam == *in.ManageTeam) {
		in.ManageTeam = nil
	}
	if len(conflicts) > 0 {
		return in, fail(StaleVersion, SettingsConflictPrefix+strings.Join(conflicts, ","))
	}
	in.ExpectedVersion = current.Version
	return in, nil
}
