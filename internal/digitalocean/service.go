package digitalocean

import (
	"context"
	"fmt"
)

func HasIP(rules []FirewallRule, ip string) bool {
	if ip == "" {
		return false
	}
	for _, r := range rules {
		if r.Type == RuleTypeIPAddr && r.Value == ip {
			return true
		}
	}
	return false
}

type MutationResult struct {
	Changed        bool
	RemainingRules int
	Rules          []FirewallRule
}

// AddIP fetches the latest rules before appending ip so concurrent changes are preserved.
func AddIP(ctx context.Context, client Client, clusterID, ip string) (MutationResult, error) {
	if ip == "" {
		return MutationResult{}, fmt.Errorf("cannot add empty IP")
	}

	rules, err := client.GetFirewallRules(ctx, clusterID)
	if err != nil {
		return MutationResult{}, err
	}

	if HasIP(rules, ip) {
		return MutationResult{
			Changed:        false,
			RemainingRules: len(rules),
			Rules:          rules,
		}, nil
	}

	updated := append(append([]FirewallRule(nil), rules...), FirewallRule{
		Type:  RuleTypeIPAddr,
		Value: ip,
	})

	if err := client.UpdateFirewallRules(ctx, clusterID, updated); err != nil {
		return MutationResult{}, err
	}

	return MutationResult{
		Changed:        true,
		RemainingRules: len(updated),
		Rules:          updated,
	}, nil
}

// RemoveIP fetches the latest rules and removes only the ip_addr rule matching ip.
func RemoveIP(ctx context.Context, client Client, clusterID, ip string) (MutationResult, error) {
	if ip == "" {
		return MutationResult{}, fmt.Errorf("cannot remove empty IP")
	}

	rules, err := client.GetFirewallRules(ctx, clusterID)
	if err != nil {
		return MutationResult{}, err
	}

	updated := make([]FirewallRule, 0, len(rules))
	removed := false
	for _, r := range rules {
		if r.Type == RuleTypeIPAddr && r.Value == ip {
			removed = true
			continue
		}
		updated = append(updated, r)
	}

	if !removed {
		return MutationResult{
			Changed:        false,
			RemainingRules: len(rules),
			Rules:          rules,
		}, nil
	}

	if err := client.UpdateFirewallRules(ctx, clusterID, updated); err != nil {
		return MutationResult{}, err
	}

	return MutationResult{
		Changed:        true,
		RemainingRules: len(updated),
		Rules:          updated,
	}, nil
}

// ReplaceIP swaps oldIP for newIP in a single UpdateFirewallRules call, leaving all other rules untouched.
func ReplaceIP(ctx context.Context, client Client, clusterID, oldIP, newIP string) (MutationResult, error) {
	if oldIP == "" || newIP == "" {
		return MutationResult{}, fmt.Errorf("both oldIP and newIP must be non-empty")
	}
	if oldIP == newIP {
		return AddIP(ctx, client, clusterID, newIP)
	}

	rules, err := client.GetFirewallRules(ctx, clusterID)
	if err != nil {
		return MutationResult{}, err
	}

	updated := make([]FirewallRule, 0, len(rules)+1)
	removedOld := false
	hasNew := false
	for _, r := range rules {
		if r.Type == RuleTypeIPAddr && r.Value == oldIP {
			removedOld = true
			continue
		}
		if r.Type == RuleTypeIPAddr && r.Value == newIP {
			hasNew = true
		}
		updated = append(updated, r)
	}

	if !hasNew {
		updated = append(updated, FirewallRule{
			Type:  RuleTypeIPAddr,
			Value: newIP,
		})
	}

	if !removedOld && hasNew {
		return MutationResult{
			Changed:        false,
			RemainingRules: len(rules),
			Rules:          rules,
		}, nil
	}

	if err := client.UpdateFirewallRules(ctx, clusterID, updated); err != nil {
		return MutationResult{}, err
	}

	return MutationResult{
		Changed:        true,
		RemainingRules: len(updated),
		Rules:          updated,
	}, nil
}
