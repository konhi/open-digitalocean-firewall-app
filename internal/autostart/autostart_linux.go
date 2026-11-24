//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

func New() (Manager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}

	resolved := ResolveExecutablePath(execPath, func(p string) bool {
		_, statErr := os.Stat(p)
		return statErr == nil
	})
	desktopPath := filepath.Join(configDir, "autostart", BinaryName+".desktop")
	return newFileManager(desktopPath, resolved, RenderDesktopEntry, osFS{}), nil
}
