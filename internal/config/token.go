package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	EnvTokenVar    = "DIGITALOCEAN_TOKEN"
	keyringService = "open-digitalocean-firewall-app"
	keyringUser    = "digitalocean-api-token"
)

var ErrPromptCanceled = errors.New("token prompt canceled by user")

type TokenSource string

const (
	SourceEnv     TokenSource = "env"
	SourceKeyring TokenSource = "keyring"
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
	Getenv  func(string) string
	Prompt  PromptFunc
}

func NewTokenStore() *TokenStore {
	return &TokenStore{
		Keyring: osKeyring{},
		Getenv:  os.Getenv,
		Prompt:  promptForToken,
	}
}

// Load reads a token from DIGITALOCEAN_TOKEN or the OS keyring without prompting.
func (s *TokenStore) Load() (string, TokenSource, error) {
	if s.Getenv != nil {
		if tok := strings.TrimSpace(s.Getenv(EnvTokenVar)); tok != "" {
			return tok, SourceEnv, nil
		}
	}

	if s.Keyring != nil {
		tok, err := s.Keyring.Get(keyringService, keyringUser)
		if err == nil {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				return tok, SourceKeyring, nil
			}
		} else if !errors.Is(err, keyring.ErrNotFound) {
			return "", "", fmt.Errorf("read token from OS keyring: %w", err)
		}
	}

	return "", "", nil
}

// Resolve returns a token from env or OS keyring, or prompts via GUI and saves to OS keyring if missing.
func (s *TokenStore) Resolve() (string, TokenSource, error) {
	tok, src, err := s.Load()
	if err != nil {
		return "", "", err
	}
	if tok != "" {
		return tok, src, nil
	}

	tok, err = s.PromptAndSave()
	if err != nil {
		return "", "", err
	}
	return tok, SourceKeyring, nil
}

func (s *TokenStore) PromptAndSave() (string, error) {
	if s.Prompt == nil {
		return "", errors.New("no token prompt configured")
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
		return errors.New("no keyring configured")
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
