# tu

A terminal update orchestrator: one menu (or one command) to run every
update action on your machine — devenv trees, distrobox containers, flatpaks,
nix flakes, podman containers, firmware, and the
rpm-ostree system image.

Written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[bubbles](https://github.com/charmbracelet/bubbles),
[lipgloss](https://github.com/charmbracelet/lipgloss) and
[harmonica](https://github.com/charmbracelet/harmonica).

## Actions

| Flag          | Label                      | What it does                                                        |
|---------------|----------------------------|---------------------------------------------------------------------|
| `--devenv`    | Devenv packages            | Find every `devenv.nix` under `$HOME` and run `devenv update` in it |
| `--distrobox` | Distrobox containers       | Upgrade all distrobox containers                                    |
| `--firmware`  | Firmware (fwupd)           | Refresh metadata and apply available firmware updates               |
| `--flatpak`   | Flatpak apps               | Update all installed flatpak apps                                   |
| `--flakes`    | Nix flakes (all)           | Find every `flake.nix` under `$HOME` and update it                  |
| `--podman`    | Podman auto-update         | Pull + recreate labeled podman containers                           |
| `--system`    | System image (rpm-ostree)  | Apply pending rpm-ostree upgrade                                    |

Actions that need pipes/loops (`devenv`, `flakes`, `firmware`) are
fixed shell command literals run via `sh -c`; the rest are executed directly
(no shell involved). Edit `actions.go` to match your setup.

## Build

Requirements: **Go 1.23 or newer**. There are no other dependencies — no
ncurses, no cgo; the Charm libraries are downloaded from the Go module proxy
on first build.

```sh
make            # produces build/tu
# or:
go build -trimpath -ldflags "-s -w" -o build/tu .
```

Other targets:

```sh
make tu         # alias for the default build target
make deps       # go mod tidy
make clean      # remove build/
```

If your host Go is older than the version required in `go.mod`, install a
newer toolchain (e.g. into `~/.local`, no root needed):

```sh
curl -sSLo /tmp/go.tgz https://go.dev/dl/go1.23.6.linux-amd64.tar.gz
mkdir -p ~/.local && tar -C ~/.local -xzf /tmp/go.tgz
export PATH="$HOME/.local/go/bin:$PATH"
```

Cross-compiling works out of the box, e.g.:

```sh
GOARCH=arm64 go build -o build/tu-arm64 .
```

## Usage

With no options, opens the interactive menu:

```sh
tu
```

### Command line

```
Usage: tu [options]

With no options, opens the interactive menu.

Options:
  --all              run every update action
  --devenv           Find every devenv.nix under $HOME and run devenv update in it
  --distrobox        Upgrade all distrobox containers
  ...                (one flag per action)
  --dry-run          print commands instead of running them
  --list             list available actions and exit
  -h, --help         show this help
```

- Flags may appear in any order and can be combined: `tu --flatpak --podman`.
- `--all` runs every action in table order.
- The exit code is the last nonzero command status (`127` if a program could
  not be spawned), so it is safe to script around.
- Non-option words are ignored, matching classic getopt behavior.

Examples:

```sh
tu --list             # show actions and exit
tu --all --dry-run    # preview every command
tu --flatpak --system # update flatpaks, then the system image
```

### Interactive menu

| Key        | Action                                              |
|------------|-----------------------------------------------------|
| `↑`/`↓` (or `k`/`j`) | Move cursor (wraps)                       |
| `space`    | Toggle selection of highlighted action              |
| `a`        | Select every action                                 |
| `enter`    | Run selected actions (highlighted one if none selected) |
| `q` / `ctrl+c` | Quit                                            |

While running: each step shows live output in a scrolling panel with an
animated progress bar; `ctrl+c` aborts (kills the running child) and exits
with status `130`. When finished: `esc` returns to the menu (selections kept),
`q`/`enter` exits with the overall status.

Add `--dry-run` before entering the menu (`tu --dry-run`) to preview
commands instead of executing them.
