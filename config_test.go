package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeConfigRejectsNegativeDepth(t *testing.T) {
	if _, err := normalizeConfig(Config{Depth: -1}); err == nil {
		t.Fatal("negative depth accepted")
	}
	cfg, err := normalizeConfig(Config{Depth: 3})
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if cfg.Depth != 3 {
		t.Fatalf("depth = %d, want 3", cfg.Depth)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{"include":[".idea"],"exclude":["dist"],"depth":6,"skip":[".git"],"confirm":false}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !reflect.DeepEqual(cfg.Include, []string{".idea"}) || !reflect.DeepEqual(cfg.Exclude, []string{"dist"}) {
		t.Fatalf("include/exclude = %v / %v", cfg.Include, cfg.Exclude)
	}
	if cfg.Depth != 6 {
		t.Fatalf("depth = %d, want 6", cfg.Depth)
	}
	if cfg.Confirm == nil || *cfg.Confirm {
		t.Fatal("confirm not parsed as false")
	}

	if _, err := loadConfig(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing config file accepted")
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(bad); err == nil {
		t.Fatal("malformed config accepted")
	}
}

func TestResolveConfigPath(t *testing.T) {
	dir := t.TempDir()

	// Explicit path always wins, even if it does not exist (load reports the error).
	path, ok, err := resolveConfigPath(dir, "/some/explicit.json")
	if err != nil || !ok || path != "/some/explicit.json" {
		t.Fatalf("explicit path: %q %v %v", path, ok, err)
	}

	// Project-local config is discovered.
	local := filepath.Join(dir, ".devkill.json")
	if err := os.WriteFile(local, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, ok, err = resolveConfigPath(dir, "")
	if err != nil || !ok || path != local {
		t.Fatalf("local discovery: %q %v %v", path, ok, err)
	}
}

func TestMergeSkipDirs(t *testing.T) {
	base := defaultSkipDirs()
	merged := mergeSkipDirs(base, []string{".cache", "", "custom"})
	for _, want := range []string{".git", ".hg", ".svn", ".cache", "custom"} {
		if _, ok := merged[want]; !ok {
			t.Errorf("merged skip dirs missing %q", want)
		}
	}
	if _, ok := merged[""]; ok {
		t.Error("empty skip entry was merged")
	}
}

func TestBuildTargetMapWithList(t *testing.T) {
	targets := buildTargetMapWithList([]string{".idea", ""}, []string{"dist", "node_modules"})

	if _, ok := targets["node_modules"]; ok {
		t.Error("excluded built-in target still present")
	}
	if _, ok := targets["dist"]; ok {
		t.Error("excluded built-in target still present")
	}
	custom, ok := targets[".idea"]
	if !ok {
		t.Fatal("included custom target missing")
	}
	if custom.Category != "custom" {
		t.Errorf("custom target category = %q, want custom", custom.Category)
	}
	if _, ok := targets[""]; ok {
		t.Error("empty include was added as a target")
	}
}

func TestDefaultTargetsExcludeSourceDirectoryNames(t *testing.T) {
	// Bare framework names are real-world source/project directory names.
	// Listing them as deletable artifacts would let "queue all + delete"
	// destroy user code.
	forbidden := []string{
		"express", "koa", "hapi", "sails.js", "loopback",
		"adonisjs", "nestjs", "feathersjs", "src", "lib", "app",
	}
	names := map[string]struct{}{}
	for _, def := range defaultTargets {
		names[def.Name] = struct{}{}
	}
	for _, name := range forbidden {
		if _, ok := names[name]; ok {
			t.Errorf("default targets contain source directory name %q", name)
		}
	}
}

func TestParseTargetList(t *testing.T) {
	tests := []struct {
		raw  string
		want []string
	}{
		{raw: "", want: nil},
		{raw: "a,b", want: []string{"a", "b"}},
		{raw: " a , ,b ", want: []string{"a", "b"}},
		{raw: ",,", want: []string{}},
	}
	for _, tc := range tests {
		got := parseTargetList(tc.raw)
		if len(got) != len(tc.want) {
			t.Errorf("parseTargetList(%q) = %v, want %v", tc.raw, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseTargetList(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		}
	}
}
