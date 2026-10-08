// Package config holds the settings `tero init` writes under /etc/tero. The
// directory is owned by root and readable by the tero service, which cannot
// change it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/leanhanc/tero/internal/hostfs"
)

const (
	// Dir holds Tero's root-owned configuration.
	Dir = "/etc/tero"

	configPath = Dir + "/config.json"
	markerPath = Dir + "/initialized"
)

// Config is what `tero serve` needs to run.
type Config struct {
	DashboardDomain string `json:"dashboardDomain"`
}

// Marker records that `tero init` finished.
type Marker struct {
	Version     string    `json:"version"`
	Domain      string    `json:"domain"`
	CompletedAt time.Time `json:"completedAt"`
}

// ErrNotInitialized means `tero init` has not completed on this server.
var ErrNotInitialized = errors.New("This server has not been set up yet: run `sudo tero init`")

// Load reads the config written by init.
func Load(files hostfs.FS) (Config, error) {
	var cfg Config
	if err := readJSON(files, configPath, &cfg); err != nil {
		return Config{}, err
	}

	if cfg.DashboardDomain == "" {
		return Config{}, fmt.Errorf("%s has no dashboardDomain", configPath)
	}

	return cfg, nil
}

// Save writes the config, readable by everyone and writable only by root.
func Save(files hostfs.FS, cfg Config) error {
	return writeJSON(files, configPath, cfg)
}

// LoadMarker returns the init marker, or ErrNotInitialized.
func LoadMarker(files hostfs.FS) (Marker, error) {
	var marker Marker
	err := readJSON(files, markerPath, &marker)
	return marker, err
}

// SaveMarker records that init completed.
func SaveMarker(files hostfs.FS, marker Marker) error {
	return writeJSON(files, markerPath, marker)
}

func readJSON(files hostfs.FS, path string, target any) error {
	data, err := files.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotInitialized
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	return nil
}

func writeJSON(files hostfs.FS, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(path, append(data, '\n'), 0o644)
	return err
}
