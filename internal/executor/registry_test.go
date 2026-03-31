package executor

import (
	"context"
	"errors"
	"testing"
)

// ── test doubles ──────────────────────────────────────────────────────────────

type stubExecutor struct{}

func (s *stubExecutor) Send(_ context.Context, _ string, _ ...string) (string, error) {
	return "ok", nil
}

type stubLifecycleExecutor struct {
	closeCalled bool
	closeErr    error
}

func (s *stubLifecycleExecutor) Send(_ context.Context, _ string, _ ...string) (string, error) {
	return "ok", nil
}

func (s *stubLifecycleExecutor) Close() error {
	s.closeCalled = true
	return s.closeErr
}

// ── Register / Get ────────────────────────────────────────────────────────────

func TestRegistry_RegisterAndGet_ReturnsExecutor(t *testing.T) {
	reg := NewRegistry()
	ex := &stubExecutor{}
	reg.Register("game_tmux", ex)

	got, err := reg.Get("game_tmux")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ex {
		t.Error("Get should return the same executor that was registered")
	}
}

func TestRegistry_Get_UnknownName_ReturnsDescriptiveError(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown executor, got nil")
	}
	if !containsAny(err.Error(), "nonexistent", "not found") {
		t.Errorf("error should mention the executor name or 'not found', got: %v", err)
	}
}

func TestRegistry_Register_Duplicate_Panics(t *testing.T) {
	reg := NewRegistry()
	reg.Register("game_tmux", &stubExecutor{})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate registration, got none")
		}
	}()

	reg.Register("game_tmux", &stubExecutor{})
}

func TestRegistry_MultipleExecutors_AllRetrievable(t *testing.T) {
	reg := NewRegistry()
	tmux := &stubExecutor{}
	rcon := &stubLifecycleExecutor{}

	reg.Register("game_tmux", tmux)
	reg.Register("game_rcon", rcon)

	gotTmux, err := reg.Get("game_tmux")
	if err != nil || gotTmux != tmux {
		t.Errorf("expected game_tmux executor, got err=%v", err)
	}

	gotRcon, err := reg.Get("game_rcon")
	if err != nil || gotRcon != rcon {
		t.Errorf("expected game_rcon executor, got err=%v", err)
	}
}

// ── ValidateNames ─────────────────────────────────────────────────────────────

func TestRegistry_ValidateNames_AllPresent_ReturnsNil(t *testing.T) {
	reg := NewRegistry()
	reg.Register("game_tmux", &stubExecutor{})
	reg.Register("game_rcon", &stubLifecycleExecutor{})

	if err := reg.ValidateNames([]string{"game_tmux", "game_rcon"}); err != nil {
		t.Errorf("expected nil when all names present, got: %v", err)
	}
}

func TestRegistry_ValidateNames_MissingName_ReturnsError(t *testing.T) {
	reg := NewRegistry()
	reg.Register("game_tmux", &stubExecutor{})

	err := reg.ValidateNames([]string{"game_tmux", "game_rcon"})
	if err == nil {
		t.Fatal("expected error for missing executor name, got nil")
	}
	if !containsAny(err.Error(), "game_rcon") {
		t.Errorf("error should name the missing executor, got: %v", err)
	}
}

func TestRegistry_ValidateNames_EmptySlice_ReturnsNil(t *testing.T) {
	reg := NewRegistry()

	if err := reg.ValidateNames([]string{}); err != nil {
		t.Errorf("expected nil for empty name slice, got: %v", err)
	}
}

func TestRegistry_ValidateNames_NilSlice_ReturnsNil(t *testing.T) {
	reg := NewRegistry()

	if err := reg.ValidateNames(nil); err != nil {
		t.Errorf("expected nil for nil name slice, got: %v", err)
	}
}

func TestRegistry_ValidateNames_MultipleMissing_AllReportedInError(t *testing.T) {
	reg := NewRegistry()
	// Register nothing — all names will be missing.

	err := reg.ValidateNames([]string{"tmux_one", "rcon_two"})
	if err == nil {
		t.Fatal("expected error for missing executors, got nil")
	}
	// Both missing names should appear in the error message.
	if !containsAny(err.Error(), "tmux_one") || !containsAny(err.Error(), "rcon_two") {
		t.Errorf("error should list all missing executors, got: %v", err)
	}
}

// ── CloseAll ──────────────────────────────────────────────────────────────────

func TestRegistry_CloseAll_CallsCloseOnLifecycleExecutors(t *testing.T) {
	reg := NewRegistry()
	lc := &stubLifecycleExecutor{}
	reg.Register("game_rcon", lc)

	reg.CloseAll()

	if !lc.closeCalled {
		t.Error("expected Close to be called on LifecycleExecutor")
	}
}

func TestRegistry_CloseAll_SkipsPlainExecutors(t *testing.T) {
	// Plain executors don't implement Close — CloseAll must not panic on them.
	reg := NewRegistry()
	reg.Register("game_tmux", &stubExecutor{})

	// Must not panic.
	reg.CloseAll()
}

func TestRegistry_CloseAll_EmptyRegistry_NoPanic(t *testing.T) {
	reg := NewRegistry()
	// Must not panic.
	reg.CloseAll()
}

func TestRegistry_CloseAll_CloseError_DoesNotStopOthers(t *testing.T) {
	reg := NewRegistry()

	failingEx := &stubLifecycleExecutor{closeErr: errors.New("connection reset")}
	successEx := &stubLifecycleExecutor{}

	reg.Register("failing", failingEx)
	reg.Register("success", successEx)

	// Must not panic or abort — both Close calls must execute.
	reg.CloseAll()

	if !failingEx.closeCalled {
		t.Error("expected Close to be called on failing executor")
	}
	if !successEx.closeCalled {
		t.Error("expected Close to be called on success executor even after failing one errored")
	}
}

func TestRegistry_CloseAll_LifecycleConnectionNilAfterClose(t *testing.T) {
	// RconExecutor sets conn = nil on Close. Calling CloseAll twice should
	// not panic because Close handles nil conn gracefully.
	reg := NewRegistry()
	lc := &stubLifecycleExecutor{}
	reg.Register("game_rcon", lc)

	reg.CloseAll()
	reg.CloseAll() // second call — must not panic
}

// ── helper ────────────────────────────────────────────────────────────────────

func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(sub) > 0 {
			idx := 0
			for idx <= len(s)-len(sub) {
				if s[idx:idx+len(sub)] == sub {
					return true
				}
				idx++
			}
		}
	}
	return false
}
