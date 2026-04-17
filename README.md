<div align="center">

<img src="docs/images/amnesiai_icon.png" alt="amnesiai" width="160">

# git your ai setup

[![CI](https://github.com/thepixelabs/amnesiai/actions/workflows/ci.yml/badge.svg)](https://github.com/thepixelabs/amnesiai/actions)
[![Go 1.24](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

You've spent hours tuning your AI coding assistants — custom instructions, memory files, agent configs, theme preferences. Then you get a new machine, or something overwrites your setup, and it's gone. **amnesiai** versions those configs the same way you version your code.

---

## Install

```sh
brew install thepixelabs/tap/amnesiai
```

---

## Quick start

```sh
# Open the interactive terminal UI
amnesiai

# Or use the classic argument-based commands
amnesiai backup

# See what's stored
amnesiai list

# Check what's drifted since last backup
amnesiai diff

# Recover configs on a new machine (or after a bad day)
amnesiai restore
```

Restore a specific snapshot by ID:

```sh
amnesiai restore --id 20240416T143022
```

---

## Providers

| Provider | Backed up | Never touched |
|---|---|---|
| **Claude Code** (`~/.claude/`) | `CLAUDE.md`, `settings.json`, `settings.local.json`, `todos/`, `ide/` | `projects/` (history), `statsig/`, `.credentials.json` |
| **Gemini CLI** (`~/.gemini/`) | `GEMINI.md`, `settings.json`, `themes/` | `*.key` files, any file prefixed `auth` |
| **GitHub Copilot** (OS config dir) | All `*.json` files, including `hosts.json` | Any file whose name contains `token`, `secret`, `key`, `credential`, or `auth` |
| **Codex CLI** (`~/.codex/`) | `config.json`, `instructions.md`, `themes/` | `*.key` files, any file containing `token` or `credential` |

Copilot's base directory is `~/Library/Application Support/GitHub Copilot/` on macOS, `~/.config/github-copilot/` on Linux, and `%APPDATA%/GitHub Copilot/` on Windows.

Back up a subset of providers:

```sh
amnesiai backup --providers claude,gemini
```

---

## Storage modes

- **`local`** — compressed tarballs in `~/.amnesiai/backups/`. Default. Fully implemented.
- **`git-local`** — planned, but not implemented in the current binary yet.
- **`git-remote`** — planned, but not implemented in the current binary yet.

Set the mode in `~/.amnesiai/config.toml` or pass `--storage-mode` on any command.

---

## Shell completion

`completion` is a command, not a flag. It prints the shell script your shell uses for tab completion.

```sh
# zsh
amnesiai completion zsh > ~/.zfunc/_amnesiai

# bash
amnesiai completion bash > ~/.local/share/bash-completion/completions/amnesiai

# fish
amnesiai completion fish > ~/.config/fish/completions/amnesiai.fish
```

Reload your shell after writing the file.

---

## Encryption

Backups are encrypted with [age](https://age-encryption.org) using a passphrase you supply. amnesiai stores no keys. Set `AMNESIAI_PASSPHRASE` in your environment, or pass `--passphrase` at the command line. To skip encryption explicitly, pass `--no-encrypt`.

```sh
export AMNESIAI_PASSPHRASE="correct horse battery staple"
amnesiai backup
```

Restore decrypts automatically when the passphrase is available.

---

## Secret scanning

Before any file enters a backup, [gitleaks](https://github.com/gitleaks/gitleaks) scans it for credentials. Detected secrets are redacted inline as `<REDACTED:type>` — the backup is still written, but the value is never committed. A warning is printed to stderr per provider: `WARNING: 3 secret(s) redacted in claude`.

---

## Config reference

`~/.amnesiai/config.toml` — created automatically on first run with these defaults:

| Key | Default | Description |
|---|---|---|
| `storage_mode` | `"local"` | `local`, `git-local`, or `git-remote` |
| `backup_dir` | `~/.amnesiai/backups` | Where snapshots are written |
| `providers` | all four | Subset to back up by default |
| `git_remote.url` | — | Remote URL (required for `git-remote` mode) |
| `git_remote.branch` | `"main"` | Branch to push to |
| `auto_commit` | `true` | Commit after each backup in git modes |
| `auto_push` | `false` | Push after each commit in `git-remote` mode |

All keys can be overridden by environment variables (`AMNESIAI_STORAGE_MODE`, `AMNESIAI_BACKUP_DIR`, etc.) or by CLI flags. Run `amnesiai --help` for the full flag list.

---

## License

MIT — see [LICENSE](LICENSE).

[amnesiai.pixelabs.net](https://amnesiai.pixelabs.net) · [thepixelabs/amnesiai](https://github.com/thepixelabs/amnesiai)
