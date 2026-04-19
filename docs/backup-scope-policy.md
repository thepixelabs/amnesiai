# amnesiai Backup Scope Policy

**Status:** Authoritative  
**Audience:** Contributors, security auditors, users evaluating trust  
**Note:** This is an internal engineering policy document, not a legal instrument. It codifies team decisions for auditability. It has not been reviewed by legal counsel and does not constitute legal advice.

---

## 1. Purpose

This document defines exactly what amnesiai reads, writes, and ignores — and why. It is the authoritative reference for any scope decision. When the code and this document conflict, treat the conflict as a bug and open an issue.

---

## 2. Governing Principles

These rules apply to all four providers and to any future provider. A scope change that violates a principle requires an explicit principle revision, not just a path addition.

| # | Principle |
|---|-----------|
| P1 | **Instruction data IN, everything else OUT.** Back up what the user authored: system prompts, custom instructions, hooks, agent definitions, key bindings. Never back up runtime state, conversation history, credentials, or machine-local overrides. |
| P2 | **Allowlist over blocklist.** Prefer explicit allowlists (`allowedTopLevel`) to blocklists. Unknown future files from a provider are excluded by default, not included. |
| P3 | **Credentials are never touched.** No file whose name matches a credential pattern is read, backed up, or restored, even if it appears inside an otherwise in-scope directory. Guards exist at both Discover() and Restore(). |
| P4 | **Instruction files are never parsed for operational instructions.** CLAUDE.md, GEMINI.md, copilot-instructions.md, and instructions.md are treated as opaque byte blobs. amnesiai does not execute, interpret, or act on their contents. |
| P5 | **Fail closed on scan errors.** If the gitleaks scan cannot complete, the backup is aborted. No partial backups with unknown secret exposure. |
| P6 | **No symlink traversal.** Every provider uses `os.Lstat` to detect and skip symlinks at both file and directory level. |
| P7 | **Path traversal is rejected at restore.** Every provider validates that the resolved destination path is a descendant of the provider's base directory before writing. |
| P8 | **metadata.json leaks nothing sensitive.** The unencrypted sidecar contains: tool categories changed, file counts, timestamp, ciphertext SHA256. It never contains filenames, paths, or file content. |
| P9 | **Hooks changes require explicit separate confirmation on restore.** Hooks can execute arbitrary code. A diff is shown and a second confirmation is required before any hook-bearing file is written. |
| P10 | **Scope changes require a compliance review.** Adding a path, loosening an exclusion, or weakening a credential filter is a CODEOWNERS-gated change requiring an update to this document. |

---

## 3. Per-Provider Scope Tables

### 3.1 Claude Code (`~/.claude/`)

Implementation: `pkg/provider/claude/claude.go`  
Strategy: walk the base directory; apply a directory blocklist and a file blocklist.

| Path | Status | Rationale |
|------|--------|-----------|
| `~/.claude/CLAUDE.md` | IN | Global user instructions — the primary artifact being backed up. |
| `~/.claude/settings.json` | IN | Hooks, permissions, model preferences. Gitleaks scans for inline secrets. |
| `~/.claude/keybindings.json` | IN (if present) | User-authored key bindings. |
| Any other file directly under `~/.claude/` | IN | Walk is permissive at the file level outside excluded dirs/files — see Known Gaps §7. |
| Per-project `<project>/CLAUDE.md` | IN | Project-scoped instructions. Discovered during project-level backup (see §7). |
| Per-project `<project>/.claude/settings.json` | IN | Project-scoped hooks and permissions. |
| `~/.claude/projects/` | OUT | Conversation history. May contain PII, business context, third-party data. |
| `~/.claude/statsig/` | OUT | Internal telemetry state owned by Anthropic tooling. Not user-authored. |
| `~/.claude/todos/` | OUT | Ephemeral task lists. May contain names, business context, partial credentials. Not portable across machines. |
| `~/.claude/ide/` | OUT | Unaudited machine-local IDE integration state. May contain VS Code extension state, OAuth tokens, caches. |
| `~/.claude/.credentials.json` | OUT | Credential file. Never read, never written. |
| `~/.claude/settings.local.json` | OUT | Machine-local override; Claude Code's own convention signals this must not sync. |

