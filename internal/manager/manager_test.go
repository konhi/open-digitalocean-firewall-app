package manager

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
	"github.com/konhi/open-digitalocean-firewall-app/internal/state"
)

type stubDetector struct {
	ip string
}

func (s *stubDetector) Detect(ctx context.Context) (string, error) {
	return s.ip, nil
}

type stubDOClient struct {
	dbs   []digitalocean.Database
	rules map[string][]digitalocean.FirewallRule
}

func (c *stubDOClient) ListDatabases(ctx context.Context) ([]digitalocean.Database, error) {
	out := make([]digitalocean.Database, len(c.dbs))
	copy(out, c.dbs)
	return out, nil
}

func (c *stubDOClient) GetFirewallRules(ctx context.Context, clusterID string) ([]digitalocean.FirewallRule, error) {
	src := c.rules[clusterID]
	out := make([]digitalocean.FirewallRule, len(src))
	copy(out, src)
	return out, nil
}

func (c *stubDOClient) UpdateFirewallRules(ctx context.Context, clusterID string, rules []digitalocean.FirewallRule) error {
	out := make([]digitalocean.FirewallRule, len(rules))
	copy(out, rules)
	c.rules[clusterID] = out
	return nil
}

func TestManager_Lifecycle_Toggle_IPChange_Expiry_QuitCleanup(t *testing.T) {
	ctx := context.Background()
	store, err := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	client := &stubDOClient{
		dbs: []digitalocean.Database{
			{ID: "db-1", Name: "valkey-test", EngineSlug: "valkey", RegionSlug: "fra1"},
			{ID: "db-2", Name: "pg-prod", EngineSlug: "pg", RegionSlug: "fra1"},
		},
		rules: map[string][]digitalocean.FirewallRule{
			"db-1": {
				{UUID: "deny-1", Type: digitalocean.RuleTypeIPAddr, Value: "192.0.2.1"},
				{UUID: "coworker-1", Type: digitalocean.RuleTypeIPAddr, Value: "198.51.100.50"},
			},
			"db-2": {
				{UUID: "deny-2", Type: digitalocean.RuleTypeIPAddr, Value: "192.0.2.1"},
			},
		},
	}

	detector := &stubDetector{ip: "203.0.113.10"}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	mgr, err := New(client, detector, store, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	mgr.now = func() time.Time { return now }

	if err := mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	snap := mgr.Snapshot()
	if snap.CurrentIP != "203.0.113.10" || len(snap.Databases) != 2 || snap.ActiveCount != 0 {
		t.Fatalf("unexpected initial snapshot: %+v", snap)
	}

	if err := mgr.ToggleDatabase(ctx, "db-1"); err != nil {
		t.Fatalf("ToggleDatabase(db-1) error: %v", err)
	}
	if !digitalocean.HasIP(client.rules["db-1"], "203.0.113.10") {
		t.Fatal("expected db-1 to contain current IP")
	}
	if !digitalocean.HasIP(client.rules["db-1"], "198.51.100.50") {
		t.Fatal("coworker IP must be preserved")
	}

	detector.ip = "203.0.113.99"
	if err := mgr.Tick(ctx); err != nil {
		t.Fatalf("Tick() after IP change error: %v", err)
	}
	if digitalocean.HasIP(client.rules["db-1"], "203.0.113.10") {
		t.Fatal("old IP should have been removed after IP change")
	}
	if !digitalocean.HasIP(client.rules["db-1"], "203.0.113.99") {
		t.Fatal("new IP should have been added after IP change")
	}
	if !digitalocean.HasIP(client.rules["db-1"], "198.51.100.50") || !digitalocean.HasIP(client.rules["db-1"], "192.0.2.1") {
		t.Fatal("other users' rules must remain untouched after IP change")
	}

	now = now.Add(61 * time.Minute)
	if err := mgr.Tick(ctx); err != nil {
		t.Fatalf("Tick() after expiry error: %v", err)
	}
	if digitalocean.HasIP(client.rules["db-1"], "203.0.113.99") {
		t.Fatal("expired IP should have been revoked")
	}
	if !digitalocean.HasIP(client.rules["db-1"], "198.51.100.50") || !digitalocean.HasIP(client.rules["db-1"], "192.0.2.1") {
		t.Fatal("other users' rules must remain untouched after expiry")
	}

	mgr.SetDefaultDuration(0)
	if err := mgr.AllowAll(ctx); err != nil {
		t.Fatalf("AllowAll() error: %v", err)
	}
	if mgr.Snapshot().ActiveCount != 2 {
		t.Fatalf("expected 2 active DBs, got %d", mgr.Snapshot().ActiveCount)
	}

	if err := mgr.CleanupOnQuit(ctx); err != nil {
		t.Fatalf("CleanupOnQuit() error: %v", err)
	}
	if digitalocean.HasIP(client.rules["db-1"], "203.0.113.99") || digitalocean.HasIP(client.rules["db-2"], "203.0.113.99") {
		t.Fatal("expected CleanupOnQuit to remove our IP from all managed DBs")
	}
	if !digitalocean.HasIP(client.rules["db-1"], "198.51.100.50") || !digitalocean.HasIP(client.rules["db-1"], "192.0.2.1") || !digitalocean.HasIP(client.rules["db-2"], "192.0.2.1") {
		t.Fatal("expected CleanupOnQuit to preserve all other rules")
	}
}
