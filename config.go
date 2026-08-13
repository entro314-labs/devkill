package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
	Depth   int      `json:"depth"`
	Skip    []string `json:"skip"`
	Confirm *bool    `json:"confirm"`
}

func resolveConfigPath(root, explicit string) (string, bool, error) {
	if explicit != "" {
		return explicit, true, nil
	}
	for _, candidate := range defaultConfigPaths(root) {
		if fileExists(candidate) {
			return candidate, true, nil
		}
	}
	return "", false, nil
}

func loadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func defaultConfigPaths(root string) []string {
	paths := []string{}
	if root != "" {
		paths = append(paths, filepath.Join(root, ".devkill.json"))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "devkill", "config.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "devkill", "config.json"))
	}
	return paths
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func mergeSkipDirs(base map[string]struct{}, extra []string) map[string]struct{} {
	if len(extra) == 0 {
		return base
	}
	if base == nil {
		base = map[string]struct{}{}
	}
	for _, item := range extra {
		if item == "" {
			continue
		}
		base[item] = struct{}{}
	}
	return base
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Depth < 0 {
		return Config{}, errors.New("config: depth must be >= 0")
	}
	var err error
	if cfg.Include, err = normalizeDirectoryNames("config include", cfg.Include); err != nil {
		return Config{}, err
	}
	if cfg.Exclude, err = normalizeDirectoryNames("config exclude", cfg.Exclude); err != nil {
		return Config{}, err
	}
	if cfg.Skip, err = normalizeDirectoryNames("config skip", cfg.Skip); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeDirectoryNames(field string, names []string) ([]string, error) {
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.ContainsRune(name, '\x00') {
			return nil, fmt.Errorf("%s: %q must be a directory name, not a path", field, raw)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	return normalized, nil
}
