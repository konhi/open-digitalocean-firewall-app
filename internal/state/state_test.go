package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_LoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "nested", "state.json"))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	initial, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !initial.Settings.AutoUpdateOnIPChange || !initial.Settings.AutoCleanupOnQuit || initial.Settings.DefaultDurationMinutes != 60 {
		t.Fatalf("unexpected default settings: %+v", initial.Settings)
	}
	if len(initial.ManagedRules) != 0 {
		t.Fatalf("expected empty ManagedRules, got %v", initial.ManagedRules)
	}

	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(time.Hour)
	initial.ManagedRules["cluster-1"] = ManagedRule{
		IP:        "203.0.113.10",
		AddedAt:   now,
		ExpiresAt: &exp,
	}
	initial.Settings.DefaultDurationMinutes = 15
	initial.Settings.AutoCleanupOnQuit = false

	if err := store.Save(initial); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after save error: %v", err)
	}

	if loaded.Settings.DefaultDurationMinutes != 15 || loaded.Settings.AutoCleanupOnQuit {
		t.Fatalf("settings did not round-trip: %+v", loaded.Settings)
	}

	rule, ok := loaded.ManagedRules["cluster-1"]
	if !ok {
		t.Fatal("expected cluster-1 rule to exist")
	}
	if rule.IP != "203.0.113.10" || rule.ExpiresAt == nil || !rule.ExpiresAt.Equal(exp) {
		t.Fatalf("rule did not round-trip: %+v", rule)
	}
}

func TestManagedRule_IsExpired(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	noTimer := ManagedRule{IP: "203.0.113.10"}
	if noTimer.IsExpired(now) {
		t.Fatal("rule without ExpiresAt should never expire")
	}

	future := now.Add(time.Minute)
	if (ManagedRule{ExpiresAt: &future}).IsExpired(now) {
		t.Fatal("future rule should not be expired")
	}

	if !(ManagedRule{ExpiresAt: &now}).IsExpired(now) {
		t.Fatal("rule expiring at now should be expired")
	}
}
