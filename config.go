package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if err := json.Unmarshal(content, &cfg); err != nil {
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
	return cfg, nil
}
