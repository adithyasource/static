import os
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


dirs = AppDirs("rip", "adithya")
appFolder = dirs.user_data_dir
appConfig = getAppConfig()


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
        elif userAction is None:
            return False


main()
