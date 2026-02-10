package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type rowData struct {
	RelPath   string
	Target    string
	Category  string
	SizeBytes int64
	Marked    bool
	Deleted   bool
	DeleteErr string
}

type sortMode int

const (
	sortBySizeDesc sortMode = iota
	sortBySizeAsc
	sortByNameAsc
)

func (m sortMode) String() string {
	switch m {
	case sortBySizeAsc:
		return "size ↑"
	case sortByNameAsc:
		return "name"
	default:
		return "size ↓"
	}
}

type confirmAction int

const (
	confirmNone confirmAction = iota
	confirmDeleteOne
	confirmDeleteMarked
)

type confirmState struct {
	active bool
	action confirmAction
	paths  []string
}

type scanStreamMsg struct {
	ID int
	Ch <-chan tea.Msg
}

type scanRowMsg struct {
	ID  int
	Row rowData
}

type scanProgressMsg struct {
	ID      int
	Visited int
	Found   int
}

type scanFinishedMsg struct {
	ID       int
	Warnings []string
	Err      error
	Elapsed  time.Duration
	Visited  int
	Found    int
}

type scanPulseMsg struct{}

type recalcSizeMsg struct {
	Path string
	Size int64
	Err  error
}

type deleteResult struct {
	Path string
	Err  error
}

type deleteResultMsg struct {
	Result deleteResult
}

type keyMap struct {
	ToggleMark    key.Binding
	MarkAll       key.Binding
	ClearMarks    key.Binding
	Delete        key.Binding
	DeleteMarked  key.Binding
	Rescan        key.Binding
	Sort          key.Binding
	RecalcSize    key.Binding
	ToggleConfirm key.Binding
	Help          key.Binding
	Quit          key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		ToggleMark: key.NewBinding(
			key.WithKeys("space"),
			key.WithHelp("space", "queue"),
		),
		MarkAll: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "queue all"),
		),
		ClearMarks: key.NewBinding(
			key.WithKeys("A"),
			key.WithHelp("A", "clear queue"),
		),
		Delete: key.NewBinding(
			key.WithKeys("enter", "d"),
			key.WithHelp("enter/d", "delete"),
		),
		DeleteMarked: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "delete marked"),
		),
		Rescan: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "rescan"),
		),
		Sort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "sort"),
		),
		RecalcSize: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "recalc size"),
		),
		ToggleConfirm: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "toggle confirm"),
		),
		Help: key.NewBinding(
			key.WithKeys("?", "h"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.ToggleMark, k.MarkAll, k.Delete, k.DeleteMarked, k.Sort, k.Rescan, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.ToggleMark, k.MarkAll, k.ClearMarks, k.Delete, k.DeleteMarked}, {k.Sort, k.RecalcSize, k.ToggleConfirm, k.Rescan, k.Help, k.Quit}}
}

type model struct {
	table          table.Model
	spinner        spinner.Model
	help           help.Model
	keys           keyMap
	rows           []rowData
	loading        bool
	err            error
	warnings       []string
	lastScan       time.Duration
	lastEvent      string
	sortMode       sortMode
	confirm        confirmState
	confirmDeletes bool
	width          int
	height         int
	scanOpts       ScanOptions
	scanID         int
	baseCtx        context.Context
	baseCancel     context.CancelFunc
	scanCtx        context.Context
	scanCancel     context.CancelFunc
	scanStream     <-chan tea.Msg
	scanVisited    int
	scanFound      int
	scanStart      time.Time
	scanPulse      float64
	scanPulseDir   float64
	scanProgress   progress.Model
	deleteProgress progress.Model
	deleting       bool
	deleteQueue    []string
	deleteTotal    int
	deleteDone     int
	deleteErrors   int
}

type styles struct {
	base      lipgloss.Style
	header    lipgloss.Style
	title     lipgloss.Style
	subtitle  lipgloss.Style
	status    lipgloss.Style
	muted     lipgloss.Style
	accent    lipgloss.Style
	danger    lipgloss.Style
	warning   lipgloss.Style
	confirm   lipgloss.Style
	chip      lipgloss.Style
	container lipgloss.Style
}

