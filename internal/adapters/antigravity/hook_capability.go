package antigravity

import (
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const CapabilityFileEnv = "CLAWDLINE_ANTIGRAVITY_CAPABILITY_FILE"

// LoadHookLaunch reads only the explicitly inherited, private launch file.
// An absent environment value is unmanaged; an invalid present value refuses.
func LoadHookLaunch(stateDir, path string) (*HookLaunch, error) {
	if path == "" {
		return nil, nil
	}
	bad := func() (*HookLaunch, error) { return nil, refuse("antigravity_capability_invalid") }
	id := filepath.Base(filepath.Dir(path))
	if !validID(id) || path != filepath.Join(stateDir, "launches", id, "capability") {
		return bad()
	}
	for _, dir := range []string{stateDir, filepath.Join(stateDir, "launches"), filepath.Dir(path)} {
		if privateHookDirectory(dir, false) != nil {
			return bad()
		}
	}
	before, err := os.Lstat(path)
	if err != nil || !privateCapabilityFile(before) {
		return bad()
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return bad()
	}
	defer root.Close()
	f, err := root.Open("capability")
	if err != nil {
		return bad()
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !privateCapabilityFile(opened) || !os.SameFile(before, opened) {
		return bad()
	}
	// 64 is the fixed cryptographic wire encoding, not a tunable capacity.
	data, err := io.ReadAll(io.LimitReader(f, 65))
	after, statErr := os.Lstat(path)
	if err != nil || statErr != nil || !privateCapabilityFile(after) || !os.SameFile(before, after) || len(data) != 64 || strings.ToLower(string(data)) != string(data) {
		return bad()
	}
	if _, err = hex.DecodeString(string(data)); err != nil {
		return bad()
	}
	return &HookLaunch{ID: id, Capability: string(data)}, nil
}

func privateCapabilityFile(info os.FileInfo) bool {
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != 64 {
		return false
	}
	v := reflect.ValueOf(info.Sys())
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return false
	}
	uid := v.Elem().FieldByName("Uid")
	links := v.Elem().FieldByName("Nlink")
	return uid.IsValid() && uid.CanUint() && uid.Uint() == uint64(os.Geteuid()) && links.IsValid() && links.CanUint() && links.Uint() == 1
}
