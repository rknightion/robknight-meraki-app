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

// alertsStateFile is the on-disk persistence file name for the alerts
// reconcile summary. We deliberately do NOT round-trip the user's group
// toggles / threshold overrides through this file — those live in the
// plugin's jsonData (written by Grafana on settings save, see §4.5.6).
// The only state this file owns is the reconcile telemetry needed to
// populate the status endpoint after a plugin restart.
const alertsStateFile = "alerts-state.json"

// AlertsState is the persisted view of the last reconcile. Kept deliberately
// small: just enough for /alerts/status to answer "when was the last
// reconcile and how did it go" after a plugin restart.
type AlertsState struct {
	LastReconciledAt      time.Time              `json:"lastReconciledAt"`
	LastReconcileSummary  AlertsReconcileSummary `json:"lastReconcileSummary"`
}

// AlertsReconcileSummary mirrors src/types.ts AlertsReconcileSummary.
// The four counters are the product of a reconcile — detailed per-rule
// outcomes live in the ReconcileResult returned synchronously to the
// caller, not in persisted state.
type AlertsReconcileSummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	Failed  int `json:"failed"`
}

// alertsStore is a tiny JSON-file-backed persistence layer for AlertsState.
// Concurrency: guarded by a single mutex — the file is rewritten in full on
// every write, so lock contention is not a hot path (reconciles are
// human-triggered). Writes are best-effort: if the disk write fails the
// in-memory state is still updated so the current process keeps serving the
// latest summary, and the error is returned for logging by the caller.
type alertsStore struct {
	mu   sync.RWMutex
	path string
	st   AlertsState
}

// newAlertsStore constructs the store and eagerly loads any previous state
// from disk. A missing file is NOT an error (fresh install); other I/O
// failures are returned so the App factory can decide whether to abort.
func newAlertsStore(dir string) (*alertsStore, error) {
	if dir == "" {
		// Caller provided no directory — fall back to a process-local
		// temp dir so the store still works in tests / edge cases where
		// GF_PATHS_DATA isn't set. The trade-off: state doesn't survive
		// Grafana restarts, but tests don't need it to.
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &alertsStore{path: filepath.Join(dir, alertsStateFile)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *alertsStore) load() error {
	buf, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // fresh install — keep the zero-valued state
		}
		return err
	}
	// Empty file = fresh init that crashed mid-write. Treat as no state.
	if len(buf) == 0 {
		return nil
	}
	var st AlertsState
	if err := json.Unmarshal(buf, &st); err != nil {
		return err
	}
	s.st = st
	return nil
}

// Get returns a copy of the current state. Copy semantics keep the caller
// free to mutate without racing on the store.
func (s *alertsStore) Get() AlertsState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.st
}

// Set writes a new state both in memory and to disk. Returns the disk-write
// error (if any); the in-memory state is always updated so the current
// process's subsequent reads reflect the latest summary even if persistence
// failed.
func (s *alertsStore) Set(st AlertsState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st = st
	buf, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// Write via tmpfile + rename for atomicity so a crash mid-write doesn't
	// leave a half-written file the next process can't decode.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// alertsDataDir returns the directory the alerts store should write to.
// Grafana passes GF_PATHS_DATA to plugin processes; when absent (tests,
// standalone execution) fall back to the OS temp dir.
func alertsDataDir() string {
	if p := os.Getenv("GF_PATHS_DATA"); p != "" {
		return filepath.Join(p, "plugins", pluginID)
	}
	return filepath.Join(os.TempDir(), pluginID)
}
