package plugin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// recordingsStateFile is the on-disk persistence file name for the
// recording-rule reconcile summary. Kept next to the alerts-state.json
// in the same plugin data dir — separate file so each bundle's
// telemetry is independently readable and one corrupted write can't
// poison the other bundle's status surface.
//
// Like alerts-state.json, this file intentionally does NOT mirror the
// user's group toggles / threshold overrides / target-DS pick — those
// live authoritatively in the plugin's jsonData. Only reconcile
// telemetry needed to answer `/recordings/status` after a restart is
// persisted here.
const recordingsStateFile = "recordings-state.json"

// RecordingsState is the persisted view of the last recording reconcile.
// Kept deliberately small: just enough for /recordings/status to answer
// "when was the last run + how did it go" after a plugin restart.
type RecordingsState struct {
	LastReconciledAt     time.Time                  `json:"lastReconciledAt"`
	LastReconcileSummary RecordingsReconcileSummary `json:"lastReconcileSummary"`
}

// RecordingsReconcileSummary mirrors src/types.ts
// RecordingsReconcileSummary. Four counters — per-rule outcomes live in
// the ReconcileResult returned synchronously to the caller, not in
// persisted state.
type RecordingsReconcileSummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	Failed  int `json:"failed"`
}

// recordingsStore is a tiny JSON-file-backed persistence layer for
// RecordingsState. Same shape as alertsStore — single mutex, atomic
// rewrite via tmpfile + rename. Reconciles are human-triggered, so
// lock contention is not a hot path.
type recordingsStore struct {
	mu   sync.RWMutex
	path string
	st   RecordingsState
}

// newRecordingsStore constructs the store and eagerly loads any previous
// state from disk. A missing file is NOT an error (fresh install);
// other I/O failures are returned so the App factory can decide whether
// to abort.
func newRecordingsStore(dir string) (*recordingsStore, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &recordingsStore{path: filepath.Join(dir, recordingsStateFile)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *recordingsStore) load() error {
	buf, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(buf) == 0 {
		return nil
	}
	var st RecordingsState
	if err := json.Unmarshal(buf, &st); err != nil {
		return err
	}
	s.st = st
	return nil
}

// Get returns a copy of the current state. Copy semantics keep the caller
// free to mutate without racing on the store.
func (s *recordingsStore) Get() RecordingsState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.st
}

// Set writes a new state both in memory and to disk. Returns the
// disk-write error (if any); the in-memory state is always updated so
// the current process's subsequent reads reflect the latest summary even
// if persistence failed.
func (s *recordingsStore) Set(st RecordingsState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st = st
	buf, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
