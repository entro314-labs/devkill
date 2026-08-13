package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel(t *testing.T, rows []rowData) model {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	})

	m := NewModel(context.Background(), ScanOptions{
		Root:       dir,
		RootHandle: root,
		Targets:    buildTargetMapWithList(nil, nil),
		SkipDirs:   defaultSkipDirs(),
	}, true)
	m.updateLayout(120, 40)
	m.loading = false
	m.rows = rows
	m.setTableRows()
	return m
}

func sendKey(t *testing.T, m model, msg tea.KeyMsg) model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", updated)
	}
	return next
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func testRows(n int) []rowData {
	rows := make([]rowData, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, rowData{
			RelPath:   filepath.Join("proj", string(rune('a'+i)), "node_modules"),
			Target:    "node_modules",
			Category:  "node",
			SizeBytes: int64((i + 1) * 100),
		})
	}
	return rows
}

func TestSpaceKeyTogglesMarkWithoutPaging(t *testing.T) {
	m := newTestModel(t, testRows(30))
	m.table.SetCursor(0)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeySpace})

	if !m.rows[0].Marked {
		t.Fatal("space did not queue the selected row")
	}
	if got := m.table.Cursor(); got != 0 {
		t.Fatalf("space moved the table cursor to %d, want 0", got)
	}

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if m.rows[0].Marked {
		t.Fatal("space did not un-queue the selected row")
	}
}

func TestActionKeysDoNotScrollTable(t *testing.T) {
	// "d" and "u" are app actions (delete, recalc); the table's default
	// keymap also binds them to half-page scrolling, which must stay off.
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "delete key", key: runeKey('d')},
		{name: "recalc key", key: runeKey('u')},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, testRows(30))
			m.table.SetCursor(15)

			m = sendKey(t, m, tc.key)

			if got := m.table.Cursor(); got != 15 {
				t.Fatalf("cursor moved to %d, want 15", got)
			}
		})
	}
}

func TestDeleteBlockedWhileSizePending(t *testing.T) {
	rows := testRows(2)
	rows[0].SizePending = true
	m := newTestModel(t, rows)
	m.table.SetCursor(0)

	m = sendKey(t, m, runeKey('d'))

	if m.confirm.active || m.deleting {
		t.Fatal("delete started while size is pending")
	}
	if m.lastEvent != "Cannot delete while size is still calculating" {
		t.Fatalf("lastEvent = %q, want sizing guard message", m.lastEvent)
	}

	rows[0].SizePending = false
	rows[0].Marked = true
	rows[1].SizePending = true
	rows[1].Marked = true
	m.rows = rows

	m = sendKey(t, m, runeKey('D'))

	if m.confirm.active || m.deleting {
		t.Fatal("delete marked started while a queued row is still sizing")
	}
	if m.lastEvent != "Cannot delete: 1 queued item(s) still sizing" {
		t.Fatalf("lastEvent = %q, want batch sizing guard message", m.lastEvent)
	}
}

func TestConfirmQuitExitsApp(t *testing.T) {
	m := newTestModel(t, testRows(1))
	m = sendKey(t, m, runeKey('d'))
	if !m.confirm.active {
		t.Fatal("confirm prompt not active")
	}

	updated, cmd := m.Update(runeKey('q'))
	if cmd == nil {
		t.Fatal("quit during confirm returned nil command")
	}
	if _, ok := updated.(model); !ok {
		t.Fatalf("Update returned %T, want model", updated)
	}
}

func TestDeleteKeyOpensConfirmForSelectedRow(t *testing.T) {
	m := newTestModel(t, testRows(3))
	m.table.SetCursor(1)

	m = sendKey(t, m, runeKey('d'))

	if !m.confirm.active {
		t.Fatal("confirm prompt not active")
	}
	if m.confirm.action != confirmDeleteOne {
		t.Fatalf("confirm action = %v, want confirmDeleteOne", m.confirm.action)
	}
	if len(m.confirm.paths) != 1 || m.confirm.paths[0] != m.rows[1].RelPath {
		t.Fatalf("confirm paths = %v, want selected row path %q", m.confirm.paths, m.rows[1].RelPath)
	}
}

func TestConfirmDecline(t *testing.T) {
	for _, keyStr := range []string{"n", "N", "esc"} {
		t.Run(keyStr, func(t *testing.T) {
			m := newTestModel(t, testRows(2))
			m = sendKey(t, m, runeKey('d'))
			if !m.confirm.active {
				t.Fatal("confirm prompt not active")
			}

			var msg tea.KeyMsg
			if keyStr == "esc" {
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			} else {
				msg = runeKey(rune(keyStr[0]))
			}
			m = sendKey(t, m, msg)

			if m.confirm.active {
				t.Fatal("confirm prompt still active after decline")
			}
			if m.deleting {
				t.Fatal("deletion started despite decline")
			}
		})
	}
}

