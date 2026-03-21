package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
	Path string
	Def  TargetDef
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
	lastProgress := time.Now()
	warningsMu := sync.Mutex{}

	sendProgress := func(force bool) {
		if force || time.Since(lastProgress) > 200*time.Millisecond {
			out <- scanProgressMsg{ID: id, Visited: visited, Found: found}
			lastProgress = time.Now()
		}
	}

	maxDepth := opts.MaxDepth
	rootFS := opts.RootHandle.FS()

	jobs := make(chan scanCandidate, workers*8)
	results := make(chan scanSizeResult, workers*8)

	var workerWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					return
				}

				size, sizeErr := dirSize(ctx, opts.RootHandle, candidate.Path)
				if errors.Is(sizeErr, context.Canceled) {
					return
				}

				select {
				case <-ctx.Done():
					return
				case results <- scanSizeResult{Candidate: candidate, Size: size, Err: sizeErr}:
				}
			}
		}()
	}

	doneResults := make(chan struct{})
	go func() {
		defer close(doneResults)
		for result := range results {
			if ctx.Err() != nil {
				return
			}

			if result.Err != nil {
				reason := classifyScanFailure(result.Err)
				warningsMu.Lock()
				warnings = append(warnings, fmt.Sprintf("size %s: %s (%v)", reason, filepath.FromSlash(result.Candidate.Path), result.Err))
				warningsMu.Unlock()
			}

			msg := scanSizeMsg{
				ID:   id,
				Path: filepath.FromSlash(result.Candidate.Path),
				Size: result.Size,
				Err:  result.Err,
			}

			select {
			case <-ctx.Done():
				return
			case out <- msg:
			}
		}
	}()

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

				row := rowData{
					RelPath:     filepath.FromSlash(path),
					Target:      def.Name,
					Category:    def.Category,
					SizePending: true,
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case out <- scanRowMsg{ID: id, Row: row}:
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case jobs <- scanCandidate{Path: path, Def: def}:
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

	close(jobs)
	workerWG.Wait()
	close(results)
	<-doneResults

	sendProgress(true)
	finished := scanFinishedMsg{
		ID:       id,
		Warnings: warnings,
		Err:      err,
		Elapsed:  time.Since(start),
		Visited:  visited,
		Found:    found,
		Workers:  workers,
	}

	select {
	case <-ctx.Done():
		return
	case out <- finished:
	}
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