var ui = styles{
	base: lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")),
	container: lipgloss.NewStyle().Padding(0, 1),
	header:    lipgloss.NewStyle().Padding(0, 1),
	title:     lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true),
	subtitle:  lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	status:    lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
	muted:     lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
	accent:    lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true),
	danger:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
	warning:   lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
	confirm:   lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("203")).Bold(true).Padding(0, 1),
	chip:      lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62")).Padding(0, 1),
}

func NewModel(ctx context.Context, opts ScanOptions, confirmDeletes bool) model {
	baseCtx, baseCancel := context.WithCancel(ctx)
	scanCtx, scanCancel := context.WithCancel(baseCtx)

	columns := []table.Column{
		{Title: "Path", Width: 60},
		{Title: "Size", Width: 10},
		{Title: "Target", Width: 14},
		{Title: "Category", Width: 12},
		{Title: "Status", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
	)

	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("238")).
		BorderBottom(true).
		Bold(true)
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(true)
	t.SetStyles(styles)

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	scanBar := progress.New(
		progress.WithDefaultGradient(),
		progress.WithoutPercentage(),
	)
	deleteBar := progress.New(progress.WithDefaultGradient())

	return model{
		table:          t,
		spinner:        sp,
		help:           help.New(),
		keys:           newKeyMap(),
		loading:        true,
		sortMode:       sortBySizeDesc,
		scanOpts:       opts,
		scanID:         1,
		baseCtx:        baseCtx,
		baseCancel:     baseCancel,
		scanCtx:        scanCtx,
		scanCancel:     scanCancel,
		scanStart:      time.Now(),
		scanPulseDir:   1,
		scanProgress:   scanBar,
		deleteProgress: deleteBar,
		confirmDeletes: confirmDeletes,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, scanStartCmd(m.scanCtx, m.scanOpts, m.scanID), scanPulseCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.updateLayout(msg.Width, msg.Height)
	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
	case progress.FrameMsg:
		var cmd tea.Cmd
		var updated tea.Model
		updated, cmd = m.deleteProgress.Update(msg)
		if next, ok := updated.(progress.Model); ok {
			m.deleteProgress = next
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	case scanStreamMsg:
		if msg.ID != m.scanID {
			break
		}
		m.scanStream = msg.Ch
		cmds = append(cmds, waitScanMsg(msg.Ch))
	case scanRowMsg:
		if msg.ID != m.scanID {
			break
		}
		m.rows = append(m.rows, msg.Row)
		m.scanFound++
		m.setTableRows()
		m.lastEvent = fmt.Sprintf("Found: %s", msg.Row.RelPath)
		if m.scanStream != nil {
			cmds = append(cmds, waitScanMsg(m.scanStream))
		}
	case scanProgressMsg:
		if msg.ID != m.scanID {
			break
		}
		m.scanVisited = msg.Visited
		m.scanFound = msg.Found
		if m.scanStream != nil {
			cmds = append(cmds, waitScanMsg(m.scanStream))
		}
	case scanFinishedMsg:
		if msg.ID != m.scanID {
			break
		}
		m.loading = false
		m.err = msg.Err
		m.warnings = msg.Warnings
		m.lastScan = msg.Elapsed
		m.scanVisited = msg.Visited
		m.scanFound = msg.Found
		m.sortRows()
		m.setTableRows()
		if msg.Err == nil {
			m.lastEvent = fmt.Sprintf("Scan complete: %d items", len(m.rows))
		} else {
			m.lastEvent = fmt.Sprintf("Scan failed: %v", msg.Err)
		}
	case scanPulseMsg:
		if m.loading {
			m.scanPulse += 0.06 * m.scanPulseDir
			if m.scanPulse >= 1 {
				m.scanPulse = 1
				m.scanPulseDir = -1
			} else if m.scanPulse <= 0 {
				m.scanPulse = 0
				m.scanPulseDir = 1
			}
			cmds = append(cmds, scanPulseCmd())
		}
	case deleteResultMsg:
		nextCmd := m.applyDeleteResult(msg.Result)
		m.setTableRows()
		if nextCmd != nil {
			cmds = append(cmds, nextCmd)
		}
	case recalcSizeMsg:
		m.applyRecalcResult(msg)
	case tea.KeyMsg:
		if m.confirm.active {
			switch msg.String() {
			case "y", "Y":
				paths := append([]string{}, m.confirm.paths...)
				m.confirm = confirmState{}
				if cmd := m.startDelete(paths); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case "n", "N", "esc":
				m.confirm = confirmState{}
				m.lastEvent = "Deletion cancelled"
			}
			break
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			if m.baseCancel != nil {
				m.baseCancel()
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Rescan):
			var scanCmds []tea.Cmd
			m, scanCmds = m.startScan()
			cmds = append(cmds, scanCmds...)
		case key.Matches(msg, m.keys.Sort):
			m.sortMode = nextSortMode(m.sortMode)
			m.sortRows()
			m.setTableRows()
			m.lastEvent = fmt.Sprintf("Sorted by %s", m.sortMode.String())
		case key.Matches(msg, m.keys.ToggleMark):
			m.toggleMark()
		case key.Matches(msg, m.keys.MarkAll):
			m.markAll()
		case key.Matches(msg, m.keys.ClearMarks):
			m.clearMarks()
		case key.Matches(msg, m.keys.DeleteMarked):
			if cmd := m.requestDeleteMarked(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		case key.Matches(msg, m.keys.Delete):
			if cmd := m.requestDeleteSelected(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		case key.Matches(msg, m.keys.RecalcSize):
			if cmd := m.requestRecalcSelected(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		case key.Matches(msg, m.keys.ToggleConfirm):
			m.confirmDeletes = !m.confirmDeletes
			if m.confirmDeletes {
				m.lastEvent = "Confirm prompts enabled"
			} else {
				m.lastEvent = "Confirm prompts disabled"
			}
		}
	}

	if !m.confirm.active {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	content := ui.base.Render(m.table.View())
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		m.headerView(),
		content,
		m.statusView(),
		m.footerView(),
	)
	return ui.container.Render(view)
}

func (m *model) updateLayout(width, height int) {
	if width == 0 || height == 0 {
		return
	}
	if m.width == width && m.height == height {
		return
	}
	if width < 60 {
		width = 60
	}
	if height < 12 {
		height = 12
	}
	if m.width == width && m.height == height {
		return
	}
	if m.width == 0 {
		m.width = width
		m.height = height
	} else {
		m.width = width
		m.height = height
	}

	sizeWidth := 10
	targetWidth := 16
	categoryWidth := 12
	statusWidth := 10
	pathWidth := max(width-sizeWidth-targetWidth-categoryWidth-statusWidth-12, 20)

	m.table.SetColumns([]table.Column{
		{Title: "Path", Width: pathWidth},
		{Title: "Size", Width: sizeWidth},
		{Title: "Target", Width: targetWidth},
		{Title: "Category", Width: categoryWidth},
		{Title: "Status", Width: statusWidth},
	})

	headerHeight := lipgloss.Height(m.headerView())
	statusHeight := lipgloss.Height(m.statusView())
	footerHeight := lipgloss.Height(m.footerView())
	available := max(height-headerHeight-statusHeight-footerHeight-4, 5)
	m.table.SetHeight(available)
	m.table.SetWidth(width - 4)
	progressWidth := max(width-28, 20)
	m.scanProgress.Width = progressWidth
	m.deleteProgress.Width = progressWidth
}

func (m model) startScan() (model, []tea.Cmd) {
	if m.scanCancel != nil {
		m.scanCancel()
	}
	ctx, cancel := context.WithCancel(m.baseCtx)
	m.scanCtx = ctx
	m.scanCancel = cancel
	m.scanID++
	m.loading = true
	m.err = nil
	m.warnings = nil
	m.rows = nil
	m.scanVisited = 0
	m.scanFound = 0
	m.lastScan = 0
	m.scanStart = time.Now()
	m.scanPulse = 0
	m.scanPulseDir = 1
	m.lastEvent = "Scanning…"
	m.setTableRows()

	cmds := []tea.Cmd{m.spinner.Tick, scanStartCmd(ctx, m.scanOpts, m.scanID), scanPulseCmd()}
	return m, cmds
}

func (m model) headerView() string {
	title := ui.title.Render("devkill")
	subtitle := ui.subtitle.Render("Modern cleanup for heavy dev artifacts")
	root := ui.muted.Render(fmt.Sprintf("Root: %s", m.scanOpts.Root))
	if m.loading {
		root = ui.muted.Render(fmt.Sprintf("Root: %s", m.scanOpts.Root))
	}
	line := lipgloss.JoinHorizontal(lipgloss.Left, title, " ", ui.chip.Render(fmt.Sprintf("targets: %d", len(m.scanOpts.Targets))))
	return ui.header.Render(lipgloss.JoinVertical(lipgloss.Left, line, lipgloss.JoinHorizontal(lipgloss.Left, subtitle, " · ", root)))
}

func (m model) statusView() string {
	_, queued, deleted := m.stats()
	if m.loading {
		elapsed := time.Since(m.scanStart).Truncate(100 * time.Millisecond)
		totalBytes, _, _ := m.stats()
		line := fmt.Sprintf("%s Scanning… visited %d · found %d · total %s · %s", m.spinner.View(), m.scanVisited, m.scanFound, formatBytes(totalBytes), elapsed)
		bar := m.scanProgress.ViewAs(m.scanPulse)
		return lipgloss.JoinVertical(lipgloss.Left, ui.status.Render(line), ui.muted.Render(bar))
	}

	items := len(m.rows)
	totalBytes, _, _ := m.stats()
	parts := []string{
		fmt.Sprintf("Items: %d", items),
		fmt.Sprintf("Total: %s", formatBytes(totalBytes)),
		fmt.Sprintf("Queued: %d", queued),
		fmt.Sprintf("Deleted: %d", deleted),
		fmt.Sprintf("Sort: %s", m.sortMode.String()),
		fmt.Sprintf("Confirm: %s", boolLabel(m.confirmDeletes)),
	}
	if m.lastScan > 0 {
		parts = append(parts, fmt.Sprintf("Scan: %s", m.lastScan.Truncate(10*time.Millisecond)))
	}
	if len(m.warnings) > 0 {
		parts = append(parts, ui.warning.Render(fmt.Sprintf("Warnings: %d", len(m.warnings))))
	}
	status := strings.Join(parts, " · ")
	if m.err != nil {
		status = ui.danger.Render(fmt.Sprintf("Error: %v", m.err))
	}
	lines := []string{ui.status.Render(status)}
	if m.deleting {
		progressLine := fmt.Sprintf("Deleting %d/%d", m.deleteDone, m.deleteTotal)
		bar := m.deleteProgress.View()
		lines = append(lines, ui.muted.Render(progressLine), ui.muted.Render(bar))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m model) footerView() string {
	if m.confirm.active {
		label := "Confirm delete"
		if m.confirm.action == confirmDeleteMarked {
			label = fmt.Sprintf("Delete %d marked item(s)? (y/n)", len(m.confirm.paths))
		} else if len(m.confirm.paths) == 1 {
			label = fmt.Sprintf("Delete %s? (y/n)", m.confirm.paths[0])
		}
		return ui.confirm.Render(label)
	}
	if m.lastEvent != "" {
		return lipgloss.JoinVertical(lipgloss.Left, ui.muted.Render(m.lastEvent), m.help.View(m.keys))
	}
	return m.help.View(m.keys)
}

func (m *model) setTableRows() {
	rows := make([]table.Row, 0, len(m.rows))
	for _, row := range m.rows {
		status := ui.muted.Render("ready")
		if row.DeleteErr != "" {
			status = ui.danger.Render("error")
		} else if row.Deleted {
			status = ui.danger.Render("deleted")
		} else if row.Marked {
			status = ui.accent.Render("queued")
		}
		rows = append(rows, table.Row{
			row.RelPath,
			formatBytes(row.SizeBytes),
			row.Target,
			row.Category,
			status,
		})
	}
	m.table.SetRows(rows)
}

func (m *model) sortRows() {
	sort.SliceStable(m.rows, func(i, j int) bool {
		left := m.rows[i]
		right := m.rows[j]
		if left.Deleted != right.Deleted {
			return !left.Deleted
		}
		switch m.sortMode {
		case sortBySizeAsc:
			if left.SizeBytes == right.SizeBytes {
				return strings.ToLower(left.RelPath) < strings.ToLower(right.RelPath)
			}
			return left.SizeBytes < right.SizeBytes
		case sortByNameAsc:
			return strings.ToLower(left.RelPath) < strings.ToLower(right.RelPath)
		default:
			if left.SizeBytes == right.SizeBytes {
				return strings.ToLower(left.RelPath) < strings.ToLower(right.RelPath)
			}
			return left.SizeBytes > right.SizeBytes
		}
	})
}

func nextSortMode(current sortMode) sortMode {
	switch current {
	case sortBySizeDesc:
		return sortBySizeAsc
	case sortBySizeAsc:
		return sortByNameAsc
	default:
		return sortBySizeDesc
	}
}

func (m *model) toggleMark() {
	if len(m.rows) == 0 {
		return
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rows) {
		return
	}
	if m.rows[idx].Deleted {
		return
	}
	m.rows[idx].Marked = !m.rows[idx].Marked
	if m.rows[idx].Marked {
		m.lastEvent = "Added to queue"
	} else {
		m.lastEvent = "Removed from queue"
	}
	m.setTableRows()
}

func (m *model) markAll() {
	if len(m.rows) == 0 {
		return
	}
	count := 0
	for idx := range m.rows {
		if m.rows[idx].Deleted {
			continue
		}
		if !m.rows[idx].Marked {
			m.rows[idx].Marked = true
			count++
		}
	}
	if count > 0 {
		m.lastEvent = fmt.Sprintf("Queued %d item(s)", count)
	} else {
		m.lastEvent = "Queue already full"
	}
	m.setTableRows()
}

func (m *model) clearMarks() {
	if len(m.rows) == 0 {
		return
	}
	count := 0
	for idx := range m.rows {
		if m.rows[idx].Marked {
			m.rows[idx].Marked = false
			count++
		}
	}
	if count > 0 {
		m.lastEvent = "Cleared queue"
	} else {
		m.lastEvent = "Queue already empty"
	}
	m.setTableRows()
}

func (m *model) requestDeleteSelected() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rows) {
		return nil
	}
	row := m.rows[idx]
	if row.Deleted {
		return nil
	}
	if m.confirmDeletes {
		m.confirm = confirmState{active: true, action: confirmDeleteOne, paths: []string{row.RelPath}}
		return nil
	}
	return m.startDelete([]string{row.RelPath})
}

func (m *model) requestDeleteMarked() tea.Cmd {
	paths := []string{}
	for _, row := range m.rows {
		if row.Marked && !row.Deleted {
			paths = append(paths, row.RelPath)
		}
	}
	if len(paths) == 0 {
		m.lastEvent = "Queue is empty"
		return nil
	}
	if m.confirmDeletes {
		m.confirm = confirmState{active: true, action: confirmDeleteMarked, paths: paths}
		return nil
	}
	return m.startDelete(paths)
}

func (m *model) requestRecalcSelected() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rows) {
		return nil
	}
	row := m.rows[idx]
	if row.Deleted {
		return nil
	}
	m.lastEvent = "Recalculating size…"
	return recalcSizeCmd(m.baseCtx, m.scanOpts.RootHandle, row.RelPath)
}

func (m *model) applyDeleteResult(result deleteResult) tea.Cmd {
	idx := m.findRow(result.Path)
	if idx != -1 {
		if result.Err != nil {
			m.rows[idx].DeleteErr = result.Err.Error()
			m.deleteErrors++
		} else {
			m.rows[idx].Deleted = true
			m.rows[idx].Marked = false
			m.rows[idx].DeleteErr = ""
		}
	}

	if m.deleting {
		m.deleteDone++
		percent := 1.0
		if m.deleteTotal > 0 {
			percent = float64(m.deleteDone) / float64(m.deleteTotal)
		}
		progressCmd := m.deleteProgress.SetPercent(percent)
		if m.deleteDone >= m.deleteTotal {
			m.deleting = false
			m.deleteQueue = nil
			if m.deleteErrors > 0 {
				m.lastEvent = fmt.Sprintf("Deleted %d item(s), %d failed", m.deleteTotal-m.deleteErrors, m.deleteErrors)
			} else {
				m.lastEvent = fmt.Sprintf("Deleted %d item(s)", m.deleteTotal)
			}
			return progressCmd
		}
		nextPath := m.deleteQueue[m.deleteDone]
		return tea.Batch(progressCmd, deleteCmd(m.scanOpts.RootHandle, nextPath))
	}

	return nil
}

func (m *model) startDelete(paths []string) tea.Cmd {
	if len(paths) == 0 || m.deleting {
		return nil
	}
	m.deleting = true
	m.deleteQueue = paths
	m.deleteTotal = len(paths)
	m.deleteDone = 0
	m.deleteErrors = 0
	m.lastEvent = fmt.Sprintf("Deleting %d item(s)…", len(paths))
	progressCmd := m.deleteProgress.SetPercent(0)
	return tea.Batch(progressCmd, deleteCmd(m.scanOpts.RootHandle, paths[0]))
}

func (m *model) applyRecalcResult(msg recalcSizeMsg) {
	idx := m.findRow(msg.Path)
	if idx == -1 {
		return
	}
	if msg.Err != nil {
		m.lastEvent = fmt.Sprintf("Recalc failed: %v", msg.Err)
		return
	}
	m.rows[idx].SizeBytes = msg.Size
	m.lastEvent = "Size recalculated"
	m.setTableRows()
}

func (m *model) findRow(path string) int {
	for idx, row := range m.rows {
		if row.RelPath == path {
			return idx
		}
	}
	return -1
}

func (m model) stats() (int64, int, int) {
	var total int64
	queued := 0
	deleted := 0
	for _, row := range m.rows {
		if !row.Deleted {
			total += row.SizeBytes
		}
		if row.Marked {
			queued++
		}
		if row.Deleted {
			deleted++
		}
	}
	return total, queued, deleted
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f %s", value, units[len(units)-1])
}

func scanStartCmd(ctx context.Context, opts ScanOptions, id int) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan tea.Msg)
		go runScanStream(ctx, opts, id, ch)
		return scanStreamMsg{ID: id, Ch: ch}
	}
}

func waitScanMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func deleteCmd(root *os.Root, relPath string) tea.Cmd {
	return func() tea.Msg {
		cleaned, err := validateDeletePath(relPath)
		if err != nil {
			return deleteResultMsg{Result: deleteResult{Path: relPath, Err: err}}
		}
		if root == nil {
			return deleteResultMsg{Result: deleteResult{Path: cleaned, Err: errors.New("delete: root handle is nil")}}
		}
		removeErr := root.RemoveAll(cleaned)
		return deleteResultMsg{Result: deleteResult{Path: cleaned, Err: removeErr}}
	}
}

func recalcSizeCmd(ctx context.Context, root *os.Root, relPath string) tea.Cmd {
	return func() tea.Msg {
		size, err := dirSize(ctx, root, relPath)
		return recalcSizeMsg{Path: relPath, Size: size, Err: err}
	}
}

func scanPulseCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return scanPulseMsg{}
	})
}

func boolLabel(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func validateDeletePath(relPath string) (string, error) {
	if relPath == "" {
		return "", errors.New("delete: empty path")
	}
	cleaned := filepath.Clean(relPath)
	if cleaned == "." || cleaned == string(os.PathSeparator) {
		return "", errors.New("delete: refusing to delete root")
	}
	if filepath.IsAbs(cleaned) {
		return "", errors.New("delete: absolute paths are not allowed")
	}
	return cleaned, nil
}
