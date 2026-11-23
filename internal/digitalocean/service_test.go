package digitalocean

import (
	"context"
	"testing"
)

type fakeClient struct {
	rules       map[string][]FirewallRule
	updateCalls int
}

func (f *fakeClient) ListDatabases(ctx context.Context) ([]Database, error) {
	return nil, nil
}

func (f *fakeClient) GetFirewallRules(ctx context.Context, clusterID string) ([]FirewallRule, error) {
	src := f.rules[clusterID]
	out := make([]FirewallRule, len(src))
	copy(out, src)
	return out, nil
}

func (f *fakeClient) UpdateFirewallRules(ctx context.Context, clusterID string, rules []FirewallRule) error {
	f.updateCalls++
	out := make([]FirewallRule, len(rules))
	copy(out, rules)
	f.rules[clusterID] = out
	return nil
}

func TestAddRemoveReplaceIP_PreservesOtherUsersRules(t *testing.T) {
	ctx := context.Background()
	clusterID := "cluster-1"

	client := &fakeClient{
		rules: map[string][]FirewallRule{
			clusterID: {
				{UUID: "u-deny", Type: RuleTypeIPAddr, Value: "192.0.2.1"},
				{UUID: "u-teammate", Type: RuleTypeIPAddr, Value: "198.51.100.77"},
				{UUID: "u-droplet", Type: "droplet", Value: "123456"},
			},
		},
	}

	res, err := AddIP(ctx, client, clusterID, "203.0.113.10")
	if err != nil {
		t.Fatalf("AddIP() error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 4 || client.updateCalls != 1 {
		t.Fatalf("unexpected AddIP result: %+v (calls=%d)", res, client.updateCalls)
	}

	res, err = AddIP(ctx, client, clusterID, "203.0.113.10")
	if err != nil {
		t.Fatalf("AddIP() duplicate error: %v", err)
	}
	if res.Changed || client.updateCalls != 1 {
		t.Fatalf("expected no-op on duplicate AddIP, got changed=%v calls=%d", res.Changed, client.updateCalls)
	}

	res, err = ReplaceIP(ctx, client, clusterID, "203.0.113.10", "203.0.113.99")
	if err != nil {
		t.Fatalf("ReplaceIP() error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 4 || client.updateCalls != 2 {
		t.Fatalf("unexpected ReplaceIP result: %+v (calls=%d)", res, client.updateCalls)
	}
	if HasIP(client.rules[clusterID], "203.0.113.10") {
		t.Fatal("old IP should have been removed")
	}
	if !HasIP(client.rules[clusterID], "203.0.113.99") {
		t.Fatal("new IP should have been added")
	}
	if !HasIP(client.rules[clusterID], "198.51.100.77") || !HasIP(client.rules[clusterID], "192.0.2.1") {
		t.Fatal("other users' rules were modified")
	}

	res, err = RemoveIP(ctx, client, clusterID, "203.0.113.99")
	if err != nil {
		t.Fatalf("RemoveIP() error: %v", err)
	}
	if !res.Changed || res.RemainingRules != 3 || client.updateCalls != 3 {
		t.Fatalf("unexpected RemoveIP result: %+v (calls=%d)", res, client.updateCalls)
	}
	if HasIP(client.rules[clusterID], "203.0.113.99") {
		t.Fatal("my IP should have been removed")
	}
	if !HasIP(client.rules[clusterID], "198.51.100.77") || !HasIP(client.rules[clusterID], "192.0.2.1") {
		t.Fatal("other users' rules were unexpectedly removed")
	}
	foundDroplet := false
	for _, r := range client.rules[clusterID] {
		if r.Type == "droplet" && r.Value == "123456" && r.UUID == "u-droplet" {
			foundDroplet = true
		}
	}
	if !foundDroplet {
		t.Fatal("droplet rule was lost")
	}
}
