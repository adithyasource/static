package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bogem/id3v2/v2"
)

const (
	spotifyAPI   = "https://api.spotify.com/v1"
	redirectURI  = "http://127.0.0.1:9321"
	spotifyScope = "user-read-private user-read-email playlist-read-private"
)

var httpClient = &http.Client{Timeout: 60 * time.Second}

// --- spotify api types ------------------------------------------------------

type Artist struct {
	Name string `json:"name"`
}

type Image struct {
	URL string `json:"url"`
}

type Album struct {
	Name        string   `json:"name"`
	Artists     []Artist `json:"artists"`
	Images      []Image  `json:"images"`
	ReleaseDate string   `json:"release_date"`
}

type Track struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Artists     []Artist `json:"artists"`
	Album       Album    `json:"album"`
	Explicit    bool     `json:"explicit"`
	DiscNumber  int      `json:"disc_number"`
	TrackNumber int      `json:"track_number"`
}

type TrackTotal struct {
	Total int `json:"total"`
}

type Playlist struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	SnapshotID string     `json:"snapshot_id"`
	Items      TrackTotal `json:"items"`
	Tracks     TrackTotal `json:"tracks"`
}

func (p Playlist) Total() int {
	if p.Items.Total > 0 {
		return p.Items.Total
	}
	return p.Tracks.Total
}

type PlaylistsPage struct {
	Items []Playlist `json:"items"`
	Next  string     `json:"next"`
}

type PlaylistItem struct {
	Item  *Track `json:"item"`
	Track *Track `json:"track"`
}

func (pi PlaylistItem) track() *Track {
	if pi.Item != nil {
		return pi.Item
	}
	return pi.Track
}

type PlaylistItemsPage struct {
	Items []PlaylistItem `json:"items"`
	Next  string         `json:"next"`
}

// --- auth -------------------------------------------------------------------

func setupClient(preConfirm bool) bool {
	if !preConfirm {
		if cfg.ClientID != "" || cfg.ClientSecret != "" {
			if !confirm("do you want to overwrite the current client secrets?") {
				return false
			}
			clearScreen()
		}

		say("in order for static to access your spotify data, it needs a client id and secret associated to your account")
		say("")
		say("follow the given step to obtain them")
		say("- create a spotify developers account at https://developer.spotify.com")
		say("- go to the dashboard and create an app")
		say("- make sure to add the redirect uri as http://127.0.0.1:9321")
		say("")
		ask("press enter to continue")

		clearScreen()
		cfg.ClientID = ask("enter your spotify client id: ")
		clearScreen()
		cfg.ClientSecret = ask("enter your spotify client secret: ")
		clearScreen()
		saveConfig()
	}

	return cfg.ClientID != "" && cfg.ClientSecret != ""
}

func setupUser(preConfirm bool) {
	clearScreen()

	if !setupClient(preConfirm) {
		say("a spotify client id and secret are required")
		return
	}

	if !preConfirm && cfg.UserName != "" {
		if !confirm("are you sure you want to overwrite the already connected account?") {
			return
		}
	}

	say("opening your browser to connect spotify...")
	accessToken, err := createAccessToken()
	if err != nil {
		say("could not connect spotify: %v", err)
		return
	}
	cfg.AccessToken = accessToken

	var me struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	}
	if status, err := spotifyGet("/me", &me); err != nil {
		say("could not load your spotify profile (%d): %v", status, err)
		return
	}

	cfg.UserName = me.DisplayName
	cfg.UserID = me.ID
	saveConfig()

	say("connected as %s", display(cfg.UserName))
}

func createAccessToken() (string, error) {
	codeCh := make(chan string, 1)
	var server *http.Server

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		fmt.Fprintln(w, "you can close this window and head back to static!")

		if code := query.Get("code"); code != "" {
			select {
			case codeCh <- code:
			default:
			}
			if server != nil {
				go server.Shutdown(context.Background())
			}
			return
		}
		if query.Get("error") != "" {
			select {
			case codeCh <- "":
			default:
			}
			if server != nil {
				go server.Shutdown(context.Background())
			}
		}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:9321")
	if err != nil {
		return "", fmt.Errorf("could not start login listener: %w", err)
	}
	server = &http.Server{Handler: handler}
	go server.Serve(listener)

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", cfg.ClientID)
	params.Set("scope", spotifyScope)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", randomString())
	openURL("https://accounts.spotify.com/authorize?" + params.Encode())

	var code string
	select {
	case code = <-codeCh:
	case <-time.After(5 * time.Minute):
		server.Shutdown(context.Background())
		return "", errors.New("timed out waiting for the spotify login")
	}
	server.Shutdown(context.Background())

	if code == "" {
		return "", errors.New("spotify login was cancelled")
	}

	form := url.Values{}
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	response, err := httpClient.PostForm("https://accounts.spotify.com/api/token", form)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	var token struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("no access token returned (%s %s)", token.Error, token.Description)
	}
	return token.AccessToken, nil
}

