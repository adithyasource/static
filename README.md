# static

<table>
    <tbody>
        <tr>
            <td><a href="#instructions"> run it!</a></td>
            <td>a local copy of your spotify, statically stored</td>
        </tr>
    </tbody>
</table>

![Group 2](https://github.com/user-attachments/assets/e7f5a60c-0866-4b7d-8797-4108b5ccd5dc)

effortlessly and accurately syncs up all your spotify playlists into a folder on your computer so that you can have an offline copy at all times. i made this so that i can sync that folder up my phone using syncthing and have free access to all my spotify songs on the [foobar2000](https://www.foobar2000.org/) app, neatly separated by playlist folders.

> currently only tested on macOS

> ⚠️ for personal use

## instructions
### installation
- clone the repository with ```git clone https://github.com/adithyasource/static/```
- setup the project using [uv](https://docs.astral.sh/uv/#installation) with ```uv sync```
- run the program using ```uv run main.py```

### spotify setup
> these instructions are also prompted during the spotify account connection process where you'll be entering the obtained client id and secret
- create a spotify developers account at [developer.spotify.com](https://developer.spotify.com)
- go to the dashboard and create an app
- make sure to add the redirect uri as http://localhost:9321
- obtain the client id and secret

## contributing
first of all, thank you so much for using the script and deciding to contribute! i really appreciate it ^-^ \
please try to do the following before opening a pull request!
- lint and format the code using [ruff](https://docs.astral.sh/ruff/) (i use [ty](https://github.com/astral-sh/ty) for type check and lsp)
- keep code clean and minimal while only adding dependencies when absolutely necessary
- make sure that if a feature is added, it is kept inline with the projects aesthetic and goals

## feedback

if there are any features or bug fixes you'd like to suggest, please open a new issue!

## acknowledgments

<table>
    <tbody>
        <tr>
            <th>tech</th>
            <td><a href="https://www.python.org/" target="_blank">python</a></td>
        </tr>
    </tbody>
</table>

<table>
    <tbody>
        <tr>
            <th>notable libs</th>
            <td><a href="https://questionary.readthedocs.io/en/stable/" target="_blank">questionary</a></td>
            <td><a href="https://github.com/yt-dlp/yt-dlp" target="_blank">yt_dlp</a></td>
        </tr>
    </tbody>
</table>

<table>
    <tbody>
        <tr>
            <th>apis</th>
            <td><a href="https://developer.spotify.com/documentation/web-api" target="_blank">spotify</a></td>
        </tr>
    </tbody>
</table>
