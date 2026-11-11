package main

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/raitonoberu/ytsearch"
)

// searchVideo returns the first youtube search result for the query.
func searchVideo(query string) (*ytsearch.VideoItem, error) {
	result, err := ytsearch.VideoSearch(query).Next()
	if err != nil {
		return nil, err
	}
	if len(result.Videos) == 0 {
		return nil, errors.New("no results found")
	}
	return result.Videos[0], nil
}

// ytdlpDownload downloads the best audio for a video and converts it to mp3.
// It shells out to the yt-dlp installed on the system (same as the python version).
func ytdlpDownload(videoID, basePath string) error {
	args := []string{
		"-f", "bestaudio/best",
		"-x", "--audio-format", "mp3",
		"--audio-quality", "192K",
		"--embed-metadata",
		"--no-playlist",
		"-o", basePath + ".%(ext)s",
	}

	if cookies := cookieFile(); cookies != "" {
		args = append(args, "--cookies", cookies)
	}

	args = append(args, "https://www.youtube.com/watch?v="+videoID)

	cmd := exec.Command("yt-dlp", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, tail(string(output), 3))
	}
	return nil
}
