//go:build linux

package config

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func promptForToken(title, message string) (string, error) {
	if path, err := exec.LookPath("zenity"); err == nil {
		return runLinuxPrompt(path, "--entry", "--hide-text", "--title="+title, "--text="+message)
	}
	if path, err := exec.LookPath("kdialog"); err == nil {
		return runLinuxPrompt(path, "--title", title, "--password", message)
	}
	return "", errors.New("no GUI prompt tool found (install zenity or kdialog, or set DIGITALOCEAN_TOKEN)")
}

func runLinuxPrompt(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", ErrPromptCanceled
		}
		return "", fmt.Errorf("token prompt failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
