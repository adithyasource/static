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

var (
	tuiProgram *tea.Program
	tuiActive  bool
)

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
	desc   string
	action menuAction
}

func (m menuEntry) Title() string       { return m.title }
func (m menuEntry) Description() string { return m.desc }
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
	tuiActive = true
	cliAutoReconnect = true
	defer func() {
		tuiActive = false
		tuiProgram = nil
	}()

	program := tea.NewProgram(newTUIModel(), tea.WithAltScreen())
	tuiProgram = program
	_, err := program.Run()
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
		header := strings.Trim(banner, "\n")
		return m.frame(header + "\n\n" + "main menu" + "\n\n" + m.menu.View() + "\n" + "↑/↓ move • enter select • ctrl+c quit")

	case screenInput:
		body := m.inputTitle + "\n\n" + m.input.View()
		if m.inputHint != "" {
			body += "\n\n" + m.inputHint
		}
		if m.inputErr != "" {
			body += "\n\n" + m.inputErr
		}
		body += "\n\n" + "enter confirm • esc back"
		return m.frame(body)

	case screenConfirm:
		body := m.confirmTitle + "\n\n" + m.confirmBody + "\n\n" + "y/enter yes • n/esc no"
		return m.frame(body)

	case screenPlaylists:
		return m.frame("choose playlists" + "\n\n" + m.playlists.View() + "\n" + "space toggle • enter save • esc back")

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
				menuEntry{"sync", "download and tag every unsynced playlist", actSync},
				menuEntry{"reset snapshot ids (force sync)", "make the next sync fetch everything again", actReset},
			)
		}
		items = append(items, menuEntry{"choose playlists to sync", "pick which playlists to keep offline", actPlaylists})
	}

	items = append(items,
		menuEntry{fmt.Sprintf("choose sync folder [%s]", display(cfg.SyncFolder)), "where the playlist folders live", actFolder},
		menuEntry{fmt.Sprintf("connect spotify account [%s]", display(cfg.UserName)), "run the spotify login flow", actConnect},
	)

	if cfg.SyncFolder != "" {
		items = append(items, menuEntry{"open sync folder", "show it in your file manager", actOpenSync})
	}

	items = append(items,
		menuEntry{"open data folder", "config and logs", actOpenData},
		menuEntry{"built by adithya.zip", "visit the website", actWebsite},
		menuEntry{"coffee?", "buy adithya a coffee", actCoffee},
	)

	return items
}

func newMenuList(width, height int) list.Model {
	l := list.New(buildMenuItems(), plainDelegate{showDescription: true, height: 2, spacing: 1}, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	return l
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

	delegate := plainDelegate{showDescription: false, height: 1, spacing: 1}

	l := list.New(items, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	return l
}

// plainDelegate renders list items without any styling or color.
type plainDelegate struct {
	showDescription bool
	height          int
	spacing         int
}

func (d plainDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	var title, desc string
	switch it := item.(type) {
	case menuEntry:
		title, desc = it.Title(), it.Description()
	case playlistEntry:
		title, desc = it.Title(), it.Description()
	default:
		return
	}

	prefix := "  "
	if index == m.Index() {
		prefix = "> "
	}

	width := m.Width() - 2
	if width < 1 {
		width = 1
	}

	if d.showDescription {
		descWidth := width - 2
		if descWidth < 1 {
			descWidth = 1
		}
		fmt.Fprintf(w, "%s\n  %s", truncate(prefix+title, width), truncate(desc, descWidth)) //nolint:errcheck
		return
	}
	fmt.Fprint(w, truncate(prefix+title, width)) //nolint:errcheck
}

func (d plainDelegate) Height() int  { return d.height }
func (d plainDelegate) Spacing() int { return d.spacing }
func (d plainDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

func progressBar(done, total, width int) string {
	if total <= 0 || width < 3 {
		return ""
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
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
