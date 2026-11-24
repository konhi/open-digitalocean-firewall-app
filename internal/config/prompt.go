package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ncruces/zenity"
)

func promptForToken(title, message string) (string, error) {
	val, err := zenity.Entry(
		message,
		zenity.Title(title),
		zenity.OKLabel("Save"),
		zenity.HideText(),
	)
	if errors.Is(err, zenity.ErrCanceled) {
		return "", ErrPromptCanceled
	}
	if err != nil {
		return "", fmt.Errorf("token prompt failed: %w", err)
	}
	return strings.TrimSpace(val), nil
}

// PromptText shows a native text input dialog pre-filled with defaultValue.
func PromptText(title, message, defaultValue string) (string, error) {
	val, err := zenity.Entry(
		message,
		zenity.Title(title),
		zenity.OKLabel("Save"),
		zenity.EntryText(defaultValue),
	)
	if errors.Is(err, zenity.ErrCanceled) {
		return "", ErrPromptCanceled
	}
	if err != nil {
		return "", fmt.Errorf("text prompt failed: %w", err)
	}
	return strings.TrimSpace(val), nil
}
