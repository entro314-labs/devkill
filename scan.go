package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type ScanOptions struct {
	Root       string
	RootHandle *os.Root
	Targets    map[string]TargetDef
	MaxDepth   int
	SkipDirs   map[string]struct{}
}

func defaultSkipDirs() map[string]struct{} {
	return map[string]struct{}{
		".git": {},
		".hg":  {},
		".svn": {},
	}
}

func runScanStream(ctx context.Context, opts ScanOptions, id int, out chan<- tea.Msg) {
	defer close(out)

	if opts.RootHandle == nil {
		out <- scanFinishedMsg{ID: id, Err: errors.New("scan: root handle is nil")}
		return
	}

	start := time.Now()
	warnings := []string{}
	visited := 0
	found := 0
	lastProgress := time.Now()

	sendProgress := func(force bool) {
		if force || time.Since(lastProgress) > 200*time.Millisecond {
			out <- scanProgressMsg{ID: id, Visited: visited, Found: found}
			lastProgress = time.Now()
		}
	}

	maxDepth := opts.MaxDepth
	rootFS := opts.RootHandle.FS()

	err := fs.WalkDir(rootFS, ".", func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				warnings = append(warnings, fmt.Sprintf("permission denied: %s", filepath.FromSlash(path)))
				return fs.SkipDir
			}
			return err
		}

		if entry.IsDir() {
			visited++
			sendProgress(false)
			name := entry.Name()
			if _, ok := opts.SkipDirs[name]; ok {
				return filepath.SkipDir
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fs.SkipDir
			}
			if maxDepth > 0 {
				depth := relativeDepth(path)
				if depth > maxDepth {
					return fs.SkipDir
				}
			}

			if def, ok := opts.Targets[name]; ok {
				size, sizeErr := dirSize(ctx, opts.RootHandle, path)
				if sizeErr != nil {
					if errors.Is(sizeErr, fs.ErrPermission) {
						warnings = append(warnings, fmt.Sprintf("permission denied: %s", filepath.FromSlash(path)))
						return fs.SkipDir
					}
					return sizeErr
				}
				found++
				rel := filepath.FromSlash(path)
				out <- scanRowMsg{
					ID: id,
					Row: rowData{
						RelPath:   rel,
						Target:    def.Name,
						Category:  def.Category,
						SizeBytes: size,
					},
				}
				sendProgress(true)
				return fs.SkipDir
			}
		}

		return nil
	})

	if errors.Is(err, context.Canceled) {
		err = nil
	}

	sendProgress(true)
	out <- scanFinishedMsg{
		ID:       id,
		Warnings: warnings,
		Err:      err,
		Elapsed:  time.Since(start),
		Visited:  visited,
		Found:    found,
	}
}

func dirSize(ctx context.Context, root *os.Root, relPath string) (int64, error) {
	if root == nil {
		return 0, errors.New("dirSize: root handle is nil")
	}

	var size int64
	relSlash := filepath.ToSlash(relPath)
	rootFS := root.FS()

	err := fs.WalkDir(rootFS, relSlash, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				return fs.SkipDir
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		size += info.Size()
		return nil
	})

	if err != nil {
		return 0, err
	}
	return size, nil
}

func relativeDepth(relPath string) int {
	trimmed := strings.TrimPrefix(relPath, "./")
	if trimmed == "." || trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/")
}
