package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var tuiProgram *tea.Program

// messages sent from the worker goroutines into the tui
type (
	tuiLogMsg          string
	tuiProgressMsg     struct{ done, total int }
	playlistsLoadedMsg struct {
		playlists []Playlist
		err       error
	}
	runFinishedMsg struct{ err error }
	spinTickMsg    struct{}
)

type screen int

const (
	screenMenu screen = iota
	screenInput
	screenConfirm
	screenPlaylists
	screenRun
)

type menuAction int

const (
	actSync menuAction = iota
	actReset
	actPlaylists
	actFolder
	actConnect
	actOpenSync
	actOpenData
	actWebsite
	actCoffee
)

type menuEntry struct {
	title  string
	action menuAction
}

func (m menuEntry) Title() string       { return m.title }
func (m menuEntry) FilterValue() string { return m.title }

type playlistEntry struct {
	playlist Playlist
	selected bool
}

func (p playlistEntry) Title() string {
	mark := "[ ]"
	if p.selected {
		mark = "[x]"
	}
	return fmt.Sprintf("%s %s [%d]", mark, p.playlist.Name, p.playlist.Total())
}
func (p playlistEntry) Description() string { return "" }
func (p playlistEntry) FilterValue() string { return p.playlist.Name }

type inputKind int

const (
	inputFolder inputKind = iota
	inputClientID
	inputClientSecret
)

type tuiModel struct {
	width, height int
	screen        screen

	menu           list.Model
	playlists      list.Model
	playlistsReady bool
	input          textinput.Model
	spinFrame      int

	inputKind  inputKind
	inputTitle string
	inputHint  string
	inputErr   string

	pendingClientID string
	pendingFolder   string

	confirmTitle string
	confirmBody  string

	runTitle      string
	runLog        []string
	runDone       bool
	runErr        error
	progressDone  int
	progressTotal int
}

func runTUI() error {
	autoReconnect = true

	program := tea.NewProgram(newTUIModel(), tea.WithAltScreen())
	tuiProgram = program
	_, err := program.Run()
	tuiProgram = nil
	return err
}

