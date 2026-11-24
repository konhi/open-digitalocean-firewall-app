package manager

import (
	"context"
	"errors"
	"io"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
)

const (
	testDeviceIP          = "203.0.113.10"
	testNewIP             = "203.0.113.99"
	testCluster1          = "db-1"
	testCluster2          = "db-2"
	testDefaultExpiryDesc = "delete after 2026-11-20 13:00 UTC"
)

var (
	errAPIUnauthorized = errors.New("api unauthorized")

	sentinelRule = digitalocean.FirewallRule{UUID: "deny-1", Type: digitalocean.RuleTypeIPAddr, Value: "192.0.2.1"}
	coworkerRule = digitalocean.FirewallRule{UUID: "coworker-1", Type: digitalocean.RuleTypeIPAddr, Value: "198.51.100.50"}
	dropletRule  = digitalocean.FirewallRule{UUID: "droplet-1", Type: "droplet", Value: "123456"}
)

type stubDetector struct {
	ip  string
	err error
}

func (s *stubDetector) Detect(ctx context.Context) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.ip, nil
}

type stubDOClient struct {
	dbs       []digitalocean.Database
	rules     map[string][]digitalocean.FirewallRule
	listErr   error
	getErr    error
	updateErr error
}

func (c *stubDOClient) ListDatabases(ctx context.Context) ([]digitalocean.Database, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	out := make([]digitalocean.Database, len(c.dbs))
	copy(out, c.dbs)
	return out, nil
}

func (c *stubDOClient) GetFirewallRules(ctx context.Context, clusterID string) ([]digitalocean.FirewallRule, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	src := c.rules[clusterID]
	out := make([]digitalocean.FirewallRule, len(src))
	copy(out, src)
	return out, nil
}

func (c *stubDOClient) UpdateFirewallRules(ctx context.Context, clusterID string, rules []digitalocean.FirewallRule) error {
	if c.updateErr != nil {
		return c.updateErr
	}
	out := make([]digitalocean.FirewallRule, len(rules))
	copy(out, rules)
	c.rules[clusterID] = out
	return nil
}

type managerFixture struct {
	mgr      *Manager
	client   *stubDOClient
	detector *stubDetector
	now      time.Time
}

func newManagerFixture(t *testing.T) *managerFixture {
	t.Helper()

	client := &stubDOClient{
		dbs: []digitalocean.Database{
			{ID: testCluster1, Name: "valkey-test", EngineSlug: "valkey", RegionSlug: "fra1"},
			{ID: testCluster2, Name: "pg-prod", EngineSlug: "pg", RegionSlug: "fra1"},
		},
		rules: map[string][]digitalocean.FirewallRule{
			testCluster1: {sentinelRule, coworkerRule, dropletRule},
			testCluster2: {sentinelRule},
		},
	}
	detector := &stubDetector{ip: testDeviceIP}

	mgr, err := New(client, detector, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	f := &managerFixture{
		mgr:      mgr,
		client:   client,
		detector: detector,
		now:      time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}
	mgr.now = func() time.Time { return f.now }
	return f
}

func assertClusterRules(t *testing.T, got, want []digitalocean.FirewallRule) {
	t.Helper()
	gotCounts := make(map[digitalocean.FirewallRule]int, len(got))
	for _, r := range got {
		gotCounts[r]++
	}
	wantCounts := make(map[digitalocean.FirewallRule]int, len(want))
	for _, r := range want {
		wantCounts[r]++
	}
	if !reflect.DeepEqual(gotCounts, wantCounts) {
		t.Fatalf("cluster rules mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestManager_Refresh_LoadsIPAndDatabaseStatuses(t *testing.T) {
	f := newManagerFixture(t)
	f.client.rules[testCluster2] = []digitalocean.FirewallRule{
		sentinelRule,
		{Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP},
	}

	err := f.mgr.Refresh(context.Background())

	if err != nil {
		t.Fatalf("Refresh() unexpected error: %v", err)
	}
	snap := f.mgr.Snapshot()
	if !snap.HasToken || snap.CurrentIP != testDeviceIP || len(snap.Databases) != 2 || snap.ActiveCount != 1 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.Databases[0].Allowed || !snap.Databases[1].Allowed {
		t.Fatalf("unexpected allowed flags: db-1=%v db-2=%v", snap.Databases[0].Allowed, snap.Databases[1].Allowed)
	}
}

func TestManager_Refresh_DetectsIPWhenTokenIsMissing(t *testing.T) {
	detector := &stubDetector{ip: testDeviceIP}
	mgr, err := New(nil, detector, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	err = mgr.Refresh(context.Background())

	if err != nil {
		t.Fatalf("Refresh() unexpected error: %v", err)
	}
	snap := mgr.Snapshot()
	if snap.HasToken || snap.CurrentIP != testDeviceIP {
		t.Fatalf("unexpected snapshot without token: %+v", snap)
	}
}

func TestManager_Refresh_RecordsErrorWhenListDatabasesFails(t *testing.T) {
	f := newManagerFixture(t)
	f.client.listErr = errAPIUnauthorized

	err := f.mgr.Refresh(context.Background())

	if !errors.Is(err, errAPIUnauthorized) {
		t.Fatalf("Refresh() error = %v, want %v", err, errAPIUnauthorized)
	}
	if !strings.Contains(f.mgr.Snapshot().LastError, errAPIUnauthorized.Error()) {
		t.Fatalf("LastError = %q, want substring %q", f.mgr.Snapshot().LastError, errAPIUnauthorized.Error())
	}
}

func TestManager_Refresh_StopsTrackingRuleManuallyRemovedOutsideApp(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("initial Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase() error: %v", err)
	}
	f.client.rules[testCluster1] = []digitalocean.FirewallRule{sentinelRule, coworkerRule, dropletRule}

	err := f.mgr.Refresh(ctx)

	if err != nil {
		t.Fatalf("Refresh() unexpected error: %v", err)
	}
	db1 := f.mgr.Snapshot().Databases[0]
	if db1.Allowed || db1.Managed {
		t.Fatalf("expected db-1 to be neither allowed nor managed after external removal, got %+v", db1)
	}
}

func TestManager_ToggleDatabase_AddsCurrentIPWithDefaultTimerPreservingOtherRules(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	err := f.mgr.ToggleDatabase(ctx, testCluster1)

	if err != nil {
		t.Fatalf("ToggleDatabase() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule,
		coworkerRule,
		dropletRule,
		{Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP, Description: testDefaultExpiryDesc},
	})
	db1 := f.mgr.Snapshot().Databases[0]
	wantExpiry := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC)
	if !db1.Allowed || !db1.Managed || db1.ExpiresAt == nil || !db1.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("unexpected db-1 status: %+v", db1)
	}
}

func TestManager_ToggleDatabase_RemovesManagedIPPreservingOtherRules(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase(add) error: %v", err)
	}

	err := f.mgr.ToggleDatabase(ctx, testCluster1)

	if err != nil {
		t.Fatalf("ToggleDatabase(remove) unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule, coworkerRule, dropletRule,
	})
	db1 := f.mgr.Snapshot().Databases[0]
	if db1.Allowed || db1.Managed || db1.ExpiresAt != nil {
		t.Fatalf("unexpected db-1 status after removal: %+v", db1)
	}
}

