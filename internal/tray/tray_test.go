package tray

import (
	"bytes"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/konhi/open-digitalocean-firewall-app/internal/digitalocean"
	"github.com/konhi/open-digitalocean-firewall-app/internal/manager"
)

func TestShieldIcon_ValidImage(t *testing.T) {
	for _, filled := range []bool{false, true} {
		data := shieldIcon(filled, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
		if len(data) == 0 {
			t.Fatalf("shieldIcon(filled=%v) returned empty bytes", filled)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("shieldIcon(filled=%v) is not valid PNG: %v", filled, err)
		}
		if img.Bounds().Dx() != 44 || img.Bounds().Dy() != 44 {
			t.Fatalf("unexpected icon dimensions: %v", img.Bounds())
		}
	}
}

func TestFormatDBLabel(t *testing.T) {
	exp := time.Now().Add(95 * time.Minute)
	label := formatDBLabel(manager.DatabaseStatus{
		Database: digitalocean.Database{
			Name:       "valkey-test",
			RegionSlug: "fra1",
			EngineSlug: "valkey",
		},
		Allowed:   true,
		Managed:   true,
		ExpiresAt: &exp,
	})
	if !strings.Contains(label, "valkey-test (fra1 · valkey)") || !strings.Contains(label, "1h 3") {
		t.Fatalf("unexpected label: %q", label)
	}

	untilQuit := formatDBLabel(manager.DatabaseStatus{
		Database: digitalocean.Database{
			Name:       "pg-prod",
			RegionSlug: "nyc1",
			EngineSlug: "pg",
		},
		Allowed: true,
		Managed: true,
	})
	if untilQuit != "pg-prod (nyc1 · pg) [until quit]" {
		t.Fatalf("unexpected until-quit label: %q", untilQuit)
	}
}