// --- api helpers ------------------------------------------------------------

func spotifyGet(pathOrURL string, out any) (int, error) {
	requestURL := pathOrURL
	if !strings.HasPrefix(requestURL, "http://") && !strings.HasPrefix(requestURL, "https://") {
		requestURL = spotifyAPI + requestURL
	}

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)

	response, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("spotify returned %s: %s", response.Status, tail(string(body), 2))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

func fetchBytes(url string) ([]byte, error) {
	response, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %s", response.Status)
	}
	return io.ReadAll(response.Body)
}

// --- playlists --------------------------------------------------------------

func getPlaylists() ([]Playlist, int, error) {
	var page PlaylistsPage
	status, err := spotifyGet("/me/playlists?limit=50", &page)
	return page.Items, status, err
}

func selectPlaylists() {
	clearScreen()

	playlists, status, err := getPlaylists()
	if status == http.StatusUnauthorized {
		say("looks like your spotify account got disconnected (token expired)")
		if !confirm("do you want to reconnect?") {
			return
		}
		setupUser(true)
		clearScreen()
		playlists, status, err = getPlaylists()
	}
	if err != nil {
		say("could not load playlists: %v", err)
		return
	}
	if len(playlists) == 0 {
		say("no playlists found on this account")
		return
	}

	selected := map[string]PlaylistMeta{}
	for _, entry := range cfg.SelectedPlaylists {
		for id, meta := range entry {
			selected[id] = meta
		}
	}

	for i, playlist := range playlists {
		mark := " "
		if _, ok := selected[playlist.ID]; ok {
			mark = "x"
		}
		fmt.Printf("[%s] %2d. %s [%d]\n", mark, i+1, playlist.Name, playlist.Total())
	}
	say("")

	answer, ok := askOK("select the playlists you want to sync (numbers separated by spaces or commas, enter keeps current, q cancels): ")
	if !ok {
		return
	}
	if answer == "" || strings.EqualFold(answer, "q") {
		return
	}

	var picked []Playlist
	seen := map[int]bool{}
	for _, field := range strings.FieldsFunc(answer, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	}) {
		n, err := strconv.Atoi(field)
		if err != nil || n < 1 || n > len(playlists) || seen[n] {
			continue
		}
		seen[n] = true
		picked = append(picked, playlists[n-1])
	}
	if len(picked) == 0 {
		say("no valid playlists selected")
		return
	}

	var newSelection []map[string]PlaylistMeta
	for _, playlist := range picked {
		meta := PlaylistMeta{Name: playlist.Name}
		if old, ok := selected[playlist.ID]; ok {
			meta.SnapshotID = old.SnapshotID
		}
		newSelection = append(newSelection, map[string]PlaylistMeta{playlist.ID: meta})
	}

	cfg.SelectedPlaylists = newSelection
	saveConfig()
	say("saved %d playlist(s)", len(newSelection))
}

func selectedPlaylist(entry map[string]PlaylistMeta) (string, PlaylistMeta) {
	for id, meta := range entry {
		return id, meta
	}
	return "", PlaylistMeta{}
}

func getUnsyncedPlaylists() ([]Playlist, []Playlist) {
	if len(cfg.SelectedPlaylists) == 0 {
		return nil, nil
	}

	firstID, _ := selectedPlaylist(cfg.SelectedPlaylists[0])
	var check Playlist
	status, err := spotifyGet("/playlists/"+firstID, &check)
	if status == http.StatusUnauthorized {
		say("looks like your spotify account got disconnected (token expired)")
		if !confirm("do you want to reconnect?") {
			return nil, nil
		}
		setupUser(true)
		clearScreen()
	} else if err != nil {
		say("could not check playlists: %v", err)
		return nil, nil
	}

	clearScreen()
	say("checking for unsynced playlists")
	say("")

	var unsynced []Playlist
	var synced []Playlist
	for i, entry := range cfg.SelectedPlaylists {
		id, meta := selectedPlaylist(entry)

		var playlist Playlist
		if _, err := spotifyGet("/playlists/"+id, &playlist); err != nil {
			logf("could not load playlist %s: %v", id, err)
			// don't continue with an incomplete picture, we'd delete folders
			say("could not check every playlist, sync aborted")
			return nil, nil
		}

		if meta.SnapshotID == playlist.SnapshotID {
			synced = append(synced, playlist)
		} else {
			unsynced = append(unsynced, playlist)
		}
		progress(i+1, len(cfg.SelectedPlaylists))
	}
	progressDone()

	return unsynced, synced
}