func newTUIModel() tuiModel {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 500
	input.Width = 60

	return tuiModel{
		screen: screenMenu,
		menu:   newMenuList(80, 20),
		input:  input,
	}
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tuiLogMsg:
		m.runLog = append(m.runLog, string(msg))
		if len(m.runLog) > 500 {
			m.runLog = m.runLog[len(m.runLog)-500:]
		}
		return m, nil

	case tuiProgressMsg:
		m.progressDone, m.progressTotal = msg.done, msg.total
		return m, nil

	case spinTickMsg:
		if m.screen != screenRun || m.runDone {
			return m, nil
		}
		m.spinFrame++
		return m, spinTick()

	case playlistsLoadedMsg:
		if msg.err != nil {
			m.runDone = true
			m.runErr = msg.err
			return m, nil
		}
		if len(msg.playlists) == 0 {
			m.runDone = true
			m.runErr = fmt.Errorf("no playlists found on this account")
			return m, nil
		}
		m.playlists = newPlaylistList(msg.playlists, m.contentWidth(), m.playlistHeight())
		m.playlistsReady = true
		m.screen = screenPlaylists
		return m, nil

	case runFinishedMsg:
		m.runDone = true
		m.runErr = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	switch m.screen {
	case screenMenu:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		return m, cmd
	case screenPlaylists:
		var cmd tea.Cmd
		m.playlists, cmd = m.playlists.Update(msg)
		return m, cmd
	case screenInput:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.screen {
	case screenMenu:
		if msg.String() == "enter" {
			return m.activateMenu()
		}
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		return m, cmd

	case screenInput:
		switch msg.String() {
		case "esc":
			m.screen = screenMenu
			return m, nil
		case "enter":
			return m.submitInput()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case screenConfirm:
		switch msg.String() {
		case "y", "Y", "enter":
			m.confirmYes()
		case "n", "N", "esc", "q":
			m.screen = screenInput
		}
		return m, nil

	case screenPlaylists:
		switch msg.String() {
		case "esc", "q":
			m.screen = screenMenu
			return m, nil
		case " ":
			if entry, ok := m.playlists.SelectedItem().(playlistEntry); ok {
				entry.selected = !entry.selected
				cmd := m.playlists.SetItem(m.playlists.Index(), entry)
				return m, cmd
			}
			return m, nil
		case "enter":
			m.savePlaylistSelection()
			m.screen = screenMenu
			m.refreshMenu()
			return m, nil
		}
		var cmd tea.Cmd
		m.playlists, cmd = m.playlists.Update(msg)
		return m, cmd

	case screenRun:
		if m.runDone && (msg.String() == "enter" || msg.String() == "esc" || msg.String() == "q" || msg.String() == " ") {
			m.screen = screenMenu
			m.refreshMenu()
		}
		return m, nil
	}

	return m, nil
}

func (m tuiModel) activateMenu() (tea.Model, tea.Cmd) {
	entry, ok := m.menu.SelectedItem().(menuEntry)
	if !ok {
		return m, nil
	}

	switch entry.action {
	case actSync:
		return m.startRun("syncing playlists", func() error {
			syncPlaylists(true)
			return nil
		})

	case actReset:
		return m.startRun("resetting snapshots", func() error {
			resetSnapshotIDs()
			return nil
		})

	case actPlaylists:
		model, cmd := m.startRun("loading playlists", nil)
		return model, tea.Batch(cmd, func() tea.Msg { return loadPlaylists() })

	case actFolder:
		m.inputKind = inputFolder
		m.inputTitle = "sync folder"
		m.inputHint = "where your playlist folders are stored"
		m.inputErr = ""
		m.input.EchoMode = textinput.EchoNormal
		m.input.SetValue(cfg.SyncFolder)
		m.input.CursorEnd()
		m.screen = screenInput
		return m, m.input.Focus()

	case actConnect:
		if cfg.ClientID == "" || cfg.ClientSecret == "" {
			m.inputKind = inputClientID
			m.inputTitle = "spotify client id"
			m.inputHint = "create an app at developer.spotify.com\nuse http://127.0.0.1:9321 as the redirect uri"
			m.inputErr = ""
			m.input.EchoMode = textinput.EchoNormal
			m.input.SetValue(cfg.ClientID)
			m.input.CursorEnd()
			m.screen = screenInput
			return m, m.input.Focus()
		}
		return m.startAuthRun()

	case actOpenSync:
		openFolder(cfg.SyncFolder)
		return m, nil

	case actOpenData:
		openFolder(appFolder)
		return m, nil

	case actWebsite:
		openURL("https://adithya.zip")
		return m, nil

	case actCoffee:
		openURL("https://ko-fi.com/adithyasource")
		return m, nil
	}

	return m, nil
}

func (m tuiModel) submitInput() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())

	switch m.inputKind {
	case inputFolder:
		path := expandHome(value)
		if path == "" {
			m.inputErr = "enter a folder path"
			return m, nil
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			m.inputErr = "not an existing directory"
			return m, nil
		}
		if path != cfg.SyncFolder {
			entries, _ := os.ReadDir(path)
			if len(entries) > 0 {
				m.pendingFolder = path
				m.confirmTitle = "use this folder anyway?"
				m.confirmBody = path + "\n\nit is not empty, and static manages everything inside the sync folder."
				m.screen = screenConfirm
				return m, nil
			}
		}
		cfg.SyncFolder = path
		saveConfig()
		m.screen = screenMenu
		m.refreshMenu()
		return m, nil

	case inputClientID:
		if value == "" {
			m.inputErr = "client id can't be empty"
			return m, nil
		}
		m.pendingClientID = value
		m.inputKind = inputClientSecret
		m.inputTitle = "spotify client secret"
		m.inputHint = "from the same spotify app dashboard"
		m.inputErr = ""
		m.input.EchoMode = textinput.EchoPassword
		m.input.SetValue("")
		m.input.CursorEnd()
		return m, textinput.Blink

	case inputClientSecret:
		if value == "" {
			m.inputErr = "client secret can't be empty"
			return m, nil
		}
		cfg.ClientID = m.pendingClientID
		cfg.ClientSecret = value
		saveConfig()
		return m.startAuthRun()
	}

	return m, nil
}

