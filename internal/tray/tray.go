package tray

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/konhi/open-digitalocean-firewall-app/internal/autostart"
	"github.com/konhi/open-digitalocean-firewall-app/internal/config"
	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
	"github.com/konhi/open-digitalocean-firewall-app/internal/manager"
)

type durationOption struct {
	label   string
	minutes int
}

var durationOptions = []durationOption{
	{label: "15 minutes", minutes: 15},
	{label: "1 hour", minutes: 60},
	{label: "4 hours", minutes: 240},
	{label: "Until quit (no timer)", minutes: 0},
}

type menuHandles struct {
	built      bool
	hasToken   bool
	dbIDs      []string
	trayInited bool
	lastActive bool
	lastTitle  string

	statusItem      *systray.MenuItem
	ipItem          *systray.MenuItem
	emptyItem       *systray.MenuItem
	dbItems         map[string]*systray.MenuItem
	allowAllItem    *systray.MenuItem
	revokeAllItem   *systray.MenuItem
	refreshItem     *systray.MenuItem
	durMenuItem     *systray.MenuItem
	durSubItems     map[int]*systray.MenuItem
	autoUpdateItem   *systray.MenuItem
	autoCleanupItem  *systray.MenuItem
	startAtLoginItem *systray.MenuItem
	usernameItem     *systray.MenuItem
}

func (m *menuHandles) needsRebuild(snap manager.Snapshot) bool {
	if !m.built || m.hasToken != snap.HasToken {
		return true
	}
	if len(m.dbIDs) != len(snap.Databases) {
		return true
	}
	for i, db := range snap.Databases {
		if m.dbIDs[i] != db.Database.ID {
			return true
		}
	}
	return false
}

type App struct {
	mgr        *manager.Manager
	tokenStore *config.TokenStore
	autostart  autostart.Manager
	logger     *log.Logger
	icons      iconSet
	menu       menuHandles

	mu          sync.Mutex
	renderCh    chan manager.Snapshot
	renderStop  context.CancelFunc
	bgCancel    context.CancelFunc
	quitOnce    sync.Once
	cleanupDone chan struct{}
}

func New(mgr *manager.Manager, tokenStore *config.TokenStore, autostartMgr autostart.Manager, logger *log.Logger) *App {
	if logger == nil {
		logger = log.Default()
	}

	return &App{
		mgr:         mgr,
		tokenStore:  tokenStore,
		autostart:   autostartMgr,
		logger:      logger,
		icons:       newIconSet(),
		renderCh:    make(chan manager.Snapshot, 1),
		cleanupDone: make(chan struct{}),
	}
}

func (a *App) Run() {
	systray.Run(a.onReady, a.onExit)
}

func (a *App) RequestQuit() {
	a.quitOnce.Do(func() {
		a.mu.Lock()
		if a.bgCancel != nil {
			a.bgCancel()
		}
		a.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.mgr.CleanupOnQuit(ctx); err != nil {
			a.logger.Printf("quit cleanup finished with errors: %v", err)
		}
		close(a.cleanupDone)
		systray.Quit()
	})
}

func (a *App) onReady() {
	systray.SetTitle("")
	systray.SetTooltip("Open DigitalOcean Firewall App")
	systray.SetTemplateIcon(a.icons.trayIdle.template, a.icons.trayIdle.regular)

	bgCtx, bgCancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.bgCancel = bgCancel
	a.mu.Unlock()

	go a.renderLoop(bgCtx)

	a.mgr.SetOnChange(func(snap manager.Snapshot) {
		select {
		case a.renderCh <- snap:
		default:
			select {
			case <-a.renderCh:
			default:
			}
			a.renderCh <- snap
		}
	})

	go func() {
		ctx, cancel := context.WithTimeout(bgCtx, 20*time.Second)
		if err := a.mgr.Refresh(ctx); err != nil {
			a.logger.Printf("initial refresh error: %v", err)
		}
		cancel()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				tctx, tcancel := context.WithTimeout(bgCtx, 20*time.Second)
				if err := a.mgr.Tick(tctx); err != nil {
					a.logger.Printf("background tick error: %v", err)
				}
				tcancel()
			}
		}
	}()
}

