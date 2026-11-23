package tray

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"log"
	"sync"
	"time"

	"fyne.io/systray"

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

type App struct {
	mgr        *manager.Manager
	tokenStore *config.TokenStore
	logger     *log.Logger

	idleTemplate   []byte
	activeTemplate []byte
	idleRegular    []byte
	activeRegular  []byte

	mu          sync.Mutex
	renderCh    chan manager.Snapshot
	renderStop  context.CancelFunc
	bgCancel    context.CancelFunc
	quitOnce    sync.Once
	cleanupDone chan struct{}
}

func New(mgr *manager.Manager, tokenStore *config.TokenStore, logger *log.Logger) *App {
	if logger == nil {
		logger = log.Default()
	}
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	doBlue := color.NRGBA{R: 0, G: 128, B: 255, A: 255}
	slate := color.NRGBA{R: 130, G: 145, B: 165, A: 255}

	return &App{
		mgr:            mgr,
		tokenStore:     tokenStore,
		logger:         logger,
		idleTemplate:   shieldIcon(false, black),
		activeTemplate: shieldIcon(true, black),
		idleRegular:    shieldIcon(false, slate),
		activeRegular:  shieldIcon(true, doBlue),
		renderCh:       make(chan manager.Snapshot, 1),
		cleanupDone:    make(chan struct{}),
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
	systray.SetTemplateIcon(a.idleTemplate, a.idleRegular)

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
	a.mu.Lock()
	if a.renderStop != nil {
		a.renderStop()
	}
	menuCtx, cancel := context.WithCancel(parentCtx)
	a.renderStop = cancel
	a.mu.Unlock()

	if snap.ActiveCount > 0 {
		systray.SetTemplateIcon(a.activeTemplate, a.activeRegular)
		systray.SetTooltip(fmt.Sprintf("Open DigitalOcean Firewall App — Active on %d database(s)", snap.ActiveCount))
	} else {
		systray.SetTemplateIcon(a.idleTemplate, a.idleRegular)
		systray.SetTooltip("Open DigitalOcean Firewall App")
	}

	systray.ResetMenu()

	ipLabel := "IP: Detecting…"
	if snap.CurrentIP != "" {
		ipLabel = fmt.Sprintf("IP: %s", snap.CurrentIP)
	}
	ipItem := systray.AddMenuItem(ipLabel, "Current public IPv4 address")
	ipItem.Disable()

	statusItem := systray.AddMenuItem(formatStatus(snap), "Current status")
	statusItem.Disable()

	systray.AddSeparator()

	if !snap.HasToken {
		cfgItem := systray.AddMenuItem("Configure DigitalOcean Token…", "Enter your DigitalOcean personal access token")
		go func() {
			select {
			case <-menuCtx.Done():
				return
			case <-cfgItem.ClickedCh:
				a.configureToken(parentCtx)
			}
		}()
	} else if len(snap.Databases) == 0 {
		emptyText := "No managed databases found"
		if snap.Busy {
			emptyText = "Loading databases…"
		}
		emptyItem := systray.AddMenuItem(emptyText, "")
		emptyItem.Disable()
	} else {
		for _, db := range snap.Databases {
			dbCopy := db
			label := formatDBLabel(dbCopy)
			tooltip := fmt.Sprintf("Click to toggle %s on %s", snap.CurrentIP, dbCopy.Database.Name)
			item := systray.AddMenuItemCheckbox(label, tooltip, dbCopy.Allowed)
			if snap.Busy {
				item.Disable()
			}
			go func() {
				select {
				case <-menuCtx.Done():
					return
				case <-item.ClickedCh:
					ctx, cancel := context.WithTimeout(parentCtx, 20*time.Second)
					defer cancel()
					if err := a.mgr.ToggleDatabase(ctx, dbCopy.Database.ID); err != nil {
						a.logger.Printf("toggle %s failed: %v", dbCopy.Database.Name, err)
					}
				}
			}()
		}
	}

	systray.AddSeparator()

	allowAll := systray.AddMenuItem("Allow My IP on All", "Add your current IP to all managed databases")
	removeAll := systray.AddMenuItem("Remove My IP from All", "Remove your IP from all managed databases")
	refresh := systray.AddMenuItem("Refresh", "Re-detect public IP and reload databases")

	if !snap.HasToken || len(snap.Databases) == 0 || snap.Busy {
		allowAll.Disable()
		removeAll.Disable()
	}
	if snap.Busy {
		refresh.Disable()
	}

	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-allowAll.ClickedCh:
			ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
			defer cancel()
			if err := a.mgr.AllowAll(ctx); err != nil {
				a.logger.Printf("allow all failed: %v", err)
			}
		}
	}()

	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-removeAll.ClickedCh:
			ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
			defer cancel()
			if err := a.mgr.RemoveAll(ctx); err != nil {
				a.logger.Printf("remove all failed: %v", err)
			}
		}
	}()

	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-refresh.ClickedCh:
			ctx, cancel := context.WithTimeout(parentCtx, 20*time.Second)
			defer cancel()
			if err := a.mgr.Refresh(ctx); err != nil {
				a.logger.Printf("refresh failed: %v", err)
			}
		}
	}()

	systray.AddSeparator()

	durMenu := systray.AddMenuItem("Access Duration", "Automatically revoke access after this duration")
	for _, opt := range durationOptions {
		optCopy := opt
		checked := snap.Settings.DefaultDurationMinutes == optCopy.minutes
		sub := durMenu.AddSubMenuItemCheckbox(optCopy.label, "", checked)
		go func() {
			select {
			case <-menuCtx.Done():
				return
			case <-sub.ClickedCh:
				a.mgr.SetDefaultDuration(optCopy.minutes)
			}
		}()
	}

	settingsMenu := systray.AddMenuItem("Settings", "Preferences and API token")
	autoUpdateItem := settingsMenu.AddSubMenuItemCheckbox(
		"Auto-update when IP changes",
		"Replace your previously added IP automatically when your public IP changes",
		snap.Settings.AutoUpdateOnIPChange,
	)
	autoCleanupItem := settingsMenu.AddSubMenuItemCheckbox(
		"Auto-cleanup on quit",
		"Remove IPs added by this app when quitting",
		snap.Settings.AutoCleanupOnQuit,
	)
	settingsMenu.AddSeparator()
	setTokenItem := settingsMenu.AddSubMenuItem("Set DigitalOcean Token…", "Save a new token to the OS keyring")
	clearTokenItem := settingsMenu.AddSubMenuItem("Clear Saved Token", "Remove the saved token from the OS keyring")

	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-autoUpdateItem.ClickedCh:
			a.mgr.SetAutoUpdateOnIPChange(!snap.Settings.AutoUpdateOnIPChange)
		}
	}()

	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-autoCleanupItem.ClickedCh:
			a.mgr.SetAutoCleanupOnQuit(!snap.Settings.AutoCleanupOnQuit)
		}
	}()

	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-setTokenItem.ClickedCh:
			a.configureToken(parentCtx)
		}
	}()

	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-clearTokenItem.ClickedCh:
			if err := a.tokenStore.Clear(); err != nil {
				a.logger.Printf("clear token failed: %v", err)
				return
			}
			// Re-check if DIGITALOCEAN_TOKEN env var is still set.
			if tok, _, err := a.tokenStore.Load(); err == nil && tok != "" {
				a.mgr.SetClient(digitalocean.NewClient(tok))
			} else {
				a.mgr.SetClient(nil)
			}
		}
	}()

	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Quit", "Quit Open DigitalOcean Firewall App")
	go func() {
		select {
		case <-menuCtx.Done():
			return
		case <-quitItem.ClickedCh:
			go a.RequestQuit()
		}
	}()
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

