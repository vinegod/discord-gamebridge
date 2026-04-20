package cooldown

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// openTemp opens a Store backed by a temp-dir database.
func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ── nil-safe behaviour ────────────────────────────────────────────────────────

func TestStore_NilStore_CheckReturnsFalse(t *testing.T) {
	var s *Store
	if _, ok := s.Check("restart", "u1"); ok {
		t.Error("nil Store.Check should return false")
	}
}

func TestStore_NilStore_SetNoPanic(t *testing.T) {
	var s *Store
	s.Set("restart", "u1", time.Now().Add(time.Minute)) // must not panic
}

func TestStore_NilStore_CloseNilError(t *testing.T) {
	var s *Store
	if err := s.Close(); err != nil {
		t.Errorf("nil Store.Close should return nil, got: %v", err)
	}
}

// ── in-memory behaviour ───────────────────────────────────────────────────────

func TestStore_Set_CheckReturnsRemaining(t *testing.T) {
	s := openTemp(t)
	s.Set("kick", "u1", time.Now().Add(10*time.Second))
	remaining, ok := s.Check("kick", "u1")
	if !ok {
		t.Fatal("expected active cooldown, got false")
	}
	if remaining <= 0 || remaining > 10*time.Second {
		t.Errorf("remaining %v outside expected range (0, 10s]", remaining)
	}
}

func TestStore_Check_NoEntry_ReturnsFalse(t *testing.T) {
	s := openTemp(t)
	if _, ok := s.Check("kick", "unknown"); ok {
		t.Error("expected no cooldown for unknown key")
	}
}

func TestStore_Check_ExpiredEntry_ReturnsFalse(t *testing.T) {
	s := openTemp(t)
	s.Set("kick", "u1", time.Now().Add(-time.Millisecond)) // already expired
	if _, ok := s.Check("kick", "u1"); ok {
		t.Error("expired cooldown should return false")
	}
}

func TestStore_Check_ExpiredEntry_EvictedFromMemory(t *testing.T) {
	s := openTemp(t)
	s.Set("kick", "u1", time.Now().Add(-time.Millisecond))
	s.Check("kick", "u1") // triggers eviction
	// second check must also return false (not re-loaded from stale mem)
	if _, ok := s.Check("kick", "u1"); ok {
		t.Error("evicted entry should not reappear")
	}
}

func TestStore_Set_OverwritesExpiry(t *testing.T) {
	s := openTemp(t)
	s.Set("ping", "u1", time.Now().Add(time.Second))
	s.Set("ping", "u1", time.Now().Add(time.Minute)) // extend
	remaining, ok := s.Check("ping", "u1")
	if !ok {
		t.Fatal("expected active cooldown after overwrite")
	}
	if remaining < 50*time.Second {
		t.Errorf("expected ~1 minute remaining after overwrite, got %v", remaining)
	}
}

func TestStore_DifferentCommands_TrackedSeparately(t *testing.T) {
	s := openTemp(t)
	s.Set("kick", "u1", time.Now().Add(time.Minute))
	s.Set("restart", "u1", time.Now().Add(-time.Millisecond)) // expired

	if _, ok := s.Check("kick", "u1"); !ok {
		t.Error("kick cooldown should be active")
	}
	if _, ok := s.Check("restart", "u1"); ok {
		t.Error("restart cooldown should be expired")
	}
}

func TestStore_DifferentUsers_TrackedSeparately(t *testing.T) {
	s := openTemp(t)
	s.Set("kick", "u1", time.Now().Add(time.Minute))

	if _, ok := s.Check("kick", "u1"); !ok {
		t.Error("u1 should be on cooldown")
	}
	if _, ok := s.Check("kick", "u2"); ok {
		t.Error("u2 should not be on cooldown")
	}
}

// ── persistence ───────────────────────────────────────────────────────────────

func TestStore_Persistence_ActiveEntryReloaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open s1: %v", err)
	}
	s1.Set("restart", "u1", time.Now().Add(time.Minute))
	if err := s1.Close(); err != nil {
		t.Fatalf("Close s1: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open s2: %v", err)
	}
	defer func() { _ = s2.Close() }()

	if _, ok := s2.Check("restart", "u1"); !ok {
		t.Error("active cooldown should survive close/reopen")
	}
}

func TestStore_Persistence_ExpiredEntryPruned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open s1: %v", err)
	}
	s1.Set("restart", "u1", time.Now().Add(-time.Second)) // already expired
	if err := s1.Close(); err != nil {
		t.Fatalf("Close s1: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open s2: %v", err)
	}
	defer func() { _ = s2.Close() }()

	if _, ok := s2.Check("restart", "u1"); ok {
		t.Error("expired entry should be pruned on reload")
	}
}

// ── concurrency ───────────────────────────────────────────────────────────────

func TestStore_ConcurrentSetCheck_NoRace(t *testing.T) {
	s := openTemp(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			s.Set("cmd", "u1", time.Now().Add(time.Duration(i)*time.Second))
		}(i)
		go func() {
			defer wg.Done()
			s.Check("cmd", "u1")
		}()
	}
	wg.Wait()
}
