package main

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/konhi/open-digitalocean-firewall-app/internal/config"
	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
	"github.com/konhi/open-digitalocean-firewall-app/internal/ip"
	"github.com/konhi/open-digitalocean-firewall-app/internal/manager"
	"github.com/konhi/open-digitalocean-firewall-app/internal/state"
	"github.com/konhi/open-digitalocean-firewall-app/internal/tray"
)

func main() {
	logger := log.New(os.Stderr, "Open DigitalOcean Firewall App: ", log.LstdFlags)

	store, err := state.NewStore("")
	if err != nil {
		logger.Fatalf("failed to initialize state store: %v", err)
	}

	tokenStore := config.NewTokenStore()

	var doClient digitalocean.Client
	tok, src, err := tokenStore.Resolve()
	if err != nil {
		if errors.Is(err, config.ErrPromptCanceled) {
			logger.Printf("no DigitalOcean token configured yet; configure one from the tray menu")
		} else {
			logger.Printf("warning: failed to resolve DigitalOcean token: %v", err)
		}
	} else if tok != "" {
		logger.Printf("loaded DigitalOcean token from %s", src)
		doClient = digitalocean.NewClient(tok)
	}

	mgr, err := manager.New(doClient, ip.NewDetector(), store, logger)
	if err != nil {
		logger.Fatalf("failed to initialize manager: %v", err)
	}

	app := tray.New(mgr, tokenStore, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("received signal %s; cleaning up and exiting", sig)
		app.RequestQuit()
	}()

	app.Run()
}
