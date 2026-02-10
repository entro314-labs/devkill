package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

type stringFlag struct {
	value string
	set   bool
}

func (s *stringFlag) String() string { return s.value }
func (s *stringFlag) Set(val string) error {
	s.value = val
	s.set = true
	return nil
}

type intFlag struct {
	value int
	set   bool
}

func (i *intFlag) String() string { return fmt.Sprintf("%d", i.value) }
func (i *intFlag) Set(val string) error {
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return err
	}
	i.value = parsed
	i.set = true
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var includeTargets stringFlag
	var excludeTargets stringFlag
	var maxDepth intFlag
	var configPath stringFlag
	var noConfirm bool
	var listTargets bool

	flag.Var(&includeTargets, "include", "Comma-separated additional target directory names to scan")
	flag.Var(&excludeTargets, "exclude", "Comma-separated target directory names to skip")
	flag.Var(&maxDepth, "depth", "Maximum directory depth to scan (0 = unlimited)")
	flag.Var(&configPath, "config", "Path to a JSON config file")
	flag.BoolVar(&noConfirm, "no-confirm", false, "Delete without confirmation prompts")
	flag.BoolVar(&listTargets, "list-targets", false, "Print target directories and exit")
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error resolving path:", err)
		os.Exit(1)
	}

	rootHandle, err := os.OpenRoot(absRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error opening root:", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := rootHandle.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "Error closing root:", closeErr)
		}
	}()

	config := Config{}
	if path, ok, err := resolveConfigPath(absRoot, configPath.value); err != nil {
		fmt.Fprintln(os.Stderr, "Error resolving config:", err)
		os.Exit(1)
	} else if ok {
		cfg, err := loadConfig(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error loading config:", err)
			os.Exit(1)
		}
		normalized, err := normalizeConfig(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in config:", err)
			os.Exit(1)
		}
		config = normalized
	}

	includes := config.Include
	excludes := config.Exclude
	depth := config.Depth
	confirmDeletes := true
	if config.Confirm != nil {
		confirmDeletes = *config.Confirm
	}
	if noConfirm {
		confirmDeletes = false
	}
	if includeTargets.set {
		includes = parseTargetList(includeTargets.value)
	}
	if excludeTargets.set {
		excludes = parseTargetList(excludeTargets.value)
	}
	if maxDepth.set {
		depth = maxDepth.value
	}

	skip := mergeSkipDirs(defaultSkipDirs(), config.Skip)
	targets := buildTargetMapWithList(includes, excludes)
	if listTargets {
		for _, name := range sortedTargetNames(targets) {
			fmt.Println(name)
		}
		return
	}

	opts := ScanOptions{
		Root:       absRoot,
		RootHandle: rootHandle,
		Targets:    targets,
		MaxDepth:   depth,
		SkipDirs:   skip,
	}

	m := NewModel(ctx, opts, confirmDeletes)
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error running program:", err)
		os.Exit(1)
	}
}