func (a *App) onExit() {
	a.mu.Lock()
	if a.renderStop != nil {
		a.renderStop()
	}
	if a.bgCancel != nil {
		a.bgCancel()
	}
	a.mu.Unlock()

	// Ensure cleanup runs even if systray exited via OS quit signal.
	a.quitOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.mgr.CleanupOnQuit(ctx); err != nil {
			a.logger.Printf("exit cleanup finished with errors: %v", err)
		}
		close(a.cleanupDone)
	})
	<-a.cleanupDone
}

func (a *App) renderLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case snap := <-a.renderCh:
			a.renderMenu(ctx, snap)
		}
	}
}

func (a *App) renderMenu(parentCtx context.Context, snap manager.Snapshot) {
	a.updateTrayBar(snap)

	if a.menu.needsRebuild(snap) {
		a.rebuildMenu(parentCtx, snap)
	}
	a.updateMenuInPlace(snap)
}

func (a *App) updateTrayBar(snap manager.Snapshot) {
	active := snap.ActiveCount > 0
	title := ""
	tooltip := "Open DigitalOcean Firewall App — No active database access"
	if active {
		title = fmt.Sprintf("%d", snap.ActiveCount)
		tooltip = fmt.Sprintf("Open DigitalOcean Firewall App — Access open on %s", pluralizeDB(snap.ActiveCount))
	}

	if !a.menu.trayInited || a.menu.lastActive != active {
		if active {
			systray.SetTemplateIcon(a.icons.trayActive.template, a.icons.trayActive.regular)
		} else {
			systray.SetTemplateIcon(a.icons.trayIdle.template, a.icons.trayIdle.regular)
		}
		a.menu.lastActive = active
	}
	if !a.menu.trayInited || a.menu.lastTitle != title {
		systray.SetTitle(title)
		systray.SetTooltip(tooltip)
		a.menu.lastTitle = title
	}
	a.menu.trayInited = true
}