func TestConfirmAcceptStartsDelete(t *testing.T) {
	m := newTestModel(t, testRows(2))
	m = sendKey(t, m, runeKey('d'))
	m = sendKey(t, m, runeKey('y'))

	if !m.deleting {
		t.Fatal("deletion did not start after confirm")
	}
	if m.confirm.active {
		t.Fatal("confirm prompt still active after accept")
	}
	if m.cleanup.Requested != 1 {
		t.Fatalf("cleanup.Requested = %d, want 1", m.cleanup.Requested)
	}
}

func TestMarkAllAndClearSkipDeleted(t *testing.T) {
	rows := testRows(3)
	rows[1].Deleted = true
	m := newTestModel(t, rows)

	m = sendKey(t, m, runeKey('a'))
	if !m.rows[0].Marked || !m.rows[2].Marked {
		t.Fatal("mark all did not queue live rows")
	}
	if m.rows[1].Marked {
		t.Fatal("mark all queued a deleted row")
	}

	m = sendKey(t, m, runeKey('A'))
	for i, row := range m.rows {
		if row.Marked {
			t.Fatalf("row %d still queued after clear", i)
		}
	}
}

func TestToggleMarkIgnoresDeletedRow(t *testing.T) {
	rows := testRows(1)
	rows[0].Deleted = true
	m := newTestModel(t, rows)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeySpace})

	if m.rows[0].Marked {
		t.Fatal("deleted row was queued")
	}
}

func TestDeleteMarkedWithEmptyQueue(t *testing.T) {
	m := newTestModel(t, testRows(2))

	m = sendKey(t, m, runeKey('D'))

	if m.confirm.active || m.deleting {
		t.Fatal("delete-marked acted on an empty queue")
	}
	if m.lastEvent != "Queue is empty" {
		t.Fatalf("lastEvent = %q, want queue-empty notice", m.lastEvent)
	}
}

func TestRequestsBlockedWhileDeleting(t *testing.T) {
	m := newTestModel(t, testRows(3))
	m.deleting = true

	if cmd := (&m).requestDeleteSelected(); cmd != nil {
		t.Fatal("requestDeleteSelected returned a command while deleting")
	}
	if m.confirm.active {
		t.Fatal("confirm prompt opened while deleting")
	}
	if cmd := (&m).requestDeleteMarked(); cmd != nil {
		t.Fatal("requestDeleteMarked returned a command while deleting")
	}
	if cmd := (&m).requestRecalcSelected(); cmd != nil {
		t.Fatal("requestRecalcSelected returned a command while deleting")
	}
	if cmd := (&m).startDelete([]string{"x"}); cmd != nil {
		t.Fatal("startDelete started a second chain while deleting")
	}
}

func TestQuitBlockedWhileDeleting(t *testing.T) {
	m := newTestModel(t, testRows(1))
	m.deleting = true

	updated, cmd := m.Update(runeKey('q'))
	m = updated.(model)

	if cmd != nil {
		t.Fatal("quit returned a command while deletion was in progress")
	}
	if !m.deleting {
		t.Fatal("quit changed deletion state")
	}
	if m.lastEvent != "Cannot quit while deletion is in progress" {
		t.Fatalf("lastEvent = %q", m.lastEvent)
	}
}

func TestRescanBlockedWhileDeleting(t *testing.T) {
	m := newTestModel(t, testRows(2))
	m.deleting = true
	scanIDBefore := m.scanID
	rowsBefore := len(m.rows)

	m = sendKey(t, m, runeKey('r'))

	if m.scanID != scanIDBefore {
		t.Fatal("rescan started while deleting")
	}
	if len(m.rows) != rowsBefore {
		t.Fatal("rescan cleared rows while deleting")
	}
	if m.loading {
		t.Fatal("rescan set loading while deleting")
	}
}

