package antigravity

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
)

// InstalledHooks is an observed named block and its current whole-file digest.
// It is not person consent, effective native permission or a loading receipt.
type InstalledHooks struct{ Digest, PlanDigest string }

// ManagedHookPlanDigest binds the six managed commands to one helper path and
// config root, independent of workspace-specific unowned hooks or formatting.
func ManagedHookPlanDigest(helper, configRoot string) (string, error) {
	if !filepath.IsAbs(helper) || strings.ContainsAny(helper, "\x00\r\n") {
		return "", refuse("antigravity_helper_invalid")
	}
	if _, err := ProviderStateDir(configRoot); err != nil {
		return "", err
	}
	return configDigest(writeRawObject(desiredHooks(helper, configRoot)), true), nil
}

// InspectInstallForState checks the exact managed block without writing or
// treating an existing file as consent. Unknown owned fields refuse authority;
// unrelated top-level blocks remain allowed and are covered by Digest.
func InspectInstallForState(workspace, helper, configRoot string) (InstalledHooks, error) {
	p, err := PreviewInstallForState(workspace, helper, configRoot, false)
	if err != nil {
		return InstalledHooks{}, err
	}
	if p.Disabled {
		return InstalledHooks{}, refuse("antigravity_hooks_disabled")
	}
	data, exists, err := readHookConfig(p.Path)
	if err != nil {
		return InstalledHooks{}, err
	}
	if !exists {
		return InstalledHooks{}, refuse("antigravity_hooks_not_installed")
	}
	if configDigest(data, exists) != p.BeforeDigest {
		return InstalledHooks{}, refuse("antigravity_config_changed")
	}
	root, err := rawObject(data)
	if err != nil {
		return InstalledHooks{}, err
	}
	owned, err := rawObject(root["clawdline"])
	if err != nil {
		return InstalledHooks{}, refuse("antigravity_hooks_not_installed")
	}
	wanted := desiredHooks(helper, configRoot)
	if len(owned) != len(wanted) {
		return InstalledHooks{}, refuse("antigravity_hooks_not_installed")
	}
	for event, value := range wanted {
		var actual, expected bytes.Buffer
		if json.Compact(&actual, owned[event]) != nil || json.Compact(&expected, value) != nil || !bytes.Equal(actual.Bytes(), expected.Bytes()) {
			return InstalledHooks{}, refuse("antigravity_hooks_not_installed")
		}
	}
	plan, err := ManagedHookPlanDigest(helper, configRoot)
	if err != nil {
		return InstalledHooks{}, err
	}
	return InstalledHooks{Digest: p.BeforeDigest, PlanDigest: plan}, nil
}
