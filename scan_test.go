package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openTestRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	})
	return root
}

// collectScan drains a scan stream and returns rows, size results, and the
// finished message.
func collectScan(t *testing.T, opts ScanOptions) ([]rowData, map[string]scanSizeMsg, scanFinishedMsg) {
	t.Helper()
	ch := make(chan tea.Msg)
	go runScanStream(context.Background(), opts, 1, ch)

	rows := []rowData{}
	sizes := map[string]scanSizeMsg{}
	var finished scanFinishedMsg
	gotFinished := false
	timeout := time.After(30 * time.Second)
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				if !gotFinished {
					t.Fatal("scan stream closed without a finished message")
				}
				return rows, sizes, finished
			}
			switch msg := msg.(type) {
			case scanRowMsg:
				rows = append(rows, msg.Row)
			case scanSizeMsg:
				sizes[msg.Path] = msg
			case scanFinishedMsg:
				finished = msg
				gotFinished = true
			}
		case <-timeout:
			t.Fatal("scan did not finish in time")
		}
	}
}

func TestScanFindsTargetsAndSizes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app", "node_modules", "lib", "a.js"), 1000)
	writeFile(t, filepath.Join(dir, "app", "node_modules", "b.js"), 500)
	writeFile(t, filepath.Join(dir, "svc", ".venv", "lib", "c.py"), 300)
	writeFile(t, filepath.Join(dir, "src", "main.go"), 100) // not a target
	// Targets inside skip dirs must not be reported.
	writeFile(t, filepath.Join(dir, ".git", "node_modules", "x"), 50)

	root := openTestRoot(t, dir)
	rows, sizes, finished := collectScan(t, ScanOptions{
		Root:       dir,
		RootHandle: root,
		Targets:    buildTargetMapWithList(nil, nil),
		SkipDirs:   defaultSkipDirs(),
	})

	if finished.Err != nil {
		t.Fatalf("scan error: %v", finished.Err)
	}
	byPath := map[string]rowData{}
	for _, row := range rows {
		byPath[row.RelPath] = row
	}
	nm := filepath.Join("app", "node_modules")
	venv := filepath.Join("svc", ".venv")
	if _, ok := byPath[nm]; !ok {
		t.Fatalf("node_modules not found; rows: %v", byPath)
	}
	if _, ok := byPath[venv]; !ok {
		t.Fatalf(".venv not found; rows: %v", byPath)
	}
	if len(rows) != 2 {
		t.Fatalf("found %d rows, want 2 (git-internal target must be skipped)", len(rows))
	}
	if got := sizes[nm].Size; got != 1500 {
		t.Fatalf("node_modules size = %d, want 1500", got)
	}
	if got := sizes[venv].Size; got != 300 {
		t.Fatalf(".venv size = %d, want 300", got)
	}
	if byPath[nm].Category != "node" || byPath[venv].Category != "python" {
		t.Fatalf("categories wrong: %+v %+v", byPath[nm], byPath[venv])
	}
}

func TestScanDoesNotDescendIntoTargets(t *testing.T) {
	dir := t.TempDir()
	// A target nested inside another target must be counted as part of the
	// outer target, not reported separately.
	writeFile(t, filepath.Join(dir, "node_modules", "pkg", "node_modules", "inner.js"), 100)

	root := openTestRoot(t, dir)
	rows, sizes, finished := collectScan(t, ScanOptions{
		Root:       dir,
		RootHandle: root,
		Targets:    buildTargetMapWithList(nil, nil),
		SkipDirs:   defaultSkipDirs(),
	})

	if finished.Err != nil {
		t.Fatalf("scan error: %v", finished.Err)
	}
	if len(rows) != 1 {
		t.Fatalf("found %d rows, want 1", len(rows))
	}
	if got := sizes["node_modules"].Size; got != 100 {
		t.Fatalf("outer target size = %d, want 100 (must include nested target)", got)
	}
}

func TestScanRespectsMaxDepth(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "node_modules", "a"), 10)                // depth 0
	writeFile(t, filepath.Join(dir, "x", "node_modules", "b"), 10)           // depth 1
	writeFile(t, filepath.Join(dir, "x", "y", "z", "node_modules", "c"), 10) // depth 3

	root := openTestRoot(t, dir)
	rows, _, finished := collectScan(t, ScanOptions{
		Root:       dir,
		RootHandle: root,
		Targets:    buildTargetMapWithList(nil, nil),
		MaxDepth:   1,
		SkipDirs:   defaultSkipDirs(),
	})

	if finished.Err != nil {
		t.Fatalf("scan error: %v", finished.Err)
	}
	if len(rows) != 2 {
		paths := []string{}
		for _, row := range rows {
			paths = append(paths, row.RelPath)
		}
		t.Fatalf("found %v, want the two targets at depth <= 1", paths)
	}
}

func TestScanCanceledStreamDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(dir, "p", string(rune('a'+i%26)), "node_modules", "f"), 10)
	}

	root := openTestRoot(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tea.Msg)
	done := make(chan struct{})
	go func() {
		runScanStream(ctx, ScanOptions{
			Root:       dir,
			RootHandle: root,
			Targets:    buildTargetMapWithList(nil, nil),
			SkipDirs:   defaultSkipDirs(),
		}, 1, ch)
		close(done)
	}()

	// Simulate the UI abandoning the stream mid-scan: read one message, then
	// cancel and stop reading. The scan goroutine must still terminate.
	<-ch
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("scan goroutine leaked after context cancellation with no reader")
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cache", "a"), 100)
	writeFile(t, filepath.Join(dir, "cache", "sub", "b"), 250)

	root := openTestRoot(t, dir)

	size, err := dirSize(context.Background(), root, "cache")
	if err != nil {
		t.Fatalf("dirSize error: %v", err)
	}
	if size != 350 {
		t.Fatalf("size = %d, want 350", size)
	}

	// A vanished target must remain visible as a sizing failure. Treating it as
	// a successful zero-byte measurement would present a stale row as ready.
	size, err = dirSize(context.Background(), root, "missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dirSize on missing path error = %v, want not-exist", err)
	}
	if size != 0 {
		t.Fatalf("size for missing path = %d, want 0", size)
	}
}

func TestRelativeDepth(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{path: ".", want: 0},
		{path: "", want: 0},
		{path: "a", want: 0},
		{path: "a/b", want: 1},
		{path: "a/b/c", want: 2},
	}
	for _, tc := range tests {
		if got := relativeDepth(tc.path); got != tc.want {
			t.Errorf("relativeDepth(%q) = %d, want %d", tc.path, got, tc.want)
		}
	}
}
