package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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

type scanCandidate struct {
	Index int
	Path  string
	Def   TargetDef
}

type scanSizeResult struct {
	Candidate scanCandidate
	Size      int64
	Err       error
}

func defaultScanWorkers() int {
	workers := runtime.NumCPU()
	if workers < 2 {
		return 2
	}
	if workers > 12 {
		return 12
	}
	return workers
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
	workers := defaultScanWorkers()
	candidates := []scanCandidate{}
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
				found++
				candidates = append(candidates, scanCandidate{
					Index: len(candidates),
					Path:  path,
					Def:   def,
				})
				sendProgress(true)
				return fs.SkipDir
			}
		}

		return nil
	})

	if errors.Is(err, context.Canceled) {
		err = nil
	}

	if err == nil && len(candidates) > 0 {
		results := sizeTargetsConcurrently(ctx, opts, candidates, workers)
		for _, result := range results {
			if result.Err != nil {
				reason := classifyScanFailure(result.Err)
				warnings = append(warnings, fmt.Sprintf("size %s: %s (%v)", reason, filepath.FromSlash(result.Candidate.Path), result.Err))
			}

			row := rowData{
				RelPath:   filepath.FromSlash(result.Candidate.Path),
				Target:    result.Candidate.Def.Name,
				Category:  result.Candidate.Def.Category,
				SizeBytes: result.Size,
			}
			if result.Err != nil {
				row.SizeErr = result.Err.Error()
			}

			out <- scanRowMsg{ID: id, Row: row}
			sendProgress(true)
		}
	}

	sendProgress(true)
	out <- scanFinishedMsg{
		ID:       id,
		Warnings: warnings,
		Err:      err,
		Elapsed:  time.Since(start),
		Visited:  visited,
		Found:    found,
		Workers:  workers,
	}
}

func sizeTargetsConcurrently(ctx context.Context, opts ScanOptions, candidates []scanCandidate, workers int) []scanSizeResult {
	if len(candidates) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 1
	}

	jobs := make(chan scanCandidate)
	results := make(chan scanSizeResult, len(candidates))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					return
				}

				size, err := dirSize(ctx, opts.RootHandle, candidate.Path)
				if errors.Is(err, context.Canceled) {
					return
				}

				select {
				case <-ctx.Done():
					return
				case results <- scanSizeResult{Candidate: candidate, Size: size, Err: err}:
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, candidate := range candidates {
			if ctx.Err() != nil {
				return
			}
			jobs <- candidate
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	output := make([]scanSizeResult, 0, len(candidates))
	for result := range results {
		output = append(output, result)
	}

	sort.Slice(output, func(i, j int) bool {
		return output[i].Candidate.Index < output[j].Candidate.Index
	})

	return output
}

func classifyScanFailure(err error) string {
	if err == nil {
		return "unknown"
	}
	switch {
	case errors.Is(err, fs.ErrPermission), errors.Is(err, os.ErrPermission):
		return "permission denied"
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, os.ErrNotExist):
		return "path not found"
	default:
		return "scan error"
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
