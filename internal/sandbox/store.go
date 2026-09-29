package sandbox

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Snapshot is the on-disk rule state: published versions and all drafts.
type Snapshot struct {
	Versions []RuleVersion
	Drafts   []Draft
}

// Store persists the rule state as one JSON file, written atomically in the
// same style as internal/journal.SnapshotFile. Structs keep default Go field
// names to match the rest of the persisted models.
type Store struct {
	path string
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Path() string { return s.path }

func (s *Store) Save(snapshot Snapshot) error {
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	file, err := os.OpenFile(tmp, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Load() (Snapshot, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Snapshot{}, nil
		}
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}
