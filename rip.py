import os
import threading

from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlencode, urlparse, parse_qs
import webbrowser
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
            emptyConfig = {"syncFolder": None, "accessToken": None, "userName": None}
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


dirs = AppDirs("rip", "adithya")
appFolder = dirs.user_data_dir
appConfig = getAppConfig()


def setupUser():
    accessToken = createAccessToken()

    appConfig = getAppConfig()
    appConfig["accessToken"] = accessToken

    response = requests.get(
        "https://api.spotify.com/v1/me",
        headers={"Authorization": f"Bearer {accessToken}"},
    )

    data = response.json()

    appConfig["userName"] = data.get("display_name")

    writeAppConfig(appConfig)


def main():
    while True:
        clearScreen()
        appConfig = getAppConfig()

        print("ripify - spotify to folder sync utility")
        print(f"sync folder: {appConfig["syncFolder"]}")
        print(f"spotify account: {appConfig["userName"]}")

        userAction = questionary.select(
            "what do you want to do?",
            choices=["choose sync folder", "connect spotify account"],
        ).ask()
        if userAction == "choose sync folder":
            chooseSyncFolder()
        elif userAction == "connect spotify account":
            setupUser()
        elif userAction is None:
            return False


main()
