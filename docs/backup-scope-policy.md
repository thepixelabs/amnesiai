# amnesiai Backup Scope Policy

**Status:** Authoritative  
**Audience:** Contributors, security auditors, users evaluating trust  
**Note:** This document is provided for transparency — it is not a legal instrument. It documents the rationale for scope decisions. It has not been reviewed by legal counsel and does not constitute legal advice.

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
| P6 | **Controlled symlink handling.** Symlinks-to-files are followed so dotfile-manager setups work correctly. Symlinks-to-directories are always skipped to avoid traversal loops. On restore, if the destination is a symlink the write goes through to the symlink target — the link itself is never replaced with a regular file. |
| P7 | **Path traversal is rejected at restore.** Every provider validates that the resolved destination path is a descendant of the provider's base directory before writing. |
| P8 | **metadata.json leaks nothing sensitive.** The unencrypted sidecar contains: tool categories changed, file counts, timestamp, ciphertext SHA256. It never contains filenames, paths, or file content. |
| P9 | **Hooks changes require explicit separate confirmation on restore.** Hooks can execute arbitrary code. A diff is shown and a second confirmation is required before any hook-bearing file is written. |
| P10 | **Scope changes require a compliance review.** Adding a path, loosening an exclusion, or weakening a credential filter is a CODEOWNERS-gated change requiring an update to this document. |

---

## 3. Per-Provider Scope Tables

### 3.1 Claude Code (`~/.claude/`)

Implementation: `pkg/provider/claude/claude.go`  
Strategy: explicit top-level file allowlist (`CLAUDE.md`, `settings.json`, `keybindings.json`) plus recursive walking of `agents/`, `commands/`, and `skills/` subdirectories.

| Path | Status | Rationale |
|------|--------|-----------|
| `~/.claude/CLAUDE.md` | IN | Global user instructions — the primary artifact being backed up. |
| `~/.claude/settings.json` | IN | Hooks, permissions, model preferences. Gitleaks scans for inline secrets. |
| `~/.claude/keybindings.json` | IN (if present) | User-authored key bindings. |
| `~/.claude/agents/*.md` | IN | User-authored subagent definitions. |
| `~/.claude/commands/*.md` | IN | User-authored slash commands. |
| `~/.claude/skills/<name>/*` | IN | User-authored agent skills (every non-hidden file under each skill dir). |
| Any other file directly under `~/.claude/` | OUT | Explicit allowlist — unknown files are excluded by default (P2). |
| Per-project `<project>/CLAUDE.md` | IN | Project-scoped instructions. Discovered for each path in `project_paths`. |
| Per-project `<project>/.claude/settings.json` | IN | Project-scoped hooks and permissions (NOT settings.local.json). |
| `~/.claude/projects/` | OUT | Conversation history. May contain PII, business context, third-party data. |
| `~/.claude/statsig/` | OUT | Internal telemetry state owned by Anthropic tooling. Not user-authored. |
| `~/.claude/todos/` | OUT | Ephemeral task lists. May contain names, business context, partial credentials. Not portable across machines. |
| `~/.claude/ide/` | OUT | Unaudited machine-local IDE integration state. May contain VS Code extension state, OAuth tokens, caches. |
| `~/.claude/.credentials.json` | OUT | Credential file. Never read, never written. |
| `~/.claude/settings.local.json` | OUT | Machine-local override; Claude Code's own convention signals this must not sync. |

### 3.2 Gemini CLI (`~/.gemini/`)

Implementation: `pkg/provider/gemini/gemini.go`  
Strategy: explicit top-level file allowlist (`settings.json`, `GEMINI.md`, `projects.json`, `trustedFolders.json`) plus recursive walking of `themes/`, `agents/`, `commands/`, and `extensions/`. Anything not in the allowlist is skipped at directory entry without descending.

| Path | Status | Rationale |
|------|--------|-----------|
| `~/.gemini/settings.json` | IN | Includes `customInstructions` and MCP server config. Gitleaks scans for API keys. |
| `~/.gemini/GEMINI.md` | IN | User-authored global instructions. |
| `~/.gemini/projects.json` | IN | Which project paths the user has trusted. |
| `~/.gemini/trustedFolders.json` | IN | Per-folder trust. |
| `~/.gemini/themes/` (all files) | IN | User-authored UI themes. |
| `~/.gemini/agents/` (all files) | IN | User-authored subagent definitions. |
| `~/.gemini/commands/` (all files) | IN | User-authored slash commands. |
| `~/.gemini/extensions/` (all files) | IN | User-installed CLI extensions. |
| `~/.gemini/antigravity/mcp_config.json` | OUT (code gap — see §6) | Planned IN per project decisions; not in current `allowedTopLevelDirs`. |
| Any file whose name starts with `auth` (case-insensitive) | OUT | OAuth tokens and auth state. |
| Any file whose name ends with `.key` | OUT | Cryptographic key material. |
| Any file containing `oauth`, `creds`, `credential`, `token`, or `secret` (case-insensitive) | OUT | Credential material. |
| Everything else under `~/.gemini/` | OUT | Not in allowlist; skipped by default (P2). |

### 3.3 GitHub Copilot