func TestDeleteLifecycle(t *testing.T) {
	rows := testRows(2)
	rows[0].Marked = true
	rows[1].Marked = true
	m := newTestModel(t, rows)

	paths := []string{rows[0].RelPath, rows[1].RelPath}
	if cmd := (&m).startDelete(paths); cmd == nil {
		t.Fatal("startDelete returned nil")
	}
	if !m.deleting || m.deleteTotal != 2 {
		t.Fatalf("delete state: deleting=%v total=%d, want deleting with total 2", m.deleting, m.deleteTotal)
	}
	if m.cleanup.PlannedBytes != rows[0].SizeBytes+rows[1].SizeBytes {
		t.Fatalf("PlannedBytes = %d, want %d", m.cleanup.PlannedBytes, rows[0].SizeBytes+rows[1].SizeBytes)
	}

	// First result: success.
	if cmd := (&m).applyDeleteResult(deleteResult{Path: paths[0]}); cmd == nil {
		t.Fatal("expected follow-up command for next queued delete")
	}
	if !m.rows[0].Deleted || m.rows[0].Marked {
		t.Fatal("successful delete did not transition row to Deleted/unmarked")
	}
	if m.cleanup.Deleted != 1 || m.cleanup.FreedBytes != rows[0].SizeBytes {
		t.Fatalf("cleanup after success = %+v", m.cleanup)
	}
	if m.cleanup.ByCategory["node"] != rows[0].SizeBytes || m.cleanup.ByCatCount["node"] != 1 {
		t.Fatalf("category breakdown = %v / %v", m.cleanup.ByCategory, m.cleanup.ByCatCount)
	}

	// Second result: failure ends the chain.
	if cmd := (&m).applyDeleteResult(deleteResult{Path: paths[1], Err: os.ErrPermission}); cmd == nil {
		t.Fatal("expected final progress command")
	}
	if m.deleting {
		t.Fatal("deleting flag still set after final result")
	}
	if m.rows[1].Deleted {
		t.Fatal("failed delete marked row as Deleted")
	}
	if m.rows[1].DeleteErr == "" {
		t.Fatal("failed delete did not record error on row")
	}
	if m.cleanup.Failed != 1 || m.cleanup.FailureKinds["permission denied"] != 1 {
		t.Fatalf("failure accounting = %+v", m.cleanup)
	}
	if m.cleanup.CompletedAt.IsZero() {
		t.Fatal("CompletedAt not set on completion")
	}
}

