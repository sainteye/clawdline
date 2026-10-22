package antigravity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	MaxHookConfigBytes    = 1 << 20
	MaxHookTimeoutSeconds = 3
)

// InstallPreview contains no authority to change a different file. Apply
// regenerates the proposed bytes from the command and current document.
type InstallPreview struct {
	Path, BeforeDigest, Helper string
	After                      []byte
	Disabled, Remove           bool
}

func configDigest(data []byte, exists bool) string {
	if !exists {
		return "absent"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hookConfigPath(workspace string) (string, error) {
	if !filepath.IsAbs(workspace) {
		return "", refuse("antigravity_workspace_invalid")
	}
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil || canonical != filepath.Clean(workspace) {
		return "", refuse("antigravity_workspace_invalid")
	}
	path := filepath.Join(workspace, ".agents", "hooks.json")
	for _, p := range []string{filepath.Dir(path), path} {
		info, e := os.Lstat(p)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return "", refuse("antigravity_config_unreadable")
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", refuse("antigravity_config_symlink")
		}
	}
	return path, nil
}

func readHookConfig(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, refuse("antigravity_config_unreadable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxHookConfigBytes+1))
	if err != nil {
		return nil, false, refuse("antigravity_config_unreadable")
	}
	if len(data) > MaxHookConfigBytes {
		return nil, false, refuse("antigravity_config_too_large")
	}
	return data, true, nil
}

// rawObject preserves every unowned value byte for byte. Duplicate keys are
// rejected so ownership cannot depend on a JSON reader's first/last-key rule.
func rawObject(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, refuse("antigravity_config_invalid")
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, refuse("antigravity_config_invalid")
		}
		key, ok := token.(string)
		if !ok {
			return nil, refuse("antigravity_config_invalid")
		}
		if _, ok = m[key]; ok {
			return nil, refuse("antigravity_config_duplicate_key")
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, refuse("antigravity_config_invalid")
		}
		m[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, refuse("antigravity_config_invalid")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, refuse("antigravity_config_invalid")
	}
	return m, nil
}
func writeRawObject(m map[string]json.RawMessage) []byte {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	out.WriteString("{\n")
	for i, key := range keys {
		if i > 0 {
			out.WriteString(",\n")
		}
		encoded, _ := json.Marshal(key)
		out.WriteString("  ")
		out.Write(encoded)
		out.WriteString(": ")
		out.Write(m[key])
	}
	out.WriteString("\n}\n")
	return out.Bytes()
}
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
func desiredHooks(helper string) map[string]json.RawMessage {
	result := map[string]json.RawMessage{}
	for _, event := range []string{"SessionStart", "PreInvocation", "PostInvocation", "PreToolUse", "PostToolUse", "Stop"} {
		hook := map[string]any{"type": "command", "command": shellWord(helper) + " provider observe antigravity --event " + event, "timeout": MaxHookTimeoutSeconds}
		var value any = []any{hook}
		if event == "PreToolUse" || event == "PostToolUse" {
			value = []any{map[string]any{"matcher": "*", "hooks": []any{hook}}}
		}
		result[event], _ = json.Marshal(value)
	}
	return result
}

// PreviewInstall only proposes the named workspace block. It never touches
// global preferences, status-line commands, credentials or vendor accounts.
func PreviewInstall(workspace, helper string, remove bool) (InstallPreview, error) {
	path, err := hookConfigPath(workspace)
	if err != nil {
		return InstallPreview{}, err
	}
	if !filepath.IsAbs(helper) || strings.ContainsAny(helper, "\x00\r\n") {
		return InstallPreview{}, refuse("antigravity_helper_invalid")
	}
	data, exists, err := readHookConfig(path)
	if err != nil {
		return InstallPreview{}, err
	}
	root := map[string]json.RawMessage{}
	if exists {
		root, err = rawObject(data)
		if err != nil {
			return InstallPreview{}, err
		}
	}
	owned := map[string]json.RawMessage{}
	if value, ok := root["clawdline"]; ok {
		owned, err = rawObject(value)
		if err != nil {
			return InstallPreview{}, refuse("antigravity_owned_block_conflict")
		}
	}
	p := InstallPreview{Path: path, BeforeDigest: configDigest(data, exists), Helper: helper, Remove: remove}
	if string(owned["enabled"]) == "false" || string(owned["disabled"]) == "true" {
		p.Disabled = true
		p.After = append([]byte(nil), data...)
		return p, nil
	}
	wanted := desiredHooks(helper)
	for key, value := range wanted {
		if old, ok := owned[key]; ok {
			var a, b bytes.Buffer
			_ = json.Compact(&a, old)
			_ = json.Compact(&b, value)
			if a.String() != b.String() {
				return InstallPreview{}, refuse("antigravity_owned_block_conflict")
			}
		}
		if remove {
			delete(owned, key)
		} else {
			owned[key] = value
		}
	}
	if len(owned) == 0 {
		delete(root, "clawdline")
	} else {
		root["clawdline"] = writeRawObject(owned)
	}
	p.After = writeRawObject(root)
	if len(p.After) > MaxHookConfigBytes {
		return InstallPreview{}, refuse("antigravity_config_too_large")
	}
	return p, nil
}

// ApplyInstall is an optimistic digest-checked atomic replacement. A private
// lock excludes other bridge writers; a second digest check catches editors
// that changed the file while the proposed output was being prepared.
func ApplyInstall(p InstallPreview) error {
	workspace := filepath.Dir(filepath.Dir(p.Path))
	path, err := hookConfigPath(workspace)
	if err != nil || path != p.Path {
		return refuse("antigravity_preview_invalid")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return refuse("antigravity_config_write_failed")
	}
	lock, err := os.OpenFile(path+".clawdline-lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return refuse("antigravity_config_busy")
	}
	defer os.Remove(lock.Name())
	_ = lock.Close()
	fresh, err := PreviewInstall(workspace, p.Helper, p.Remove)
	if err != nil {
		return err
	}
	if fresh.BeforeDigest != p.BeforeDigest || !bytes.Equal(fresh.After, p.After) || fresh.Disabled != p.Disabled {
		return refuse("antigravity_config_changed")
	}
	if fresh.Disabled {
		return nil
	}
	data, exists, err := readHookConfig(path)
	if err != nil {
		return err
	}
	if configDigest(data, exists) != p.BeforeDigest {
		return refuse("antigravity_config_changed")
	}
	mode := os.FileMode(0600)
	if exists {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() {
			return refuse("antigravity_config_unreadable")
		}
		mode = info.Mode().Perm()
	}
	if err = atomicReplace(path, p.After, mode); err != nil {
		return refuse("antigravity_config_write_failed")
	}
	return nil
}