func TestManager_ToggleDatabase_SetsNoExpiryWhenDurationIsZero(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	f.mgr.SetDefaultDuration(0)
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	err := f.mgr.ToggleDatabase(ctx, testCluster1)

	if err != nil {
		t.Fatalf("ToggleDatabase() unexpected error: %v", err)
	}
	db1 := f.mgr.Snapshot().Databases[0]
	if !db1.Allowed || !db1.Managed || db1.ExpiresAt != nil {
		t.Fatalf("expected managed rule with nil ExpiresAt, got %+v", db1)
	}
}

func TestManager_ToggleDatabase_ReturnsErrorWithoutClient(t *testing.T) {
	mgr, err := New(nil, &stubDetector{ip: testDeviceIP}, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	err = mgr.ToggleDatabase(context.Background(), testCluster1)

	if !errors.Is(err, ErrNoTokenConfigured) {
		t.Fatalf("ToggleDatabase() error = %v, want ErrNoTokenConfigured", err)
	}
}

func TestManager_AllowAll_AddsCurrentIPToAllDatabasesPreservingOtherRules(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	err := f.mgr.AllowAll(ctx)

	if err != nil {
		t.Fatalf("AllowAll() unexpected error: %v", err)
	}
	if f.mgr.Snapshot().ActiveCount != 2 {
		t.Fatalf("ActiveCount = %d, want 2", f.mgr.Snapshot().ActiveCount)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule, coworkerRule, dropletRule, {Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP, Description: testDefaultExpiryDesc},
	})
	assertClusterRules(t, f.client.rules[testCluster2], []digitalocean.FirewallRule{
		sentinelRule, {Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP, Description: testDefaultExpiryDesc},
	})
}

func TestManager_RemoveAll_RemovesCurrentIPFromAllDatabasesPreservingOtherRules(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.AllowAll(ctx); err != nil {
		t.Fatalf("AllowAll() error: %v", err)
	}

	err := f.mgr.RemoveAll(ctx)

	if err != nil {
		t.Fatalf("RemoveAll() unexpected error: %v", err)
	}
	if f.mgr.Snapshot().ActiveCount != 0 {
		t.Fatalf("ActiveCount = %d, want 0", f.mgr.Snapshot().ActiveCount)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule, coworkerRule, dropletRule,
	})
	assertClusterRules(t, f.client.rules[testCluster2], []digitalocean.FirewallRule{sentinelRule})
}

func TestManager_Tick_ReplacesManagedIPWhenPublicIPChanges(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase() error: %v", err)
	}

	f.detector.ip = testNewIP
	err := f.mgr.Tick(ctx)

	if err != nil {
		t.Fatalf("Tick() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule,
		coworkerRule,
		dropletRule,
		{Type: digitalocean.RuleTypeIPAddr, Value: testNewIP, Description: testDefaultExpiryDesc},
	})
	if f.mgr.Snapshot().CurrentIP != testNewIP {
		t.Fatalf("CurrentIP = %q, want %q", f.mgr.Snapshot().CurrentIP, testNewIP)
	}
}

