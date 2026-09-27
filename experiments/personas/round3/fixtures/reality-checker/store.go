package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("task not found")

type Task struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	Due       Date      `json:"due"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

// fileV2 is the on-disk format since TICKET-142.
type fileV2 struct {
	Version int     `json:"version"`
	NextID  int     `json:"next_id"`
	Tasks   []*Task `json:"tasks"`
}

type Store struct {
	mu     sync.Mutex
	path   string
	nextID int
	tasks  map[string]*Task
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, tasks: map[string]*Task{}, nextID: 1}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f fileV2
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	for _, t := range f.Tasks {
		s.tasks[t.ID] = t
	}
	s.nextID = f.NextID
	return s, nil
}

// save writes the whole file to a temp file and renames it over the old one, so a crash
// mid-write never leaves a half-written tasks file.
func (s *Store) save() error {
	f := fileV2{Version: 2, NextID: s.nextID, Tasks: s.sortedLocked()}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".tasks-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// IDs are zero-padded ("t0007"), so sorting the strings sorts them numerically.
func (s *Store) sortedLocked() []*Task {
	out := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.sortedLocked() {
		out = append(out, *t)
	}
	return out
}

func (s *Store) Get(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	return *t, nil
}

func (s *Store) Create(t Task, now time.Time) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = fmt.Sprintf("t%04d", s.nextID)
	s.nextID++
	t.CreatedAt = now.UTC()
	s.tasks[t.ID] = &t
	if err := s.save(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// Update applies fn to the stored task and saves.
func (s *Store) Update(id string, fn func(*Task)) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("update %s: %v", id, ErrNotFound)
	}
	fn(t)
	if err := s.save(); err != nil {
		return Task{}, err
	}
	return *t, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return s.save()
}
