import json
import os
import random
import re
import shutil
import string
import sys
import threading
import time
import urllib.request
import webbrowser
from http.server import BaseHTTPRequestHandler, HTTPServer
from os import devnull
from subprocess import call
from urllib.parse import parse_qs, urlencode, urlparse

import questionary
import requests
import yt_dlp
from alive_progress import alive_bar
from appdirs import AppDirs
from mutagen.id3 import APIC, ID3, error
from mutagen.mp3 import MP3
from questionary import Style
from sanitize_filename import sanitize
from youtube_search import YoutubeSearch
from yt_dlp import utils


def disablePrint():
    sys.stdout = open(devnull, "w")


def enablePrint():
    sys.stdout = sys.__stdout__


def clearScreen():
    os.system("clear")
    print("""
⠀⠀⠀⠀⢀⡉⠐⠀⠀⠀⠀ s
⠀⠀⣲⠞⢻⣿⣶⡀⠀⠀⠀ t⠀⠀⠀⠀⢠⣴⡶⣦⣤⣤⣖⣀⡀⠀⠀⠀⠀⠀
⠐⣮⡏⠀⠉⢹⣿⢷⠀⠀⠀ a⠀⠀⠀⢠⡞⠉⠀⠉⠉⣿⣿⣿⣿⣿⣄⣂⡄⠀
⠀⠀⢃⠀⠀⢀⣿⡨⠀⠀⠀ t⠀⠀⠀⠋⠀⠀⠀⠀⠀⠈⣿⣿⠋⠛⣿⠋⠀⠀
⠀⠀⠐⢄⡐⠈⢁⠘⠀⠀⠀ i⠀⠀⠀⠀⠀⠀⠀⡠⠀⠔⠿⠋⠀⢀⠇⠀⠀⠀
⠀⠀⠀⠀⠈⠉⠉⠀⠀⠀⠀ c⠀⠀⠀⠀⠀⠀⠀⠐⢀⠀⠀⢀⡰⠃⠀⠀⠀⠀
""")


def getAppConfig():
    if not os.path.exists(appFolder):
        os.makedirs(appFolder)
    try:
        with open(os.path.join(appFolder, "data.json"), "x") as f:
            emptyConfig = {
                "syncFolder": None,
                "accessToken": None,
                "userName": None,
                "clientSecret": None,
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
    clearScreen()

    if appConfig["syncFolder"]:
        userAction = questionary.confirm(
            "are you sure you want to override the already selected folder?",
            style=Style([("question", "nobold")]),
            qmark="",
        ).ask()

        if not userAction:
            return

        clearScreen()

    userFolderChoice = questionary.path(
        "enter spotify sync folder path [<tab> to autocomplete]",
        style=Style([("question", "nobold")]),
        qmark="",
        only_directories=True,
    ).ask()

    clearScreen()

    userFolderChoice = os.path.expanduser(userFolderChoice)

    contentsOfFolder = os.listdir(userFolderChoice)

    if len(contentsOfFolder) != 0:
        userAction = questionary.confirm(
            "are you sure you want to overwrite the contents of the selected folder? (all existing data will be deleted)",
            style=Style([("question", "nobold")]),
            qmark="",
        ).ask()
        if not userAction:
            return

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
            self.wfile.write(b"you can close this window and head back to static!\n")
            threading.Thread(target=server.shutdown).start()  # shutdown the server

    state = randomString()
    scope = "user-read-private user-read-email"

    authParams = {
        "response_type": "code",
        "client_id": appConfig["clientId"],
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
            "code": authCode[0],
            "redirect_uri": "http://localhost:9321",
            "grant_type": "authorization_code",
            "client_id": appConfig["clientId"],
            "client_secret": appConfig["clientSecret"],
        },
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )

    return response.json()["access_token"]


