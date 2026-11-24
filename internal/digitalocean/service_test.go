package digitalocean

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

const (
	testClusterID       = "cluster-1"
	testDeviceIP        = "203.0.113.10"
	testNewIP           = "203.0.113.99"
	testRuleDescription = "alice - delete after 2026-11-23 11:00 UTC"
)

var (
	errAPIUnavailable = errors.New("do api unavailable")
	errRateLimited    = errors.New("rate limited")
	errForbidden      = errors.New("forbidden")
	errServerError    = errors.New("server error")

	sentinelRule = FirewallRule{UUID: "u-sentinel", Type: RuleTypeIPAddr, Value: "192.0.2.1"}
	teammateRule = FirewallRule{UUID: "u-teammate", Type: RuleTypeIPAddr, Value: "198.51.100.77"}
	dropletRule  = FirewallRule{UUID: "u-droplet", Type: "droplet", Value: "123456"}
	tagRule      = FirewallRule{UUID: "u-tag", Type: "tag", Value: "prod-web"}
	k8sRule      = FirewallRule{UUID: "u-k8s", Type: "k8s", Value: "k8s-cluster-uuid"}
	appRule      = FirewallRule{UUID: "u-app", Type: "app", Value: "app-platform-uuid"}
)

type fakeClient struct {
	rules       map[string][]FirewallRule
	getErr      error
	updateErr   error
	updateCalls int
}

func newFakeClient(clusterID string, initial []FirewallRule) *fakeClient {
	cp := make([]FirewallRule, len(initial))
	copy(cp, initial)
	return &fakeClient{
		rules: map[string][]FirewallRule{clusterID: cp},
	}
}

func (f *fakeClient) ListDatabases(ctx context.Context) ([]Database, error) {
	return nil, nil
}

func (f *fakeClient) GetFirewallRules(ctx context.Context, clusterID string) ([]FirewallRule, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	src := f.rules[clusterID]
	out := make([]FirewallRule, len(src))
	copy(out, src)
	return out, nil
}

func (f *fakeClient) UpdateFirewallRules(ctx context.Context, clusterID string, rules []FirewallRule) error {
	f.updateCalls++
	if f.updateErr != nil {
		return f.updateErr
	}
	out := make([]FirewallRule, len(rules))
	copy(out, rules)
	f.rules[clusterID] = out
	return nil
}

