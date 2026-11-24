package manager

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
	"github.com/konhi/open-digitalocean-firewall-app/internal/ip"
	"github.com/konhi/open-digitalocean-firewall-app/internal/state"
)

var ErrNoTokenConfigured = errors.New("DigitalOcean API token is not configured")

type DatabaseStatus struct {
	Database  digitalocean.Database
	Allowed   bool
	Managed   bool
	ManagedIP string
	ExpiresAt *time.Time
	RuleCount int
}

type Snapshot struct {
	HasToken    bool
	CurrentIP   string
	Databases   []DatabaseStatus
	Settings    state.Settings
	LastError   string
	Busy        bool
	ActiveCount int
}

type Manager struct {
	mu       sync.Mutex
	client   digitalocean.Client
	detector ip.Detector
	store    *state.Store
	data     state.Data
	now      func() time.Time
	logger   *log.Logger
	onChange func(Snapshot)

	currentIP string
	databases []DatabaseStatus
	lastErr   string
	busy      bool
}

func New(client digitalocean.Client, detector ip.Detector, store *state.Store, logger *log.Logger) (*Manager, error) {
	if detector == nil {
		detector = ip.NewDetector()
	}
	if logger == nil {
		logger = log.Default()
	}

	data := state.DefaultData()
	if store != nil {
		loaded, err := store.Load()
		if err != nil {
			logger.Printf("warning: failed to load state, using defaults: %v", err)
		} else {
			data = loaded
		}
	}

	return &Manager{
		client:   client,
		detector: detector,
		store:    store,
		data:     data,
		now:      func() time.Time { return time.Now().UTC() },
		logger:   logger,
	}, nil
}

// SetOnChange registers fn and invokes it immediately with the current snapshot.
func (m *Manager) SetOnChange(fn func(Snapshot)) {
	m.mu.Lock()
	m.onChange = fn
	snap := m.snapshotLocked()
	m.mu.Unlock()

	if fn != nil {
		fn(snap)
	}
}