func (a *App) rebuildMenu(parentCtx context.Context, snap manager.Snapshot) {
	a.mu.Lock()
	if a.renderStop != nil {
		a.renderStop()
	}
	menuCtx, cancel := context.WithCancel(parentCtx)
	a.renderStop = cancel
	a.mu.Unlock()

	systray.ResetMenu()

	m := menuHandles{
		built:       true,
		hasToken:    snap.HasToken,
		dbIDs:       make([]string, len(snap.Databases)),
		trayInited:  a.menu.trayInited,
		lastActive:  a.menu.lastActive,
		lastTitle:   a.menu.lastTitle,
		dbItems:     make(map[string]*systray.MenuItem, len(snap.Databases)),
		durSubItems: make(map[int]*systray.MenuItem, len(durationOptions)),
	}
	for i, db := range snap.Databases {
		m.dbIDs[i] = db.Database.ID
	}

	m.statusItem = systray.AddMenuItem(formatStatus(snap), "Current firewall status")
	m.statusItem.Disable()

	m.ipItem = systray.AddMenuItem(formatIPLabel(snap.CurrentIP), "Current public IPv4 address")
	m.ipItem.SetTemplateIcon(a.icons.globe.template, a.icons.globe.regular)
	m.ipItem.Disable()

	systray.AddSeparator()

	if !snap.HasToken {
		cfgItem := systray.AddMenuItem("Sign In with DigitalOcean Token…", "Enter your DigitalOcean personal access token")
		cfgItem.SetTemplateIcon(a.icons.key.template, a.icons.key.regular)
		go func() {
			for {
				select {
				case <-menuCtx.Done():
					return
				case <-cfgItem.ClickedCh:
					a.configureToken(parentCtx)
				}
			}
		}()
	} else if len(snap.Databases) == 0 {
		m.emptyItem = systray.AddMenuItem("No managed databases found", "")
		m.emptyItem.SetTemplateIcon(a.icons.database.template, a.icons.database.regular)
		m.emptyItem.Disable()
	} else {
		header := systray.AddMenuItem("Managed Databases", "Click a database to toggle access for your IP")
		header.Disable()

		now := time.Now()
		for _, db := range snap.Databases {
			dbID := db.Database.ID
			dbName := db.Database.Name
			item := systray.AddMenuItemCheckbox(formatDBLabel(db, now), "", db.Allowed)
			item.SetTemplateIcon(a.icons.database.template, a.icons.database.regular)
			m.dbItems[dbID] = item
			go func() {
				for {
					select {
					case <-menuCtx.Done():
						return
					case <-item.ClickedCh:
						ctx, cancel := context.WithTimeout(parentCtx, 20*time.Second)
						if err := a.mgr.ToggleDatabase(ctx, dbID); err != nil {
							a.logger.Printf("toggle %s failed: %v", dbName, err)
						}
						cancel()
					}
				}
			}()
		}
	}

	systray.AddSeparator()

	m.allowAllItem = systray.AddMenuItem("Allow My IP on All", "Add your current IP to all managed databases")
	m.allowAllItem.SetTemplateIcon(a.icons.allow.template, a.icons.allow.regular)

	m.revokeAllItem = systray.AddMenuItem("Revoke My IP from All", "Remove your IP from all managed databases")
	m.revokeAllItem.SetTemplateIcon(a.icons.revoke.template, a.icons.revoke.regular)

	m.refreshItem = systray.AddMenuItem("Refresh Now", "Re-detect public IP and reload databases")
	m.refreshItem.SetTemplateIcon(a.icons.refresh.template, a.icons.refresh.regular)

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-m.allowAllItem.ClickedCh:
				ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
				if err := a.mgr.AllowAll(ctx); err != nil {
					a.logger.Printf("allow all failed: %v", err)
				}
				cancel()
			}
		}
	}()

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-m.revokeAllItem.ClickedCh:
				ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
				if err := a.mgr.RemoveAll(ctx); err != nil {
					a.logger.Printf("remove all failed: %v", err)
				}
				cancel()
			}
		}
	}()

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-m.refreshItem.ClickedCh:
				ctx, cancel := context.WithTimeout(parentCtx, 20*time.Second)
				if err := a.mgr.Refresh(ctx); err != nil {
					a.logger.Printf("refresh failed: %v", err)
				}
				cancel()
			}
		}
	}()

	systray.AddSeparator()

	m.durMenuItem = systray.AddMenuItem("", "Automatically revoke access after this duration")
	m.durMenuItem.SetTemplateIcon(a.icons.clock.template, a.icons.clock.regular)
	for _, opt := range durationOptions {
		optCopy := opt
		sub := m.durMenuItem.AddSubMenuItemCheckbox(optCopy.label, "", snap.Settings.DefaultDurationMinutes == optCopy.minutes)
		m.durSubItems[optCopy.minutes] = sub
		go func() {
			for {
				select {
				case <-menuCtx.Done():
					return
				case <-sub.ClickedCh:
					a.mgr.SetDefaultDuration(optCopy.minutes)
				}
			}
		}()
	}

	settingsMenu := systray.AddMenuItem("Settings", "Preferences, rule username, and API token")
	settingsMenu.SetTemplateIcon(a.icons.gear.template, a.icons.gear.regular)

	m.autoUpdateItem = settingsMenu.AddSubMenuItemCheckbox(
		"Auto-update when IP changes",
		"Replace your previously added IP automatically when your public IP changes",
		snap.Settings.AutoUpdateOnIPChange,
	)
	m.autoCleanupItem = settingsMenu.AddSubMenuItemCheckbox(
		"Revoke access on quit",
		"Remove IPs added by this app when quitting",
		snap.Settings.AutoCleanupOnQuit,
	)
	if a.autostart != nil {
		startEnabled, err := a.autostart.IsEnabled()
		if err != nil {
			a.logger.Printf("check start at login failed: %v", err)
		}
		m.startAtLoginItem = settingsMenu.AddSubMenuItemCheckbox(
			"Start at login",
			"Launch Open DigitalOcean Firewall App automatically when you log in",
			startEnabled,
		)
	}
	settingsMenu.AddSeparator()

	m.usernameItem = settingsMenu.AddSubMenuItem("", "Optional username included in DigitalOcean firewall rule descriptions")
	m.usernameItem.SetTemplateIcon(a.icons.user.template, a.icons.user.regular)

	settingsMenu.AddSeparator()
	setTokenItem := settingsMenu.AddSubMenuItem("Change DigitalOcean Token…", "Save a new token to the OS keyring")
	setTokenItem.SetTemplateIcon(a.icons.key.template, a.icons.key.regular)
	clearTokenItem := settingsMenu.AddSubMenuItem("Sign Out (Clear Token)", "Remove the saved token from the OS keyring")

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-m.autoUpdateItem.ClickedCh:
				cur := a.mgr.Snapshot().Settings.AutoUpdateOnIPChange
				a.mgr.SetAutoUpdateOnIPChange(!cur)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-m.autoCleanupItem.ClickedCh:
				cur := a.mgr.Snapshot().Settings.AutoCleanupOnQuit
				a.mgr.SetAutoCleanupOnQuit(!cur)
			}
		}
	}()

	if m.startAtLoginItem != nil {
		item := m.startAtLoginItem
		go func() {
			for {
				select {
				case <-menuCtx.Done():
					return
				case <-item.ClickedCh:
					a.toggleStartAtLogin(item)
				}
			}
		}()
	}

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-m.usernameItem.ClickedCh:
				cur := a.mgr.Snapshot().Settings.Username
				a.configureUsername(cur)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-setTokenItem.ClickedCh:
				a.configureToken(parentCtx)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-clearTokenItem.ClickedCh:
				if err := a.tokenStore.Clear(); err != nil {
					a.logger.Printf("clear token failed: %v", err)
					continue
				}
				a.mgr.SetClient(nil)
			}
		}
	}()

	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Quit Open DigitalOcean Firewall App", "Quit Open DigitalOcean Firewall App")
	quitItem.SetTemplateIcon(a.icons.power.template, a.icons.power.regular)
	go func() {
		for {
			select {
			case <-menuCtx.Done():
				return
			case <-quitItem.ClickedCh:
				go a.RequestQuit()
			}
		}
	}()

	a.menu = m
}

