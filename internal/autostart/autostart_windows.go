//go:build windows

package autostart

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const windowsRunKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

type windowsManager struct {
	execPath string
}

func New() (Manager, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	return &windowsManager{execPath: execPath}, nil
}

func (m *windowsManager) IsEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open windows run registry key: %w", err)
	}
	defer key.Close()

	_, _, err = key.GetStringValue(AppName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read windows run registry value: %w", err)
	}
	return true, nil
}

func (m *windowsManager) Enable() error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open windows run registry key: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue(AppName, fmt.Sprintf(`"%s"`, m.execPath)); err != nil {
		return fmt.Errorf("write windows run registry value: %w", err)
	}
	return nil
}

func (m *windowsManager) Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open windows run registry key: %w", err)
	}
	defer key.Close()

	if err := key.DeleteValue(AppName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("delete windows run registry value: %w", err)
	}
	return nil
}