func (m *tuiModel) confirmYes() {
	if m.pendingFolder != "" {
		cfg.SyncFolder = m.pendingFolder
		m.pendingFolder = ""
		saveConfig()
	}
	m.screen = screenMenu
	m.refreshMenu()
}

func (m *tuiModel) savePlaylistSelection() {
	var picked []Playlist
	for _, item := range m.playlists.Items() {
		if entry, ok := item.(playlistEntry); ok && entry.selected {
			picked = append(picked, entry.playlist)
		}
	}
	storePlaylistSelection(picked)
}

func (m tuiModel) startAuthRun() (tea.Model, tea.Cmd) {
	return m.startRun("connecting spotify", func() error {
		setupUser(true, true)
		return nil
	})
}

func (m tuiModel) startRun(title string, work func() error) (tea.Model, tea.Cmd) {
	m.screen = screenRun
	m.runTitle = title
	m.runDone = false
	m.runErr = nil
	m.runLog = nil
	m.progressDone, m.progressTotal = 0, 0

	cmds := []tea.Cmd{spinTick()}
	if work != nil {
		cmds = append(cmds, func() tea.Msg {
			return runFinishedMsg{err: work()}
		})
	}
	return m, tea.Batch(cmds...)
}

func (m *tuiModel) refreshMenu() {
	m.menu.SetItems(buildMenuItems())
}

func (m *tuiModel) resize(width, height int) {
	m.width, m.height = width, height
	m.menu.SetSize(m.contentWidth(), m.menuHeight())
	if m.playlistsReady {
		m.playlists.SetSize(m.contentWidth(), m.playlistHeight())
	}
	m.input.Width = clamp(width-14, 20, 80)
}

// frame draws a simple rounded box with one line of vertical padding and
// two columns of horizontal padding, surrounded by a small margin.
func (m tuiModel) frame(content string) string {
	inner := m.contentWidth()
	hbar := strings.Repeat("─", inner+4)
	blank := "  │" + strings.Repeat(" ", inner+4) + "│"

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  ╭" + hbar + "╮\n")
	b.WriteString(blank + "\n")
	for _, line := range strings.Split(content, "\n") {
		b.WriteString("  │  " + padRight(line, inner) + "  │\n")
	}
	b.WriteString(blank + "\n")
	b.WriteString("  ╰" + hbar + "╯\n")
	return b.String()
}

func (m tuiModel) contentWidth() int   { return clamp(m.width-10, 30, 190) }
func (m tuiModel) menuHeight() int     { return clamp(m.height-16, 4, 60) }
func (m tuiModel) playlistHeight() int { return clamp(m.height-9, 8, 60) }

func (m tuiModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	switch m.screen {
	case screenMenu:
		return m.frame(strings.Trim(banner, "\n") + "\n\n" + m.menu.View() + "\n↑/↓ move • enter select • ctrl+c quit")

	case screenInput:
		body := m.inputTitle + "\n\n" + m.input.View()
		if m.inputHint != "" {
			body += "\n\n" + m.inputHint
		}
		if m.inputErr != "" {
			body += "\n\n" + m.inputErr
		}
		body += "\n\nenter confirm • esc back"
		return m.frame(body)

	case screenConfirm:
		body := m.confirmTitle + "\n\n" + m.confirmBody + "\n\ny/enter yes • n/esc no"
		return m.frame(body)

	case screenPlaylists:
		return m.frame("choose playlists\n\n" + m.playlists.View() + "\nspace toggle • enter save • esc back")

	case screenRun:
		return m.frame(m.runView())
	}

	return ""
}

func (m tuiModel) runView() string {
	var b strings.Builder

	if !m.runDone {
		b.WriteString(spinFrames[m.spinFrame%len(spinFrames)] + " " + m.runTitle)
	} else if m.runErr != nil {
		b.WriteString("✖ " + m.runTitle)
	} else {
		b.WriteString("✔ " + m.runTitle)
	}

	b.WriteString("\n\n")

	if m.progressTotal > 0 {
		b.WriteString(progressBar(m.progressDone, m.progressTotal, clamp(m.width-16, 10, m.contentWidth()-2)))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%d/%d", m.progressDone, m.progressTotal))
		b.WriteString("\n\n")
	}

	width := clamp(m.width-14, 20, 100)
	for _, line := range m.runLines(width, clamp(m.height-14, 3, 20)) {
		b.WriteString(line)
		b.WriteString("\n")
	}

	if m.runDone {
		b.WriteString("\n" + "enter back • ctrl+c quit")
	} else {
		b.WriteString("\n" + "ctrl+c quit")
	}

	return b.String()
}