func (a *App) updateMenuInPlace(snap manager.Snapshot) {
	m := &a.menu

	m.statusItem.SetTitle(formatStatus(snap))
	m.statusItem.SetIcon(a.statusDot(snap))
	m.ipItem.SetTitle(formatIPLabel(snap.CurrentIP))

	if m.emptyItem != nil {
		if snap.Busy {
			m.emptyItem.SetTitle("Loading databases…")
		} else {
			m.emptyItem.SetTitle("No managed databases found")
		}
	}

	now := time.Now()
	for _, db := range snap.Databases {
		item, ok := m.dbItems[db.Database.ID]
		if !ok {
			continue
		}
		item.SetTitle(formatDBLabel(db, now))
		if db.Allowed {
			item.SetTooltip(fmt.Sprintf("Click to revoke %s from %s", snap.CurrentIP, db.Database.Name))
			item.Check()
		} else {
			item.SetTooltip(fmt.Sprintf("Click to allow %s on %s", snap.CurrentIP, db.Database.Name))
			item.Uncheck()
		}
		if snap.Busy {
			item.Disable()
		} else {
			item.Enable()
		}
	}

	if !snap.HasToken || len(snap.Databases) == 0 || snap.Busy {
		m.allowAllItem.Disable()
		m.revokeAllItem.Disable()
	} else {
		m.allowAllItem.Enable()
		m.revokeAllItem.Enable()
	}
	if snap.Busy {
		m.refreshItem.Disable()
	} else {
		m.refreshItem.Enable()
	}

	m.durMenuItem.SetTitle(fmt.Sprintf("Auto-Revoke After: %s", formatDurationLabel(snap.Settings.DefaultDurationMinutes)))
	for _, opt := range durationOptions {
		sub, ok := m.durSubItems[opt.minutes]
		if !ok {
			continue
		}
		if snap.Settings.DefaultDurationMinutes == opt.minutes {
			sub.Check()
		} else {
			sub.Uncheck()
		}
	}

	if snap.Settings.AutoUpdateOnIPChange {
		m.autoUpdateItem.Check()
	} else {
		m.autoUpdateItem.Uncheck()
	}
	if snap.Settings.AutoCleanupOnQuit {
		m.autoCleanupItem.Check()
	} else {
		m.autoCleanupItem.Uncheck()
	}

	usernameLabel := "Set Rule Username…"
	if snap.Settings.Username != "" {
		usernameLabel = fmt.Sprintf("Rule Username: %s…", snap.Settings.Username)
	}
	m.usernameItem.SetTitle(usernameLabel)
}

func (a *App) statusDot(snap manager.Snapshot) []byte {
	if snap.Busy || !snap.HasToken {
		return a.icons.dotWarning
	}
	if snap.LastError != "" {
		return a.icons.dotError
	}
	if snap.ActiveCount > 0 {
		return a.icons.dotActive
	}
	return a.icons.dotIdle
}

