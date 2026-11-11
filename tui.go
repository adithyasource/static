package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

var (
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"})
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#02BA84", Dark: "#02BF87"})
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#FF4672", Dark: "#ED567A"})
	docStyle     = lipgloss.NewStyle().Margin(1, 2)
	frameStyle   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"}).
			Padding(1, 2)
)

type tuiModel struct {
	width, height int
	screen        screen

	menu           list.Model
	playlists      list.Model
	playlistsReady bool
	input          textinput.Model
	spinner        spinner.Model
	progress       progress.Model

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
	input.PromptStyle = subtleStyle
	input.Cursor.Style = accentStyle

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = accentStyle

	return tuiModel{
		screen:   screenMenu,
		menu:     newMenuList(80, 20),
		input:    input,
		spinner:  spin,
		progress: newProgress(40),
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
		if msg.total > 0 {
			return m, m.progress.SetPercent(float64(msg.done) / float64(msg.total))
		}
		return m, nil

	case progress.FrameMsg:
		updated, cmd := m.progress.Update(msg)
		m.progress = updated.(progress.Model)
		return m, cmd

	case spinner.TickMsg:
		if m.screen != screenRun || m.runDone {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

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
	m.progress = newProgress(clamp(m.width-16, 20, 60))

	cmds := []tea.Cmd{m.spinner.Tick}
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
	m.progress = newProgress(clamp(width-16, 20, 60))
	if m.progressTotal > 0 {
		m.progress.SetPercent(float64(m.progressDone) / float64(m.progressTotal))
	}
}

func (m tuiModel) frame(content string) string {
	return docStyle.Render(frameStyle.Width(clamp(m.width-6, 30, 200)).Render(content))
}

func (m tuiModel) contentWidth() int   { return clamp(m.width-10, 30, 190) }
func (m tuiModel) menuHeight() int     { return clamp(m.height-12, 8, 60) }
func (m tuiModel) playlistHeight() int { return clamp(m.height-9, 8, 60) }

func (m tuiModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	switch m.screen {
	case screenMenu:
		header := accentStyle.Render(strings.Trim(banner, "\n"))
		return m.frame(header + "\n\n" + m.menu.View())

	case screenInput:
		body := accentStyle.Render(m.inputTitle) + "\n\n" + m.input.View()
		if m.inputHint != "" {
			body += "\n\n" + subtleStyle.Render(m.inputHint)
		}
		if m.inputErr != "" {
			body += "\n\n" + errStyle.Render(m.inputErr)
		}
		body += "\n\n" + subtleStyle.Render("enter confirm • esc back")
		return m.frame(body)

	case screenConfirm:
		body := accentStyle.Render(m.confirmTitle) + "\n\n" + m.confirmBody + "\n\n" + subtleStyle.Render("y/enter yes • n/esc no")
		return m.frame(body)

	case screenPlaylists:
		return m.frame(m.playlists.View())

	case screenRun:
		return m.frame(m.runView())
	}

	return ""
}

func (m tuiModel) runView() string {
	var b strings.Builder

	if !m.runDone {
		b.WriteString(m.spinner.View() + " " + accentStyle.Render(m.runTitle))
	} else if m.runErr != nil {
		b.WriteString(errStyle.Render("✖ " + m.runTitle))
	} else {
		b.WriteString(successStyle.Render("✔ " + m.runTitle))
	}

	b.WriteString("\n\n")

	if m.progressTotal > 0 {
		b.WriteString(m.progress.View())
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render(fmt.Sprintf("%d/%d", m.progressDone, m.progressTotal)))
		b.WriteString("\n\n")
	}

	width := clamp(m.width-14, 20, 100)
	for _, line := range m.runLines(width, clamp(m.height-14, 3, 20)) {
		b.WriteString(subtleStyle.Render(line))
		b.WriteString("\n")
	}

	if m.runDone {
		b.WriteString("\n" + subtleStyle.Render("enter back • ctrl+c quit"))
	} else {
		b.WriteString("\n" + subtleStyle.Render("ctrl+c quit"))
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
	l := list.New(buildMenuItems(), list.NewDefaultDelegate(), width, height)
	l.Title = "main menu"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))}
	}
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

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false

	l := list.New(items, delegate, width, height)
	l.Title = "choose playlists"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	}
	return l
}

func newProgress(width int) progress.Model {
	return progress.New(progress.WithDefaultGradient(), progress.WithWidth(clamp(width, 20, 100)))
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
