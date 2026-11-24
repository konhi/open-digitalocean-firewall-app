package digitalocean

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/digitalocean/godo"
)

const RuleTypeIPAddr = "ip_addr"

type Database struct {
	ID         string
	Name       string
	EngineSlug string
	RegionSlug string
	Status     string
}

type FirewallRule struct {
	UUID        string
	ClusterUUID string
	Type        string
	Value       string
	Description string
}

type firewallRulePayload struct {
	UUID        string `json:"uuid,omitempty"`
	ClusterUUID string `json:"cluster_uuid,omitempty"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type firewallRulesEnvelope struct {
	Rules []firewallRulePayload `json:"rules"`
}

type Client interface {
	ListDatabases(ctx context.Context) ([]Database, error)
	GetFirewallRules(ctx context.Context, clusterID string) ([]FirewallRule, error)
	UpdateFirewallRules(ctx context.Context, clusterID string, rules []FirewallRule) error
}

type GodoClient struct {
	client *godo.Client
}

func NewClient(token string) *GodoClient {
	return &GodoClient{
		client: godo.NewFromToken(token),
	}
}

// ListDatabases returns all managed database clusters across all pages, sorted by name.
func (c *GodoClient) ListDatabases(ctx context.Context) ([]Database, error) {
	var all []Database
	opts := &godo.ListOptions{Page: 1, PerPage: 100}

	for {
		dbs, resp, err := c.client.Databases.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list DigitalOcean databases: %w", err)
		}

		for _, db := range dbs {
			all = append(all, Database{
				ID:         db.ID,
				Name:       db.Name,
				EngineSlug: db.EngineSlug,
				RegionSlug: db.RegionSlug,
				Status:     db.Status,
			})
		}

		if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		page, err := resp.Links.CurrentPage()
		if err != nil {
			break
		}
		opts.Page = page + 1
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Name != all[j].Name {
			return all[i].Name < all[j].Name
		}
		return all[i].ID < all[j].ID
	})

	return all, nil
}

func (c *GodoClient) GetFirewallRules(ctx context.Context, clusterID string) ([]FirewallRule, error) {
	path := fmt.Sprintf("v2/databases/%s/firewall", clusterID)
	req, err := c.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("build get firewall rules request for %s: %w", clusterID, err)
	}

	var env firewallRulesEnvelope
	if _, err := c.client.Do(ctx, req, &env); err != nil {
		return nil, fmt.Errorf("get firewall rules for %s: %w", clusterID, err)
	}

	rules := make([]FirewallRule, 0, len(env.Rules))
	for _, r := range env.Rules {
		rules = append(rules, FirewallRule(r))
	}
	return rules, nil
}

// UpdateFirewallRules replaces the trusted sources for a database cluster.
func (c *GodoClient) UpdateFirewallRules(ctx context.Context, clusterID string, rules []FirewallRule) error {
	payload := firewallRulesEnvelope{
		Rules: make([]firewallRulePayload, 0, len(rules)),
	}
	for _, r := range rules {
		payload.Rules = append(payload.Rules, firewallRulePayload(r))
	}

	path := fmt.Sprintf("v2/databases/%s/firewall", clusterID)
	req, err := c.client.NewRequest(ctx, http.MethodPut, path, payload)
	if err != nil {
		return fmt.Errorf("build update firewall rules request for %s: %w", clusterID, err)
	}
	if _, err := c.client.Do(ctx, req, nil); err != nil {
		return fmt.Errorf("update firewall rules for %s: %w", clusterID, err)
	}
	return nil
}
