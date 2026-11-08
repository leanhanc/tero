package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/leanhanc/tero/internal/appentry"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/podman"
)

// Podman writes a container's --env values, and its secrets, in clear to
// config.json in its storage on disk, where a snapshot or a backup can read
// them. So variables go in a file on tmpfs, mounted read-only into the
// container, where `tero app-entry` reads them and becomes the image's own
// command (OWASP ASVS v5.0.0-13.3.1, NIST SP 800-190 4.1.4).

// SourceLabel records in an image the source the agent built it from. The
// build's own configuration can't set it: the agent writes it into the image's
// configuration at export.
const SourceLabel = "tero.source"

// envFileMode lets the app's process read its file: --userns=auto maps it away
// from tero-agent's uid. The directory, which only tero-agent can enter, keeps
// everyone else out, and the container reaches its file through the mount.
const envFileMode = 0o644

// runtimeDir is tmpfs, created by systemd for tero-agent's user manager.
var runtimeDir = func() string {
	return fmt.Sprintf("/run/user/%d", os.Getuid())
}

func envDir() string {
	return filepath.Join(runtimeDir(), "tero-env")
}

// appContainerPattern is how Name names app containers. Names come from a
// database the dashboard can write, so one that doesn't match never reaches a
// path.
var appContainerPattern = regexp.MustCompile(`^tero-app-[a-z][a-z0-9-]*-[0-9]+$`)

// IsReleaseOf reports whether a live release's container and image, read from
// the shared database, are the ones the agent made for this app and deploy.
func IsReleaseOf(appName string, deployID int64, container, image string) bool {
	return container == Name(appName, deployID) && IsAppImage(appName, image)
}

// errInvalidContainer means a container name isn't one Name makes.
var errInvalidContainer = errors.New("not an app container's name")

func envFilePath(container string) (string, error) {
	if !appContainerPattern.MatchString(container) {
		return "", fmt.Errorf("%w: %q", errInvalidContainer, container)
	}

	return filepath.Join(envDir(), container), nil
}

// WriteEnvFile replaces a container's file of variables.
func WriteEnvFile(container string, variables map[string]string) error {
	path, err := envFilePath(container)
	if err != nil {
		return err
	}
	encoded, err := appentry.Encode(variables)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(envDir(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(envDir(), 0o700); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(envDir(), ".new-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(envFileMode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}

	return os.Rename(temporary.Name(), path)
}

// HasEnvFile reports whether a container's variables are still in memory.
// After a reboot they aren't.
func HasEnvFile(container string) bool {
	path, err := envFilePath(container)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// RemoveEnvFile removes a container's variables.
func RemoveEnvFile(container string) error {
	path, err := envFilePath(container)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return err
}

// RemoveEnvFilesExcept removes the files of containers that aren't kept, left
// by an agent that stopped before removing them.
func RemoveEnvFilesExcept(kept map[string]bool) error {
	entries, err := os.ReadDir(envDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if kept[entry.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(envDir(), entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

// IsAppImage reports whether image is named as the agent names what it builds
// for this app.
func IsAppImage(appName, image string) bool {
	tag, hasPrefix := strings.CutPrefix(image, "localhost/tero/"+appName+":deploy-")
	if !hasPrefix || tag == "" {
		return false
	}

	return strings.Trim(tag, "0123456789") == ""
}

// ImageSource is the source the agent recorded in image when it built it. An
// image with another app's name, or any other name, has no source.
func ImageSource(ctx context.Context, appName, image string) (keys.AppSource, error) {
	if !IsAppImage(appName, image) {
		return keys.AppSource{}, nil
	}

	label, err := podman.Run(ctx, "image", "inspect", "--format", fmt.Sprintf(`{{index .Config.Labels %q}}`, SourceLabel), image)
	if err != nil {
		return keys.AppSource{}, err
	}

	return parseSourceLabel(label), nil
}

// parseSourceLabel reads an image's label. An image without a valid one gets
// an empty source, which is never the approved one.
func parseSourceLabel(label string) keys.AppSource {
	var source keys.AppSource
	if err := json.Unmarshal([]byte(strings.TrimSpace(label)), &source); err != nil {
		return keys.AppSource{}
	}

	return source
}

// passVariables returns what `podman run` needs for variables: the options
// before the image and the command after it. With no variables, a container
// runs as the image says.
func passVariables(ctx context.Context, container, image string, variables map[string]string) ([]string, []string, error) {
	if len(variables) == 0 {
		return nil, nil, nil
	}

	binary, err := entryBinary()
	if err != nil {
		return nil, nil, err
	}
	command, err := imageCommand(ctx, image)
	if err != nil {
		return nil, nil, err
	}
	if err := WriteEnvFile(container, variables); err != nil {
		return nil, nil, err
	}
	envFile, err := envFilePath(container)
	if err != nil {
		return nil, nil, err
	}

	entrypoint, err := json.Marshal([]string{appentry.BinaryPath, appentry.Command})
	if err != nil {
		return nil, nil, err
	}

	return []string{
		"--mount", "type=bind,src=" + envFile + ",dst=" + appentry.EnvPath + ",ro",
		"--mount", "type=bind,src=" + binary + ",dst=" + appentry.BinaryPath + ",ro",
		"--entrypoint", string(entrypoint),
	}, command, nil
}

func entryBinary() (string, error) {
	binary, err := os.Executable()
	if err != nil {
		return "", err
	}

	return filepath.EvalSymlinks(binary)
}

// imageCommand is what the image itself would run: its entrypoint followed by
// its command.
func imageCommand(ctx context.Context, image string) ([]string, error) {
	output, err := podman.Run(ctx, "image", "inspect", "--format", "{{json .Config.Entrypoint}}|{{json .Config.Cmd}}", image)
	if err != nil {
		return nil, err
	}

	return parseImageCommand(output)
}

func parseImageCommand(output string) ([]string, error) {
	entrypointJSON, commandJSON, isSplit := strings.Cut(strings.TrimSpace(output), "|")
	if !isSplit {
		return nil, fmt.Errorf("read the image's command from %q", output)
	}

	var entrypoint, command []string
	if err := json.Unmarshal([]byte(entrypointJSON), &entrypoint); err != nil {
		return nil, fmt.Errorf("read the image's entrypoint: %w", err)
	}
	if err := json.Unmarshal([]byte(commandJSON), &command); err != nil {
		return nil, fmt.Errorf("read the image's command: %w", err)
	}

	return append(entrypoint, command...), nil
}
