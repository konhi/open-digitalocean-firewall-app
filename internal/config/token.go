package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "open-digitalocean-firewall-app"
	keyringUser    = "digitalocean-api-token"
)

var (
	ErrPromptCanceled = errors.New("token prompt canceled by user")
	ErrNoPrompt       = errors.New("no token prompt configured")
	ErrNoKeyring      = errors.New("no keyring configured")
)

// Keyring abstracts OS credential storage for testing.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osKeyring struct{}

func (osKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (osKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (osKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

type PromptFunc func(title, message string) (string, error)

type TokenStore struct {
	Keyring Keyring
	Prompt  PromptFunc
}

func NewTokenStore() *TokenStore {
	return &TokenStore{
		Keyring: osKeyring{},
		Prompt:  promptForToken,
	}
}

// Load reads a saved token from the OS keyring without prompting.
func (s *TokenStore) Load() (string, error) {
	if s.Keyring != nil {
		tok, err := s.Keyring.Get(keyringService, keyringUser)
		if err == nil {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				return tok, nil
			}
		} else if !errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("read token from OS keyring: %w", err)
		}
	}

	return "", nil
}

// Resolve returns a token from the OS keyring, or prompts via GUI and saves to the OS keyring if missing.
func (s *TokenStore) Resolve() (string, error) {
	tok, err := s.Load()
	if err != nil {
		return "", err
	}
	if tok != "" {
		return tok, nil
	}

	return s.PromptAndSave()
}

func (s *TokenStore) PromptAndSave() (string, error) {
	if s.Prompt == nil {
		return "", ErrNoPrompt
	}

	const title = "Open DigitalOcean Firewall App"
	const msg = "Enter your DigitalOcean API token (requires database:read and database:update scopes):"

	tok, err := s.Prompt(title, msg)
	if err != nil {
		return "", err
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", ErrPromptCanceled
	}

	if err := s.Save(tok); err != nil {
		return "", err
	}
	return tok, nil
}

func (s *TokenStore) Save(token string) error {
	if s.Keyring == nil {
		return ErrNoKeyring
	}
	if err := s.Keyring.Set(keyringService, keyringUser, strings.TrimSpace(token)); err != nil {
		return fmt.Errorf("save token to OS keyring: %w", err)
	}
	return nil
}

func (s *TokenStore) Clear() error {
	if s.Keyring == nil {
		return nil
	}
	err := s.Keyring.Delete(keyringService, keyringUser)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete token from OS keyring: %w", err)
	}
	return nil
}