func assertRulesEqual(t *testing.T, got, want []FirewallRule) {
	t.Helper()
	gotCounts := make(map[FirewallRule]int, len(got))
	for _, r := range got {
		gotCounts[r]++
	}
	wantCounts := make(map[FirewallRule]int, len(want))
	for _, r := range want {
		wantCounts[r]++
	}
	if !reflect.DeepEqual(gotCounts, wantCounts) {
		t.Fatalf("rules mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestHasIP_ReturnsTrueWhenMatchingIPAddrRuleExists(t *testing.T) {
	rules := []FirewallRule{sentinelRule, {Type: RuleTypeIPAddr, Value: testDeviceIP}}

	got := HasIP(rules, testDeviceIP)

	if !got {
		t.Fatal("HasIP() = false, want true")
	}
}

func TestHasIP_IgnoresNonIPRuleWithSameValue(t *testing.T) {
	rules := []FirewallRule{{Type: "tag", Value: testDeviceIP}}

	got := HasIP(rules, testDeviceIP)

	if got {
		t.Fatal("HasIP() = true for non-ip_addr rule, want false")
	}
}

func TestHasIP_ReturnsFalseForEmptyIP(t *testing.T) {
	rules := []FirewallRule{{Type: RuleTypeIPAddr, Value: ""}}

	got := HasIP(rules, "")

	if got {
		t.Fatal("HasIP() = true for empty IP, want false")
	}
}

func TestAddIP_AppendsIPWithDescriptionWhilePreservingOtherUsersAndNonIPRules(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{
		sentinelRule, teammateRule, dropletRule, tagRule, k8sRule, appRule,
	})

	res, err := AddIP(context.Background(), client, testClusterID, testDeviceIP, testRuleDescription)

	if err != nil {
		t.Fatalf("AddIP() unexpected error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 7 {
		t.Fatalf("AddIP() = (Changed=%v, RemainingRules=%d), want (true, 7)", res.Changed, res.RemainingRules)
	}
	want := []FirewallRule{
		sentinelRule, teammateRule, dropletRule, tagRule, k8sRule, appRule,
		{Type: RuleTypeIPAddr, Value: testDeviceIP, Description: testRuleDescription},
	}
	assertRulesEqual(t, client.rules[testClusterID], want)
}

func TestAddIP_SkipsUpdateWhenIPAlreadyPresent(t *testing.T) {
	existing := []FirewallRule{sentinelRule, {Type: RuleTypeIPAddr, Value: testDeviceIP}}
	client := newFakeClient(testClusterID, existing)

	res, err := AddIP(context.Background(), client, testClusterID, testDeviceIP, testRuleDescription)

	if err != nil {
		t.Fatalf("AddIP() unexpected error: %v", err)
	}
	if res.Changed || res.RemainingRules != 2 || client.updateCalls != 0 {
		t.Fatalf("AddIP() = (Changed=%v, RemainingRules=%d, updateCalls=%d), want (false, 2, 0)", res.Changed, res.RemainingRules, client.updateCalls)
	}
}

func TestAddIP_RejectsEmptyIP(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{sentinelRule})

	_, err := AddIP(context.Background(), client, testClusterID, "", testRuleDescription)

	if !errors.Is(err, ErrEmptyIP) {
		t.Fatalf("AddIP() error = %v, want ErrEmptyIP", err)
	}
	if client.updateCalls != 0 {
		t.Fatalf("AddIP() called UpdateFirewallRules %d times for empty IP, want 0", client.updateCalls)
	}
}

func TestAddIP_PropagatesGetFirewallRulesError(t *testing.T) {
	client := newFakeClient(testClusterID, nil)
	client.getErr = errAPIUnavailable

	_, err := AddIP(context.Background(), client, testClusterID, testDeviceIP, testRuleDescription)

	if !errors.Is(err, errAPIUnavailable) {
		t.Fatalf("AddIP() error = %v, want %v", err, errAPIUnavailable)
	}
	if client.updateCalls != 0 {
		t.Fatalf("AddIP() called UpdateFirewallRules %d times after get error, want 0", client.updateCalls)
	}
}

func TestAddIP_PropagatesUpdateFirewallRulesError(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{sentinelRule})
	client.updateErr = errRateLimited

	_, err := AddIP(context.Background(), client, testClusterID, testDeviceIP, testRuleDescription)

	if !errors.Is(err, errRateLimited) {
		t.Fatalf("AddIP() error = %v, want %v", err, errRateLimited)
	}
}

func TestRemoveIP_RemovesOnlyMatchingIPAndPreservesOtherRules(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{
		sentinelRule,
		teammateRule,
		dropletRule,
		tagRule,
		k8sRule,
		appRule,
		{UUID: "u-mine", Type: RuleTypeIPAddr, Value: testDeviceIP},
	})

	res, err := RemoveIP(context.Background(), client, testClusterID, testDeviceIP)

	if err != nil {
		t.Fatalf("RemoveIP() unexpected error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 6 {
		t.Fatalf("RemoveIP() = (Changed=%v, RemainingRules=%d), want (true, 6)", res.Changed, res.RemainingRules)
	}
	assertRulesEqual(t, client.rules[testClusterID], []FirewallRule{
		sentinelRule, teammateRule, dropletRule, tagRule, k8sRule, appRule,
	})
}

func TestRemoveIP_SkipsUpdateWhenIPNotPresent(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{sentinelRule, teammateRule})

	res, err := RemoveIP(context.Background(), client, testClusterID, testDeviceIP)

	if err != nil {
		t.Fatalf("RemoveIP() unexpected error: %v", err)
	}
	if res.Changed || res.RemainingRules != 2 || client.updateCalls != 0 {
		t.Fatalf("RemoveIP() = (Changed=%v, RemainingRules=%d, updateCalls=%d), want (false, 2, 0)", res.Changed, res.RemainingRules, client.updateCalls)
	}
}

func TestRemoveIP_RejectsEmptyIP(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{sentinelRule})

	_, err := RemoveIP(context.Background(), client, testClusterID, "")

	if !errors.Is(err, ErrEmptyIP) {
		t.Fatalf("RemoveIP() error = %v, want ErrEmptyIP", err)
	}
}

func TestRemoveIP_PropagatesUpdateFirewallRulesError(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{
		sentinelRule,
		{Type: RuleTypeIPAddr, Value: testDeviceIP},
	})
	client.updateErr = errForbidden

	_, err := RemoveIP(context.Background(), client, testClusterID, testDeviceIP)

	if !errors.Is(err, errForbidden) {
		t.Fatalf("RemoveIP() error = %v, want %v", err, errForbidden)
	}
}

