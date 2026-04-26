<div align="center">

<img src="docs/images/amnesiai_icon.png" alt="amnesiai" width="160">

# git your ai setup

[![CI](https://github.com/thepixelabs/amnesiai/actions/workflows/ci.yml/badge.svg)](https://github.com/thepixelabs/amnesiai/actions)
[![Go 1.24](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)](https://go.dev)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache_2.0-blue.svg)](LICENSE)

</div>

You've spent hours tuning your AI coding assistants — custom instructions, memory files, agent configs, theme preferences. Then you get a new machine, or something overwrites your setup, and it's gone. **amnesiai** versions those configs the same way you version your code.

---

## Install

```sh
brew install thepixelabs/tap/amnesiai
```

---

## Quick start

The first time you run `amnesiai`, it walks you through a setup wizard: storage mode, git provider (if applicable), backup directory, which providers to include, encryption default, and auto-commit/push preferences. The wizard takes about two minutes.

```sh
amnesiai
```

After setup, the TUI is your home base. Single-letter hotkeys fire immediately:

| Key | Action |
|-----|--------|
| `b` | Backup |
| `r` | Restore |
| `d` | Diff |
| `l` | List snapshots |
| `?` | Help |
| `q` | Quit |

Or skip the TUI and use commands directly:

```sh
# Back up all providers
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

Re-run the setup wizard at any time:

```sh
amnesiai --settings
```

---

## Providers

| Provider | Backed up | Never touched |
|---|---|---|
| **Claude Code** (`~/.claude/`) | `CLAUDE.md`, `settings.json` | `projects/` (history), `todos/`, `ide/`, `settings.local.json`, `statsig/`, `.credentials.json` |
| **Gemini CLI** (`~/.gemini/`) | `GEMINI.md`, `settings.json`, `themes/` | `*.key` files, any file prefixed `auth` |
| **GitHub Copilot** (OS config dir) | All `*.json` files, including `hosts.json` | Any file whose name contains `token`, `secret`, `key`, `credential`, or `auth` |
| **Codex CLI** (`~/.codex/`) | `config.json`, `instructions.md`, `themes/` | `*.key` files, any file containing `token` or `credential` |

Copilot's base directory is `~/Library/Application Support/GitHub Copilot/` on macOS, `~/.config/github-copilot/` on Linux, and `%APPDATA%/GitHub Copilot/` on Windows.

For the full scope policy, including the reasoning behind each inclusion and exclusion, see [docs/backup-scope-policy.md](docs/backup-scope-policy.md).

Back up a subset of providers:

```sh
amnesiai backup --providers claude,gemini
```

---

## Storage modes

- **`local`** — compressed tarballs in `~/.amnesiai/backups/`. Default. No git required.
- **`git-local`** — local git repo with full history, never pushes. Good for point-in-time diffs without a remote.
- **`git-remote`** — commit + push via `gh` (GitHub) or `glab` (GitLab) CLI. Requires one of those CLIs to be installed and authenticated. amnesiai checks that the destination repo is private before every push and aborts if it is not.

Set the mode during the onboarding wizard, in `~/.amnesiai/config.toml`, or with `--storage-mode` on any command.

### Multi-account git (git-remote mode)

During onboarding, amnesiai reads `gh auth list` and lets you pick which account to use per remote URL. That binding is saved in `~/.amnesiai/state.json` — not in `config.toml`. You can create a new private repo directly from the wizard via `gh repo create`.

---

## Shell completion

`completion` is a command, not a flag. It prints the shell script your shell uses for tab completion. You can also reach it from the `?` help screen inside the TUI.

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

Backups are encrypted with [age](https://age-encryption.org) using a passphrase you supply. amnesiai stores no keys.

When encryption is on (the default), gitleaks scans your files for secrets but does **not** modify them — the raw bytes go into the encrypted archive. Restore is fully lossless. During backup, amnesiai reports how many secrets were found: `3 secrets found (encrypted in archive) [press d for details]`.

Set your passphrase via environment variable or file descriptor (preferred for scripts):

```sh
# Environment variable
export AMNESIAI_PASSPHRASE="correct horse battery staple"
amnesiai backup

# File descriptor — safer in scripts; passphrase never appears in argv or shell history
amnesiai backup --passphrase-fd 3  3<<<$'correct horse battery staple'
```

To skip encryption, pass `--no-encrypt`. If gitleaks detects secrets, `--no-encrypt` is refused. Use `--force-no-encrypt` to explicitly proceed — doing so writes secrets to the archive in plaintext and is irreversible.

Restore decrypts automatically when the passphrase is available.

---

## Secret scanning

Before any file enters a backup, [gitleaks](https://github.com/gitleaks/gitleaks) scans it for credentials.

- **Encryption on (default):** raw bytes are encrypted in the archive. Nothing is redacted. Restore is lossless.
- **`--force-no-encrypt`:** secrets are written to the archive as plaintext. amnesiai prints a warning. This path is explicitly opt-in and not recommended.

---

## Config reference

### `~/.amnesiai/config.toml`

Created automatically on first run with these defaults:

| Key | Default | Description |
|---|---|---|
| `storage_mode` | `"local"` | `local`, `git-local`, or `git-remote` |
| `backup_dir` | `~/.amnesiai/backups` | Where snapshots are written |
| `providers` | all four | Subset to back up by default |
| `git_remote.url` | — | Remote URL (required for `git-remote` mode) |
| `git_remote.branch` | `"main"` | Branch to push to |
| `auto_commit` | `true` | Commit after each backup in git modes |
| `auto_push` | `false` | Push after each commit in `git-remote` mode |
| `first_run` | `true` | Set to `false` after onboarding wizard completes. Reset to `true` to re-run the wizard (or use `--settings`). |
| `backup_count` | `0` | Running count of backups completed. Used to decide when to collapse contextual help. |
| `verbose_help` | `true` | Show contextual help tips inside the TUI. Auto-set to `false` after 3 backups. |
| `telemetry` | `false` | When `true`, amnesiai writes local usage counts to `~/.amnesiai/metrics.json`. Nothing is transmitted. Off by default. |

All keys can be overridden by environment variables (`AMNESIAI_STORAGE_MODE`, `AMNESIAI_BACKUP_DIR`, etc.) or by CLI flags. Run `amnesiai --help` for the full flag list.

### `~/.amnesiai/state.json`

Stores runtime state that is not user configuration: git account bindings for `git-remote` mode, onboarding completion markers, and similar. Do not edit this file by hand. It is not backed up.

---

## Commands

### `amnesiai` (root)

| Flag | Description |
|---|---|
| `--settings` | Re-run the onboarding wizard |
| `--storage-mode` | Override storage mode for this invocation |
| `--help` | Show help |

### `amnesiai backup`

| Flag | Description |
|---|---|
| `--providers` | Comma-separated list of providers to back up |
| `--dry-run` | Show what would be backed up without writing anything |
| `--message` | Override the auto-generated commit message |
| `--no-encrypt` | Skip encryption. Refused if secrets are detected — use `--force-no-encrypt` to override. |
| `--force-no-encrypt` | Skip encryption even when secrets are detected. Writes secrets in plaintext. |
| `--passphrase-fd <int>` | Read passphrase from this file descriptor instead of the environment |
| `--storage-mode` | Override storage mode for this invocation |

> `--passphrase` has been removed. Use `AMNESIAI_PASSPHRASE` or `--passphrase-fd` instead.

### `amnesiai restore`

| Flag | Description |
|---|---|
| `--id` | Snapshot ID to restore |
| `--providers` | Restore only these providers |
| `--merge` | Merge restored files with current (default for JSON) |
| `--overwrite` | Overwrite current files without merging |
| `--dry-run` | Preview changes without writing anything |

### `amnesiai list`

| Flag | Description |
|---|---|
| `--limit` | Maximum number of snapshots to show |

### `amnesiai diff`

```sh
amnesiai diff <refA> [refB]
```

Omit `refB` to diff `refA` against the current state on disk.

### `amnesiai completion`

```sh
amnesiai completion zsh|bash|fish
```

---

## Security model

- **Encryption is on by default.** The entire backup payload is a single `payload.age` archive encrypted with your passphrase. filenames, paths, and content are all inside the ciphertext.
- **gitleaks scans every file before backup.** When encryption is on, detected secrets stay in the payload — encrypted and safe. When `--force-no-encrypt` is used, secrets are written in plaintext and amnesiai warns you explicitly.
- **`--no-encrypt` is refused when secrets are detected.** Use `--force-no-encrypt` to acknowledge you understand the risk.
- **`metadata.json` sits unencrypted** alongside `payload.age`. It contains: tool categories changed, file counts, timestamp, ciphertext SHA256. It never contains filenames, paths, or content.
- **Restore shows a diff of hook changes** and requires separate explicit confirmation before applying them.
- **Private-repo check before every push.** amnesiai aborts if the destination repo is public.
- **Path traversal validation on every restore.**
- **Passphrase never stored.** amnesiai does not write your passphrase anywhere. Use `AMNESIAI_PASSPHRASE` or `--passphrase-fd` for non-interactive use.

---

## License

This project is open source distributed under the **Apache License, Version 2.0**.

You may use, modify, and distribute this software for personal and internal business operations. Commercial use is permitted, provided it does not directly compete with the primary product or services offered by the repository owner.

Please refer to the [`LICENSE`](LICENSE) file for the complete terms and conditions.

[amnesiai.pixelabs.net](https://amnesiai.pixelabs.net) · [thepixelabs/amnesiai](https://github.com/thepixelabs/amnesiai)
