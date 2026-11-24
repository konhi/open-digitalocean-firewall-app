package config

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

const (
	testKeyringToken = "dop_v1_from_keyring"
	testPromptToken  = "dop_v1_from_prompt"
	testSavedToken   = "dop_v1_saved"
)

var (
	errKeychainLocked     = errors.New("keychain locked")
	errKeyringUnavailable = errors.New("keyring unavailable")
	errPermissionDenied   = errors.New("permission denied")
)

type memKeyring struct {
	values map[string]string
	getErr error
	setErr error
	delErr error
}

func newMemKeyring() *memKeyring {
	return &memKeyring{values: make(map[string]string)}
}

func (m *memKeyring) Get(service, user string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.values[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (m *memKeyring) Set(service, user, password string) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.values[service+"/"+user] = password
	return nil
}

func (m *memKeyring) Delete(service, user string) error {
	if m.delErr != nil {
		return m.delErr
	}
	if _, ok := m.values[service+"/"+user]; !ok {
		return keyring.ErrNotFound
	}
	delete(m.values, service+"/"+user)
	return nil
}

func newTestTokenStore(kr *memKeyring, prompt PromptFunc) *TokenStore {
	return &TokenStore{
		Keyring: kr,
		Prompt:  prompt,
	}
}

func saveToken(t *testing.T, store *TokenStore, token string) {
	t.Helper()
	if err := store.Save(token); err != nil {
		t.Fatalf("Save() setup error: %v", err)
	}
}

func TestTokenStore_Load_ReturnsTrimmedKeyringTokenWhenSaved(t *testing.T) {
	store := newTestTokenStore(newMemKeyring(), nil)
	saveToken(t, store, "  "+testKeyringToken+"  ")

	tok, err := store.Load()

	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if tok != testKeyringToken {
		t.Fatalf("Load() = %q, want %q", tok, testKeyringToken)
	}
}

func TestTokenStore_Load_ReturnsEmptyWithoutErrorWhenNoTokenConfigured(t *testing.T) {
	store := newTestTokenStore(newMemKeyring(), nil)

	tok, err := store.Load()

	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if tok != "" {
		t.Fatalf("Load() = %q, want empty token", tok)
	}
}

func TestTokenStore_Load_SurfacesUnexpectedKeyringError(t *testing.T) {
	kr := newMemKeyring()
	kr.getErr = errKeychainLocked
	store := newTestTokenStore(kr, nil)

	_, err := store.Load()

	if err == nil || !errors.Is(err, errKeychainLocked) {
		t.Fatalf("Load() error = %v, want wrapped %v", err, errKeychainLocked)
	}
}

func TestTokenStore_Resolve_PromptsAndSavesToKeyringOnFirstRun(t *testing.T) {
	store := newTestTokenStore(newMemKeyring(), func(title, msg string) (string, error) {
		return "  " + testPromptToken + "  ", nil
	})

	tok, err := store.Resolve()

	if err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if tok != testPromptToken {
		t.Fatalf("Resolve() = %q, want %q", tok, testPromptToken)
	}
	loadedTok, err := store.Load()
	if err != nil || loadedTok != testPromptToken {
		t.Fatalf("Load() after Resolve() = (%q, %v), want (%q, nil)", loadedTok, err, testPromptToken)
	}
}

func TestTokenStore_Resolve_UsesKeyringWithoutPromptingWhenSaved(t *testing.T) {
	prompted := false
	store := newTestTokenStore(newMemKeyring(), func(title, msg string) (string, error) {
		prompted = true
		return "dop_v1_unexpected", nil
	})
	saveToken(t, store, testSavedToken)

	tok, err := store.Resolve()

	if err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if prompted {
		t.Fatal("Resolve() invoked prompt even though token was saved in keyring")
	}
	if tok != testSavedToken {
		t.Fatalf("Resolve() = %q, want %q", tok, testSavedToken)
	}
}

func TestTokenStore_Resolve_ReturnsErrPromptCanceledWhenUserCancels(t *testing.T) {
	store := newTestTokenStore(newMemKeyring(), func(title, msg string) (string, error) {
		return "", ErrPromptCanceled
	})

	_, err := store.Resolve()

	if !errors.Is(err, ErrPromptCanceled) {
		t.Fatalf("Resolve() error = %v, want ErrPromptCanceled", err)
	}
	loadedTok, loadErr := store.Load()
	if loadErr != nil || loadedTok != "" {
		t.Fatalf("expected keyring to remain empty after cancel, got (%q, %v)", loadedTok, loadErr)
	}
}

func TestTokenStore_PromptAndSave_RejectsBlankInputAsCanceled(t *testing.T) {
	store := newTestTokenStore(newMemKeyring(), func(title, msg string) (string, error) {
		return "   \t\n ", nil
	})

	_, err := store.PromptAndSave()

	if !errors.Is(err, ErrPromptCanceled) {
		t.Fatalf("PromptAndSave() error = %v, want ErrPromptCanceled", err)
	}
}

func TestTokenStore_PromptAndSave_SurfacesKeyringWriteError(t *testing.T) {
	kr := newMemKeyring()
	kr.setErr = errKeyringUnavailable
	store := newTestTokenStore(kr, func(title, msg string) (string, error) {
		return testPromptToken, nil
	})

	_, err := store.PromptAndSave()

	if err == nil || !errors.Is(err, errKeyringUnavailable) {
		t.Fatalf("PromptAndSave() error = %v, want wrapped %v", err, errKeyringUnavailable)
	}
}

func TestTokenStore_Clear_DeletesSavedTokenFromKeyring(t *testing.T) {
	store := newTestTokenStore(newMemKeyring(), nil)
	saveToken(t, store, testSavedToken)

	err := store.Clear()

	if err != nil {
		t.Fatalf("Clear() unexpected error: %v", err)
	}
	loadedTok, loadErr := store.Load()
	if loadErr != nil || loadedTok != "" {
		t.Fatalf("expected token to be cleared, got (%q, %v)", loadedTok, loadErr)
	}
}

func TestTokenStore_Clear_SucceedsWhenKeyringHasNoToken(t *testing.T) {
	store := newTestTokenStore(newMemKeyring(), nil)

	err := store.Clear()

	if err != nil {
		t.Fatalf("Clear() unexpected error when keyring is empty: %v", err)
	}
}

func TestTokenStore_Clear_SurfacesUnexpectedKeyringDeleteError(t *testing.T) {
	kr := newMemKeyring()
	kr.delErr = errPermissionDenied
	store := newTestTokenStore(kr, nil)

	err := store.Clear()

	if err == nil || !errors.Is(err, errPermissionDenied) {
		t.Fatalf("Clear() error = %v, want wrapped %v", err, errPermissionDenied)
	}
}
