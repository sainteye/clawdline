package browserbridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

// TabsFile keeps which Session owns which tab, the person's takeovers and
// the muted Sessions across a daemon restart. Without it a restart (an
// update, another Session's dev install) left every agent tab ownerless and
// the panel saying the Session had none (seen on 2026-11-12).
const TabsFile = "browser-tabs.json"

type tabsState struct {
	Mode    string            `json:"mode,omitempty"`
	Closing map[int]string    `json:"closing,omitempty"`
	Stopped bool              `json:"stopped"`
	Owner   map[string]string `json:"owner"`
	Current map[string]int    `json:"current"`
	Person  []int             `json:"person"`
	Muted   []string          `json:"muted"`
}

// saveTabs writes the control state atomically; the caller holds b.mu.
// A failed write remains visible until a later explicit save succeeds.
func (b *Bridge) saveTabs() (err error) {
	defer func() { b.stateErr = err }()
	if b.tabsPath == "" {
		return nil
	}
	st := tabsState{Mode: b.mode, Closing: b.closing, Stopped: b.stopped, Owner: map[string]string{}, Current: b.current, Person: []int{}, Muted: []string{}}
	for t, c := range b.owner {
		st.Owner[strconv.Itoa(t)] = c
	}
	for t, on := range b.person {
		if on {
			st.Person = append(st.Person, t)
		}
	}
	for c := range b.muted {
		st.Muted = append(st.Muted, c)
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.tabsPath), ".browser-tabs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0o600)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), b.tabsPath)
}

func (b *Bridge) loadTabs(path string) (err error) {
	defer func() {
		if err != nil {
			b.stateErr = err
			b.stopped = true
		}
	}()
	b.tabsPath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var st tabsState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	b.stopped = st.Stopped
	if st.Mode == "user_chrome" {
		b.mode = st.Mode
	}
	for tab, id := range st.Closing {
		if tab > 0 && id != "" {
			b.closing[tab] = id
		}
	}
	for k, c := range st.Owner {
		if t, err := strconv.Atoi(k); err == nil && t > 0 && c != "" {
			b.owner[t] = c
		}
	}
	for c, t := range st.Current {
		if b.owner[t] == c {
			b.current[c] = t
		}
	}
	for _, t := range st.Person {
		b.person[t] = true
	}
	for _, c := range st.Muted {
		b.muted[c] = true
	}
	return nil
}

// Forget drops a tab the extension reports closed, so it stops being listed
// as a Session's tab.
func (b *Bridge) Forget(tab int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.forgetLocked(tab)
	b.saveTabs()
}

func (b *Bridge) forgetLocked(tab int) {
	delete(b.closing, tab)
	c := b.owner[tab]
	delete(b.owner, tab)
	delete(b.person, tab)
	delete(b.controlEpoch, tab)
	if c != "" && b.current[c] == tab {
		delete(b.current, c)
	}
}