func TestReplaceIP_SwapsOldForNewWithDescriptionInSingleUpdatePreservingOtherRules(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{
		sentinelRule,
		teammateRule,
		dropletRule,
		{UUID: "u-old", Type: RuleTypeIPAddr, Value: testDeviceIP},
	})

	res, err := ReplaceIP(context.Background(), client, testClusterID, testDeviceIP, testNewIP, testRuleDescription)

	if err != nil {
		t.Fatalf("ReplaceIP() unexpected error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 4 || client.updateCalls != 1 {
		t.Fatalf("ReplaceIP() = (Changed=%v, RemainingRules=%d, updateCalls=%d), want (true, 4, 1)", res.Changed, res.RemainingRules, client.updateCalls)
	}
	assertRulesEqual(t, client.rules[testClusterID], []FirewallRule{
		sentinelRule,
		teammateRule,
		dropletRule,
		{Type: RuleTypeIPAddr, Value: testNewIP, Description: testRuleDescription},
	})
}

func TestReplaceIP_RemovesOldWithoutDuplicatingExistingNewIP(t *testing.T) {
	existingNew := FirewallRule{UUID: "u-new", Type: RuleTypeIPAddr, Value: testNewIP}
	client := newFakeClient(testClusterID, []FirewallRule{
		sentinelRule,
		{UUID: "u-old", Type: RuleTypeIPAddr, Value: testDeviceIP},
		existingNew,
	})

	res, err := ReplaceIP(context.Background(), client, testClusterID, testDeviceIP, testNewIP, testRuleDescription)

	if err != nil {
		t.Fatalf("ReplaceIP() unexpected error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 2 {
		t.Fatalf("ReplaceIP() = (Changed=%v, RemainingRules=%d), want (true, 2)", res.Changed, res.RemainingRules)
	}
	assertRulesEqual(t, client.rules[testClusterID], []FirewallRule{sentinelRule, existingNew})
}

func TestReplaceIP_SkipsUpdateWhenOldAbsentAndNewAlreadyPresent(t *testing.T) {
	existingNew := FirewallRule{UUID: "u-new", Type: RuleTypeIPAddr, Value: testNewIP}
	client := newFakeClient(testClusterID, []FirewallRule{sentinelRule, existingNew})

	res, err := ReplaceIP(context.Background(), client, testClusterID, testDeviceIP, testNewIP, testRuleDescription)

	if err != nil {
		t.Fatalf("ReplaceIP() unexpected error: %v", err)
	}
	if res.Changed || client.updateCalls != 0 {
		t.Fatalf("ReplaceIP() = (Changed=%v, updateCalls=%d), want (false, 0)", res.Changed, client.updateCalls)
	}
}

func TestReplaceIP_AddsNewIPWhenOldAndNewAreBothAbsent(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{sentinelRule})

	res, err := ReplaceIP(context.Background(), client, testClusterID, testDeviceIP, testNewIP, testRuleDescription)

	if err != nil {
		t.Fatalf("ReplaceIP() unexpected error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 2 {
		t.Fatalf("ReplaceIP() = (Changed=%v, RemainingRules=%d), want (true, 2)", res.Changed, res.RemainingRules)
	}
	assertRulesEqual(t, client.rules[testClusterID], []FirewallRule{
		sentinelRule,
		{Type: RuleTypeIPAddr, Value: testNewIP, Description: testRuleDescription},
	})
}

func TestReplaceIP_RejectsEmptyOldOrNewIP(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{sentinelRule})

	_, err := ReplaceIP(context.Background(), client, testClusterID, "", testNewIP, testRuleDescription)

	if !errors.Is(err, ErrEmptyIP) {
		t.Fatalf("ReplaceIP() error = %v, want ErrEmptyIP", err)
	}
}

func TestReplaceIP_PropagatesUpdateFirewallRulesError(t *testing.T) {
	client := newFakeClient(testClusterID, []FirewallRule{
		sentinelRule,
		{Type: RuleTypeIPAddr, Value: testDeviceIP},
	})
	client.updateErr = errServerError

	_, err := ReplaceIP(context.Background(), client, testClusterID, testDeviceIP, testNewIP, testRuleDescription)

	if !errors.Is(err, errServerError) {
		t.Fatalf("ReplaceIP() error = %v, want %v", err, errServerError)
	}
}
