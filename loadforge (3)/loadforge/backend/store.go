package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

// Store persists finished runs as JSON files on a volume.
type Store struct {
	dir   string
	mu    sync.RWMutex
	index map[string]RunListItem
}

var idRe = regexp.MustCompile(`^[0-9A-Za-z-]{6,64}$`)

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	st := &Store{dir: dir, index: map[string]RunListItem{}}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r Run
		if json.Unmarshal(b, &r) == nil && r.ID != "" {
			st.index[r.ID] = listItem(&r)
		}
	}
	return st, nil
}

func listItem(r *Run) RunListItem {
	return RunListItem{ID: r.ID, Name: r.Config.Name, Type: r.Config.Type, Status: r.Status, StartedAt: r.StartedAt,
		FinishedAt: r.FinishedAt, Requests: r.Summary.Requests, ErrorRate: r.Summary.ErrorRate,
		AvgRPS: r.Summary.AvgRPS, P95: r.Summary.Latency.P95, Passed: r.Summary.Passed && len(r.Summary.Thresholds) > 0}
}

func (s *Store) path(id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", errors.New("invalid id")
	}
	return filepath.Join(s.dir, id+".json"), nil
}

func (s *Store) Save(r *Run) error {
	p, err := s.path(r.ID)
	if err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	s.mu.Lock()
	s.index[r.ID] = listItem(r)
	s.mu.Unlock()
	return nil
}

func (s *Store) Get(id string) (*Run, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var r Run
	return &r, json.Unmarshal(b, &r)
}

func (s *Store) Delete(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.index, id)
	s.mu.Unlock()
	return os.Remove(p)
}

func (s *Store) List() []RunListItem {
	s.mu.RLock()
	out := make([]RunListItem, 0, len(s.index))
	for _, v := range s.index {
		out = append(out, v)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}
