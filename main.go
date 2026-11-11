package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	stdlog "log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const version = "v1.1.4"

const banner = `
   ▗   ▗ ▘  
 ▛▘▜▘▀▌▜▘▌▛▘
 ▄▌▐▖█▌▐▖▌▙▖  ` + version + `
`

var (
	stdin     = bufio.NewReader(os.Stdin)
	appFolder string
	cfg       Config
	logger    *stdlog.Logger
	logFile   *os.File

	cliMode          bool // true when a subcommand was given
	cliAutoReconnect bool // set by --reconnect-if-disconnected
)

var numberedPrefix = regexp.MustCompile(`^\d{3}\. `)

// Config is the same shape as the old python data.json.
type Config struct {
	SyncFolder        string                    `json:"syncFolder"`
	AccessToken       string                    `json:"accessToken"`
	UserName          string                    `json:"userName"`
	UserID            string                    `json:"userId,omitempty"`
	ClientID          string                    `json:"clientId"`
	ClientSecret      string                    `json:"clientSecret"`
	SelectedPlaylists []map[string]PlaylistMeta `json:"selectedPlaylists"`
}

type PlaylistMeta struct {
	Name       string `json:"name"`
	SnapshotID string `json:"snapshotId"`
}

type menuItem struct {
	label  string
	action func()
}

func main() {
	appFolder = defaultAppFolder()
	if err := os.MkdirAll(appFolder, 0o755); err != nil {
		fmt.Println("could not create data folder:", err)
		return
	}
	initLog()
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()
	cliMode = len(os.Args) > 1
	if !cliMode {
		logf("starting")
	}

	cfg = loadConfig()

	if cliMode {
		runCLI(os.Args[1:])
		return
	}

	for {
		clearScreen()
		cfg = loadConfig()

		var items []menuItem
		if cfg.UserName != "" {
			if len(cfg.SelectedPlaylists) > 0 {
				items = append(items,
					menuItem{"sync", func() { syncPlaylists(true) }},
					menuItem{"reset snapshot ids (force sync)", resetSnapshotIDs},
				)
			}
			items = append(items, menuItem{"choose playlists to sync", func() { selectPlaylists(false, nil, true) }})
		}
		items = append(items,
			menuItem{fmt.Sprintf("choose sync folder [%s]", display(cfg.SyncFolder)), chooseSyncFolder},
			menuItem{fmt.Sprintf("connect spotify account [%s]", display(cfg.UserName)), func() { setupUser(false, true) }},
		)
		if cfg.SyncFolder != "" {
			items = append(items, menuItem{"open sync folder", func() { openFolder(cfg.SyncFolder) }})
		}
		items = append(items,
			menuItem{"open data folder", func() { openFolder(appFolder) }},
			menuItem{"built by adithya.zip", func() { openURL("https://adithya.zip") }},
			menuItem{"coffee?", func() { openURL("https://ko-fi.com/adithyasource") }},
		)

		fmt.Println("hey there!")
		for i, item := range items {
			fmt.Printf("%d. %s\n", i+1, item.label)
		}
		fmt.Print("> ")

		line, ok := readLine()
		if !ok {
			return
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(items) {
			continue
		}
		items[n-1].action()
		pause()
	}
}

// --- cli --------------------------------------------------------------------

func runCLI(args []string) {
	switch args[0] {
	case "help", "--help", "-h":
		printHelp()

	case "sync":
		fs := flag.NewFlagSet("sync", flag.ExitOnError)
		reconnect := fs.Bool("reconnect-if-disconnected", false, "reconnect automatically if the spotify token expired")
		openBrowser := fs.Bool("auto-open-browser-link", false, "open the spotify login link in your browser")
		fs.Parse(args[1:])
		cliAutoReconnect = *reconnect
		syncPlaylists(*openBrowser)

	case "reset-snapshots", "reset":
		resetSnapshotIDs()

	case "playlists":
		fs := flag.NewFlagSet("playlists", flag.ExitOnError)
		listOnly := fs.Bool("list", false, "only list the playlists, don't change the selection")
		reconnect := fs.Bool("reconnect-if-disconnected", false, "reconnect automatically if the spotify token expired")
		openBrowser := fs.Bool("auto-open-browser-link", false, "open the spotify login link in your browser")
		fs.Parse(args[1:])
		cliAutoReconnect = *reconnect
		selectPlaylists(*listOnly, fs.Args(), *openBrowser)

	case "folder":
		fs := flag.NewFlagSet("folder", flag.ExitOnError)
		fs.Parse(args[1:])
		if fs.NArg() > 0 {
			setSyncFolder(fs.Arg(0))
		} else {
			say("sync folder: %s", display(cfg.SyncFolder))
		}

	case "connect":
		fs := flag.NewFlagSet("connect", flag.ExitOnError)
		clientID := fs.String("client-id", "", "spotify client id")
		clientSecret := fs.String("client-secret", "", "spotify client secret")
		openBrowser := fs.Bool("auto-open-browser-link", false, "open the spotify login link in your browser")
		fs.Parse(args[1:])
		connectSpotify(*clientID, *clientSecret, *openBrowser)

	case "open":
		target := "sync"
		if len(args) > 1 {
			target = args[1]
		}
		openTarget(target)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`static %s - a local copy of your spotify, statically stored

usage:
  static                          open the interactive menu
  static help                     show this help
  static sync [flags]             sync all selected playlists
  static reset-snapshots          force every playlist to sync again
  static playlists [flags] [n...] choose playlists (numbers from the list)
  static folder [path]            show or set the sync folder
  static connect [flags]          connect your spotify account
  static open [sync|data]         open the sync folder or the data folder

flags:
  sync, playlists
    --reconnect-if-disconnected   reconnect automatically when the token expired
    --auto-open-browser-link      open the spotify login link in your browser
  playlists
    --list                        only list playlists, don't change the selection
  connect
    --client-id <id>              spotify client id
    --client-secret <secret>      spotify client secret
    --auto-open-browser-link      open the spotify login link in your browser

examples:
  static sync
  static sync --reconnect-if-disconnected --auto-open-browser-link
  static playlists --list
  static playlists 1 3 4
  static folder ~/Music/spotify
  static connect --auto-open-browser-link

config: %s
log:    %s
`, version, filepath.Join(appFolder, "data.json"), filepath.Join(appFolder, "static.log"))
}

func connectSpotify(clientID, clientSecret string, openBrowser bool) {
	if (clientID == "") != (clientSecret == "") {
		say("provide both --client-id and --client-secret")
		return
	}

	if clientID != "" {
		cfg.ClientID = clientID
		cfg.ClientSecret = clientSecret
		saveConfig()
	}

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		setupClient(false)
		if cfg.ClientID == "" || cfg.ClientSecret == "" {
			return
		}
	}

	setupUser(true, openBrowser)
}

