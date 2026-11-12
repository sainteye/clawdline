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
	Owner   map[string]string `json:"owner"`
	Current map[string]int    `json:"current"`
	Person  []int             `json:"person"`
	Muted   []string          `json:"muted"`
}

// saveTabs writes the state; the caller holds b.mu. A failed write leaves the
// previous file, so a restart forgets only what changed since.
func (b *Bridge) saveTabs() {
	if b.tabsPath == "" {
		return
	}
	st := tabsState{Owner: map[string]string{}, Current: b.current, Person: []int{}, Muted: []string{}}
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
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.tabsPath), ".browser-tabs-*")
	if err != nil {
		return
	}
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0o600)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil || os.Rename(tmp.Name(), b.tabsPath) != nil {
		os.Remove(tmp.Name())
	}
}

func (b *Bridge) loadTabs(path string) error {
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
	c := b.owner[tab]
	delete(b.owner, tab)
	delete(b.person, tab)
	if c != "" && b.current[c] == tab {
		delete(b.current, c)
	}
	b.saveTabs()
}
