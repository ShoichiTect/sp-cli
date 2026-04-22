# sp-cli

Small Spotify CLI for staying in the terminal while using the native Spotify app or another Spotify Connect device.

## What It Does

- Runs Spotify OAuth in a browser once and stores tokens locally
- Refreshes expired access tokens automatically
- Lists available Spotify devices
- Searches tracks, albums, and playlists with clean terminal output
- Plays tracks, albums, and playlists from the command line
- Saves and removes tracks and albums from Your Library
- Lists saved tracks and albums, and checks whether a given item is saved

This CLI uses the Spotify Web API. Audio still plays on the Spotify app, Web Player, or another Spotify Connect device.

## Install

From `spotify/sp-cli`:

```bash
go install .
```

Or from the repo root:

```bash
go install ./sp-cli
```

Make sure `$(go env GOPATH)/bin` is on your `PATH` so `sp-cli` is available globally.

## First Run

1. Create or open your app in the Spotify Developer Dashboard.
2. Add this redirect URI:

```text
http://127.0.0.1:8888/callback
```

3. Authenticate:

```bash
sp-cli auth
```

If `SPOTIFY_CLIENT_ID` and `SPOTIFY_CLIENT_SECRET` are unset, `sp-cli auth` prompts for them and hides the client secret while you type.

Tokens are stored in:

```text
~/.config/spotify-cli/config.json
```

## Usage

```bash
sp-cli -h
sp-cli devices
sp-cli use-device <device-id>
sp-cli search "宇多田ヒカル First Love"
sp-cli search --type album "宇多田ヒカル"
sp-cli play spotify:track:39HrUxcvKF3jtLz7fUDWXc
sp-cli play spotify:album:2CVV8PtUYYsux8XOzWkCP0
sp-cli play spotify:playlist:37i9dQZF1DXcBWIGoYBM5M
sp-cli play
sp-cli pause
sp-cli next
sp-cli save spotify:track:39HrUxcvKF3jtLz7fUDWXc
sp-cli save spotify:album:2CVV8PtUYYsux8XOzWkCP0
sp-cli unsave spotify:track:39HrUxcvKF3jtLz7fUDWXc
sp-cli library
sp-cli library --type album --limit 10
sp-cli check spotify:track:39HrUxcvKF3jtLz7fUDWXc
```

## Environment Variables

- `SPOTIFY_CLIENT_ID`
- `SPOTIFY_CLIENT_SECRET`
- `SPOTIFY_REDIRECT_URI`
- `SPOTIFY_DEVICE_ID`

You can still pass credentials as arguments:

```bash
sp-cli auth YOUR_CLIENT_ID YOUR_CLIENT_SECRET
```

## Development

Use the installed binary for normal usage:

```bash
go install .
```

Use the local source tree for development checks without leaving behind a repo-local binary:

```bash
go run . search --type album "First Love"
go test ./...
```

## Notes

- `sp-cli play` without a URI resumes playback
- `sp-cli search` defaults to `track`, `album`, and `playlist`; use `--type` to narrow results
- `sp-cli use-device` saves a preferred device id
- If no preferred device is set, the CLI uses the active device or the first available one
- `sp-cli save` and `unsave` accept `spotify:track:...` or `spotify:album:...` URIs
- `sp-cli library` lists saved items; defaults to `--type track` with a limit of 20
- `sp-cli check` tells you whether the given URI is saved in Your Library