func TestManager_Tick_DoesNotReplaceManagedIPWhenAutoUpdateDisabled(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	f.mgr.SetAutoUpdateOnIPChange(false)
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase() error: %v", err)
	}

	f.detector.ip = testNewIP
	err := f.mgr.Tick(ctx)

	if err != nil {
		t.Fatalf("Tick() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule,
		coworkerRule,
		dropletRule,
		{Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP, Description: testDefaultExpiryDesc},
	})
}

func TestManager_Tick_RevokesManagedIPAfterTimerExpires(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	f.mgr.SetDefaultDuration(15)
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase() error: %v", err)
	}

	f.now = f.now.Add(15 * time.Minute)
	err := f.mgr.Tick(ctx)

	if err != nil {
		t.Fatalf("Tick() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule, coworkerRule, dropletRule,
	})
	if f.mgr.Snapshot().ActiveCount != 0 {
		t.Fatalf("ActiveCount = %d, want 0", f.mgr.Snapshot().ActiveCount)
	}
}

func TestManager_Tick_KeepsManagedIPActiveBeforeTimerExpires(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	f.mgr.SetDefaultDuration(15)
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase() error: %v", err)
	}

	f.now = f.now.Add(14 * time.Minute)
	err := f.mgr.Tick(ctx)

	if err != nil {
		t.Fatalf("Tick() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule,
		coworkerRule,
		dropletRule,
		{Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP, Description: "delete after 2026-11-20 12:15 UTC"},
	})
}

func TestManager_CleanupOnQuit_RemovesOnlyAppManagedIPsWhenEnabled(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	unmanagedSameIP := digitalocean.FirewallRule{UUID: "manual-1", Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP}
	f.client.rules[testCluster2] = []digitalocean.FirewallRule{sentinelRule, unmanagedSameIP}

	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase(db-1) error: %v", err)
	}

	err := f.mgr.CleanupOnQuit(ctx)

	if err != nil {
		t.Fatalf("CleanupOnQuit() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule, coworkerRule, dropletRule,
	})
	assertClusterRules(t, f.client.rules[testCluster2], []digitalocean.FirewallRule{
		sentinelRule, unmanagedSameIP,
	})
}

func TestManager_CleanupOnQuit_LeavesManagedIPsUntouchedWhenDisabled(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	f.mgr.SetAutoCleanupOnQuit(false)
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if err := f.mgr.ToggleDatabase(ctx, testCluster1); err != nil {
		t.Fatalf("ToggleDatabase() error: %v", err)
	}

	err := f.mgr.CleanupOnQuit(ctx)

	if err != nil {
		t.Fatalf("CleanupOnQuit() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule,
		coworkerRule,
		dropletRule,
		{Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP, Description: testDefaultExpiryDesc},
	})
}

func TestManager_SetDefaultDuration_ClampsNegativeMinutesToZero(t *testing.T) {
	f := newManagerFixture(t)

	f.mgr.SetDefaultDuration(-10)

	if got := f.mgr.Snapshot().Settings.DefaultDurationMinutes; got != 0 {
		t.Fatalf("DefaultDurationMinutes = %d, want 0", got)
	}
}

func TestManager_ToggleDatabase_IncludesConfiguredUsernameInRuleDescription(t *testing.T) {
	f := newManagerFixture(t)
	ctx := context.Background()
	f.mgr.SetUsername("  jan  ")
	if err := f.mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	err := f.mgr.ToggleDatabase(ctx, testCluster1)

	if err != nil {
		t.Fatalf("ToggleDatabase() unexpected error: %v", err)
	}
	assertClusterRules(t, f.client.rules[testCluster1], []digitalocean.FirewallRule{
		sentinelRule,
		coworkerRule,
		dropletRule,
		{Type: digitalocean.RuleTypeIPAddr, Value: testDeviceIP, Description: "jan - delete after 2026-11-20 13:00 UTC"},
	})
}

func TestFormatRuleDescription_IncludesUsernameAndExpiryDateWhenBothPresent(t *testing.T) {
	added := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)

	got := FormatRuleDescription("jan", added, &expires)

	if got != "jan - delete after 2026-11-20 16:00 UTC" {
		t.Fatalf("FormatRuleDescription() = %q, want %q", got, "jan - delete after 2026-11-20 16:00 UTC")
	}
}

func TestFormatRuleDescription_OmitsUsernameWhenBlank(t *testing.T) {
	added := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC)

	got := FormatRuleDescription("   ", added, &expires)

	if got != testDefaultExpiryDesc {
		t.Fatalf("FormatRuleDescription() = %q, want %q", got, testDefaultExpiryDesc)
	}
}

func TestFormatRuleDescription_FormatsDeleteOnQuitDateWhenNoTimerSet(t *testing.T) {
	added := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	got := FormatRuleDescription("jan", added, nil)

	if got != "jan - delete on quit (added 2026-11-20)" {
		t.Fatalf("FormatRuleDescription() = %q, want %q", got, "jan - delete on quit (added 2026-11-20)")
	}
}