func formatStatus(snap manager.Snapshot) string {
	if snap.Busy {
		return "Status: Updating…"
	}
	if !snap.HasToken {
		return "Status: DigitalOcean token required"
	}
	if snap.LastError != "" {
		msg := snap.LastError
		if len(msg) > 64 {
			msg = msg[:61] + "…"
		}
		return "Error: " + msg
	}
	return fmt.Sprintf("Status: Active on %d of %d DBs", snap.ActiveCount, len(snap.Databases))
}

func formatDBLabel(db manager.DatabaseStatus) string {
	meta := db.Database.RegionSlug
	if db.Database.EngineSlug != "" {
		if meta != "" {
			meta += " · " + db.Database.EngineSlug
		} else {
			meta = db.Database.EngineSlug
		}
	}

	label := db.Database.Name
	if meta != "" {
		label = fmt.Sprintf("%s (%s)", db.Database.Name, meta)
	}

	if db.Allowed && db.Managed {
		if db.ExpiresAt != nil {
			rem := time.Until(*db.ExpiresAt)
			if rem > 0 {
				mins := int(rem.Minutes()) + 1
				if mins >= 60 {
					label += fmt.Sprintf(" [%dh %dm left]", mins/60, mins%60)
				} else {
					label += fmt.Sprintf(" [%dm left]", mins)
				}
			}
		} else {
			label += " [until quit]"
		}
	}
	return label
}