def selectPlaylists():
    clearScreen()

    def getData():
        response = requests.get(
            "https://api.spotify.com/v1/me/playlists",
            headers={"Authorization": f"Bearer {appConfig["accessToken"]}"},
        )
        return response

    response = getData()

    if response.status_code == 401:
        userAction = questionary.confirm(
            "looks like your spotify account got disconnected (token expired) do you want to reconnect?",
            style=Style([("question", "nobold")]),
            qmark="",
        ).ask()
        if not userAction:
            return

        setupUser(True)
        response = getData()
        clearScreen()

    data = response.json()

    playlistChoices = []

    for x in data["items"]:
        alreadySelected = False

        if {x["id"]: x["name"]} in appConfig["selectedPlaylists"]:
            alreadySelected = True

        playlistChoices.append(
            questionary.Choice(
                f"{x["name"]} [{x["tracks"]["total"]}]",
                value={x["id"]: x["name"]},
                checked=alreadySelected,
            )
        )

    playlistsSelected = questionary.checkbox(
        "select the playlists you want to sync",
        choices=playlistChoices,
        qmark="",
        pointer="⏵",
        instruction="[<space> to pick, <enter> to confirm]",
        style=Style([("question", "nobold")]),
    ).ask()

    if playlistsSelected is None:
        return False

    appConfig["selectedPlaylists"] = playlistsSelected
    writeAppConfig(appConfig)


dirs = AppDirs("static", "adithya")
appFolder = dirs.user_data_dir
appConfig = getAppConfig()


def setupClient(preConfirm=False):
    if not preConfirm:
        if appConfig["clientId"] or appConfig["clientSecret"]:
            userAction = questionary.confirm(
                "do you want to overwrite the current client secrets?",
                style=Style([("question", "nobold")]),
                qmark="",
            ).ask()

            clearScreen()

            if not userAction:
                return

        print(
            "in order for static to access your spotify data, it needs a client id and secret associated to your account"
        )
        print()
        print("follow the given step to obtain them")
        print("- create a spotify developers account at https://developer.spotify.com")
        print("- go to the dashboard and create an app")
        print("- make sure to add the redirect uri as http://localhost:9321")
        print()
        input("press enter to continue")

        clearScreen()

        clientId = questionary.text(
            "enter your spotify client id",
            style=Style([("question", "nobold")]),
            qmark="",
        ).ask()

        clearScreen()

        clientSecret = questionary.text(
            "enter your spotify client secret",
            style=Style([("question", "nobold")]),
            qmark="",
        ).ask()

        clearScreen()

        appConfig["clientId"] = clientId
        appConfig["clientSecret"] = clientSecret

        writeAppConfig(appConfig)