### 3.2 Gemini CLI (`~/.gemini/`)

Implementation: `pkg/provider/gemini/gemini.go`  
Strategy: explicit top-level allowlist (`settings.json`, `GEMINI.md`, `themes/`). Anything not in the allowlist is skipped at directory entry without descending.

| Path | Status | Rationale |
|------|--------|-----------|
| `~/.gemini/settings.json` | IN | Includes `customInstructions` and MCP server config. Gitleaks scans for API keys. |
| `~/.gemini/GEMINI.md` | IN | User-authored global instructions. |
| `~/.gemini/themes/` (all files) | IN | User-authored UI themes. |
| `~/.gemini/antigravity/mcp_config.json` | OUT (code gap — see §7) | Planned IN per project decisions; not in current allowedTopLevel. |
| Any file whose name starts with `auth` (case-insensitive) | OUT | OAuth tokens and auth state. |
| Any file whose name ends with `.key` | OUT | Cryptographic key material. |
| Everything else under `~/.gemini/` | OUT | Not in allowlist; skipped by default (P2). |

### 3.3 GitHub Copilot

Implementation: `pkg/provider/copilot/copilot.go`  
Base directory (OS-dependent):
- macOS: `~/Library/Application Support/GitHub Copilot/`
- Linux: `~/.config/github-copilot/`
- Windows: `%APPDATA%/GitHub Copilot/`

Strategy: walk the base directory; include all `*.json` files that do not match a sensitive-name filter.

| Path | Status | Rationale |
|------|--------|-----------|
| `hosts.json` | IN | Hostname-to-settings mapping. Tokens are in the OS keychain, not in this file. |
| `<base>/*.json` (non-sensitive) | IN | All JSON config files not matching the sensitive-name filter. |
| `.github/copilot-instructions.md` | IN (per decisions; see §7) | Repository-level Copilot instructions. Not in current provider code. |
| VS Code `github.copilot.*` settings extraction | OUT (Phase 3) | Surgical extraction of Copilot-scoped keys from VS Code settings.json deferred to Phase 3. Whole-file VS Code backup is never done. |
| Any file whose name contains `token`, `secret`, `key`, `credential`, or `auth` (case-insensitive) | OUT | Sensitive credential terms — excluded at Discover() and Restore(). |
| All non-JSON files | OUT | Only JSON is in scope; other file types are skipped. |

### 3.4 OpenAI Codex CLI (`~/.codex/`)

Implementation: `pkg/provider/codex/codex.go`  
Strategy: explicit top-level allowlist (`config.json`, `instructions.md`, `themes/`).

| Path | Status | Rationale |
|------|--------|-----------|
| `~/.codex/config.json` | IN | Model preferences and tool configuration. |
| `~/.codex/instructions.md` | IN | User-authored global instructions. |
| `~/.codex/themes/` (all files) | IN | User-authored UI themes. |
| `~/.codex/agents/*.toml` | OUT (code gap — see §7) | Planned IN per project decisions; not in current allowedTopLevel. |
| `~/.codex/rules/default.rules` | OUT (code gap — see §7) | Planned IN per project decisions; not in current allowedTopLevel. |
| Any file whose name ends with `.key`, contains `token`, or contains `credential` | OUT | Credential/key material. |
| Everything else under `~/.codex/` | OUT | Not in allowlist; skipped by default (P2). |

---

## 4. Secret Handling Policy

### 4.1 Gitleaks scan

- Embedded gitleaks library (not a subprocess) scans every in-scope file before any bytes reach the archive.
- Scan runs on the raw file content before encryption or compression.
- If the scan cannot complete for any reason, the backup is aborted (P5 — fail closed).

### 4.2 Encryption ON (default)

- gitleaks runs in report-only mode; it does not modify file content.
- Raw bytes (including any inline secrets) are encrypted into `payload.age`.
- User is shown: `N secrets detected (encrypted in archive) [press d for details]`
- Findings are written to `metadata.json`:
  - Per-file, per-rule-ID entry
  - SHA256 of the raw secret bytes (not the raw value)
  - This lets users audit what was detected without decrypting the payload.
