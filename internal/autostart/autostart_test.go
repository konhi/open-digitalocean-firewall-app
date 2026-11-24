package autostart

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const (
	testPlistPath  = "/Users/alice/Library/LaunchAgents/com.konhi.open-digitalocean-firewall-app.plist"
	testBinaryPath = "/opt/homebrew/opt/open-digitalocean-firewall-app/bin/open-digitalocean-firewall-app"
)

var errFakeIO = errors.New("simulated filesystem failure")

type memFS struct {
	files      map[string][]byte
	mkdirErr   error
	writeErr   error
	removeErr  error
	existsErr  error
	createdDir string
}

func newMemFS() *memFS {
	return &memFS{files: make(map[string][]byte)}
}

func (f *memFS) Exists(path string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	_, ok := f.files[path]
	return ok, nil
}

func (f *memFS) MkdirAll(path string, _ os.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	f.createdDir = path
	return nil
}

func (f *memFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	f.files[path] = cp
	return nil
}

func (f *memFS) Remove(path string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	if _, ok := f.files[path]; !ok {
		return os.ErrNotExist
	}
	delete(f.files, path)
	return nil
}

func TestResolveExecutablePath_RewritesHomebrewCellarToOptSymlinkWhenPresent(t *testing.T) {
	cellarPath := "/opt/homebrew/Cellar/open-digitalocean-firewall-app/0.1.0/bin/open-digitalocean-firewall-app"

	got := ResolveExecutablePath(cellarPath, func(p string) bool {
		return p == testBinaryPath
	})

	if got != testBinaryPath {
		t.Fatalf("ResolveExecutablePath() = %q, want %q", got, testBinaryPath)
	}
}

func TestResolveExecutablePath_KeepsCellarPathWhenOptSymlinkIsAbsent(t *testing.T) {
	cellarPath := "/opt/homebrew/Cellar/open-digitalocean-firewall-app/0.1.0/bin/open-digitalocean-firewall-app"

	got := ResolveExecutablePath(cellarPath, func(string) bool { return false })

	if got != cellarPath {
		t.Fatalf("ResolveExecutablePath() = %q, want %q", got, cellarPath)
	}
}

func TestResolveExecutablePath_LeavesNonHomebrewPathUnchanged(t *testing.T) {
	customPath := "/usr/local/bin/open-digitalocean-firewall-app"

	got := ResolveExecutablePath(customPath, func(string) bool { return true })

	if got != customPath {
		t.Fatalf("ResolveExecutablePath() = %q, want %q", got, customPath)
	}
}

func TestRenderLaunchAgentPlist_ContainsLabelInteractiveTypeAndEscapedPath(t *testing.T) {
	raw, err := RenderLaunchAgentPlist("/Applications/A & B/open-digitalocean-firewall-app")
	if err != nil {
		t.Fatalf("RenderLaunchAgentPlist() error = %v", err)
	}
	content := string(raw)

	if !strings.Contains(content, "<string>com.konhi.open-digitalocean-firewall-app</string>") {
		t.Fatalf("plist missing expected label:\n%s", content)
	}
	if !strings.Contains(content, "<string>/Applications/A &amp; B/open-digitalocean-firewall-app</string>") {
		t.Fatalf("plist missing XML-escaped executable path:\n%s", content)
	}
	if !strings.Contains(content, "<key>RunAtLoad</key>\n\t<true/>") {
		t.Fatalf("plist missing RunAtLoad=true:\n%s", content)
	}
	if !strings.Contains(content, "<key>KeepAlive</key>\n\t<false/>") {
		t.Fatalf("plist missing KeepAlive=false:\n%s", content)
	}
	if !strings.Contains(content, "<key>ProcessType</key>\n\t<string>Interactive</string>") {
		t.Fatalf("plist missing ProcessType=Interactive:\n%s", content)
	}
}

func TestRenderLaunchAgentPlist_RejectsEmptyExecutablePath(t *testing.T) {
	_, err := RenderLaunchAgentPlist("   ")

	if !errors.Is(err, ErrEmptyExecutablePath) {
		t.Fatalf("RenderLaunchAgentPlist() error = %v, want %v", err, ErrEmptyExecutablePath)
	}
}

func TestRenderDesktopEntry_ContainsAppNameAndExecPath(t *testing.T) {
	raw, err := RenderDesktopEntry("/usr/bin/open-digitalocean-firewall-app")
	if err != nil {
		t.Fatalf("RenderDesktopEntry() error = %v", err)
	}
	content := string(raw)

	if !strings.Contains(content, "Name=Open DigitalOcean Firewall App\n") {
		t.Fatalf("desktop entry missing app name:\n%s", content)
	}
	if !strings.Contains(content, "Exec=/usr/bin/open-digitalocean-firewall-app\n") {
		t.Fatalf("desktop entry missing Exec line:\n%s", content)
	}
}

func TestFileManager_EnableCreatesEntryAndReportsEnabled(t *testing.T) {
	fs := newMemFS()
	mgr := newFileManager(testPlistPath, testBinaryPath, RenderLaunchAgentPlist, fs)

	if err := mgr.Enable(); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	enabled, err := mgr.IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled() error = %v", err)
	}
	if !enabled {
		t.Fatal("IsEnabled() = false after Enable(), want true")
	}
	if fs.createdDir != "/Users/alice/Library/LaunchAgents" {
		t.Fatalf("created directory = %q, want %q", fs.createdDir, "/Users/alice/Library/LaunchAgents")
	}
}

func TestFileManager_DisableRemovesEntryAndIsIdempotent(t *testing.T) {
	fs := newMemFS()
	mgr := newFileManager(testPlistPath, testBinaryPath, RenderLaunchAgentPlist, fs)
	if err := mgr.Enable(); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	if err := mgr.Disable(); err != nil {
		t.Fatalf("first Disable() error = %v", err)
	}
	if err := mgr.Disable(); err != nil {
		t.Fatalf("second Disable() error = %v, want nil when already absent", err)
	}

	enabled, err := mgr.IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled() error = %v", err)
	}
	if enabled {
		t.Fatal("IsEnabled() = true after Disable(), want false")
	}
}

func TestFileManager_EnablePropagatesWriteError(t *testing.T) {
	fs := newMemFS()
	fs.writeErr = errFakeIO
	mgr := newFileManager(testPlistPath, testBinaryPath, RenderLaunchAgentPlist, fs)

	err := mgr.Enable()

	if !errors.Is(err, errFakeIO) {
		t.Fatalf("Enable() error = %v, want wrapped %v", err, errFakeIO)
	}
}

func TestFileManager_DisablePropagatesNonNotExistError(t *testing.T) {
	fs := newMemFS()
	fs.removeErr = errFakeIO
	mgr := newFileManager(testPlistPath, testBinaryPath, RenderLaunchAgentPlist, fs)

	err := mgr.Disable()

	if !errors.Is(err, errFakeIO) {
		t.Fatalf("Disable() error = %v, want wrapped %v", err, errFakeIO)
	}
}
