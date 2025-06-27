import os
import re
from os import listdir
from os.path import isfile, join
from pathlib import Path
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlencode, urlparse, parse_qs
import urllib.request
import webbrowser
from youtube_search import YoutubeSearch
from mutagen.mp3 import MP3
import yt_dlp
from mutagen.id3 import ID3, APIC, error
import time
import string
import random
import requests
import json
from appdirs import AppDirs
import questionary


def clearScreen():
    os.system("clear")


def getAppConfig():
    if not os.path.exists(appFolder):
        os.makedirs(appFolder)
    try:
        with open(os.path.join(appFolder, "data.json"), "x") as f:
            emptyConfig = {
                "syncFolder": None,
                "accessToken": None,
                "userName": None,
                "userId": None,
                "selectedPlaylists": [],
            }
            jsonEmptyConfig = json.dumps(emptyConfig)
            f.write(jsonEmptyConfig)
            return emptyConfig
    except FileExistsError:
        with open(os.path.join(appFolder, "data.json"), "r") as f:
            return json.loads(f.read())


def writeAppConfig(appConfig):
    with open(os.path.join(appFolder, "data.json"), "w") as f:
        f.write(json.dumps(appConfig))


def chooseSyncFolder():
    if appConfig["syncFolder"]:
        userAction = questionary.confirm(
            "are you sure you want to override the already selected folder?"
        ).ask()
        if not userAction:
            return

    userFolderChoice = questionary.path("choose spotify sync folder").ask()

    appConfig["syncFolder"] = userFolderChoice
    writeAppConfig(appConfig)
    return


def randomString():
    return "".join(random.choices(string.ascii_lowercase + string.digits, k=10))


def createAccessToken():
    authCode = [None]

    class SimpleHandler(BaseHTTPRequestHandler):
        def do_GET(self):
            parsed_url = urlparse(self.path)
            query_params = parse_qs(parsed_url.query)

            print("GET query parameters received:", query_params)

            authCode[0] = query_params["code"][0]

            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"you can close this window and head back to rip!\n")
            threading.Thread(target=server.shutdown).start()  # shutdown the server

    state = randomString()
    scope = "user-read-private user-read-email"

    authParams = {
        "response_type": "code",
        "client_id": "7cda876ec9b945d8b55f3c14eb2ee5da",
        "scope": scope,
        "redirect_uri": "http://localhost:9321",
        "state": state,
    }

    print(urlencode(authParams))

    PORT = 9321
    global server
    server = HTTPServer(("localhost", PORT), SimpleHandler)

    thread = threading.Thread(target=server.serve_forever)
    thread.start()

    webbrowser.open("https://accounts.spotify.com/authorize?" + urlencode(authParams))

    thread.join()

    print("threads closed")
    print(authCode[0])

    response = requests.post(
        "https://accounts.spotify.com/api/token",
        data={
            "code": authCode,
            "redirect_uri": "http://localhost:9321",
            "grant_type": "authorization_code",
            "client_id": "7cda876ec9b945d8b55f3c14eb2ee5da",
            "client_secret": "4f74525c87b548bcb66acf8f8fdf96c5",
        },
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )

    return response.json()["access_token"]


def selectPlaylists():
    response = requests.get(
        f"https://api.spotify.com/v1/users/{appConfig["userId"]}/playlists",
        headers={"Authorization": f"Bearer {appConfig["accessToken"]}"},
    )

    data = response.json()

    playlistChoices = []

    for x in data["items"]:
        alreadySelected = False

        if {x["id"]: x["name"]} in appConfig["selectedPlaylists"]:
            alreadySelected = True

        playlistChoices.append(
            questionary.Choice(
                x["name"], value={x["id"]: x["name"]}, checked=alreadySelected
            )
        )

    playlistsSelected = questionary.checkbox(
        "select the playlists you want to sync",
        choices=playlistChoices,
    ).ask()

    if playlistsSelected is None:
        return False

    appConfig["selectedPlaylists"] = playlistsSelected
    writeAppConfig(appConfig)


dirs = AppDirs("rip", "adithya")
appFolder = dirs.user_data_dir
appConfig = getAppConfig()


def setupUser():
    appConfig = getAppConfig()

    if appConfig["userName"]:
        userAction = questionary.confirm(
            "are you sure you want to override the already connected account?"
        ).ask()
        if not userAction:
            return

    accessToken = createAccessToken()

    appConfig["accessToken"] = accessToken

    response = requests.get(
        "https://api.spotify.com/v1/me",
        headers={"Authorization": f"Bearer {accessToken}"},
    )

    data = response.json()

    appConfig["userName"] = data.get("display_name")
    appConfig["userId"] = data.get("id")

    writeAppConfig(appConfig)


