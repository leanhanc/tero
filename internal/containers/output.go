package containers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Podman's k8s-file driver keeps a container's output in its storage on
// disk, where a value an app prints, even by accident at startup, would stay
// after the app is gone. Each app container's output goes to a file on
// tmpfs instead, like its variables, and a reboot clears it (OWASP ASVS
// v5.0.0-13.3.1, NIST SP 800-190 4.1.4).

// maxOutputSize caps each container's output file, which takes memory: Podman
// starts the file again when it's full. The failure report reads only its
// last lines.
const maxOutputSize = "1mb"

const outputFileSuffix = ".log"

func outputDir() string {
	return filepath.Join(runtimeDir(), "tero-output")
}

func outputPath(container string) (string, error) {
	if !appContainerPattern.MatchString(container) {
		return "", fmt.Errorf("%w: %q", errInvalidContainer, container)
	}

	return filepath.Join(outputDir(), container+outputFileSuffix), nil
}

// prepareOutputDir creates the directory before Podman opens a container's
// output file in it: a reboot empties tmpfs, and the stopped containers it
// resumes still name their file there.
func prepareOutputDir() error {
	if err := os.MkdirAll(outputDir(), 0o700); err != nil {
		return err
	}

	return os.Chmod(outputDir(), 0o700)
}

// RemoveOutput removes a container's output.
func RemoveOutput(container string) error {
	path, err := outputPath(container)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return err
}

// RemoveOutputsExcept removes the output of containers that aren't kept,
// left by an agent that stopped before removing them.
func RemoveOutputsExcept(kept map[string]bool) error {
	entries, err := os.ReadDir(outputDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		container := strings.TrimSuffix(entry.Name(), outputFileSuffix)
		if kept[container] {
			continue
		}
		if err := os.Remove(filepath.Join(outputDir(), entry.Name())); err != nil {
			return err
		}
	}

	return nil
}
