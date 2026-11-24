//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

func New() (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home directory: %w", err)
	}
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}

	resolved := ResolveExecutablePath(execPath, func(p string) bool {
		_, statErr := os.Stat(p)
		return statErr == nil
	})
	plistPath := filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
	return newFileManager(plistPath, resolved, RenderLaunchAgentPlist, osFS{}), nil
}
