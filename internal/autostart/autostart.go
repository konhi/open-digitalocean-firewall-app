package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	AppName          = "Open DigitalOcean Firewall App"
	BinaryName       = "open-digitalocean-firewall-app"
	LaunchAgentLabel = "com.konhi.open-digitalocean-firewall-app"
)

var (
	ErrUnsupported         = errors.New("autostart is not supported on this platform")
	ErrEmptyExecutablePath = errors.New("executable path cannot be empty")
)

type Manager interface {
	IsEnabled() (bool, error)
	Enable() error
	Disable() error
}

type fileSystem interface {
	Exists(path string) (bool, error)
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	Remove(path string) error
}

type osFS struct{}

func (osFS) Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (osFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (osFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (osFS) Remove(path string) error {
	return os.Remove(path)
}

type fileManager struct {
	filePath string
	execPath string
	render   func(string) ([]byte, error)
	fs       fileSystem
}

func newFileManager(filePath, execPath string, render func(string) ([]byte, error), fs fileSystem) *fileManager {
	if fs == nil {
		fs = osFS{}
	}
	return &fileManager{
		filePath: filePath,
		execPath: execPath,
		render:   render,
		fs:       fs,
	}
}

func (m *fileManager) IsEnabled() (bool, error) {
	return m.fs.Exists(m.filePath)
}

func (m *fileManager) Enable() error {
	data, err := m.render(m.execPath)
	if err != nil {
		return err
	}
	if err := m.fs.MkdirAll(filepath.Dir(m.filePath), 0o755); err != nil {
		return fmt.Errorf("create autostart directory: %w", err)
	}
	if err := m.fs.WriteFile(m.filePath, data, 0o644); err != nil {
		return fmt.Errorf("write autostart entry: %w", err)
	}
	return nil
}

func (m *fileManager) Disable() error {
	err := m.fs.Remove(m.filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove autostart entry: %w", err)
	}
	return nil
}

// ResolveExecutablePath rewrites a versioned Homebrew Cellar binary path to its
// stable opt symlink when present so login startup survives brew upgrade.
func ResolveExecutablePath(execPath string, fileExists func(string) bool) string {
	clean := filepath.Clean(execPath)
	cellarMarker := string(filepath.Separator) + "Cellar" + string(filepath.Separator) + BinaryName + string(filepath.Separator)
	idx := strings.Index(clean, cellarMarker)
	if idx < 0 {
		return clean
	}

	prefix := clean[:idx]
	stable := filepath.Join(prefix, "opt", BinaryName, "bin", BinaryName)
	if fileExists != nil && fileExists(stable) {
		return stable
	}
	return clean
}

func RenderLaunchAgentPlist(execPath string) ([]byte, error) {
	if strings.TrimSpace(execPath) == "" {
		return nil, ErrEmptyExecutablePath
	}

	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(execPath)); err != nil {
		return nil, fmt.Errorf("escape executable path: %w", err)
	}

	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, LaunchAgentLabel, escaped.String())

	return []byte(content), nil
}

func RenderDesktopEntry(execPath string) ([]byte, error) {
	if strings.TrimSpace(execPath) == "" {
		return nil, ErrEmptyExecutablePath
	}

	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=Manage your IP in DigitalOcean managed database firewalls
Exec=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, AppName, execPath)

	return []byte(content), nil
}
