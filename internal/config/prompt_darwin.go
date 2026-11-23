//go:build darwin

package config

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func promptForToken(title, message string) (string, error) {
	script := fmt.Sprintf(
		`display dialog %q default answer "" with hidden answer with title %q buttons {"Cancel", "Save"} default button "Save"`,
		message,
		title,
	)

	cmd := exec.Command("osascript", "-e", script, "-e", "text returned of result")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "-128") || strings.Contains(stderr.String(), "User canceled") {
			return "", ErrPromptCanceled
		}
		return "", fmt.Errorf("macOS token prompt failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}
