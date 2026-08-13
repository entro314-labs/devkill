package main

import (
	"sort"
	"strings"
)

type TargetDef struct {
	Name     string
	Category string
}

// Defaults must be safe for "queue all": every name here represents a
// directory that is expected to be reproducible. Mixed cache/configuration
// directories remain available through explicit includes.
var defaultTargets = []TargetDef{
	{Name: "node_modules", Category: "node"},
	{Name: ".pnpm-store", Category: "node"},
	{Name: "pnpm-store", Category: "node"},
	{Name: "bower_components", Category: "node"},
	{Name: ".turbo", Category: "node"},
	{Name: ".next", Category: "node"},
	{Name: ".nuxt", Category: "node"},
	{Name: ".expo", Category: "node"},
	{Name: ".react-native", Category: "node"},
	{Name: ".angular", Category: "node"},

	{Name: "target", Category: "rust"},

	{Name: ".venv", Category: "python"},
	{Name: "venv", Category: "python"},
	{Name: ".virtualenvs", Category: "python"},
	{Name: "__pycache__", Category: "python"},
	{Name: ".pytest_cache", Category: "python"},
	{Name: ".mypy_cache", Category: "python"},
	{Name: ".ruff_cache", Category: "python"},
	{Name: ".tox", Category: "python"},

	{Name: ".pub-cache", Category: "dart"},
	{Name: ".dart_tool", Category: "dart"},

	{Name: "vendor", Category: "go"},
	{Name: ".cache", Category: "build"},
	{Name: "dist", Category: "build"},
	{Name: "coverage", Category: "build"},
}

func buildTargetMapWithList(includes, excludes []string) map[string]TargetDef {
	targets := map[string]TargetDef{}
	for _, def := range defaultTargets {
		targets[def.Name] = def
	}

	for _, name := range includes {
		if name == "" {
			continue
		}
		targets[name] = TargetDef{Name: name, Category: "custom"}
	}

	for _, name := range excludes {
		delete(targets, name)
	}

	return targets
}

func parseTargetList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func sortedTargetNames(targets map[string]TargetDef) []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
