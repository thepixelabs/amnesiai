<div align="center">

<img src="docs/images/amnesiai_icon.png" alt="amnesiai" width="160">

# git your ai setup

[![CI](https://github.com/thepixelabs/amnesiai/actions/workflows/ci.yml/badge.svg)](https://github.com/thepixelabs/amnesiai/actions)
[![Go 1.24](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)](https://go.dev)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache_2.0-blue.svg)](LICENSE)

</div>

You've spent hours tuning your AI coding assistants — custom instructions, memory files, agent configs, theme preferences. Then you get a new machine, or something overwrites your setup, and it's gone. **amnesiai** versions those configs the same way you version your code: encrypted snapshots, secret-scanned, optionally pushed to a private git remote.

---

## Why amnesiai

- **Configs are dotfiles now.** `CLAUDE.md`, `GEMINI.md`, agent definitions, and tool settings represent hours of careful tuning. Treat them like the source code they effectively are — versioned, diffable, restorable.
- **New machine, same brain.** `amnesiai restore` rebuilds your assistants on a fresh laptop in seconds. No copy-paste from screenshots. No "what did I have on the old box?"
- **Drift detection, on demand.** `amnesiai diff` shows what's changed since your last snapshot. Catch a setting an installer rewrote, an agent file you forgot to commit, or a memory file that got truncated.
- **Encrypted by default, secret-scanned every time.** Backups are AES-encrypted via `age`; `gitleaks` scans every file before it enters the archive. The default path keeps secrets in the encrypted payload — never redacted, never leaked.
- **No daemon, no cloud, no lock-in.** Local tarballs, a local git repo, or a private GitHub/GitLab remote — your choice. amnesiai stores no keys. Encrypted backups travel anywhere age can decrypt them.

---

## Install

**Homebrew (macOS and Linux)**

```sh
brew install thepixelabs/tap/amnesiai
```

**Requirements:** Git (for `git-local` and `git-remote` modes), `gh` or `glab` CLI authenticated (for `git-remote` mode only).

---

## Quick start

The first time you run `amnesiai`, it walks you through a setup wizard: storage mode, git provider (if applicable), backup directory, which providers to include, encryption default, and auto-commit/push preferences. The wizard takes about two minutes.

```sh
amnesiai
```

After setup, the TUI is your home base. Single-letter hotkeys fire immediately.

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

### What to try next

- `amnesiai backup --providers claude,gemini` — back up only a subset
- `amnesiai diff <refA> [refB]` — diff two snapshots (or one snapshot vs current state)
- `amnesiai backup --dry-run` — see what *would* be backed up without writing anything
- `amnesiai backup --passphrase-fd 3 3<<<"$PASS"` — non-interactive passphrase via FD (no shell history exposure)
- `amnesiai restore --out-dir ~/sandbox` — extract a backup into a sandbox directory to inspect before applying

---

## Features

| Feature | What it means |
|---|---|
| **Provider-aware backup** | Knows the on-disk layout of Claude Code, Gemini CLI, Codex CLI, and GitHub Copilot. Includes the right files. Excludes credentials and chat history by default. |
| **Three storage modes** | Local tarballs, a local git repo (full history, no remote), or a private GitHub/GitLab repo via `gh`/`glab`. |
| **Encryption by default** | AES-256 via [age](https://age-encryption.org). Single `payload.age` archive — filenames, paths, and content all inside the ciphertext. |
| **Pre-backup secret scan** | [gitleaks](https://github.com/gitleaks/gitleaks) runs on every file before it enters the archive. With encryption on (default), raw bytes go in untouched and stay safe inside the ciphertext. |
| **Multi-account git** | `gh auth list` integration during onboarding lets you bind a remote URL to a specific account. Stored in `state.json`, not `config.toml`. |
| **Private-repo guard** | amnesiai checks the destination repo is private before every push and aborts if it is not. |
| **Lossless restore** | The original raw bytes go into the encrypted archive. Restore reproduces the exact files. |
| **Drift detection** | `amnesiai diff` compares any snapshot to disk (or to another snapshot) so you see what's changed before committing. |
| **Backup retention** | `keep_last` and `max_age_days` policies prune old snapshots automatically or on demand. Opt-in — zero values (the default) disable all automatic deletion. |
| **TUI hotkeys + scripted CLI** | Single-letter hotkeys for everyday actions; full CLI with `--dry-run`, `--providers`, `--storage-mode` overrides for automation. |

---

## Providers

| Provider | Backed up | Never touched |
|---|---|---|
| **Claude Code** (`~/.claude/`) | `CLAUDE.md`, `settings.json`, `keybindings.json`, `agents/*.md`, `commands/*.md`, `skills/<name>/*`; per-project `CLAUDE.md` and `.claude/settings.json` | `projects/` (history), `todos/`, `ide/`, `settings.local.json`, `statsig/`, `plugins/` (re-installable), `sessions/`, `paste-cache/`, `file-history/`, `shell-snapshots/`, `telemetry/`, `.credentials.json`, `*.jsonl` |
| **Gemini CLI** (`~/.gemini/`) | `settings.json`, `GEMINI.md`, `projects.json`, `trustedFolders.json`, `themes/`, `agents/`, `commands/`, `extensions/` | `*.key`, files starting with `auth`, anything containing `oauth`, `creds`, `credential`, `token`, `secret` (case-insensitive); `tmp/`, `history/`, `state.json`, `installation_id`, `antigravity*/` |
| **GitHub Copilot CLI** (`~/.copilot/`, or `$COPILOT_HOME`) | `settings.json`, `config.json`, `mcp-config.json`, `lsp-config.json`, `agents/*.md`; per-project `.github/copilot-instructions.md` | Any file whose name contains `token`, `secret`, `key`, `credential`, or `auth`; `command-history-state.json`, `logs/`, `session-state/`, `ide/`, `skills/`, `hooks/` |
| **Codex CLI** (`~/.codex/`) | `config.toml`, `AGENTS.md`, `instructions.md`, `agents/*.toml`, `rules/default.rules`, `memories/**`, `skills/**` (excluding `.system/`) | `auth.json`, `history.jsonl`, `sessions/`, `log/`, `*.sqlite*`, `models_cache.json`, `installation_id`, `version.json`, `.personality_migration`, `.tmp/`, `tmp/`, `shell_snapshots/`, `cache/`, `skills/.system/`; any `*.key` or any file containing `token`, `credential`, `auth`, `secret` |

Set `COPILOT_HOME` to point at a non-default Copilot config location. Older VS Code-bundled Copilot (the `~/Library/Application Support/GitHub Copilot/` path on macOS) is out of scope.

For the full scope policy, including the reasoning behind each inclusion and exclusion, see [docs/backup-scope-policy.md](docs/backup-scope-policy.md).

### Symlink handling

- **Backup:** symlinks-to-files are followed — the symlink target's content is captured. This means dotfile-manager setups (e.g. `~/.claude/CLAUDE.md → ~/dotfiles/CLAUDE.md`) work correctly.
- **Backup:** symlinks-to-directories are skipped to prevent traversal loops.
- **Restore:** if the destination path is itself a symlink, amnesiai writes through it to the underlying target file. The symlink itself is preserved; it is never replaced with a regular file.

Back up a subset of providers:

```sh
amnesiai backup --providers claude,gemini
```

### Customising what each provider backs up

If a provider's defaults don't match your setup — say upstream added a new file
that amnesiai doesn't yet know about, or you want to exclude a generated config
that you don't want versioned — add a `provider_overrides` block to
`~/.amnesiai/config.toml`:

```toml
[provider_overrides.claude]
extra_files   = ["my-custom-prompt.md"]   # added to the top-level allowlist
exclude_files = ["keybindings.json"]      # removed from defaults

[provider_overrides.codex]
extra_files = ["scratchpad.md"]
```

Both `extra_files` and `exclude_files` are case-sensitive basenames matched
against files **directly** beneath the provider's base directory. They do not
extend recursive subdirectory walking — to add a whole new subdirectory tree,
open an issue. Entries for unknown provider names are ignored with a warning;
stale config never bricks the binary.

---

## Storage modes

- **`local`** — compressed tarballs in `~/.amnesiai/backups/`. Default. No git required.
- **`git-local`** — local git repo with full history, never pushes. Good for point-in-time diffs without a remote.
- **`git-remote`** — commit + push via `gh` (GitHub) or `glab` (GitLab) CLI. Requires one of those CLIs to be installed and authenticated. amnesiai checks that the destination repo is private before every push and aborts if it is not.

Set the mode during the onboarding wizard, in `~/.amnesiai/config.toml`, or with `--storage-mode` on any command.

### Multi-account git (git-remote mode)

During onboarding, amnesiai reads `gh auth list` and lets you pick which account to use per remote URL. That binding is saved in `~/.amnesiai/state.json` — not in `config.toml`. You can create a new private repo directly from the wizard via `gh repo create`.

---

## Encryption & secret scanning

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

| Path | Behavior |
|---|---|
| Encryption on (default) | Raw bytes are encrypted in the archive. Nothing is redacted. Restore is lossless. |
| `--no-encrypt` | Allowed only if gitleaks finds no secrets. Otherwise refused. |
| `--force-no-encrypt` | Secrets are written to the archive as plaintext. amnesiai prints a warning. Explicit opt-in only. |

New backups use scrypt work factor 20 (~4 s/attempt on reference hardware), up from age's default of 18. This doubles brute-force cost with no user-visible latency on a single backup. Older backups encrypted at factor 18 continue to decrypt without any flag or migration.

---

## Troubleshooting

**Wrong passphrase at restore**

amnesiai reports `incorrect passphrase` when the passphrase does not match. If you see age's internal `no identity matched any of the recipients` wording, you are running an older build; upgrade to the latest release.

**0 files backed up**

amnesiai prints a loud warning when a backup archives 0 files. Likely causes:

- Provider directories are empty or absent (`~/.claude`, `~/.codex`, `~/.gemini`, `~/.copilot`).
- The files present are not in amnesiai's allowlist. Add extra basenames via `[provider_overrides.<name>] extra_files` in `config.toml`.
- No per-project paths are configured. Add them to `project_paths` in `config.toml`.

**Hand-edited `config.toml` with `git-local` or `git-remote` but backup repo not initialised**

amnesiai auto-inits the local git repo before any storage operation when the configured mode is `git-local` or `git-remote`. You can safely set the mode in `config.toml` directly without running the onboarding wizard — the repo will be initialised on first use.

---

## How it works

```
~/.claude/   ┐
~/.gemini/   │ ─► gitleaks scan ─► age encrypt ─► storage
~/.codex/    │                                         │
~/.copilot/  ┘                                         ▼
                        ┌────────────────┬─────────────────┬──────────────────┐
                        │   local mode   │  git-local mode │  git-remote mode │
                        │  ~/.amnesiai/  │  local git repo │  private GitHub  │
                        │   backups/     │  full history   │  / GitLab repo   │
                        │                │  (no push)      │  (private check) │
                        └────────────────┴─────────────────┴──────────────────┘

  ~/.amnesiai/config.toml   defaults set by onboarding (or hand-edited)
  ~/.amnesiai/state.json    runtime state — git account bindings, etc. Do not edit by hand.
  ~/.amnesiai/backups/      where local snapshots live
```

A snapshot is a single `payload.age` ciphertext alongside an unencrypted `metadata.json` (tool categories changed, file counts, timestamp, ciphertext SHA256 — never filenames, paths, or content). The same archive layout is used in all three storage modes; only the destination differs.

Restore reverses the pipeline: decrypt → unpack → diff against current files → apply (with explicit confirmation for hook changes and a path-traversal check on every entry).

---

## Configuration

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
| `verbose_help` | `false` | Show contextual help tips inside the TUI. Toggle in Settings or auto-disabled after several backups. |
| `backup_show_files` | `false` | Print the full per-file path list after a backup. Default shows provider file counts only. Toggle in Settings. |
| `provider_overrides` | `{}` | Per-provider allowlist tweaks. See [Customising what each provider backs up](#customising-what-each-provider-backs-up). |
| `retention.keep_last` | `0` | Always keep the N most-recent backups (0 = no count limit). |
| `retention.max_age_days` | `0` | Delete backups older than N days (0 = no age limit). |
| `retention.auto_prune` | `false` | Run the retention policy after every successful backup. |

**Retention is opt-in.** Existing installs that upgrade without changing config will see no automatic deletions. Both limit fields default to `0`, which disables retention entirely.

The two retention windows are combined with OR: a backup is kept when its index in the newest-first list is below `keep_last` **or** its age is below `max_age_days`. Whichever window is more permissive wins. Example: `keep_last=10, max_age_days=30` always preserves the 10 newest backups and also preserves anything from the last 30 days — the actual count kept may exceed 10.

Example `~/.amnesiai/config.toml` retention block:

```toml
[retention]
keep_last     = 50    # always keep the 50 most recent (0 = no count limit)
max_age_days  = 90    # delete backups older than 90 days (0 = no age limit)
auto_prune    = true  # run pruning after every successful backup
```

All keys can be overridden by environment variables (`AMNESIAI_STORAGE_MODE`, `AMNESIAI_BACKUP_DIR`, etc.) or by CLI flags. Run `amnesiai --help` for the full flag list.

### `~/.amnesiai/state.json`

Stores runtime state that is not user configuration: git account bindings for `git-remote` mode, onboarding completion markers, and similar. Do not edit this file by hand. It is not backed up.

---

## Commands

### `amnesiai` (root)

| Flag | Description |
|---|---|
| `--settings` | Re-run the onboarding wizard |
| `--storage-mode <mode>` | Override storage mode for this invocation |
| `--help` | Show help |

### `amnesiai backup`

| Flag | Description |
|---|---|
| `--providers <list>` | Comma-separated list of providers to back up |
| `--dry-run` | Show what would be backed up without writing anything |
| `--message <msg>` | Override the auto-generated commit message |
| `--no-encrypt` | Skip encryption. Refused if secrets are detected — use `--force-no-encrypt` to override. |
| `--force-no-encrypt` | Skip encryption even when secrets are detected. Writes secrets in plaintext. |
| `--passphrase-fd <int>` | Read passphrase from this file descriptor instead of the environment |
| `--storage-mode <mode>` | Override storage mode for this invocation |

> `--passphrase` has been removed. Use `AMNESIAI_PASSPHRASE` or `--passphrase-fd` instead.

### `amnesiai restore`

| Flag | Description |
|---|---|
| `--id <id>` | Snapshot ID to restore (default: latest) |
| `--providers <list>` | Restore only these providers |
| `--dry-run` | Preview what would be restored without writing anything |
| `--out-dir <path>` | Extract files into this directory instead of overwriting real destinations. Mirrors the full destination layout so you can inspect before applying. Refuses paths that overlap any provider base or configured project path. |
| `--force` | With `--out-dir`: allow writing into a non-empty directory (additive, never deletes) |

```sh
# Safe inspection before you restore — nothing real is touched
amnesiai restore --id 20240416T143022 --out-dir ~/sandbox-restore

# Force-write into a pre-existing sandbox dir (additive; never deletes)
amnesiai restore --out-dir ~/sandbox-restore --force
```

### `amnesiai list`

| Flag | Description |
|---|---|
| `--limit <n>` | Maximum number of snapshots to show |

### `amnesiai prune`

Apply the retention policy on demand. Flags override the configured policy for the current run only — `config.toml` is not modified.

| Flag | Description |
|---|---|
| `--dry-run` | Show what would be deleted without removing anything |
| `--keep-last <n>` | Override `retention.keep_last` for this run |
| `--max-age-days <n>` | Override `retention.max_age_days` for this run |
| `--yes` | Skip the confirmation prompt (required in non-TTY contexts) |

```sh
amnesiai prune --dry-run                    # show what would be deleted
amnesiai prune                              # apply config retention (prompts in TTY)
amnesiai prune --keep-last 10 --yes         # one-off override, skip prompt
```

Without `--yes`, `prune` refuses to delete in a non-TTY context to protect scripts that loop over multiple repositories.

### `amnesiai delete <id>`

Permanently remove a single backup by ID. In git modes, the deletion is committed (and pushed if `auto_push` is on) — recovery requires git history.

| Flag | Description |
|---|---|
| `--yes` | Skip the confirmation prompt (required in non-TTY contexts) |

```sh
amnesiai delete 20260504T120000Z            # shows metadata, prompts y/N
amnesiai delete 20260504T120000Z --yes      # skip prompt
```

### `amnesiai diff`

```sh
amnesiai diff <refA> [refB]
```

Omit `refB` to diff `refA` against the current state on disk.

---

## TUI

### Keyboard shortcuts

| Key | Action |
|-----|--------|
| `b` | Backup |
| `r` | Restore |
| `d` | Diff |
| `l` | List snapshots |
| `s` | Settings |
| `?` | Help |
| `q` | Quit |

### Settings menu

Open with `s` in the main menu or `amnesiai --settings`. Entries:

| Entry | What it does |
|---|---|
| Re-run onboarding wizard | Walks through storage mode, git provider, backup dir, and provider selection again |
| View config file path | Prints the path to `~/.amnesiai/config.toml` |
| Default providers: \<list\> | Opens the multi-select provider picker; selection is saved to `cfg.providers` |
| Backup output: counts only / full file list | Toggles `backup_show_files` — whether the post-backup summary prints every file path or just per-provider counts |
| Verbose help: ON/OFF | Toggles `verbose_help` contextual tips inside the TUI |
| Auto-prune: ON/OFF | Toggles `retention.auto_prune` — whether to run the retention policy after every successful backup |
| Retention: keep last N, max age M days | Opens the two-field numeric editor for `keep_last` and `max_age_days`. Tab switches fields, Enter saves, Esc cancels. Shows "disabled" when both are 0. |
| Prune now | Runs the retention policy immediately against the current config; prints how many backups were deleted |
| View remote bindings (state.json) | Shows the git account bindings for `git-remote` mode |
| Back to main menu | Returns without changes |

### Restore modes

When you choose `r` in the TUI, you pick one of three modes:

| Mode | What happens |
|---|---|
| **Restore to disk** | Decrypts and applies files to their real destinations. Requires confirmation. |
| **Dry run** | Shows what would be restored without writing anything. |
| **Inspect (extract to directory)** | Prompts for an output directory, extracts the backup there mirroring the real layout. No real files are touched. Equivalent to `--out-dir` on the CLI. |

### Label suggestions

Before each backup the TUI shows a multi-select list of recently-used `key=value` labels drawn from prior backups (newest first, up to 20 distinct pairs). Toggle suggestions with Space, confirm with Enter, or skip with `q`/Esc to go straight to the free-form label prompt. Typed labels override any selected suggestion on key collision.

### List view

`l` in the main menu (or `amnesiai list`) opens an interactive list of snapshots. Navigate with `↑`/`↓` or `j`/`k`. Press `d` on the highlighted row to delete that backup — a confirmation prompt appears before anything is removed. Press `y` to confirm or `n`/`Esc`/`q` to cancel. `q` or `Esc` exits the list.

### Navigation

In the provider picker, label picker, and backup selection prompt: `q`, Ctrl+C, and Esc all cancel back to the previous menu without error.

---

## Security model

- **Encryption is on by default.** The entire backup payload is a single `payload.age` archive encrypted with your passphrase. Filenames, paths, and content are all inside the ciphertext.
- **gitleaks scans every file before backup.** When encryption is on, detected secrets stay in the payload — encrypted and safe. When `--force-no-encrypt` is used, secrets are written in plaintext and amnesiai warns you explicitly.
- **`--no-encrypt` is refused when secrets are detected.** Use `--force-no-encrypt` to acknowledge you understand the risk.
- **`metadata.json` sits unencrypted** alongside `payload.age`. It contains: tool categories changed, file counts, timestamp, ciphertext SHA256. It never contains filenames, paths, or content.
- **Restore shows a diff of hook changes** and requires separate explicit confirmation before applying them.
- **Private-repo check before every push.** amnesiai aborts if the destination repo is public.
- **Path-traversal validation on every restore.**
- **Passphrase never stored.** amnesiai does not write your passphrase anywhere. Use `AMNESIAI_PASSPHRASE` or `--passphrase-fd` for non-interactive use.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security vulnerabilities: see [SECURITY.md](SECURITY.md).

## License

This project is licensed under the **Apache License, Version 2.0**.

You may obtain a copy of the License at <http://www.apache.org/licenses/LICENSE-2.0>.

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the [`LICENSE`](LICENSE) file for the specific language governing permissions and limitations under the License.

---

> [amnesiai.pixelabs.net](https://amnesiai.pixelabs.net) · [thepixelabs/amnesiai](https://github.com/thepixelabs/amnesiai) · an independent project by [Pixelabs](https://pixelabs.net)
