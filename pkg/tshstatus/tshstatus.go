// Package tshstatus provides a way to execute the `tsh status` command
// and get the result as structured data.
package tshstatus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

var (
	// NotLoggedInError is returned when the user is not logged in to Teleport.
	ErrNotLoggedIn = fmt.Errorf("user not logged in")

	ErrEmptyCommandOutput = fmt.Errorf("command 'tsh status --format=json' yielded no output")
)

// Executes 'tsh status --format=json' and returns the output as struct.
// If no active profile is found, it returns nil.
// If user is not logged in, it returns an NotLoggedInError.
func GetStatus(logger *slog.Logger) (*Status, error) {
	cmd := exec.Command("tsh", "status", "--format=json")

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		stderr := strings.TrimSpace(stderrBuf.String())
		if strings.Contains(strings.ToLower(stderr), "not logged in") {
			return nil, ErrNotLoggedIn
		}
		return nil, err
	}

	// No stdout, so we check for an error
	if strings.TrimSpace(stdoutBuf.String()) == "" {
		if strings.Contains(strings.ToLower(stderrBuf.String()), "not logged in") {
			return nil, ErrNotLoggedIn
		}

		logger.Debug("tsh status command yielded error", slog.String("stderr", stderrBuf.String()))
		return nil, ErrEmptyCommandOutput
	}

	// Unmarshal the JSON output into a Status struct
	var status *Status
	if err := json.Unmarshal(stdoutBuf.Bytes(), &status); err != nil {
		return nil, err
	}

	if status.Active == nil || status.Active.ProfileURL == "" {
		return nil, nil
	}

	return status, nil
}