func TestValidateDeletePath(t *testing.T) {
	absDir := t.TempDir()
	tests := []struct {
		name    string
		path    string
		wantErr bool
		want    string
	}{
		{name: "empty", path: "", wantErr: true},
		{name: "dot", path: ".", wantErr: true},
		{name: "collapses to dot", path: "a/..", wantErr: true},
		{name: "absolute", path: absDir, wantErr: true},
		{name: "parent", path: "..", wantErr: true},
		{name: "parent traversal", path: filepath.Join("..", "victim"), wantErr: true},
		{name: "simple", path: "node_modules", want: "node_modules"},
		{name: "nested", path: "a/b/node_modules", want: filepath.Clean("a/b/node_modules")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateDeletePath(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateDeletePath(%q) = %q, want error", tc.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateDeletePath(%q) error: %v", tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("validateDeletePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestDeleteCmdRemovesTargetOnly(t *testing.T) {
	base := t.TempDir()
	rootDir := filepath.Join(base, "workspace")
	victim := filepath.Join(base, "victim")
	target := filepath.Join(rootDir, "node_modules")
	if err := os.MkdirAll(filepath.Join(target, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "pkg", "f.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	}()

	msg := deleteCmd(root, "node_modules")()
	result, ok := msg.(deleteResultMsg)
	if !ok {
		t.Fatalf("deleteCmd returned %T", msg)
	}
	if result.Result.Err != nil {
		t.Fatalf("delete failed: %v", result.Result.Err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("target directory still exists")
	}

	// Escape attempts must fail at the os.Root boundary and leave siblings intact.
	msg = deleteCmd(root, "../victim")()
	result, ok = msg.(deleteResultMsg)
	if !ok {
		t.Fatalf("deleteCmd returned %T", msg)
	}
	if result.Result.Err == nil {
		t.Fatal("delete escaped the scan root without error")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("sibling directory outside root was affected: %v", err)
	}
}

func TestScanSizeMsgKeepsPartialSizeOnError(t *testing.T) {
	m := newTestModel(t, testRows(1))
	m.loading = true

	updated, _ := m.Update(scanSizeMsg{
		ID:   m.scanID,
		Path: m.rows[0].RelPath,
		Size: 4096,
		Err:  errors.New("boom"),
	})
	m = updated.(model)

	if m.rows[0].SizeBytes != 4096 {
		t.Fatalf("SizeBytes = %d, want partial size 4096", m.rows[0].SizeBytes)
	}
	if m.rows[0].SizeErr == "" {
		t.Fatal("SizeErr not recorded")
	}
	if m.rows[0].SizePending {
		t.Fatal("SizePending still set")
	}
}

func TestApplyRecalcResultRecordsPartialSizeAndError(t *testing.T) {
	m := newTestModel(t, testRows(1))

	(&m).applyRecalcResult(recalcSizeMsg{Path: m.rows[0].RelPath, Size: 2048, Err: errors.New("boom")})

	if m.rows[0].SizeBytes != 2048 {
		t.Fatalf("SizeBytes = %d, want 2048", m.rows[0].SizeBytes)
	}
	if m.rows[0].SizeErr == "" {
		t.Fatal("SizeErr not recorded")
	}
}

func TestRecalcEntersPendingStateAndBlocksDelete(t *testing.T) {
	m := newTestModel(t, testRows(1))

	cmd := (&m).requestRecalcSelected()
	if cmd == nil {
		t.Fatal("recalculation did not start")
	}
	if !m.rows[0].SizePending {
		t.Fatal("row did not enter sizing state")
	}
	if deleteCmd := (&m).requestDeleteSelected(); deleteCmd != nil {
		t.Fatal("delete command started during recalculation")
	}
	if m.confirm.active || m.deleting {
		t.Fatal("delete flow advanced during recalculation")
	}
}

func TestStaleScanMessagesIgnored(t *testing.T) {
	m := newTestModel(t, testRows(1))
	staleID := m.scanID - 1

	updated, _ := m.Update(scanRowMsg{ID: staleID, Row: rowData{RelPath: "stale"}})
	m = updated.(model)
	if len(m.rows) != 1 {
		t.Fatal("stale scanRowMsg appended a row")
	}

	updated, _ = m.Update(scanFinishedMsg{ID: staleID, Err: errors.New("stale failure")})
	m = updated.(model)
	if m.err != nil {
		t.Fatal("stale scanFinishedMsg overwrote model error state")
	}

	originalSize := m.rows[0].SizeBytes
	updated, _ = m.Update(recalcSizeMsg{ID: staleID, Path: m.rows[0].RelPath, Size: 9999})
	m = updated.(model)
	if m.rows[0].SizeBytes != originalSize {
		t.Fatal("stale recalculation overwrote current scan state")
	}
}

func TestSortRows(t *testing.T) {
	rows := []rowData{
		{RelPath: "b", SizeBytes: 50},
		{RelPath: "a", SizeBytes: 200, Deleted: true},
		{RelPath: "c", SizeBytes: 100},
	}

	tests := []struct {
		mode sortMode
		want []string
	}{
		{mode: sortBySizeDesc, want: []string{"c", "b", "a"}},
		{mode: sortBySizeAsc, want: []string{"b", "c", "a"}},
		{mode: sortByNameAsc, want: []string{"b", "c", "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.mode.String(), func(t *testing.T) {
			m := newTestModel(t, append([]rowData(nil), rows...))
			m.sortMode = tc.mode
			(&m).sortRows()
			for i, want := range tc.want {
				if m.rows[i].RelPath != want {
					t.Fatalf("position %d = %q, want %q (deleted rows must sort last)", i, m.rows[i].RelPath, want)
				}
			}
		})
	}
}

func TestNextSortModeCycles(t *testing.T) {
	if got := nextSortMode(sortBySizeDesc); got != sortBySizeAsc {
		t.Fatalf("after size desc: %v", got)
	}
	if got := nextSortMode(sortBySizeAsc); got != sortByNameAsc {
		t.Fatalf("after size asc: %v", got)
	}
	if got := nextSortMode(sortByNameAsc); got != sortBySizeDesc {
		t.Fatalf("after name: %v", got)
	}
}

func TestStatsExcludesDeletedFromTotal(t *testing.T) {
	rows := testRows(3)
	rows[0].Marked = true
	rows[2].Deleted = true
	m := newTestModel(t, rows)

	total, queued, deleted := m.stats()

	wantTotal := rows[0].SizeBytes + rows[1].SizeBytes
	if total != wantTotal {
		t.Fatalf("total = %d, want %d (deleted rows excluded)", total, wantTotal)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{size: 0, want: "0 B"},
		{size: 1023, want: "1023 B"},
		{size: 1024, want: "1.0 KB"},
		{size: 1536, want: "1.5 KB"},
		{size: 1024 * 1024, want: "1.0 MB"},
		{size: 5 * 1024 * 1024 * 1024, want: "5.0 GB"},
	}
	for _, tc := range tests {
		if got := formatBytes(tc.size); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.size, got, tc.want)
		}
	}
}

func TestClassifyDeleteFailure(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: nil, want: "unknown"},
		{err: os.ErrPermission, want: "permission denied"},
		{err: os.ErrNotExist, want: "path not found"},
		{err: errors.New("disk on fire"), want: "filesystem error"},
	}
	for _, tc := range tests {
		if got := classifyDeleteFailure(tc.err); got != tc.want {
			t.Errorf("classifyDeleteFailure(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
