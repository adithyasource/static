# rip

<table>
    <tbody>
        <tr>
            <td><a href="#instructions"> run it!</a></td>
            <td>spotify to folder ripper</td>
        </tr>
    </tbody>
</table>

![Group 1 (2)](https://github.com/user-attachments/assets/881c7280-717d-4b99-ab53-602c46c742bb)

> currently only tested on macOS

## instructions
### installation
- clone the repository with ```git clone https://github.com/adithyasource/rip/```
- create a virtual environment with ```python3 -m venv env``` and activate it using ```source env/bin/activate```
- install the dependencies using ```pip3 install -r requirements.txt```
- run the program using ```python3 rip.py```

### spotify setup
> these instructions are also prompted during the spotify account connection process where you'll be entering the obtained client id and secret
- create a spotify developers account at [developer.spotify.com](https://developer.spotify.com)
- go to the dashboard and create an app
- make sure to add the redirect uri as http://localhost:9321
- obtain the client id and secret

## contributing
first of all, thank you so much for using the script and deciding to contribute! i really appreciate it ^-^ \
please try to do the following before opening a pull request!
- lint and format the code using [ruff](https://docs.astral.sh/ruff/)
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