func syncPlaylists() {
	clearScreen()

	if cfg.SyncFolder == "" {
		say("choose a sync folder first")
		return
	}
	if len(cfg.SelectedPlaylists) == 0 {
		say("choose some playlists to sync first")
		return
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		say("yt-dlp was not found in your PATH, install it first (https://github.com/yt-dlp/yt-dlp)")
		return
	}

	syncFolder := expandHome(cfg.SyncFolder)
	if err := os.MkdirAll(syncFolder, 0o755); err != nil {
		say("could not create the sync folder: %v", err)
		return
	}

	unsynced, synced := getUnsyncedPlaylists()
	if unsynced == nil && synced == nil {
		say("sync cancelled")
		return
	}

	finalPlaylists := map[string]bool{}

	for _, playlist := range unsynced {
		logf("downloading %s", playlist.Name)
		downloadPlaylist(playlist)
		finalPlaylists[playlist.Name] = true
	}
	for _, playlist := range synced {
		finalPlaylists[playlist.Name] = true
	}

	entries, err := os.ReadDir(syncFolder)
	if err != nil {
		say("could not read the sync folder: %v", err)
		return
	}
	for _, entry := range entries {
		if finalPlaylists[entry.Name()] {
			continue
		}
		path := filepath.Join(syncFolder, entry.Name())
		var err error
		if entry.IsDir() {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			logf("could not remove %s: %v", path, err)
		}
	}

	clearScreen()
	say("sync complete")
}

func downloadPlaylist(playlist Playlist) {
	clearScreen()

	folder := filepath.Join(expandHome(cfg.SyncFolder), playlist.Name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		say("could not create %s: %v", folder, err)
		return
	}

	// remember which spotify songs we already have, drop broken files
	downloaded := map[string]bool{}
	entries, _ := os.ReadDir(folder)
	for _, entry := range entries {
		name := entry.Name()
		if isJunkFile(name) {
			continue
		}
		path := filepath.Join(folder, name)
		id, err := readSpotifyID(path)
		if err != nil {
			logf("invalid audio file %s: %v", path, err)
			if err := os.Remove(path); err != nil {
				logf("could not delete %s: %v", path, err)
			}
			continue
		}
		downloaded[id] = true
	}

	say("spotify/%s/%s -> %s", cfg.UserName, playlist.Name, folder)
	say("")
	say("downloading and tagging files")
	say("")

	var order []string
	next := "/playlists/" + playlist.ID + "/items?limit=50"
	done := 0
	loadFailed := false

	for next != "" {
		var page PlaylistItemsPage
		if _, err := spotifyGet(next, &page); err != nil {
			logf("could not load items for %s: %v", playlist.Name, err)
			loadFailed = true
			break
		}

		for _, item := range page.Items {
			track := item.track()
			if track == nil || track.ID == "" {
				done++
				progress(done, playlist.Total())
				continue
			}

			order = append(order, track.ID)
			if !downloaded[track.ID] {
				downloadSong(track.ID, folder)
				downloaded[track.ID] = true
			}
			done++
			progress(done, playlist.Total())
		}

		next = page.Next
	}
	progressDone()

	if loadFailed {
		say("could not load the whole playlist, stopping here (it will be retried next sync)")
		return
	}

	clearScreen()
	say("spotify/%s/%s -> %s", cfg.UserName, playlist.Name, folder)
	say("")
	say("organizing files")
	say("")

	position := map[string]int{}
	for i, id := range order {
		if _, ok := position[id]; !ok {
			position[id] = i + 1
		}
	}

	entries, _ = os.ReadDir(folder)
	for _, entry := range entries {
		name := entry.Name()
		if isJunkFile(name) {
			continue
		}
		path := filepath.Join(folder, name)
		id, err := readSpotifyID(path)
		if err != nil {
			continue
		}
		number, ok := position[id]
		if !ok {
			continue
		}

		newName := name
		if numberedPrefix.MatchString(name) {
			newName = name[5:]
		}
		target := filepath.Join(folder, fmt.Sprintf("%03d. %s", number, newName))
		if target == path {
			continue
		}
		if err := os.Rename(path, target); err != nil {
			logf("could not rename %s: %v", path, err)
		}
	}

	// only mark the playlist as synced after everything is done
	cfg = loadConfig()
	for _, entry := range cfg.SelectedPlaylists {
		if meta, ok := entry[playlist.ID]; ok {
			meta.SnapshotID = playlist.SnapshotID
			entry[playlist.ID] = meta
			break
		}
	}
	saveConfig()

	logf("finished syncing %s", playlist.Name)
}