- Raw secret values are never written anywhere outside the encrypted payload.

### 4.3 Encryption OFF + `--force-no-encrypt`

Two flags are required; `--no-encrypt` alone is refused when gitleaks finds anything.

- gitleaks redacts matched secrets to `<REDACTED:rule-id>` placeholders in the bytes that reach the archive.
- On restore, placeholder values are written to disk, overwriting any real values that were there. This is intentional and irreversible.
- A loud warning is shown at both backup and restore time.
- This escape hatch exists for CI/automation environments where encryption is handled at a higher layer. It is not the recommended path.

### 4.4 `--no-encrypt` alone (no `--force-no-encrypt`)

- If gitleaks finds any secrets: backup is refused with an error.
- If gitleaks finds nothing: backup proceeds unencrypted. (Low-risk path for users with no inline credentials.)

### 4.5 metadata.json

Written unencrypted alongside `payload.age`. Contains:
- Timestamp
- Tool categories modified
- Per-category file count
- Ciphertext SHA256 (integrity check)
- Gitleaks findings: per-file, per-rule-ID, SHA256 of raw secret bytes

Never contains: filenames, file paths, file content, raw secret values, passphrase, user identity.

---

## 5. Telemetry Policy

### 5.1 Opt-in only

Telemetry is disabled by default. It is activated only when `telemetry = true` is set explicitly in `~/.amnesiai/config.toml`.

### 5.2 Storage

- Written to `~/.amnesiai/metrics.json`.
- File permissions: `0600`.
- Local only. The file is never transmitted anywhere automatically.
- Not included in bug reports without explicit user action (user must manually copy and share the file).

### 5.3 What IS captured (counts only)

| Field | Type | Description |
|-------|------|-------------|
| `backup_count` | integer | Number of completed backup operations |
| `passphrase_mismatch_count` | integer | Number of passphrase confirmation failures |
| `help_key_presses` | map[screen → integer] | Count of help keystrokes per UI screen |
| `label_skip_count` | integer | Number of times backup label entry was skipped |
| `flow_abort_count` | map[screen → integer] | Count of flow aborts per UI screen |

### 5.4 What is NEVER captured

- Label text or any user-entered string
- Provider file contents
- File paths or filenames
- Passphrase or passphrase length
- Error messages or stack traces
- IP address, hostname, or any device identifier
- Timestamps of individual operations

---

## 6. Change Management

### 6.1 What requires a compliance review

Any of the following changes must update this document and receive approval from a CODEOWNERS-designated reviewer before merge:

- Adding a path to any provider's in-scope set
- Loosening a credential filter (e.g., narrowing the `sensitiveTerms` list)
- Removing an excluded directory or file from any blocklist
- Changing the telemetry field list (additions or removals)
- Changing what metadata.json contains
- Changing the gitleaks integration behavior (report-only vs. redact, abort conditions)

### 6.2 CODEOWNERS coverage

The following paths must have CODEOWNERS entries requiring explicit reviewer approval:

- `pkg/provider/*/` (all provider implementations)
- `docs/backup-scope-policy.md` (this file)
- `internal/sanitize/` or equivalent gitleaks integration package
- `~/.amnesiai/` config and telemetry paths (in any code that writes them)

### 6.3 Audit trail

Each scope decision should be traceable to a dated decision record. The current authoritative source is `project_decisions.md` in the project memory. Future decisions should be recorded in a `docs/decisions/` ADR directory.

---

## 7. Known Gaps

These are code-vs-policy divergences found during this audit. They are not decisions — they are open items requiring team resolution.

### GAP-01: Claude provider is permissive beyond the named files

**Severity: Medium**

The claude provider walks `~/.claude/` and backs up ALL files not in `excludedDirs` or `excludedFiles`. This means any new file Anthropic tooling creates under `~/.claude/` (outside the blocked dirs) will be silently included in the next backup — no allowlist gates it. Project decisions and this policy list only three specific files (CLAUDE.md, settings.json, keybindings.json) as in-scope at the top level. **Decision needed:** Should the claude provider be tightened to an explicit top-level allowlist matching the other providers, or is open-ended capture of `~/.claude/` top-level files intentional?

