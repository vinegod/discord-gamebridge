// Package cooldown tracks per-user command cooldowns with optional bbolt persistence.
// A nil *Store is valid and falls back to pure in-memory tracking.
package cooldown

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var dbBucket = []byte("cooldowns")

// Store holds cooldown expiry times in memory, optionally backed by a bbolt database.
// Entries are loaded on Open and pruned lazily on Check.
type Store struct {
	db  *bolt.DB // nil when running in-memory only
	mem sync.Map // key: storeKey string, value: time.Time
}

// DefaultPath returns the platform-appropriate path for the state database,
// respecting XDG_CONFIG_HOME on Linux, ~/Library/Application Support on macOS,
// and %AppData% on Windows.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(dir, "discord-gamebridge", "state.db"), nil
}

// Open opens or creates the bbolt database at path and loads unexpired cooldowns
// into memory. Expired entries are pruned on load.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}

	s := &Store{db: db}
	if err := s.load(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database. Safe to call on a nil Store.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close state db: %w", err)
	}
	return nil
}

// Set records that (command, userID) is on cooldown until expiry.
// Safe to call on a nil Store (no-op).
func (s *Store) Set(command, userID string, expiry time.Time) {
	if s == nil {
		return
	}
	k := storeKey(command, userID)
	s.mem.Store(k, expiry)
	if s.db == nil {
		return
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(dbBucket)
		if err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
		return b.Put([]byte(k), encodeTime(expiry))
	}); err != nil {
		slog.Warn("cooldown persist failed", "error", err)
	}
}

// Check returns the remaining cooldown for (command, userID).
// Returns 0, false if there is no active cooldown.
// Safe to call on a nil Store (always returns 0, false).
func (s *Store) Check(command, userID string) (time.Duration, bool) {
	if s == nil {
		return 0, false
	}
	k := storeKey(command, userID)
	v, ok := s.mem.Load(k)
	if !ok {
		return 0, false
	}
	remaining := time.Until(v.(time.Time))
	if remaining <= 0 {
		s.evict(k)
		return 0, false
	}
	return remaining, true
}

// evict removes an expired entry from both memory and the database.
func (s *Store) evict(k string) {
	s.mem.Delete(k)
	if s.db == nil {
		return
	}
	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(dbBucket)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(k))
	})
}

// load reads all cooldown entries from the database, stores unexpired ones in memory,
// and deletes expired ones from the database.
func (s *Store) load() error {
	return s.db.Update(func(tx *bolt.Tx) error { //nolint:wrapcheck // internal helper, errors wrapped inside
		b, err := tx.CreateBucketIfNotExists(dbBucket)
		if err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
		now := time.Now()
		var stale [][]byte
		_ = b.ForEach(func(k, v []byte) error {
			if expiry := decodeTime(v); expiry.After(now) {
				s.mem.Store(string(k), expiry)
			} else {
				stale = append(stale, append([]byte(nil), k...))
			}
			return nil
		})
		for _, k := range stale {
			_ = b.Delete(k)
		}
		return nil
	})
}

func storeKey(command, userID string) string { return command + "\x00" + userID }

func encodeTime(t time.Time) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(t.UnixNano()))
	return b
}

func decodeTime(b []byte) time.Time {
	if len(b) < 8 {
		return time.Time{}
	}
	//nolint:gosec // timestamp nanoseconds never overflow int64 in practice
	return time.Unix(0, int64(binary.BigEndian.Uint64(b)))
}