// --- songs and tagging ------------------------------------------------------

func downloadSong(songID, folder string) {
	var track Track
	status, err := spotifyGet("/tracks/"+songID, &track)
	if err != nil {
		if status == http.StatusBadRequest {
			return // local or unavailable track
		}
		logf("could not load track %s: %v", songID, err)
		return
	}

	title := track.Name + " - " + joinArtists(track.Artists)
	logf("searching for: %s", title)

	video, err := searchVideo(title)
	if err != nil {
		logf("no youtube result for %s: %v", title, err)
		return
	}

	videoURL := "https://www.youtube.com/watch?v=" + video.ID
	logf("downloading: %s (%s)", video.Title, videoURL)

	basePath := filepath.Join(folder, sanitizeFilename(title))
	if err := ytdlpDownload(video.ID, basePath); err != nil {
		logf("download failed for %s: %v", title, err)
		return
	}

	mp3Path := basePath + ".mp3"
	if !fileExists(mp3Path) {
		logf("downloaded file missing for %s", title)
		return
	}

	if len(track.Album.Images) > 0 {
		cover, err := fetchBytes(track.Album.Images[0].URL)
		if err != nil {
			logf("could not fetch cover for %s: %v", title, err)
		} else if err := writeCover(mp3Path, cover); err != nil {
			logf("tagging image failed for %s: %v", title, err)
		}
	}

	if err := writeTags(mp3Path, track, songID, video.ID); err != nil {
		logf("tagging failed for %s: %v", title, err)
		return
	}

	logf("downloaded and tagged: %s", mp3Path)
}

func writeCover(mp3Path string, cover []byte) error {
	tag, err := id3v2.Open(mp3Path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()

	tag.SetVersion(4)
	tag.DeleteAllFrames()
	tag.AddAttachedPicture(id3v2.PictureFrame{
		Encoding:    id3v2.EncodingUTF8,
		MimeType:    "image/jpeg",
		PictureType: id3v2.PTFrontCover,
		Description: "Cover",
		Picture:     cover,
	})

	return tag.Save()
}

func writeTags(mp3Path string, track Track, spotifyID, youtubeID string) error {
	tag, err := id3v2.Open(mp3Path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()

	tag.SetVersion(4)

	addText := func(id, value string) {
		if value == "" {
			return
		}
		tag.AddTextFrame(id, id3v2.EncodingUTF8, value)
	}
	addUserText := func(description, value string) {
		tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{
			Encoding:    id3v2.EncodingUTF8,
			Description: description,
			Value:       value,
		})
	}

	addUserText("STATIC_SPOTIFY_ID", spotifyID)
	addUserText("STATIC_YOUTUBE_ID", youtubeID)
	advisory := "0"
	if track.Explicit {
		advisory = "1"
	}
	addUserText("ITUNESADVISORY", advisory)

	addText("TPE1", joinArtists(track.Artists))
	addText("TPE2", joinArtists(track.Album.Artists))
	addText("TIT2", track.Name)
	addText("TPOS", strconv.Itoa(track.DiscNumber))
	addText("TRCK", strconv.Itoa(track.TrackNumber))
	addText("TDRC", track.Album.ReleaseDate)
	addText("TALB", track.Album.Name)

	return tag.Save()
}

func readSpotifyID(path string) (string, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true, ParseFrames: []string{"TXXX"}})
	if err != nil {
		return "", err
	}
	defer tag.Close()

	for _, frame := range tag.GetFrames("TXXX") {
		var userText id3v2.UserDefinedTextFrame
		switch typed := frame.(type) {
		case id3v2.UserDefinedTextFrame:
			userText = typed
		case *id3v2.UserDefinedTextFrame:
			userText = *typed
		default:
			continue
		}
		if userText.Description == "STATIC_SPOTIFY_ID" {
			return userText.Value, nil
		}
	}

	return "", errors.New("missing STATIC_SPOTIFY_ID tag")
}

func joinArtists(artists []Artist) string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		names = append(names, artist.Name)
	}
	return strings.Join(names, ", ")
}
