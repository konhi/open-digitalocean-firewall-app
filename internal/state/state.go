package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	appDirName    = "open-digitalocean-firewall-app"
	stateFileName = "state.json"
)

// ManagedRule records the exact IP this app added so other users' rules are never touched.
type ManagedRule struct {
	IP        string     `json:"ip"`
	AddedAt   time.Time  `json:"added_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (r ManagedRule) IsExpired(now time.Time) bool {
	return r.ExpiresAt != nil && !now.Before(*r.ExpiresAt)
}

type Settings struct {
	AutoUpdateOnIPChange   bool   `json:"auto_update_on_ip_change"`
	AutoCleanupOnQuit      bool   `json:"auto_cleanup_on_quit"`
	DefaultDurationMinutes int    `json:"default_duration_minutes"` // 0 means until quit (no timer)
	Username               string `json:"username,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		AutoUpdateOnIPChange:   true,
		AutoCleanupOnQuit:      true,
		DefaultDurationMinutes: 60,
	}
}

type Data struct {
	ManagedRules map[string]ManagedRule `json:"managed_rules"`
	Settings     Settings               `json:"settings"`
}

func DefaultData() Data {
	return Data{
		ManagedRules: make(map[string]ManagedRule),
		Settings:     DefaultSettings(),
	}
}

type Store struct {
	path string
}

// NewStore uses DefaultPath when path is empty.
func NewStore(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	return &Store{path: path}, nil
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, appDirName, stateFileName), nil
}

func (s *Store) Path() string {
	return s.path
}

// Load reads state from disk, returning DefaultData if the file does not exist yet.
func (s *Store) Load() (Data, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultData(), nil
		}
		return Data{}, fmt.Errorf("read state file: %w", err)
	}

	data := DefaultData()
	if err := json.Unmarshal(raw, &data); err != nil {
		return Data{}, fmt.Errorf("parse state file: %w", err)
	}
	if data.ManagedRules == nil {
		data.ManagedRules = make(map[string]ManagedRule)
	}
	return data, nil
}

// Save writes state atomically to disk.
func (s *Store) Save(data Data) error {
	if data.ManagedRules == nil {
		data.ManagedRules = make(map[string]ManagedRule)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	encoded = append(encoded, '\n')

	tmp, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp state file: %w", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}
