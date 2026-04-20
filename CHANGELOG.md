## [1.3.0](https://github.com/thepixelabs/amnesiai/compare/v1.2.0...v1.3.0) (2026-04-20)

### Features

* **track-f:** git-local + git-remote storage, gh/glab integration, fix path-traversal in ExtractArchive ([2fd9ffe](https://github.com/thepixelabs/amnesiai/commit/2fd9ffe163940fb1db108ef5db0b1f21e038e455))

### Bug Fixes

* **track-f:** wire tokenEnv, per-push privacy, shell-injection guard, exec timeouts, token redaction, path-traversal hardening ([1253e61](https://github.com/thepixelabs/amnesiai/commit/1253e613a5d5d2a767309db8d54b4eb57fb92309))

## [1.2.0](https://github.com/thepixelabs/amnesiai/compare/v1.1.0...v1.2.0) (2026-04-20)

### Features

* **track-e:** onboarding wizard, settings menu, state.json app-owned store ([f0cb332](https://github.com/thepixelabs/amnesiai/commit/f0cb33232826775ca4519437e1698379f2f92ffa))

### Bug Fixes

* **track-e:** wire BindRemote, stderr capture, async account discovery, schema guard ([7ee5859](https://github.com/thepixelabs/amnesiai/commit/7ee585942dc71c991fe9ea0d98ed51586f864c08))

## [1.1.0](https://github.com/thepixelabs/amnesiai/compare/v1.0.5...v1.1.0) (2026-04-20)

### Features

* **track-a:** storage schema, config fields, claude security hardening ([d669ee0](https://github.com/thepixelabs/amnesiai/commit/d669ee0d1c432c1792a07fcd23da79706978c4d9))
* **track-b:** redaction Option B + passphrase flag hardening ([2a0a609](https://github.com/thepixelabs/amnesiai/commit/2a0a609ede3545da05123a471b00b306387e9a72))
* **track-g:** codex + copilot provider completion, per-project enumeration, claude allowlist refactor ([0abc0f0](https://github.com/thepixelabs/amnesiai/commit/0abc0f0de4487ca9534c442b1a21ecd7d173b83f))

### Bug Fixes

* **track-g:** wire ProjectPaths through provider factory, validate, log once ([45aaf38](https://github.com/thepixelabs/amnesiai/commit/45aaf3833befbc13aa1053ff60db4336ae237901))

### Documentation

* **1.1.0:** add scope policy, migration guide, update README + CHANGELOG ([6e693fa](https://github.com/thepixelabs/amnesiai/commit/6e693fa2374580158c4285738091775677c4cdd4))
* **qa:** add test strategy for tracks A-F refactor ([9a4afda](https://github.com/thepixelabs/amnesiai/commit/9a4afdad6d11af94eb3be314207328b9f2c1d63f))

## [1.1.0](https://github.com/thepixelabs/amnesiai/compare/v1.0.5...v1.1.0) (2026-04-19)

### Added

* **First-run onboarding wizard** — on first launch, `amnesiai` walks you through storage mode, git provider, backup directory, default providers, encryption default, and auto-commit/push toggles. Re-trigger at any time with `amnesiai --settings` or via the Settings menu in the TUI.
* **`--settings` flag** — re-runs the onboarding wizard without entering the main TUI.
* **`--passphrase-fd <int>` flag** — reads the passphrase from the given file descriptor. Use this in scripts instead of passing the passphrase in argv.
* **`--force-no-encrypt` flag** — required to skip encryption when gitleaks detects secrets. Explicit opt-in to the unsafe/lossy path.
* **`git-local` storage mode** — local git repo with full commit history, never pushes.
* **`git-remote` storage mode** — commit + push via `gh` (GitHub) or `glab` (GitLab) CLI with multi-account support. Account binding is stored in `~/.amnesiai/state.json`.
* **Private-repo enforcement** — `git-remote` mode checks repo visibility before every push and aborts if the repo is public.
* **`gh repo create` support** — create a new private repo from the onboarding wizard without leaving the TUI.
* **Telemetry config key** — `telemetry = true` in `config.toml` enables local usage counts written to `~/.amnesiai/metrics.json`. Nothing is transmitted. Off by default.
* **New config keys:** `first_run`, `backup_count`, `verbose_help`, `telemetry` (see README Config reference).
* **`state.json`** — `~/.amnesiai/state.json` now holds runtime state (git account bindings, onboarding markers) separately from user config in `config.toml`.
* **Contextual help in TUI** — help tips auto-show for first 3 backups (`backup_count` gate) and collapse automatically after.
* **`?` help screen** — includes "Install shell completion" submenu. Completion help is no longer in the main menu.

### Changed

* **Encryption model (see Breaking):** when encryption is on, gitleaks scans files but does not modify them — raw bytes go into the encrypted archive. Restore is now fully lossless.
* **TUI redesign:** new header with `thin` figlet font and ocean cyan→blue gradient, version chip with async upgrade hint, single-letter hotkeys (`b`/`r`/`d`/`l`/`?`/`q`), arrow-key provider picker with `·` middle-dot selection markers, two-field passphrase entry with `·` masking and confirm field, label step with `?`-toggle help.
* **Claude provider backup scope tightened:** `~/.claude/todos/`, `~/.claude/ide/`, and `~/.claude/settings.local.json` are no longer backed up (PII / machine-local / credential risk). See `docs/backup-scope-policy.md` for the full scope rationale.
* `--no-encrypt` is now refused when gitleaks detects secrets. Use `--force-no-encrypt` to proceed.

### Fixed

* Restoring an encrypted 1.0.x backup no longer silently writes `<REDACTED:...>` placeholders over real config values — the redaction-before-encryption bug is fixed by the new encryption model.

### Security

* **Secret scanning no longer corrupts restores.** Previously, gitleaks-redacted bytes entered the encrypted archive, meaning restores wrote `<REDACTED:type>` placeholders in place of real values — a silent data-loss bug. Secrets are now encrypted in place; no redaction occurs before the archive is sealed.
* **`--passphrase` flag removed** — it exposed the passphrase via `argv` and shell history. Use `AMNESIAI_PASSPHRASE` env var or `--passphrase-fd`.
* **`--force-no-encrypt` required to bypass encryption when secrets are present** — removes the silent lossy path that `--no-encrypt` previously allowed.

### Breaking

* **`--passphrase` flag removed.** Scripts that passed `--passphrase` must switch to `AMNESIAI_PASSPHRASE` or `--passphrase-fd`. See [Upgrading from 1.0.x](docs/migration-1.1.md).
* **Redaction model changed.** Backups created with 1.0.x under encryption may contain `<REDACTED:...>` placeholders in the archive. Re-backup after upgrading to get lossless archives. See [Upgrading from 1.0.x](docs/migration-1.1.md).
* **Claude provider no longer backs up** `~/.claude/todos/`, `~/.claude/ide/`, or `~/.claude/settings.local.json`.

---

## [1.0.5](https://github.com/thepixelabs/amnesiai/compare/v1.0.4...v1.0.5) (2026-04-17)

### Bug Fixes

* **docs:** correct wordmark spelling, consolidate theme toggle, trim footer ([07b161f](https://github.com/thepixelabs/amnesiai/commit/07b161f66a72fbc85e93b46021b8c21cb57f3ac7))

## [1.0.4](https://github.com/thepixelabs/amnesiai/compare/v1.0.3...v1.0.4) (2026-04-17)

### Performance Improvements

* **docs:** compress landing page images (~10MB → ~1MB) ([c530a58](https://github.com/thepixelabs/amnesiai/commit/c530a58e21506192ff6d167f7848b225751f3fb2))

## [1.0.3](https://github.com/thepixelabs/amnesiai/compare/v1.0.2...v1.0.3) (2026-04-17)

### Bug Fixes

* **goreleaser:** put brew formula in Formula/ directory ([df49d9d](https://github.com/thepixelabs/amnesiai/commit/df49d9d2722851edccedb06b98794902f64cf643))

## [1.0.2](https://github.com/thepixelabs/amnesiai/compare/v1.0.1...v1.0.2) (2026-04-17)

### Bug Fixes

* **goreleaser:** remove git.url block so tap push uses GitHub API ([79091e4](https://github.com/thepixelabs/amnesiai/commit/79091e4e6153c7d57d11fbf40ea83223b0b45f33))

## [1.0.1](https://github.com/thepixelabs/amnesiai/compare/v1.0.0...v1.0.1) (2026-04-17)

### Code Refactoring

* rename amensiai to amnesiai + un-LFS docs images ([#6](https://github.com/thepixelabs/amnesiai/issues/6)) ([1bed3e1](https://github.com/thepixelabs/amnesiai/commit/1bed3e11fac2f1deb1f2a695a368395b23e42b6f))

## 1.0.0 (2026-04-17)

### Features

* add CLI source, CI/CD pipelines, and release infrastructure ([ad3ba8b](https://github.com/thepixelabs/amnesiai/commit/ad3ba8b5dc382ae35ebe30394b87a3440c206053))
* add landing page with responsive how-it-works sections ([e85b000](https://github.com/thepixelabs/amnesiai/commit/e85b00052dae5a74ed49a47f4c6c72737ea6473e))
* **landing:** add PixelLabs nav link, dark/light/system theme toggle, restore bird icon visibility ([f854b39](https://github.com/thepixelabs/amnesiai/commit/f854b39e36b7a57d3f0bc49c8dce53951af994c3))

### Bug Fixes

* **ci:** correct create-github-app-token SHA ([#2](https://github.com/thepixelabs/amnesiai/issues/2)) ([fbcb0e2](https://github.com/thepixelabs/amnesiai/commit/fbcb0e24fdb73e5238e7257a78e63be1905c54fe))
* **ci:** correct lint action SHA and make vuln-scan non-blocking ([#1](https://github.com/thepixelabs/amnesiai/issues/1)) ([d946dfb](https://github.com/thepixelabs/amnesiai/commit/d946dfb9768d6e293907fe85152d7d5bb570fc17))
* **ci:** use GH_TOKEN PAT for semantic-release ([#3](https://github.com/thepixelabs/amnesiai/issues/3)) ([b01e35a](https://github.com/thepixelabs/amnesiai/commit/b01e35ab6b37dbc64253d5ab0bf5c96851db608d)), closes [#2](https://github.com/thepixelabs/amnesiai/issues/2)
* correct Go version badge to 1.24 ([#5](https://github.com/thepixelabs/amnesiai/issues/5)) ([2db4030](https://github.com/thepixelabs/amnesiai/commit/2db40302ad0e306a0cac766c1ec6e549e145d65c))
* **deps:** upgrade golang.org/x/crypto to v0.50.0, add npm overrides for picomatch and brace-expansion ([bca7c24](https://github.com/thepixelabs/amnesiai/commit/bca7c2444eebe0672d9d68fd1b86a04da6ffc35f)), closes [#1-4](https://github.com/thepixelabs/amnesiai/issues/1-4) [#5-6](https://github.com/thepixelabs/amnesiai/issues/5-6)
* retrigger v1.0.0 release ([#4](https://github.com/thepixelabs/amnesiai/issues/4)) ([cec4800](https://github.com/thepixelabs/amnesiai/commit/cec4800642a9e948c7835a4a1d3a6d9bfe2bedda))

## [1.1.1](https://github.com/thepixelabs/amnesiai/compare/v1.1.0...v1.1.1) (2026-04-17)

### Bug Fixes

* **deps:** upgrade golang.org/x/crypto to v0.50.0, add npm overrides for picomatch and brace-expansion ([bca7c24](https://github.com/thepixelabs/amnesiai/commit/bca7c2444eebe0672d9d68fd1b86a04da6ffc35f)), closes [#1-4](https://github.com/thepixelabs/amnesiai/issues/1-4) [#5-6](https://github.com/thepixelabs/amnesiai/issues/5-6)

## [1.1.0](https://github.com/thepixelabs/amnesiai/compare/v1.0.0...v1.1.0) (2026-04-16)

### Features

* **landing:** add PixelLabs nav link, dark/light/system theme toggle, restore bird icon visibility ([f854b39](https://github.com/thepixelabs/amnesiai/commit/f854b39e36b7a57d3f0bc49c8dce53951af994c3))

## 1.0.0 (2026-04-16)

### Features

* add CLI source, CI/CD pipelines, and release infrastructure ([ad3ba8b](https://github.com/thepixelabs/amnesiai/commit/ad3ba8b5dc382ae35ebe30394b87a3440c206053))
* add landing page with responsive how-it-works sections ([e85b000](https://github.com/thepixelabs/amnesiai/commit/e85b00052dae5a74ed49a47f4c6c72737ea6473e))

### Bug Fixes

* **ci:** correct create-github-app-token SHA ([#2](https://github.com/thepixelabs/amnesiai/issues/2)) ([fbcb0e2](https://github.com/thepixelabs/amnesiai/commit/fbcb0e24fdb73e5238e7257a78e63be1905c54fe))
* **ci:** correct lint action SHA and make vuln-scan non-blocking ([#1](https://github.com/thepixelabs/amnesiai/issues/1)) ([d946dfb](https://github.com/thepixelabs/amnesiai/commit/d946dfb9768d6e293907fe85152d7d5bb570fc17))
* **ci:** use GH_TOKEN PAT for semantic-release ([#3](https://github.com/thepixelabs/amnesiai/issues/3)) ([b01e35a](https://github.com/thepixelabs/amnesiai/commit/b01e35ab6b37dbc64253d5ab0bf5c96851db608d)), closes [#2](https://github.com/thepixelabs/amnesiai/issues/2)
* correct Go version badge to 1.24 ([#5](https://github.com/thepixelabs/amnesiai/issues/5)) ([2db4030](https://github.com/thepixelabs/amnesiai/commit/2db40302ad0e306a0cac766c1ec6e549e145d65c))
* retrigger v1.0.0 release ([#4](https://github.com/thepixelabs/amnesiai/issues/4)) ([cec4800](https://github.com/thepixelabs/amnesiai/commit/cec4800642a9e948c7835a4a1d3a6d9bfe2bedda))

## 1.0.0 (2026-04-16)

### Features

* add CLI source, CI/CD pipelines, and release infrastructure ([ad3ba8b](https://github.com/thepixelabs/amnesiai/commit/ad3ba8b5dc382ae35ebe30394b87a3440c206053))
* add landing page with responsive how-it-works sections ([e85b000](https://github.com/thepixelabs/amnesiai/commit/e85b00052dae5a74ed49a47f4c6c72737ea6473e))

### Bug Fixes

* **ci:** correct create-github-app-token SHA ([#2](https://github.com/thepixelabs/amnesiai/issues/2)) ([fbcb0e2](https://github.com/thepixelabs/amnesiai/commit/fbcb0e24fdb73e5238e7257a78e63be1905c54fe))
* **ci:** correct lint action SHA and make vuln-scan non-blocking ([#1](https://github.com/thepixelabs/amnesiai/issues/1)) ([d946dfb](https://github.com/thepixelabs/amnesiai/commit/d946dfb9768d6e293907fe85152d7d5bb570fc17))
* **ci:** use GH_TOKEN PAT for semantic-release ([#3](https://github.com/thepixelabs/amnesiai/issues/3)) ([b01e35a](https://github.com/thepixelabs/amnesiai/commit/b01e35ab6b37dbc64253d5ab0bf5c96851db608d)), closes [#2](https://github.com/thepixelabs/amnesiai/issues/2)
* retrigger v1.0.0 release ([#4](https://github.com/thepixelabs/amnesiai/issues/4)) ([cec4800](https://github.com/thepixelabs/amnesiai/commit/cec4800642a9e948c7835a4a1d3a6d9bfe2bedda))

## 1.0.0 (2026-04-16)

### Features

* add CLI source, CI/CD pipelines, and release infrastructure ([ad3ba8b](https://github.com/thepixelabs/amnesiai/commit/ad3ba8b5dc382ae35ebe30394b87a3440c206053))
* add landing page with responsive how-it-works sections ([e85b000](https://github.com/thepixelabs/amnesiai/commit/e85b00052dae5a74ed49a47f4c6c72737ea6473e))

### Bug Fixes

* **ci:** correct create-github-app-token SHA ([#2](https://github.com/thepixelabs/amnesiai/issues/2)) ([fbcb0e2](https://github.com/thepixelabs/amnesiai/commit/fbcb0e24fdb73e5238e7257a78e63be1905c54fe))
* **ci:** correct lint action SHA and make vuln-scan non-blocking ([#1](https://github.com/thepixelabs/amnesiai/issues/1)) ([d946dfb](https://github.com/thepixelabs/amnesiai/commit/d946dfb9768d6e293907fe85152d7d5bb570fc17))
* **ci:** use GH_TOKEN PAT for semantic-release ([#3](https://github.com/thepixelabs/amnesiai/issues/3)) ([b01e35a](https://github.com/thepixelabs/amnesiai/commit/b01e35ab6b37dbc64253d5ab0bf5c96851db608d)), closes [#2](https://github.com/thepixelabs/amnesiai/issues/2)