func (m *Manager) SetClient(client digitalocean.Client) {
	m.mu.Lock()
	m.client = client
	m.lastErr = ""
	if client == nil {
		m.databases = nil
	}
	snap, cb := m.snapshotLocked(), m.onChange
	m.mu.Unlock()

	if cb != nil {
		cb(snap)
	}
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() Snapshot {
	dbs := make([]DatabaseStatus, len(m.databases))
	active := 0
	for i, d := range m.databases {
		dbs[i] = d
		if d.ExpiresAt != nil {
			t := *d.ExpiresAt
			dbs[i].ExpiresAt = &t
		}
		if d.Allowed {
			active++
		}
	}

	return Snapshot{
		HasToken:    m.client != nil,
		CurrentIP:   m.currentIP,
		Databases:   dbs,
		Settings:    m.data.Settings,
		LastError:   m.lastErr,
		Busy:        m.busy,
		ActiveCount: active,
	}
}

func (m *Manager) notify() {
	m.mu.Lock()
	snap, cb := m.snapshotLocked(), m.onChange
	m.mu.Unlock()
	if cb != nil {
		cb(snap)
	}
}

func (m *Manager) saveStateLocked() {
	if m.store == nil {
		return
	}
	if err := m.store.Save(m.data); err != nil {
		m.logger.Printf("warning: failed to persist state: %v", err)
	}
}

// Refresh reconciles expired or changed IPs, then reloads all database statuses.
func (m *Manager) Refresh(ctx context.Context) error {
	m.mu.Lock()
	m.busy = true
	m.mu.Unlock()
	m.notify()

	defer func() {
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		m.notify()
	}()

	detectedIP, ipErr := m.detector.Detect(ctx)

	m.mu.Lock()
	if ipErr == nil {
		m.currentIP = detectedIP
	}
	client := m.client
	curIP := m.currentIP
	if client == nil {
		if ipErr != nil {
			m.lastErr = ipErr.Error()
		} else {
			m.lastErr = ""
		}
		m.mu.Unlock()
		return ipErr
	}
	m.mu.Unlock()

	var errs []error
	if ipErr != nil {
		errs = append(errs, ipErr)
	}

	if err := m.reconcileManagedRules(ctx, client, curIP); err != nil {
		errs = append(errs, err)
	}

	dbs, err := client.ListDatabases(ctx)
	if err != nil {
		m.mu.Lock()
		m.lastErr = err.Error()
		m.mu.Unlock()
		return err
	}

	statuses := make([]DatabaseStatus, 0, len(dbs))
	m.mu.Lock()
	stateChanged := false
	for _, db := range dbs {
		rules, err := client.GetFirewallRules(ctx, db.ID)
		if err != nil {
			errs = append(errs, err)
			statuses = append(statuses, DatabaseStatus{Database: db})
			continue
		}

		allowed := digitalocean.HasIP(rules, curIP)
		managedRule, isManaged := m.data.ManagedRules[db.ID]

		// If the user manually removed our exact IP in the DigitalOcean dashboard, stop tracking it.
		if isManaged && managedRule.IP == curIP && !allowed {
			delete(m.data.ManagedRules, db.ID)
			isManaged = false
			managedRule = state.ManagedRule{}
			stateChanged = true
		}

		status := DatabaseStatus{
			Database:  db,
			Allowed:   allowed,
			Managed:   isManaged,
			ManagedIP: managedRule.IP,
			RuleCount: len(rules),
		}
		if isManaged && managedRule.ExpiresAt != nil {
			t := *managedRule.ExpiresAt
			status.ExpiresAt = &t
		}
		statuses = append(statuses, status)
	}

	if stateChanged {
		m.saveStateLocked()
	}

	m.databases = statuses
	joined := errors.Join(errs...)
	if joined != nil {
		m.lastErr = joined.Error()
	} else {
		m.lastErr = ""
	}
	m.mu.Unlock()

	return joined
}

// Tick revokes expired rules and replaces managed IPs when the public IP changes.
func (m *Manager) Tick(ctx context.Context) error {
	m.mu.Lock()
	client := m.client
	prevIP := m.currentIP
	hasManaged := len(m.data.ManagedRules) > 0
	m.mu.Unlock()

	if client == nil {
		return nil
	}

	newIP, ipErr := m.detector.Detect(ctx)
	if ipErr != nil {
		m.logger.Printf("background IP check failed: %v", ipErr)
		newIP = prevIP
	}

	ipChanged := ipErr == nil && newIP != "" && newIP != prevIP
	if ipChanged {
		m.logger.Printf("public IP changed: %s -> %s", prevIP, newIP)
		m.mu.Lock()
		m.currentIP = newIP
		m.mu.Unlock()
	}

	if !hasManaged && !ipChanged {
		return nil
	}

	err := m.reconcileManagedRules(ctx, client, newIP)
	if err != nil {
		m.logger.Printf("reconcile error: %v", err)
	}

	if ipChanged || hasManaged {
		return m.Refresh(ctx)
	}
	return err
}

func (m *Manager) reconcileManagedRules(ctx context.Context, client digitalocean.Client, curIP string) error {
	m.mu.Lock()
	now := m.now()
	autoUpdate := m.data.Settings.AutoUpdateOnIPChange
	username := m.data.Settings.Username
	rulesCopy := make(map[string]state.ManagedRule, len(m.data.ManagedRules))
	for k, v := range m.data.ManagedRules {
		rulesCopy[k] = v
	}
	m.mu.Unlock()

	var errs []error
	for clusterID, rule := range rulesCopy {
		if rule.IsExpired(now) {
			m.logger.Printf("access timer expired for cluster %s (IP %s); revoking", clusterID, rule.IP)
			res, err := digitalocean.RemoveIP(ctx, client, clusterID, rule.IP)
			if err != nil {
				errs = append(errs, fmt.Errorf("revoke expired IP on %s: %w", clusterID, err))
				continue
			}
			if res.RemainingRules == 0 {
				m.logger.Printf("warning: cluster %s now has 0 firewall rules (open to all IPs)", clusterID)
			}
			m.mu.Lock()
			delete(m.data.ManagedRules, clusterID)
			m.saveStateLocked()
			m.mu.Unlock()
			continue
		}

		if autoUpdate && curIP != "" && rule.IP != "" && rule.IP != curIP {
			m.logger.Printf("replacing managed IP on cluster %s: %s -> %s", clusterID, rule.IP, curIP)
			desc := FormatRuleDescription(username, rule.AddedAt, rule.ExpiresAt)
			if _, err := digitalocean.ReplaceIP(ctx, client, clusterID, rule.IP, curIP, desc); err != nil {
				errs = append(errs, fmt.Errorf("replace IP on %s: %w", clusterID, err))
				continue
			}
			m.mu.Lock()
			updated := rule
			updated.IP = curIP
			m.data.ManagedRules[clusterID] = updated
			m.saveStateLocked()
			m.mu.Unlock()
		}
	}

	return errors.Join(errs...)
}

func (m *Manager) ToggleDatabase(ctx context.Context, clusterID string) error {
	m.mu.Lock()
	client := m.client
	if client == nil {
		m.mu.Unlock()
		return ErrNoTokenConfigured
	}
	curIP := m.currentIP
	isAllowed := false
	for _, db := range m.databases {
		if db.Database.ID == clusterID {
			isAllowed = db.Allowed
			break
		}
	}
	managedRule, isManaged := m.data.ManagedRules[clusterID]
	m.mu.Unlock()

	if curIP == "" {
		detected, err := m.detector.Detect(ctx)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.currentIP = detected
		curIP = detected
		m.mu.Unlock()
	}

	if isAllowed || isManaged {
		return m.removeFromCluster(ctx, client, clusterID, curIP, managedRule, isManaged)
	}
	return m.addToCluster(ctx, client, clusterID, curIP)
}

func (m *Manager) addToCluster(ctx context.Context, client digitalocean.Client, clusterID, ipAddr string) error {
	m.mu.Lock()
	m.busy = true
	durationMin := m.data.Settings.DefaultDurationMinutes
	username := m.data.Settings.Username
	now := m.now()
	m.mu.Unlock()
	m.notify()

	defer func() {
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		m.notify()
	}()

	var expiresAt *time.Time
	if durationMin > 0 {
		t := now.Add(time.Duration(durationMin) * time.Minute)
		expiresAt = &t
	}

	desc := FormatRuleDescription(username, now, expiresAt)
	res, err := digitalocean.AddIP(ctx, client, clusterID, ipAddr, desc)
	if err != nil {
		m.mu.Lock()
		m.lastErr = err.Error()
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	m.data.ManagedRules[clusterID] = state.ManagedRule{
		IP:        ipAddr,
		AddedAt:   now,
		ExpiresAt: expiresAt,
	}
	m.saveStateLocked()
	for i := range m.databases {
		if m.databases[i].Database.ID == clusterID {
			m.databases[i].Allowed = true
			m.databases[i].Managed = true
			m.databases[i].ManagedIP = ipAddr
			m.databases[i].ExpiresAt = expiresAt
			m.databases[i].RuleCount = res.RemainingRules
		}
	}
	m.lastErr = ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) removeFromCluster(ctx context.Context, client digitalocean.Client, clusterID, curIP string, managedRule state.ManagedRule, isManaged bool) error {
	m.mu.Lock()
	m.busy = true
	m.mu.Unlock()
	m.notify()

	defer func() {
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		m.notify()
	}()

	// Always remove the exact IP we added (if tracked) and also remove curIP if it differs.
	var ipsToRemove []string
	if isManaged && managedRule.IP != "" {
		ipsToRemove = append(ipsToRemove, managedRule.IP)
	}
	if curIP != "" && (!isManaged || managedRule.IP != curIP) {
		ipsToRemove = append(ipsToRemove, curIP)
	}

	var lastRes digitalocean.MutationResult
	for _, targetIP := range ipsToRemove {
		res, err := digitalocean.RemoveIP(ctx, client, clusterID, targetIP)
		if err != nil {
			m.mu.Lock()
			m.lastErr = err.Error()
			m.mu.Unlock()
			return err
		}
		lastRes = res
		if res.RemainingRules == 0 {
			m.logger.Printf("warning: cluster %s now has 0 firewall rules (open to all IPs)", clusterID)
		}
	}

	m.mu.Lock()
	delete(m.data.ManagedRules, clusterID)
	m.saveStateLocked()
	for i := range m.databases {
		if m.databases[i].Database.ID == clusterID {
			m.databases[i].Allowed = false
			m.databases[i].Managed = false
			m.databases[i].ManagedIP = ""
			m.databases[i].ExpiresAt = nil
			m.databases[i].RuleCount = lastRes.RemainingRules
		}
	}
	m.lastErr = ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) AllowAll(ctx context.Context) error {
	m.mu.Lock()
	client := m.client
	if client == nil {
		m.mu.Unlock()
		return ErrNoTokenConfigured
	}
	curIP := m.currentIP
	ids := make([]string, len(m.databases))
	for i, db := range m.databases {
		ids[i] = db.Database.ID
	}
	m.mu.Unlock()

	if curIP == "" {
		detected, err := m.detector.Detect(ctx)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.currentIP = detected
		curIP = detected
		m.mu.Unlock()
	}

	var errs []error
	for _, id := range ids {
		if err := m.addToCluster(ctx, client, id, curIP); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) RemoveAll(ctx context.Context) error {
	m.mu.Lock()
	client := m.client
	if client == nil {
		m.mu.Unlock()
		return ErrNoTokenConfigured
	}
	curIP := m.currentIP
	type target struct {
		id        string
		rule      state.ManagedRule
		isManaged bool
	}
	targets := make([]target, 0, len(m.databases))
	seen := make(map[string]bool)
	for _, db := range m.databases {
		rule, ok := m.data.ManagedRules[db.Database.ID]
		if db.Allowed || ok {
			targets = append(targets, target{id: db.Database.ID, rule: rule, isManaged: ok})
			seen[db.Database.ID] = true
		}
	}
	for id, rule := range m.data.ManagedRules {
		if !seen[id] {
			targets = append(targets, target{id: id, rule: rule, isManaged: true})
		}
	}
	m.mu.Unlock()

	var errs []error
	for _, t := range targets {
		if err := m.removeFromCluster(ctx, client, t.id, curIP, t.rule, t.isManaged); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CleanupOnQuit removes ONLY the firewall rules added by this app when AutoCleanupOnQuit is enabled.
func (m *Manager) CleanupOnQuit(ctx context.Context) error {
	m.mu.Lock()
	if !m.data.Settings.AutoCleanupOnQuit || m.client == nil || len(m.data.ManagedRules) == 0 {
		m.mu.Unlock()
		return nil
	}
	client := m.client
	rulesCopy := make(map[string]state.ManagedRule, len(m.data.ManagedRules))
	for k, v := range m.data.ManagedRules {
		rulesCopy[k] = v
	}
	m.mu.Unlock()

	var errs []error
	for clusterID, rule := range rulesCopy {
		m.logger.Printf("quit cleanup: removing managed IP %s from cluster %s", rule.IP, clusterID)
		res, err := digitalocean.RemoveIP(ctx, client, clusterID, rule.IP)
		if err != nil {
			errs = append(errs, fmt.Errorf("cleanup cluster %s: %w", clusterID, err))
			continue
		}
		if res.RemainingRules == 0 {
			m.logger.Printf("warning: cluster %s now has 0 firewall rules (open to all IPs)", clusterID)
		}
		m.mu.Lock()
		delete(m.data.ManagedRules, clusterID)
		m.saveStateLocked()
		m.mu.Unlock()
	}

	return errors.Join(errs...)
}

// SetDefaultDuration sets the access timer in minutes (0 means until quit).
func (m *Manager) SetDefaultDuration(minutes int) {
	if minutes < 0 {
		minutes = 0
	}
	m.mu.Lock()
	m.data.Settings.DefaultDurationMinutes = minutes
	m.saveStateLocked()
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) SetAutoUpdateOnIPChange(enabled bool) {
	m.mu.Lock()
	m.data.Settings.AutoUpdateOnIPChange = enabled
	m.saveStateLocked()
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) SetAutoCleanupOnQuit(enabled bool) {
	m.mu.Lock()
	m.data.Settings.AutoCleanupOnQuit = enabled
	m.saveStateLocked()
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) SetUsername(username string) {
	m.mu.Lock()
	m.data.Settings.Username = strings.TrimSpace(username)
	m.saveStateLocked()
	m.mu.Unlock()
	m.notify()
}

// FormatRuleDescription builds the DigitalOcean firewall rule description with optional username and deletion date.
func FormatRuleDescription(username string, addedAt time.Time, expiresAt *time.Time) string {
	var deletePart string
	if expiresAt != nil {
		deletePart = "delete after " + expiresAt.UTC().Format("2006-01-02 15:04 UTC")
	} else {
		deletePart = "delete on quit (added " + addedAt.UTC().Format("2006-01-02") + ")"
	}

	name := strings.TrimSpace(username)
	if name == "" {
		return deletePart
	}
	return name + " - " + deletePart
}
