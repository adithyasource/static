import os
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
import tkinter as tk
from tkinter import filedialog
import questionary


def clearScreen():
    os.system("cls")


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
    root = tk.Tk()
    root.withdraw()

    if appConfig["syncFolder"]:
        userAction = questionary.confirm(
            "are you sure you want to override the already selected folder?"
        ).ask()
        if not userAction:
            return

    userFolderChoice = filedialog.askdirectory(title="choose spotify sync folder")
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


def syncPlaylists():
    pass


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


def download_song(song_name: str):
    print(f"Searching for: {song_name}")
    results = YoutubeSearch(song_name, max_results=1).to_dict()
    if not results:
        print("No results found.")
        return

    result = results[0]
    video_url = f"https://www.youtube.com{result['url_suffix']}"
    video_title = result["title"]
    thumbnail_url = result["thumbnails"][0]

    thumbnail_filename = "thumb.jpg"
    urllib.request.urlretrieve(thumbnail_url, thumbnail_filename)

    # Download audio
    print(f"Downloading: {video_title}")
    ydl_opts = {
        "format": "bestaudio/best",
        "outtmpl": "%(title)s.%(ext)s",
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

    with yt_dlp.YoutubeDL(ydl_opts) as ydl:
        info_dict = ydl.extract_info(video_url, download=True)
        mp3_filename = ydl.prepare_filename(info_dict).rsplit(".", 1)[0] + ".mp3"

    # Embed thumbnail
    print("Embedding cover art...")
    audio = MP3(mp3_filename, ID3=ID3)
    try:
        audio.add_tags()
    except error:
        pass
    audio.tags.add(
        APIC(
            encoding=3,
            mime="image/jpeg",
            type=3,
            desc="Cover",
            data=open(thumbnail_filename, "rb").read(),
        )
    )
    audio.save()

    # Cleanup
    os.remove(thumbnail_filename)
    print(f"Downloaded and tagged: {mp3_filename}")


appConfig = getAppConfig()

playlist_id = "0PoraAMiywwP84p57JPAKH"
url = f"https://api.spotify.com/v1/playlists/{playlist_id}/tracks"

while url:
    response = requests.get(
        url, headers={"Authorization": f"Bearer {appConfig["accessToken"]}"}
    )
    data = response.json()

    for item in data["items"]:
        if item.get("track"):
            print(
                item["track"]["name"]
                + " - "
                + ", ".join([x["name"] for x in item["track"]["artists"]])
            )

    url = data.get("next")

time.sleep(100)


def main():
    while True:
        clearScreen()
        appConfig = getAppConfig()

        print("ripify - spotify to folder sync utility")
        print(f"sync folder: {appConfig["syncFolder"]}")
        print(f"spotify account: {appConfig["userName"]}")

        mainMenuChoices = ["choose sync folder", "connect spotify account"]

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
        elif userAction == "connect spotify account":
            setupUser()
        elif userAction is None:
            return False


main()