Implementation: `pkg/provider/copilot/copilot.go`  
Base directory: `~/.copilot/` (all platforms); override with `COPILOT_HOME` env var.  
The legacy `~/Library/Application Support/GitHub Copilot/` path (macOS, VS Code-bundled extension) is out of scope.

Strategy: explicit top-level file allowlist plus a recursive `agents/` directory; sensitive-named files excluded at both Discover() and Restore().

| Path | Status | Rationale |
|------|--------|-----------|
| `settings.json` | IN | User-editable settings (themes, defaults, etc.) |
| `config.json` | IN | App state including trustedFolders and allowed_urls |
| `mcp-config.json` | IN | MCP server configuration; Bearer headers caught by the secret scanner |
| `lsp-config.json` | IN | Language Server Protocol configuration |
| `agents/*.md` | IN | User-authored custom agents |
| Per-project `.github/copilot-instructions.md` | IN | Repository-level Copilot instructions; discovered for each path in `project_paths` |
| `command-history-state.json` | OUT | Chat/command history |
| `logs/`, `session-state/`, `ide/` | OUT | Machine-local runtime state |
| `skills/`, `hooks/` | OUT | Executable code; out of scope for v1 |
| Any file whose name contains `token`, `secret`, `key`, `credential`, or `auth` (case-insensitive) | OUT | Sensitive credential terms — excluded at Discover() and Restore() |

### 3.4 OpenAI Codex CLI (`~/.codex/`)

Implementation: `pkg/provider/codex/codex.go`  
Strategy: explicit top-level file allowlist plus recursive directory walking for `agents/`, `rules/`, `memories/`, and `skills/`.

| Path | Status | Rationale |
|------|--------|-----------|
| `~/.codex/config.toml` | IN | Top-level Codex CLI configuration |
| `~/.codex/AGENTS.md` | IN | Custom instructions (if present) |
| `~/.codex/instructions.md` | IN | Legacy instructions file (if present) |
| `~/.codex/agents/*.toml` | IN | User-authored agent definitions |
| `~/.codex/rules/default.rules` | IN | Global behavioural rules |
| `~/.codex/memories/**` | IN | Durable Codex memory files |
| `~/.codex/skills/**` (excluding `.system/`) | IN | User-authored skills; `.system/` ships with the binary and is excluded |
| `auth.json`, `history.jsonl` | OUT | Auth state, conversation history |
| `sessions/`, `log/`, `cache/`, `.tmp/`, `tmp/` | OUT | Runtime state, logs, caches |
| `*.sqlite*`, `models_cache.json`, `installation_id`, `version.json` | OUT | Machine-local runtime metadata |
| `.personality_migration`, `shell_snapshots/` | OUT | Migration markers, ephemeral shell state |
| `skills/.system/` | OUT | Binary-shipped skills; replaced on every update |
| Any `*.key` or file containing `token`, `credential`, `auth`, `secret` (case-insensitive) | OUT | Credential/key material |
| Everything else under `~/.codex/` | OUT | Not in allowlist; skipped by default (P2) |

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

## 5. Change Management

### 5.1 What requires a compliance review

Any of the following changes must update this document and receive approval from a CODEOWNERS-designated reviewer before merge:

- Adding a path to any provider's in-scope set
- Loosening a credential filter (e.g., narrowing the `sensitiveTerms` list)
- Removing an excluded directory or file from any blocklist
- Changing what metadata.json contains
- Changing the gitleaks integration behavior (report-only vs. redact, abort conditions)

### 5.2 CODEOWNERS coverage

The following paths must have CODEOWNERS entries requiring explicit reviewer approval:

- `pkg/provider/*/` (all provider implementations)
- `docs/backup-scope-policy.md` (this file)
- `internal/sanitize/` or equivalent gitleaks integration package
- Any code that writes to `~/.amnesiai/config.toml` or `~/.amnesiai/state.json`

### 5.3 Audit trail

Each scope decision should be traceable to a dated decision record. Future decisions should be recorded in a `docs/decisions/` ADR directory.

---

## 6. Known Gaps

The following are known divergences between this document and the current code. They are open issues — contributions to close them are welcome.

### GAP-01: Claude provider top-level allowlist is now explicit

**Status: Resolved**

The claude provider now uses an explicit top-level allowlist (`CLAUDE.md`, `settings.json`, `keybindings.json`) matching the other providers. Unknown new files under `~/.claude/` are excluded by default (P2).

### GAP-02: Gemini `mcp_config.json` is planned IN but not implemented

**Severity: Low**

The Gemini provider's `allowedTopLevelDirs` does not currently include `antigravity/`, so `~/.gemini/antigravity/mcp_config.json` is not backed up. Adding it requires verifying that gitleaks rule coverage will redact any embedded API keys before the path is enabled.

### GAP-03: Codex exclusion filter alignment

**Severity: Low**

The gemini provider excludes `auth*` files and the copilot provider excludes names containing `auth` and `secret`. The codex `isExcludedFile` function checks for `.key`, `token`, `credential`, `auth`, and `secret`. The explicit allowlist provides defense-in-depth regardless, but the filter consistency is intentional.

---

## 7. Summary

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
- Symlinks-to-files are followed on backup; symlinks-to-directories are skipped; on restore, writes go through symlinks to the underlying target — symlinks are never replaced with regular files
- Path traversal is blocked on restore