func setSyncFolder(path string) {
	path = expandHome(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		say("not an existing directory: %s", path)
		return
	}

	cfg.SyncFolder = path
	saveConfig()
	say("sync folder set to %s", path)
}

func openTarget(target string) {
	switch target {
	case "sync":
		if cfg.SyncFolder == "" {
			say("no sync folder is set")
			return
		}
		openFolder(cfg.SyncFolder)
	case "data":
		openFolder(appFolder)
	default:
		say("unknown target %q (use sync or data)", target)
	}
}

// --- config -----------------------------------------------------------------

func loadConfig() Config {
	var c Config
	data, err := os.ReadFile(filepath.Join(appFolder, "data.json"))
	if err != nil {
		c.SelectedPlaylists = []map[string]PlaylistMeta{}
		writeConfig(c)
		return c
	}
	if err := json.Unmarshal(data, &c); err != nil {
		logf("could not parse data.json: %v", err)
	}
	if c.SelectedPlaylists == nil {
		c.SelectedPlaylists = []map[string]PlaylistMeta{}
	}
	return c
}

func saveConfig() {
	writeConfig(cfg)
}

func writeConfig(c Config) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		logf("could not encode config: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(appFolder, "data.json"), append(data, '\n'), 0o644); err != nil {
		logf("could not write data.json: %v", err)
	}
}

func resetSnapshotIDs() {
	for _, playlist := range cfg.SelectedPlaylists {
		for id, meta := range playlist {
			meta.SnapshotID = ""
			playlist[id] = meta
		}
	}
	saveConfig()
	say("snapshot ids reset, the next sync will download every playlist again")
}

func chooseSyncFolder() {
	clearScreen()

	if cfg.SyncFolder != "" {
		if !confirm("are you sure you want to override the already selected folder?") {
			return
		}
		clearScreen()
	}

	choice := expandHome(ask("enter spotify sync folder path: "))
	if choice == "" {
		return
	}

	entries, err := os.ReadDir(choice)
	if err != nil {
		say("could not read that folder: %v", err)
		return
	}

	if len(entries) != 0 {
		if !confirm("are you sure you want to overwrite the contents of the selected folder? (all existing data will be deleted)") {
			return
		}
	}

	cfg.SyncFolder = choice
	saveConfig()
}

// --- small helpers ----------------------------------------------------------

func defaultAppFolder() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "static")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "static")
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "static")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "static")
	}
}

func initLog() {
	f, err := os.OpenFile(filepath.Join(appFolder, "static.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	logFile = f
	logger = stdlog.New(io.MultiWriter(os.Stderr, f), "", stdlog.LstdFlags)
}

func logf(format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

func say(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

func clearScreen() {
	if cliMode {
		return
	}
	fmt.Print("\033[H\033[2J\033[3J")
	fmt.Print(banner)
}

func display(value string) string {
	if value == "" {
		return "None"
	}
	return value
}

func ask(question string) string {
	answer, _ := askOK(question)
	return answer
}

func askOK(question string) (string, bool) {
	fmt.Print(question)
	return readLine()
}

func confirm(question string) bool {
	answer := ask(question + " [y/N] ")
	return strings.HasPrefix(strings.ToLower(answer), "y")
}

func readLine() (string, bool) {
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimSpace(line), true
}

func pause() {
	fmt.Print("\npress enter to return to the menu")
	stdin.ReadString('\n')
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

func randomString() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, 10)
	for i := range out {
		out[i] = chars[rand.IntN(len(chars))]
	}
	return string(out)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// cookieFile returns the first cookies.txt found in the data folder or cwd.
func cookieFile() string {
	for _, path := range []string{filepath.Join(appFolder, "cookies.txt"), "cookies.txt"} {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"<", "", ">", "", ":", "", `"`, "", "/", "", `\`, "", "|", "", "?", "", "*", "",
	)
	name = replacer.Replace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	if len(name) > 200 {
		name = name[:200]
	}
	if name == "" {
		name = "unknown"
	}
	return name
}

func isJunkFile(name string) bool {
	return name == ".DS_Store" ||
		strings.HasSuffix(name, ".part") ||
		strings.HasSuffix(name, ".ytdl") ||
		strings.HasSuffix(name, ".jpg")
}

func openFolder(path string) {
	path = expandHome(path)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		logf("could not open %s: %v", path, err)
	}
}

func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		logf("could not open %s: %v", url, err)
	}
}

func tail(text string, lines int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	parts := strings.Split(text, "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

func progress(done, total int) {
	if total > 0 {
		fmt.Printf("\r(%d/%d)", done, total)
	} else {
		fmt.Printf("\r(%d)", done)
	}
}

func progressDone() {
	fmt.Println()
}