def setupUser(preConfirm=False):
    clearScreen()

    setupClient(preConfirm)

    if not preConfirm and appConfig["userName"]:
        userAction = questionary.confirm(
            "are you sure you want to overwrite the already connected account?",
            style=Style([("question", "nobold")]),
            qmark="",
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
        headers={"Authorization": f"Bearer {appConfig["accessToken"]}"},
    )
    if response.status_code == 400:
        return

    trackData = response.json()

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

    print(f"Downloading: {videoTitle}")
    mp3FileName = f"{songTitle} {songId}"
    mp3FileName = sanitize(mp3FileName)
    mp3FullPath = os.path.join(playlistFolder, mp3FileName)
    mp3FullPath = os.path.expanduser(mp3FullPath)

    ytdlOptions = {
        "format": "bestaudio/best",
        "outtmpl": mp3FullPath,
        "noplaylist": True,
        "cookiefile": "cookies.txt",
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

    try:
        with yt_dlp.YoutubeDL(ytdlOptions) as ydl:
            ydl.download([videoUrl])
    except utils.ExtractorError:
        print(f"Video unavailable to download: {mp3FileName}")
        return  # stop processing this song
    except utils.DownloadError:
        print(f"Video unavailable to download: {mp3FileName}")
        return  # stop processing this song

    time.sleep(1)

    # downloading and embedding cover art
    coverArtUrl = trackData.get("album")["images"][0]["url"]
    coverArtPath = os.path.join(playlistFolder, "thumb.jpg")
    urllib.request.urlretrieve(coverArtUrl, coverArtPath)

    try:
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
    except Exception as e:
        print(f"Tagging or file operations failed for {mp3FileName}: {e}")


def downloadPlaylist(playlistId):
    clearScreen()

    def getData():
        response = requests.get(
            f"https://api.spotify.com/v1/playlists/{playlistId}",
            headers={"Authorization": f"Bearer {appConfig["accessToken"]}"},
        )
        return response

    response = getData()

    if response.status_code == 401:
        userAction = questionary.confirm(
            "looks like your spotify account got disconnected (token expired) do you want to reconnect?",
            style=Style([("question", "nobold")]),
            qmark="",
        ).ask()
        if not userAction:
            return

        setupUser(True)
        response = getData()
        clearScreen()

    data = response.json()

    playlistName = data.get("name")

    noOfSongs = data.get("tracks")["total"]

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

    print(f"spotify/{appConfig["userName"]}/{playlistName} ->  {playlistFolder}")
    print()

    with alive_bar(noOfSongs) as bar:
        disablePrint()
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
                bar()

            url = data.get("next")
        enablePrint()

    songsDownloadedFull = os.listdir(playlistFolder)

    for x in songsDownloadedFull:
        if not x.endswith(".mp3"):
            try:
                os.remove(os.path.join(playlistFolder, x))
            except PermissionError:
                print(f"could not delete {os.path.join(playlistFolder, x)}")
            continue
        songId = x.split(" ")[-1].split(".mp3")[0]
        if songId in songsOrder:
            songNumber = str(songsOrder.index(songId) + 1).zfill(3)

            newName = x
            if re.match(r"^\d{3}\. ", x):
                newName = x[5:]

            os.rename(
                os.path.join(playlistFolder, x),
                os.path.join(playlistFolder, f"{songNumber}. {newName}"),
            )


def syncPlaylists():
    clearScreen()

    syncFolder = os.path.expanduser(appConfig["syncFolder"])

    finalPlaylists = set()

    for x in appConfig["selectedPlaylists"]:
        downloadPlaylist(list(x.keys())[0])
        finalPlaylists.add(list(x.values())[0])

    playlistsDownloadedFull = os.listdir(syncFolder)

    for x in playlistsDownloadedFull:
        if x not in finalPlaylists:
            unnecessary = os.path.join(syncFolder, x)
            if os.path.isdir(unnecessary):
                shutil.rmtree(unnecessary)
            else:
                os.remove(unnecessary)


def main():
    while True:
        clearScreen()
        appConfig = getAppConfig()

        mainMenuChoices = []

        if len(appConfig["selectedPlaylists"]) != 0:
            mainMenuChoices.append("sync")

        if appConfig["userName"]:
            mainMenuChoices.append("choose playlists to sync")

        mainMenuChoices.extend(
            [
                f"choose sync folder [{appConfig["syncFolder"]}]",
                f"connect spotify account [{appConfig["userName"]}]",
            ]
        )

        if appConfig["syncFolder"]:
            mainMenuChoices.append("open sync folder")

        mainMenuChoices.extend(
            [
                "open data folder",
                "built by adithya.zip",
                "coffee?",
            ]
        )

        userAction = questionary.select(
            "hey there!",
            choices=mainMenuChoices,
            qmark="",
            pointer="⏵",
            instruction=" ",
            style=Style([("question", "nobold")]),
        ).ask()

        if userAction == "sync":
            syncPlaylists()
        elif userAction == "choose playlists to sync":
            selectPlaylists()
        elif userAction == f"choose sync folder [{appConfig["syncFolder"]}]":
            chooseSyncFolder()
        elif userAction == f"connect spotify account [{appConfig["userName"]}]":
            setupUser()

        elif userAction == "open sync folder":
            call(["open", "-R", os.path.expanduser(appConfig["syncFolder"])])
        elif userAction == "open data folder":
            call(["open", "-R", os.path.expanduser(appFolder)])
        elif userAction == "built by adithya.zip":
            webbrowser.open("https://adithya.zip")
        elif userAction == "coffee?":
            webbrowser.open("https://ko-fi.com/adithyasource")
        elif userAction is None:
            return False


main()
