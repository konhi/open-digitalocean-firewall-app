package digitalocean

import (
	"context"
	"fmt"
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
	godoRules, _, err := c.client.Databases.GetFirewallRules(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("get firewall rules for %s: %w", clusterID, err)
	}

	rules := make([]FirewallRule, 0, len(godoRules))
	for _, r := range godoRules {
		rules = append(rules, FirewallRule{
			UUID:        r.UUID,
			ClusterUUID: r.ClusterUUID,
			Type:        r.Type,
			Value:       r.Value,
		})
	}
	return rules, nil
}

// UpdateFirewallRules replaces the trusted sources for a database cluster.
func (c *GodoClient) UpdateFirewallRules(ctx context.Context, clusterID string, rules []FirewallRule) error {
	reqRules := make([]*godo.DatabaseFirewallRule, 0, len(rules))
	for _, r := range rules {
		reqRules = append(reqRules, &godo.DatabaseFirewallRule{
			UUID:        r.UUID,
			ClusterUUID: r.ClusterUUID,
			Type:        r.Type,
			Value:       r.Value,
		})
	}

	req := &godo.DatabaseUpdateFirewallRulesRequest{
		Rules: reqRules,
	}
	if _, err := c.client.Databases.UpdateFirewallRules(ctx, clusterID, req); err != nil {
		return fmt.Errorf("update firewall rules for %s: %w", clusterID, err)
	}
	return nil
}
