// Package appentry is `tero app-entry`, the first process in the container of
// an app with variables. Podman writes everything it is given about a container
// to disk, so values reach the container in a file on tmpfs instead. This
// process reads them, sets them in its environment and becomes the image's own
// command, which sees them as ordinary environment variables and runs as the
// container's first process.
package appentry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Command is the subcommand of the tero binary that runs Run.
const Command = "app-entry"

// Paths inside the container.
const (
	BinaryPath = "/run/tero/entry"
	EnvPath    = "/run/tero/env"
)

// Encode is the content of the env file. JSON keeps multi-line values byte for
// byte.
func Encode(variables map[string]string) ([]byte, error) {
	return json.Marshal(variables)
}

// Run sets the variables in EnvPath and replaces this process with command. It
// returns only on failure.
func Run(command []string) error {
	if len(command) == 0 {
		return errors.New("the image has no command to run")
	}

	encoded, err := os.ReadFile(EnvPath)
	if err != nil {
		return fmt.Errorf("read the app's variables: %w", err)
	}
	var variables map[string]string
	if err := json.Unmarshal(encoded, &variables); err != nil {
		return fmt.Errorf("read the app's variables: %w", err)
	}

	for name, value := range variables {
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set a variable: %w", err)
		}
	}

	// Looked up after setting the variables, as a shell would with its PATH.
	path, err := exec.LookPath(command[0])
	if err != nil {
		return err
	}

	return syscall.Exec(path, command, os.Environ())
}
