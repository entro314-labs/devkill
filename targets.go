package main

import (
	"sort"
	"strings"
)

type TargetDef struct {
	Name     string
	Category string
}

var defaultTargets = []TargetDef{
	{Name: "node_modules", Category: "node"},
	{Name: ".pnpm", Category: "node"},
	{Name: ".pnpm-store", Category: "node"},
	{Name: "pnpm-store", Category: "node"},
	{Name: ".yarn", Category: "node"},
	{Name: "bower_components", Category: "node"},
	{Name: ".turbo", Category: "node"},
	{Name: ".next", Category: "node"},
	{Name: ".nuxt", Category: "node"},
	{Name: ".expo", Category: "node"},
	{Name: ".react-native", Category: "node"},
	{Name: ".angular", Category: "node"},
	{Name: ".vue", Category: "node"},
	{Name: ".svelte", Category: "node"},
	{Name: ".ember", Category: "node"},
	{Name: ".meteor", Category: "node"},
	{Name: ".express", Category: "node"},
	{Name: "express", Category: "node"},
	{Name: ".koa", Category: "node"},
	{Name: "koa", Category: "node"},
	{Name: ".hapi", Category: "node"},
	{Name: "hapi", Category: "node"},
	{Name: ".sails.js", Category: "node"},
	{Name: "sails.js", Category: "node"},
	{Name: ".loopback", Category: "node"},
	{Name: "loopback", Category: "node"},
	{Name: ".adonisjs", Category: "node"},
	{Name: "adonisjs", Category: "node"},
	{Name: ".nestjs", Category: "node"},
	{Name: "nestjs", Category: "node"},
	{Name: ".feathersjs", Category: "node"},
	{Name: "feathersjs", Category: "node"},

	{Name: "target", Category: "rust"},
	{Name: ".cargo", Category: "rust"},

	{Name: ".venv", Category: "python"},
	{Name: "venv", Category: "python"},
	{Name: "env", Category: "python"},
	{Name: ".virtualenvs", Category: "python"},
	{Name: "__pycache__", Category: "python"},
	{Name: ".pytest_cache", Category: "python"},
	{Name: ".mypy_cache", Category: "python"},
	{Name: ".ruff_cache", Category: "python"},
	{Name: ".tox", Category: "python"},
	{Name: ".pip", Category: "python"},
	{Name: ".pipenv", Category: "python"},
	{Name: ".poetry", Category: "python"},
	{Name: ".django", Category: "python"},
	{Name: ".flask", Category: "python"},

	{Name: ".gradle", Category: "java"},
	{Name: ".m2", Category: "java"},
	{Name: ".ivy2", Category: "java"},
	{Name: ".nuget", Category: "dotnet"},

	{Name: ".pub-cache", Category: "dart"},
	{Name: ".dart_tool", Category: "dart"},

	{Name: ".gem", Category: "ruby"},
	{Name: ".rails", Category: "ruby"},

	{Name: ".laravel", Category: "php"},
	{Name: ".symfony", Category: "php"},
	{Name: ".yii", Category: "php"},
	{Name: ".codeigniter", Category: "php"},
	{Name: ".cakephp", Category: "php"},
	{Name: ".zend", Category: "php"},
	{Name: ".phalcon", Category: "php"},
	{Name: ".slim", Category: "php"},
	{Name: ".fuelphp", Category: "php"},
	{Name: ".lumen", Category: "php"},
	{Name: ".silex", Category: "php"},

	{Name: "vendor", Category: "go"},
	{Name: ".cache", Category: "build"},
	{Name: "dist", Category: "build"},
	{Name: "build", Category: "build"},
	{Name: "out", Category: "build"},
	{Name: "coverage", Category: "build"},
}

func buildTargetMap(includeRaw, excludeRaw string) map[string]TargetDef {
	return buildTargetMapWithList(parseTargetList(includeRaw), parseTargetList(excludeRaw))
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
