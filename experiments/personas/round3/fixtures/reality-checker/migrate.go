package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// taskV1 is the pre-TICKET-142 format: a bare JSON array, labels as one comma-separated
// string, and "created" instead of "created_at".
type taskV1 struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Done    bool      `json:"done"`
	Labels  string    `json:"labels"`
	Created time.Time `json:"created"`
}

// Migrate upgrades a v1 tasks file to v2 in place. It keeps the original next to it as
// <path>.v1.bak so an operator can roll back by hand. A v2 file is left untouched, so
// running it on every start is safe.
func Migrate(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	trimmed := strings.TrimSpace(string(b))
	if !strings.HasPrefix(trimmed, "[") {
		return false, nil // already v2 (an object with "version")
	}
	var old []taskV1
	if err := json.Unmarshal(b, &old); err != nil {
		return false, fmt.Errorf("migrate %s: %w", path, err)
	}
	f := fileV2{Version: 2, NextID: 1}
	for _, o := range old {
		t := &Task{ID: o.ID, Title: o.Title, Done: o.Done}
		if o.Labels != "" {
			t.Tags = strings.Split(o.Labels, ",")
		}
		f.Tasks = append(f.Tasks, t)
		var n int
		if _, err := fmt.Sscanf(o.ID, "t%d", &n); err == nil && n >= f.NextID {
			f.NextID = n + 1
		}
	}
	if err := os.WriteFile(path+".v1.bak", b, 0o644); err != nil {
		return false, err
	}
	out, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, out, 0o644)
}
