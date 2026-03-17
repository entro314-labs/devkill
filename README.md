# devkill

A modern TUI to find and delete heavy dev artifacts across languages and platforms.


## Usage

### Installation

#### Go toolchain

`$ go install github.com/entro314-labs/devkill@latest`

#### No Go toolchain

Download a binary from the Releases tab and place it in your `$PATH`.

### Starting npkill

`$ devkill <directory>` opens devkill in a directory _relative_ to `$PWD`.

`$ devkill` opens devkill in `$PWD`.

### Flags

`--include` Add extra target directory names (comma-separated).

`--exclude` Remove target directory names from the built-in list (comma-separated).

`--depth` Maximum directory depth to scan (0 = unlimited).

`--list-targets` Print target directory names and exit.

`--config` Load a JSON config file.

`--no-confirm` Delete without confirmation prompts.

### Interactions

Move through the table with the arrow keys (`↑`, `↓`).

Queue an entry with `Space`.

Queue every entry with `a`.

Clear the queue with `A`.

Delete the selected entry with `⏎` / `d` (with confirmation).

Delete all queued entries with `D` (with confirmation).

Rescan with `r`.

Cycle sorting with `s`.

Recalculate the selected entry size with `u`.

Toggle confirmations with `c`.

Toggle help with `?`.

Quit with `q`.

### Targets

Built-in targets include `target`, `node_modules`, `.venv`, `.cache`, `.m2`, `.gradle`, `.cargo`, `.pub-cache`, `.gem`, `.nuget`, `.yarn`, `.pnpm`, `.pipenv`, `.poetry`, `.virtualenvs`, `vendor`, `dist`, `.turbo`, `.next`, `.nuxt`, `.expo`, `.react-native`, and more.

Run `devkill --list-targets` to see the full list.

### Config file

The app looks for a config file in:

- `./.devkill.json`
- `$XDG_CONFIG_HOME/devkill/config.json`
- `~/.config/devkill/config.json`

Use `--config` to point to a specific file.

Example:

```json
{
	"include": [".idea", ".vscode"],
	"exclude": ["dist"],
	"depth": 6,
	"skip": [".git", ".cache"],
	"confirm": false
}
```

## Building it

Make sure you have a [Go Toolchain](https://go.dev/dl/) installed on your system.

`$ go build .` produces the executable.

## Release pipeline

Releases are fully automated through GitHub Actions + GoReleaser (`.github/workflows/release.yml` and `.goreleaser.yaml`).

On version tags (`v*.*.*`), the pipeline will:

- run tests and dependency verification
- build multi-platform binaries (Linux, macOS, Windows on amd64/arm64)
- create checksums and SBOM artifacts
- publish a GitHub Release
- publish/update package manager artifacts for:
	- Homebrew Cask (`homebrew_casks`)
	- AUR (`aurs`)
	- Winget (`winget`, via PR workflow)

### Required repository secrets

Configure these in GitHub repository secrets before your first publish:

- `GITHUB_TOKEN` (provided by Actions for this repository)
- `HOMEBREW_TAP_GITHUB_TOKEN` (PAT with write access to your tap repository)
- `AUR_SSH_PRIVATE_KEY` (SSH key for your AUR package repository)
- `WINGET_GITHUB_TOKEN` (PAT for your `winget-pkgs` fork)

### Homebrew note

GoReleaser `brews` is deprecated; this project is configured with `homebrew_casks`.