def downloadSong(songId, playlistFolder):
    os.makedirs(playlistFolder, exist_ok=True)

    response = requests.get(
        f"https://api.spotify.com/v1/tracks/{songId}",
        headers={"Authorization": f"Bearer {appConfig['accessToken']}"},
    )
    trackData = response.json()

    coverArtUrl = trackData.get("album")["images"][0]["url"]
    songTitle = (
        trackData.get("name")
        + " - "
        + ", ".join([artist["name"] for artist in trackData.get("artists")])
    )

    print(f"Searching for: {songTitle}")
    searchResults = YoutubeSearch(songTitle, max_results=1).to_dict()
    if not searchResults:
        print("No results found.")
        return

    videoData = searchResults[0]
    videoUrl = f"https://www.youtube.com{videoData['url_suffix']}"
    videoTitle = videoData["title"]

    coverArtPath = os.path.join(playlistFolder, "thumb.jpg")
    urllib.request.urlretrieve(coverArtUrl, coverArtPath)

    print(f"Downloading: {videoTitle}")
    mp3FileName = f"{songTitle} {songId}"
    mp3FullPath = os.path.join(playlistFolder, mp3FileName)
    mp3FullPath = os.path.expanduser(mp3FullPath)

    ytdlOptions = {
        "format": "bestaudio/best",
        "outtmpl": mp3FullPath,
        "noplaylist": True,
        "postprocessors": [
            {
                "key": "FFmpegExtractAudio",
                "preferredcodec": "mp3",
                "preferredquality": "192",
            },
            {
                "key": "FFmpegMetadata",
            },
        ],
    }

    with yt_dlp.YoutubeDL(ytdlOptions) as ydl:
        ydl.download([videoUrl])

    time.sleep(1)

    print("Embedding cover art...")
    audioFile = MP3(mp3FullPath + ".mp3", ID3=ID3)
    try:
        audioFile.add_tags()
    except error:
        pass

    audioFile.tags.add(
        APIC(
            encoding=3,
            mime="image/jpeg",
            type=3,
            desc="Cover",
            data=open(coverArtPath, "rb").read(),
        )
    )
    audioFile.save()

    os.remove(coverArtPath)
    print(f"Downloaded and tagged: {mp3FullPath}")


def downloadPlaylist(playlistId):
    response = requests.get(
        f"https://api.spotify.com/v1/playlists/{playlistId}",
        headers={"Authorization": f"Bearer {appConfig["accessToken"]}"},
    )
    data = response.json()
    playlistName = data.get("name")

    playlistFolder = os.path.join(appConfig["syncFolder"], playlistName)
    playlistFolder = os.path.expanduser(playlistFolder)

    if not os.path.exists(playlistFolder):
        os.makedirs(playlistFolder)

    songsDownloadedFull = os.listdir(playlistFolder)

    songsDownloadedIds = set()

    for x in songsDownloadedFull:
        songsDownloadedIds.add(x.split(" ")[-1].split(".mp3")[0])

    url = f"https://api.spotify.com/v1/playlists/{playlistId}/tracks"

    songsOrder = []

    while url:
        response = requests.get(
            url, headers={"Authorization": f"Bearer {appConfig["accessToken"]}"}
        )
        data = response.json()

        for item in data["items"]:
            if item.get("track"):
                songsOrder.append(item["track"]["id"])
                if item["track"]["id"] not in songsDownloadedIds:
                    downloadSong(item["track"]["id"], playlistFolder)

        url = data.get("next")

    songsDownloadedFull = os.listdir(playlistFolder)

    for x in songsDownloadedFull:
        if not x.endswith(".mp3"):
            os.remove(os.path.join(playlistFolder, x))
            continue
        songId = x.split(" ")[-1].split(".mp3")[0]
        songNumber = str(songsOrder.index(songId) + 1).zfill(3)

        newName = x
        if re.match(r"^\d{3}\. ", x):
            newName = x[5:]

        os.rename(
            os.path.join(playlistFolder, x),
            os.path.join(playlistFolder, f"{songNumber}. {newName}"),
        )

    print(songsOrder)

    pass


def syncPlaylists():
    pass


downloadPlaylist("2Yw68xbhzj2y1hSmzclcFc")

time.sleep(100)


def main():
    while True:
        clearScreen()
        appConfig = getAppConfig()

        print("ripify - spotify to folder sync utility")
        print(f"sync folder: {appConfig["syncFolder"]}")
        print(f"spotify account: {appConfig["userName"]}")

        mainMenuChoices = [
            "choose sync folder",
            "connect spotify account / refresh token",
        ]

        if appConfig["userName"]:
            mainMenuChoices.insert(0, "choose playlists to sync")

        if len(appConfig["selectedPlaylists"]) != 0:
            mainMenuChoices.insert(0, "sync")

        userAction = questionary.select(
            "what do you want to do?",
            choices=mainMenuChoices,
        ).ask()

        if userAction == "sync":
            syncPlaylists()
        elif userAction == "choose playlists to sync":
            selectPlaylists()
        elif userAction == "choose sync folder":
            chooseSyncFolder()
        elif userAction == "connect spotify account / refresh token":
            setupUser()
        elif userAction is None:
            return False


main()
