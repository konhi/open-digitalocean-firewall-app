package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testDeviceIP = "203.0.113.10"

func TestStore_Load_ReturnsDefaultDataWhenFileDoesNotExist(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "missing", "state.json"))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	got, err := store.Load()

	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	wantSettings := Settings{
		AutoUpdateOnIPChange:   true,
		AutoCleanupOnQuit:      true,
		DefaultDurationMinutes: 60,
	}
	if got.Settings != wantSettings {
		t.Fatalf("Load() settings = %+v, want %+v", got.Settings, wantSettings)
	}
	if got.ManagedRules == nil || len(got.ManagedRules) != 0 {
		t.Fatalf("Load() managed rules = %#v, want empty non-nil map", got.ManagedRules)
	}
}

func TestStore_SaveAndLoad_RoundTripsManagedRulesAndSettings(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "nested", "state.json"))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	addedAt := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC)
	input := Data{
		ManagedRules: map[string]ManagedRule{
			"cluster-timed": {
				IP:        testDeviceIP,
				AddedAt:   addedAt,
				ExpiresAt: &expiresAt,
			},
			"cluster-until-quit": {
				IP:      testDeviceIP,
				AddedAt: addedAt,
			},
		},
		Settings: Settings{
			AutoUpdateOnIPChange:   false,
			AutoCleanupOnQuit:      false,
			DefaultDurationMinutes: 15,
			Username:               "jan",
		},
	}

	if err := store.Save(input); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}
	loaded, err := store.Load()

	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if loaded.Settings != input.Settings {
		t.Fatalf("loaded settings = %+v, want %+v", loaded.Settings, input.Settings)
	}
	timed, ok := loaded.ManagedRules["cluster-timed"]
	if !ok || timed.IP != testDeviceIP || !timed.AddedAt.Equal(addedAt) || timed.ExpiresAt == nil || !timed.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("cluster-timed did not round-trip: %+v", timed)
	}
	untilQuit, ok := loaded.ManagedRules["cluster-until-quit"]
	if !ok || untilQuit.IP != testDeviceIP || untilQuit.ExpiresAt != nil {
		t.Fatalf("cluster-until-quit did not round-trip: %+v", untilQuit)
	}
}

func TestStore_Load_ReturnsErrorOnCorruptedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{invalid-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	_, err = store.Load()

	if err == nil {
		t.Fatal("Load() expected error for corrupted JSON, got nil")
	}
}

func TestStore_Load_InitializesEmptyMapWhenManagedRulesIsNull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"managed_rules":null,"settings":{"default_duration_minutes":240}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	loaded, err := store.Load()

	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if loaded.ManagedRules == nil {
		t.Fatal("Load() returned nil ManagedRules map, want initialized empty map")
	}
	if loaded.Settings.DefaultDurationMinutes != 240 {
		t.Fatalf("DefaultDurationMinutes = %d, want 240", loaded.Settings.DefaultDurationMinutes)
	}
}

func TestManagedRule_IsExpired_ReturnsFalseWhenNoTimerSet(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	rule := ManagedRule{IP: testDeviceIP, ExpiresAt: nil}

	if rule.IsExpired(now) {
		t.Fatal("IsExpired() = true for rule without ExpiresAt, want false")
	}
}

func TestManagedRule_IsExpired_ReturnsFalseBeforeExpiryTime(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	future := time.Date(2026, 11, 20, 12, 1, 0, 0, time.UTC)
	rule := ManagedRule{IP: testDeviceIP, ExpiresAt: &future}

	if rule.IsExpired(now) {
		t.Fatal("IsExpired() = true before ExpiresAt, want false")
	}
}

func TestManagedRule_IsExpired_ReturnsTrueAtExactExpiryTime(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	exact := now
	rule := ManagedRule{IP: testDeviceIP, ExpiresAt: &exact}

	if !rule.IsExpired(now) {
		t.Fatal("IsExpired() = false at exact ExpiresAt boundary, want true")
	}
}

func TestManagedRule_IsExpired_ReturnsTrueAfterExpiryTime(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 5, 0, 0, time.UTC)
	past := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	rule := ManagedRule{IP: testDeviceIP, ExpiresAt: &past}

	if !rule.IsExpired(now) {
		t.Fatal("IsExpired() = false after ExpiresAt, want true")
	}
}