### GAP-02: Gemini `mcp_config.json` is planned IN but not implemented

**Severity: Low**

`project_decisions.md` lists `~/.gemini/antigravity/mcp_config.json` as in scope. The current allowedTopLevel for the gemini provider does not include `antigravity/`. This file is currently not backed up. The plan also mentions API key redaction for this file; that redaction requirement needs to be verified against gitleaks rule coverage before the path is added.

### GAP-03: Codex `agents/*.toml` and `rules/default.rules` are planned IN but not implemented

**Severity: Medium**

`project_decisions.md` and `plan.md` explicitly list `~/.codex/agents/*.toml` and `~/.codex/rules/default.rules` as in-scope. The current allowedTopLevel for the codex provider contains only `config.json`, `instructions.md`, and `themes/`. Neither `agents/` nor `rules/` is included. These are core artifacts per the project plan and their absence is a functional gap, not a conservative scope decision.

### GAP-04: `.github/copilot-instructions.md` not in the Copilot provider

**Severity: Medium**

`project_decisions.md` and `plan.md` list `.github/copilot-instructions.md` as in scope. The current copilot provider only scans the OS-specific Copilot config directory. Repository-level instruction files are not discovered or backed up. This is a significant gap since copilot-instructions.md is arguably the most important Copilot customization artifact.

### GAP-05: Per-project CLAUDE.md and `.claude/settings.json` discovery is undefined

**Severity: Medium**

The policy calls for backing up per-project `<project>/CLAUDE.md` and `<project>/.claude/settings.json`. The current claude provider only walks `~/.claude/`. There is no code that discovers project-level files. The mechanism for enumerating projects (cwd? a configured project list? scanning known directories?) is not defined or implemented.

### GAP-06: Copilot `hosts.json` — verify it is truly credential-free

**Severity: Low**

The code comment asserts that `hosts.json` contains "hostname → settings, not tokens" because tokens are stored in the OS keychain. This assumption should be validated against the current GitHub Copilot CLI release before a v1 ship. If a Copilot version ever writes a token fallback into `hosts.json`, the current name-based filter will not catch it because the filename does not contain any of the `sensitiveTerms`. Recommend adding a structural content check (look for `"token"` keys in JSON) as a defense-in-depth measure, or confirming via Copilot CLI source/documentation.

### GAP-07: Codex exclusion filter does not include `auth` or `secret`

**Severity: Low**

The gemini provider excludes `auth*` files and the copilot provider excludes names containing `auth` and `secret`. The codex `isExcludedFile` function only checks for `.key`, `token`, and `credential`. If the Codex CLI ever writes an `auth.json` or `secret.json` under `~/.codex/`, it would not be caught by the current filter. The allowlist mitigates this (only `config.json`, `instructions.md`, and `themes/` are in scope), but the filter inconsistency is worth aligning for defense-in-depth.

---

## 8. User-Facing Summary

For README / landing page "What we back up / What we don't" section:

**We back up:**
- Your AI assistant system prompts and custom instructions (Claude, Gemini, Copilot, Codex)
- Your tool settings: hooks, permissions, model preferences, key bindings
- Your agent definitions and rule files
- Your custom themes

**We never back up:**
- Conversation history or session logs
- Credentials, auth tokens, or API keys (file-name-filtered out; inline secrets are encrypted into the payload or redacted if you opt out of encryption)
- Machine-local config overrides (settings.local.json and equivalents)
- IDE extension caches or OAuth session state
- Claude's internal telemetry state (statsig/) or task lists (todos/)

**Security properties:**
- Secrets found inline in config files are encrypted inside the backup archive (never written to unencrypted storage)
- Unencrypted backups are refused when secrets are detected, unless you explicitly pass `--force-no-encrypt`
- Symlinks are never followed; path traversal is blocked on restore
- Telemetry is opt-in, local-only, and captures counts only — never content, paths, or identifiers
