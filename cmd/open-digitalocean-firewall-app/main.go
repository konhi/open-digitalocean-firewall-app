package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/konhi/open-digitalocean-firewall-app/internal/autostart"
	"github.com/konhi/open-digitalocean-firewall-app/internal/config"
	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
	"github.com/konhi/open-digitalocean-firewall-app/internal/ip"
	"github.com/konhi/open-digitalocean-firewall-app/internal/manager"
	"github.com/konhi/open-digitalocean-firewall-app/internal/state"
	"github.com/konhi/open-digitalocean-firewall-app/internal/tray"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("Open DigitalOcean Firewall App %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	logger := log.New(os.Stderr, "Open DigitalOcean Firewall App: ", log.LstdFlags)

	store, err := state.NewStore("")
	if err != nil {
		logger.Fatalf("failed to initialize state store: %v", err)
	}

	tokenStore := config.NewTokenStore()

	var doClient digitalocean.Client
	tok, err := tokenStore.Resolve()
	if err != nil {
		if errors.Is(err, config.ErrPromptCanceled) {
			logger.Printf("no DigitalOcean token configured yet; configure one from the tray menu")
		} else {
			logger.Printf("warning: failed to resolve DigitalOcean token: %v", err)
		}
	} else if tok != "" {
		logger.Printf("loaded DigitalOcean token from OS keyring")
		doClient = digitalocean.NewClient(tok)
	}

	mgr, err := manager.New(doClient, ip.NewDetector(), store, logger)
	if err != nil {
		logger.Fatalf("failed to initialize manager: %v", err)
	}

	autostartMgr, err := autostart.New()
	if err != nil {
		logger.Printf("warning: start at login unavailable: %v", err)
	}

	app := tray.New(mgr, tokenStore, autostartMgr, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("received signal %s; cleaning up and exiting", sig)
		app.RequestQuit()
	}()

	app.Run()
}
