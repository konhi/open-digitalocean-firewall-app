package config

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

type memKeyring struct {
	values map[string]string
}

func newMemKeyring() *memKeyring {
	return &memKeyring{values: make(map[string]string)}
}

func (m *memKeyring) Get(service, user string) (string, error) {
	v, ok := m.values[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (m *memKeyring) Set(service, user, password string) error {
	m.values[service+"/"+user] = password
	return nil
}

func (m *memKeyring) Delete(service, user string) error {
	delete(m.values, service+"/"+user)
	return nil
}

func TestTokenStore_PrecedenceAndPrompt(t *testing.T) {
	kr := newMemKeyring()
	env := map[string]string{}
	prompted := false

	store := &TokenStore{
		Keyring: kr,
		Getenv:  func(k string) string { return env[k] },
		Prompt: func(title, msg string) (string, error) {
			prompted = true
			return "dop_v1_from_prompt", nil
		},
	}

	tok, src, err := store.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if !prompted || tok != "dop_v1_from_prompt" || src != SourceKeyring {
		t.Fatalf("unexpected Resolve() result: tok=%q src=%q prompted=%v", tok, src, prompted)
	}

	prompted = false
	tok, src, err = store.Resolve()
	if err != nil || prompted || tok != "dop_v1_from_prompt" || src != SourceKeyring {
		t.Fatalf("expected keyring hit without prompt, got tok=%q src=%q prompted=%v err=%v", tok, src, prompted, err)
	}

	env[EnvTokenVar] = "dop_v1_from_env"
	tok, src, err = store.Resolve()
	if err != nil || tok != "dop_v1_from_env" || src != SourceEnv {
		t.Fatalf("expected env precedence, got tok=%q src=%q err=%v", tok, src, err)
	}

	delete(env, EnvTokenVar)
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear() error: %v", err)
	}
	store.Prompt = func(title, msg string) (string, error) {
		return "", ErrPromptCanceled
	}
	if _, _, err := store.Resolve(); !errors.Is(err, ErrPromptCanceled) {
		t.Fatalf("expected ErrPromptCanceled after Clear, got %v", err)
	}
}