func (a *App) toggleStartAtLogin(item *systray.MenuItem) {
	if a.autostart == nil {
		return
	}
	enabled, err := a.autostart.IsEnabled()
	if err != nil {
		a.logger.Printf("check start at login failed: %v", err)
		return
	}
	if enabled {
		if err := a.autostart.Disable(); err != nil {
			a.logger.Printf("disable start at login failed: %v", err)
			return
		}
		item.Uncheck()
		return
	}
	if err := a.autostart.Enable(); err != nil {
		a.logger.Printf("enable start at login failed: %v", err)
		return
	}
	item.Check()
}

func (a *App) configureUsername(current string) {
	const title = "Open DigitalOcean Firewall App"
	const msg = "Enter your username for DigitalOcean firewall rule descriptions (optional; leave blank to omit):"
	val, err := config.PromptText(title, msg, current)
	if err != nil {
		if !errors.Is(err, config.ErrPromptCanceled) {
			a.logger.Printf("configure username failed: %v", err)
		}
		return
	}
	a.mgr.SetUsername(val)
}

func (a *App) configureToken(parentCtx context.Context) {
	tok, err := a.tokenStore.PromptAndSave()
	if err != nil {
		if !errors.Is(err, config.ErrPromptCanceled) {
			a.logger.Printf("configure token failed: %v", err)
		}
		return
	}
	a.mgr.SetClient(digitalocean.NewClient(tok))
	ctx, cancel := context.WithTimeout(parentCtx, 20*time.Second)
	defer cancel()
	if err := a.mgr.Refresh(ctx); err != nil {
		a.logger.Printf("refresh after token update failed: %v", err)
	}
}

func formatIPLabel(ip string) string {
	if ip == "" {
		return "Your IP: Detecting…"
	}
	return fmt.Sprintf("Your IP: %s", ip)
}

func formatStatus(snap manager.Snapshot) string {
	if snap.Busy {
		return "Updating…"
	}
	if !snap.HasToken {
		return "API Token Required"
	}
	if snap.LastError != "" {
		msg := snap.LastError
		if len(msg) > 64 {
			msg = msg[:61] + "…"
		}
		return "Error: " + msg
	}
	if len(snap.Databases) == 0 {
		return "Connected · No Databases"
	}
	if snap.ActiveCount == 0 {
		return "All Databases Locked"
	}
	return fmt.Sprintf("Access Open on %d of %s", snap.ActiveCount, pluralizeDB(len(snap.Databases)))
}

func formatDBLabel(db manager.DatabaseStatus, now time.Time) string {
	engine := formatEngine(db.Database.EngineSlug)
	meta := engine
	if db.Database.RegionSlug != "" {
		if meta != "" {
			meta = fmt.Sprintf("%s (%s)", meta, db.Database.RegionSlug)
		} else {
			meta = db.Database.RegionSlug
		}
	}

	label := db.Database.Name
	if meta != "" {
		label = fmt.Sprintf("%s · %s", db.Database.Name, meta)
	}

	if db.Allowed && db.Managed {
		if db.ExpiresAt != nil {
			rem := db.ExpiresAt.Sub(now)
			if rem > 0 {
				mins := int(rem.Minutes()) + 1
				if mins >= 60 {
					label += fmt.Sprintf(" · %dh %dm left", mins/60, mins%60)
				} else {
					label += fmt.Sprintf(" · %dm left", mins)
				}
			}
		} else {
			label += " · until quit"
		}
	} else if db.Allowed {
		label += " · manual"
	}

	return label
}

func formatEngine(slug string) string {
	switch slug {
	case "pg":
		return "PostgreSQL"
	case "valkey":
		return "Valkey"
	case "redis":
		return "Redis"
	case "mysql":
		return "MySQL"
	case "mongodb":
		return "MongoDB"
	case "kafka":
		return "Kafka"
	case "opensearch":
		return "OpenSearch"
	default:
		return slug
	}
}

func formatDurationLabel(minutes int) string {
	for _, opt := range durationOptions {
		if opt.minutes == minutes {
			if minutes == 0 {
				return "Until quit"
			}
			return opt.label
		}
	}
	if minutes <= 0 {
		return "Until quit"
	}
	return fmt.Sprintf("%dm", minutes)
}

func pluralizeDB(n int) string {
	if n == 1 {
		return "1 Database"
	}
	return fmt.Sprintf("%d Databases", n)
}
