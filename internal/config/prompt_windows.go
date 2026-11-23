//go:build windows

package config

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func promptForToken(title, message string) (string, error) {
	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form
$form.Text = %q
$form.Width = 460
$form.Height = 170
$form.StartPosition = 'CenterScreen'
$form.TopMost = $true

$label = New-Object System.Windows.Forms.Label
$label.Left = 14; $label.Top = 14; $label.Width = 420; $label.Height = 36
$label.Text = %q
$form.Controls.Add($label)

$box = New-Object System.Windows.Forms.TextBox
$box.Left = 14; $box.Top = 56; $box.Width = 416
$box.UseSystemPasswordChar = $true
$form.Controls.Add($box)

$ok = New-Object System.Windows.Forms.Button
$ok.Text = 'Save'; $ok.Left = 266; $ok.Top = 92; $ok.DialogResult = [System.Windows.Forms.DialogResult]::OK
$form.AcceptButton = $ok
$form.Controls.Add($ok)

$cancel = New-Object System.Windows.Forms.Button
$cancel.Text = 'Cancel'; $cancel.Left = 350; $cancel.Top = 92; $cancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$form.CancelButton = $cancel
$form.Controls.Add($cancel)

if ($form.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
    Write-Output $box.Text
} else {
    exit 2
}
`, title, message)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 2 {
			return "", ErrPromptCanceled
		}
		return "", fmt.Errorf("windows token prompt failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
