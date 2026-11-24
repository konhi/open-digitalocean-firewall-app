package tray

import (
	"testing"
	"time"

	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
	"github.com/konhi/open-digitalocean-firewall-app/internal/manager"
)

const (
	testDBID   = "db-1"
	testDBName = "valkey-test"
)

func TestFormatStatus_ReportsActiveCountWithSingularAndPlural(t *testing.T) {
	snap := manager.Snapshot{
		HasToken:    true,
		ActiveCount: 1,
		Databases: []manager.DatabaseStatus{
			{Database: digitalocean.Database{ID: testDBID, Name: testDBName}, Allowed: true},
		},
	}

	got := formatStatus(snap)

	if got != "Access Open on 1 of 1 Database" {
		t.Fatalf("formatStatus() = %q, want %q", got, "Access Open on 1 of 1 Database")
	}
}

func TestFormatStatus_ReportsAllDatabasesLockedWhenNoneActive(t *testing.T) {
	snap := manager.Snapshot{
		HasToken:    true,
		ActiveCount: 0,
		Databases: []manager.DatabaseStatus{
			{Database: digitalocean.Database{ID: testDBID, Name: testDBName}},
		},
	}

	got := formatStatus(snap)

	if got != "All Databases Locked" {
		t.Fatalf("formatStatus() = %q, want %q", got, "All Databases Locked")
	}
}

func TestFormatDBLabel_FormatsEngineRegionAndRemainingTime(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	expires := now.Add(59*time.Minute + 20*time.Second)
	db := manager.DatabaseStatus{
		Database: digitalocean.Database{
			ID:         testDBID,
			Name:       "testing-valkey",
			EngineSlug: "valkey",
			RegionSlug: "fra1",
		},
		Allowed:   true,
		Managed:   true,
		ExpiresAt: &expires,
	}

	got := formatDBLabel(db, now)

	if got != "testing-valkey · Valkey (fra1) · 1h 0m left" {
		t.Fatalf("formatDBLabel() = %q, want %q", got, "testing-valkey · Valkey (fra1) · 1h 0m left")
	}
}

func TestFormatDBLabel_MarksManuallyAllowedDatabase(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	db := manager.DatabaseStatus{
		Database: digitalocean.Database{
			ID:         testDBID,
			Name:       "prod-db",
			EngineSlug: "pg",
			RegionSlug: "nyc3",
		},
		Allowed: true,
		Managed: false,
	}

	got := formatDBLabel(db, now)

	if got != "prod-db · PostgreSQL (nyc3) · manual" {
		t.Fatalf("formatDBLabel() = %q, want %q", got, "prod-db · PostgreSQL (nyc3) · manual")
	}
}

func TestNewIconSet_ProducesNonEmptyIcons(t *testing.T) {
	icons := newIconSet()

	if len(icons.trayIdle.template) == 0 || len(icons.trayActive.template) == 0 || len(icons.database.template) == 0 || len(icons.dotActive) == 0 {
		t.Fatal("newIconSet() produced empty icon bytes")
	}
}

func TestMenuHandles_NeedsRebuild_FalseWhenOnlyBusyOrStatusChanges(t *testing.T) {
	m := menuHandles{
		built:    true,
		hasToken: true,
		dbIDs:    []string{testDBID},
	}
	snap := manager.Snapshot{
		HasToken:    true,
		Busy:        true,
		ActiveCount: 1,
		CurrentIP:   "203.0.113.10",
		Databases: []manager.DatabaseStatus{
			{Database: digitalocean.Database{ID: testDBID, Name: testDBName}, Allowed: true},
		},
	}

	if m.needsRebuild(snap) {
		t.Fatal("needsRebuild() = true when database IDs and token presence did not change, want false")
	}
}

func TestMenuHandles_NeedsRebuild_TrueWhenDatabaseListChanges(t *testing.T) {
	m := menuHandles{
		built:    true,
		hasToken: true,
		dbIDs:    []string{testDBID},
	}
	snap := manager.Snapshot{
		HasToken: true,
		Databases: []manager.DatabaseStatus{
			{Database: digitalocean.Database{ID: testDBID}},
			{Database: digitalocean.Database{ID: "db-2"}},
		},
	}

	if !m.needsRebuild(snap) {
		t.Fatal("needsRebuild() = false when a new database appeared, want true")
	}
}
