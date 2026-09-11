# Suzuna Launcher

A small, single-binary game launcher for the [Suzuna](https://suzuna.xyz)
Roblox-revival stack. It registers a custom URL protocol so the **Play** button
on the website can hand a game off to a locally-installed client, then resolves
the game server and starts the client pointed at it.

Written in Go, cross-compiles to a self-contained Windows `.exe` with no runtime
dependencies. It's the successor to an older C# / WPF launcher, which is not
part of this repository.

> **Fan project.** This is an unofficial launcher for a self-hosted Roblox
> revival. It is not affiliated with, endorsed by, or connected to Roblox
> Corporation. Use it only with a server you run or are authorized to use.

---

## How it works

```
Browser: click PLAY
        │
        ▼
suzuna-player:1+launchmode:play+gameinfo:<ticket>+placelauncherurl:<url>+clientversion:2017L
        │   (custom URL protocol, registered in HKCU)
        ▼
Suzuna Launcher
        │  • parse the URI (ticket, PlaceLauncher URL, client year)
        │  • locate the installed client (…\Versions\<hash>\<year>\*PlayerBeta.exe),
        │    downloading it once from <server>/downloads/clients if missing
        │  • write AppSettings.xml next to it
        ▼
Client (*PlayerBeta.exe -a <Negotiate.ashx> -t <ticket> -j <PlaceLauncher URL>)
        │  the client negotiates the session, polls PlaceLauncher, fetches the
        │  signed join script, and connects to the game server itself
        ▼
In-game
```

For the protocol path the launcher intentionally **does not** negotiate or poll
itself — doing so would burn the one-time auth ticket the client needs. It only
does the negotiate/poll dance for the diagnostic `--place` / `--test` flows.

## Building

Requires Go 1.23+.

```bash
./build.sh            # -> dist/SuzunaLauncher-windows-386.exe  (+ amd64, + debug)
./build.sh 1.3.0      # stamp an explicit version
```

Or by hand:

```bash
# Production (no console window on launch)
GOOS=windows GOARCH=386 go build -ldflags "-H=windowsgui -s -w" -o SuzunaLauncher.exe .

# Debug (keeps a console; use with --test)
GOOS=windows GOARCH=386 go build -o SuzunaLauncher-debug.exe .
```

## Usage

| Command | What it does |
| --- | --- |
| double-click the exe | Registers the protocol handler and shows a "you're set — click Play on the site" dialog |
| `SuzunaLauncher.exe --register` | Register the `suzuna-player:` handler in `HKCU` |
| `SuzunaLauncher.exe --unregister` | Remove the handler |
| `SuzunaLauncher.exe --test` | End-to-end diagnostic (web reachable → PlaceLauncher → join script → client found) |
| `SuzunaLauncher.exe --place <id>` | Launch a place directly (needs `--ticket` for auth) |
| `SuzunaLauncher.exe --version` | Print version and platform |
| `suzuna-player:…` (via browser) | Normal launch — invoked automatically by the Play button |

Useful flags: `--server <url>` (default `https://suzuna.xyz`), `--year <2017|2018|2020|2021>`,
`--client <path>` (override client autodetection), `--ticket <t>`, `--join <url>`,
`--quiet` (never show a window, even on error — for installer use).

A log of every run is appended to `%TEMP%\suzuna-launcher.log`, which survives
even if the window is closed or the process is killed — start there when
debugging a launch.

## Configuration (forking this for your own revival)

Three touch points, all at the top of [`main.go`](main.go):

- **`DefaultBaseURL`** — your server, used as the fallback when a request
  doesn't carry its own host. During a real launch the launcher derives the
  server from the URL the site hands it (see `serverBaseFromURL`), so pointing
  the *website* at your host is usually enough.
- **`ClientDownloadURL`** — where the launcher fetches the game client from
  when none is installed. It expects `pekora-<year tag>.zip` files (e.g.
  `pekora-2017L.zip`) that extract to `<hash>/<year tag>/*PlayerBeta.exe`.
- **`registerProtocol` scheme list** — the URI schemes registered
  (`suzuna-player`, `suzuna`, `rexursclient`). Match these to what your site
  emits.

If no client is installed, the launcher downloads one from `ClientDownloadURL`
into `%LOCALAPPDATA%\Pekora\Versions` on the first launch. It never launches a
stock Roblox install.

Client autodetection (`findRobloxClient`) searches a matrix of rebranded client
names (`ProjectXPlayerBeta`, …) under the
common `%LOCALAPPDATA%` vendor folders and both flat and year-bucketed
(`…\Versions\<hash>\2017L\…`) layouts.

## Repository layout

```
main.go              launcher entry point, protocol handling, client launch
syscall_windows.go   Win32 glue (console attach, message boxes, ShellExecute)
syscall_other.go     no-op stubs so it still builds on Linux/macOS for dev
build.sh             cross-compile helper -> dist/
go.mod               module definition (Go 1.23, standard library only)
```

## License

[MIT](LICENSE).