// runLines wraps the log and returns the last maxLines lines that fit on screen.
func (m tuiModel) runLines(width, maxLines int) []string {
	var lines []string
	for _, raw := range m.runLog {
		lines = append(lines, wrapText(raw, width)...)
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

// --- component construction -------------------------------------------------

func buildMenuItems() []list.Item {
	var items []list.Item

	if cfg.UserName != "" {
		if len(cfg.SelectedPlaylists) > 0 {
			items = append(items,
				menuEntry{"sync", actSync},
				menuEntry{"reset snapshot ids (force sync)", actReset},
			)
		}
		items = append(items, menuEntry{"choose playlists to sync", actPlaylists})
	}

	items = append(items,
		menuEntry{fmt.Sprintf("choose sync folder [%s]", display(cfg.SyncFolder)), actFolder},
		menuEntry{fmt.Sprintf("connect spotify account [%s]", display(cfg.UserName)), actConnect},
	)

	if cfg.SyncFolder != "" {
		items = append(items, menuEntry{"open sync folder", actOpenSync})
	}

	items = append(items,
		menuEntry{"open data folder", actOpenData},
		menuEntry{"built by adithya.zip", actWebsite},
		menuEntry{"coffee?", actCoffee},
	)

	return items
}

func newMenuList(width, height int) list.Model {
	return newPlainList(buildMenuItems(), plainDelegate{}, width, height)
}

func newPlaylistList(playlists []Playlist, width, height int) list.Model {
	selected := map[string]bool{}
	for _, entry := range cfg.SelectedPlaylists {
		for id := range entry {
			selected[id] = true
		}
	}

	items := make([]list.Item, 0, len(playlists))
	for _, playlist := range playlists {
		items = append(items, playlistEntry{playlist: playlist, selected: selected[playlist.ID]})
	}

	l := newPlainList(items, plainDelegate{}, width, height)
	l.DisableQuitKeybindings()
	return l
}

// newPlainList creates a list without titles, status bars, pagination or help.
func newPlainList(items []list.Item, delegate list.ItemDelegate, width, height int) list.Model {
	l := list.New(items, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	return l
}

// plainDelegate renders list items without any styling or color.
type plainDelegate struct{}

func (d plainDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	var title string
	switch it := item.(type) {
	case menuEntry:
		title = it.Title()
	case playlistEntry:
		title = it.Title()
	default:
		return
	}

	prefix := "  "
	if index == m.Index() {
		prefix = "> "
	}

	width := m.Width() - 2
	width = max(width, 1)

	fmt.Fprint(w, truncate(prefix+title, width)) //nolint:errcheck
}

func (d plainDelegate) Height() int {
	return 1
}

func (d plainDelegate) Spacing() int { return 1 }

func (d plainDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

func progressBar(done, total, width int) string {
	if total <= 0 || width < 3 {
		return ""
	}
	filled := done * width / total
	filled = min(filled, width)
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func padRight(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width])
	}
	return s
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinTickMsg{} })
}

// --- small helpers ----------------------------------------------------------

func loadPlaylists() tea.Msg {
	playlists, status, err := getPlaylists()
	if status == 401 {
		if maybeReconnect(true) {
			playlists, status, err = getPlaylists()
		} else if err == nil {
			err = fmt.Errorf("spotify account is disconnected")
		}
	}
	return playlistsLoadedMsg{playlists: playlists, err: err}
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func wrapText(text string, width int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			out = append(out, "")
			continue
		}
		for len(runes) > width {
			cut := width
			for i := width - 1; i > 0; i-- {
				if runes[i] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, string(runes[:cut]))
			runes = []rune(strings.TrimLeft(string(runes[cut:]), " "))
		}
		out = append(out, string(runes))
	}
	return out
}
