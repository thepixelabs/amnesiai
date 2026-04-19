# amnesiai Test Strategy

> For QA Engineer implementation reference. Strategy only — no test code here.
> Last updated: 2026-04-19. Tracks: A (merged), B, C+D, E, F (in-flight).

---

## 1. Layered Testing Map

### Track A — Storage schema + config + claude scope (MERGED at d669ee0)
**Unit:** `config.Validate`, `config.Load`, `config.SaveTo` — all exist, good coverage.
**Why unit is sufficient here:** pure data transformation with no I/O or subprocess boundary.
**Gap:** `SaveTo` round-trip test (`TestSaveLoad_RoundTrip`) covers all current struct fields. When Track E adds `state.json` or Track B adds new config fields (e.g. `passphrase_fd`), the round-trip test MUST be extended immediately — new fields that are serialised through viper but not set in `SaveTo` will silently drop on reload. This is a documented class of bug in the codebase already.

### Track B — Redaction Option B (encryption-on = report-only, --force-no-encrypt guard, --passphrase-fd)
**Unit:** `scan.ScanReport` vs `scan.Scan` routing logic; `BackupOptions.NoEncrypt` + `ForceNoEncrypt` guard in `core.Backup`.
**Integration (required, not optional):** The branch point between `Scan` and `ScanReport` exists at `core.Backup` lines 87-108. A unit test of `scan` alone cannot verify that the wrong path is taken when `Passphrase != ""`. The integration test at `core_test.TestBackup_ScanRedactsSecrets` already validates the `NoEncrypt` path. The `ScanReport` path (encryption on = no redaction, raw bytes stored) has no integration test yet — this is the highest-priority test gap in the entire codebase.
**Unit is insufficient** for `--force-no-encrypt` guard: the guard reads `totalSecrets` aggregated across all providers; a unit test of `scan` can't prove the aggregation and early-return logic in `core.Backup` are correct together.
**Property-based:** See Section 2.
**--passphrase-fd:** Unit-test the fd-reading function in isolation. Integration-test that it feeds correctly into `PassphraseFromEnvOrFlag` priority chain.

### Track C+D — TUI overhaul (bubbletea passphrase model, provider picker, label step)
**Unit (pure logic only):**
- `resolveProviders` — already unit-testable, pure function; test via `cmd_helpers_test.go`.
- `parseLabels` — pure function, unit test with table cases including empty input, malformed input, and Unicode values.
- `splitCSV` — pure, unit test edge cases: trailing comma, whitespace-only segments.
- Passphrase masking function (Track C+D introduces `·` masking): if exposed as a pure `mask(s string) string` function, unit test that `len([]rune(mask(s))) == len([]rune(s))` for all inputs. See Section 2.
**Do not unit-test bubbletea `View()` or `Update()` directly** — lipgloss ANSI output is terminal-width-dependent and fragile as a string assertion. This is a known hard problem with bubbletea; snapshot tests here would break on every style change and catch nothing real. Mark as manual QA.
**Integration (scripted input):** The passphrase two-field model (enter + confirm) has an obvious failure mode: mismatched entries not being caught. Test this by feeding scripted input bytes into the model's `Update` via `tea.NewProgram` with a `bytes.Reader` as stdin, asserting the model returns an error state, not a passphrase. This is the correct bubbletea testing approach — model-layer test, not visual test.
**Manual QA checklist:** `·` masking visually correct, confirm-field mismatch shows error inline, `?`-toggle help appears and dismisses, provider picker `·` markers match selection state.

### Track E — First-run onboarding wizard + state.json (multi-account repo bindings)
**Unit:** URL normalisation function (critical — see Section 2 for property tests). State marshal/unmarshal round-trip. Multi-account lookup by normalised URL.
**Integration is required** for the full onboarding wizard: the wizard writes `config.toml` + `state.json`; the only proof it works is reading them back with `config.Load` + state unmarshal and asserting values. Scripted keystrokes via stdin reader, not subprocess. Verifying that two different repo URLs map to two different account entries also requires integration-level fixture state.
**Unit is insufficient** for the multi-account binding lookup: the lookup involves URL normalisation + map key matching + state persistence. These three operations interact; bugs live at their seams.

### Track F — git-local + git-remote storage backends + gh/glab integration
**Unit:** git command argument construction. Path sanitisation within the git backend. Repo visibility check logic (parsing `gh repo view --json isPrivate` output).
**Integration (required):** git-local backend correctness cannot be proven without actually running `git`. Use `t.TempDir()` as the repo root, call the backend, assert `git log` output via `exec.Command`. This is cheap, hermetic, and deterministic — there is no reason to mock git here. The mock would validate argument construction, not whether git actually commits.
**Integration (recorder/replay for gh):** For git-remote tests without a live GitHub account, use a subprocess recorder pattern: capture `gh` stdout/stderr/exit-code into golden files during a one-time recording run against a real repo (or a local Gitea instance). In CI, replace `gh` on `PATH` with a shell script that replays the golden outputs based on argv. This is cheaper than a full service virtualisation layer and avoids `httptest` complexity around the gh CLI's internal HTTP calls. The recording step is a one-time developer action; the replay runs on every PR.
**Property-based:** Not useful for git-local — git state is inherently sequential, not random-input territory.

---

## 2. Property-Based Tests Worth Writing

Use `pgregory.net/rapid` (preferred for Go — no external binary, idiomatic) or `github.com/leanovate/gopter`. Do not use `testing/quick` — it has poor shrinking and no generators for constrained domains.

### P1 — URL normalisation idempotence (`internal/state` or wherever Track E lands)
```
Function: Normalize(url string) string
Property: Normalize(Normalize(x)) == Normalize(x)   [idempotence]
Property: Normalize(a) == Normalize(b) for all (a,b) in equivalence classes:
  - https://github.com/user/repo
  - https://github.com/user/repo.git
  - git@github.com:user/repo.git
  - git@github.com:user/repo
  - ssh://git@github.com/user/repo.git
Generator: draw from the above templates with random (user, repo) pairs;
           also fuzz with arbitrary strings to ensure no panic.
```
This property is critical: if normalisation is not idempotent, a second `amnesiai backup` will create a second account binding for the same repo. That is a silent data corruption bug.

### P2 — Encrypt/Decrypt round-trip (`internal/crypto`)
```
Function: Encrypt(passphrase, plaintext) → ciphertext; Decrypt(passphrase, ciphertext) → plaintext
Property: Decrypt(p, Encrypt(p, b)) == b  for all (p []byte, b []byte) with len(p) > 0
Generator: arbitrary byte slices for plaintext (including empty, single byte, 1MB);
           passphrase as arbitrary non-empty UTF-8 string.
```
The existing table test covers named cases. Property-based adds: zero-byte content, content that is exactly one age block boundary, content with embedded null bytes. The age library has historically had issues with specific content lengths; this catches them.

### P3 — Passphrase masking (`cmd` or `internal/tui` wherever Track C+D puts the mask function)
```
Function: maskRune(s string) string  (must be exported or tested in package)
Property: len([]rune(maskRune(s))) == len([]rune(s))
Property: every rune in maskRune(s) == '·'   (middle dot U+00B7, not period)
Generator: arbitrary UTF-8 strings including multi-byte runes, emoji, RTL characters.
```
This catches a specific bug class: masking functions that operate on bytes instead of runes produce wrong lengths for non-ASCII passphrases.

### P4 — Config Save/Load round-trip (`internal/config`)
The existing `TestSaveLoad_RoundTrip` is a table test. Add property-based for:
```
Property: for any Config with valid StorageMode and non-empty BackupDir,
          Load(viper pointed at SaveTo output) == original Config
Generator: valid StorageMode drawn from {"local","git-local","git-remote"},
           BackupDir as arbitrary non-empty path string,
           Providers as non-empty subset of {"claude","gemini","copilot","codex"},
           arbitrary bool values for flags.
```
This catches the "field added to struct but not to SaveTo" regression that the codebase comment already warns about.

### P5 — Scan redaction does not produce overlapping replacements (`internal/scan`)
```
Function: Scan(path, data) → (redacted, findings, err)
Property: redacted contains no instance of any original secret byte sequence
Property: scanning redacted again produces zero findings (idempotence — already tested as unit, worth adding as property with random data)
Generator: inject one or more real gitleaks-pattern secrets at random offsets within arbitrary clean content.
```

---

## 3. Integration Tests That Matter

Listed in priority order. All use `t.TempDir()` for isolation. None require a running service unless marked.

### I1 — Backup → Restore round-trip WITH secrets + encryption (highest priority)
**What it proves:** the Track B `ScanReport`-when-encrypted path has no test today. This is the primary correctness guarantee of the tool.
```
Fixture: settings.json containing a real AWS access key pattern
         (AKIA + 16 uppercase alphanumeric chars that pass the gitleaks rule)
Action:  core.Backup(store, BackupOptions{Passphrase: "testpass"})
Assert:  - BackupResult.Findings["claude"] contains aws-access-token entry
         - raw ciphertext in store does NOT contain the key literal (byte search)
         - core.Restore(store, RestoreOptions{Passphrase: "testpass"})
         - restored bytes are IDENTICAL to the original fixture bytes (including the raw key)
         - RestoreResult.PlaceholderFiles is EMPTY (no redaction markers in restored content)
```
The last two assertions together are the critical proof: encrypt-path stores raw bytes, decrypt-path recovers them exactly, and no `<REDACTED:>` placeholder was injected.

### I2 — Backup → Restore with --no-encrypt + --force-no-encrypt
```
Fixture: same settings.json with AWS key
Action:  core.Backup(store, BackupOptions{NoEncrypt: true, ForceNoEncrypt: true})
Assert:  - backup succeeds (no error)
         - payload in store contains "<REDACTED:aws-access-token>" literal
         - payload does NOT contain the raw key
         - core.Restore(store, RestoreOptions{}) succeeds
         - restored bytes contain "<REDACTED:aws-access-token>"
         - RestoreResult.PlaceholderFiles is non-empty
```

### I3 — --no-encrypt without --force-no-encrypt is rejected when secrets present
```
Action:  core.Backup(store, BackupOptions{NoEncrypt: true, ForceNoEncrypt: false})
         with a fixture containing a detectable secret
Assert:  error is non-nil; error message contains "refusing" (or equivalent)
         store.List() returns empty (no backup written)
```
The "no backup written" assertion is what makes this an integration test — a unit test of the guard logic cannot verify atomicity.

### I4 — Scan init failure fails closed
```
Action:  inject a failure into gitleaks detector initialisation
         (wrap scan.Scan/ScanReport behind an interface; inject error-returning stub)
Assert:  core.Backup returns a non-nil error
         store.List() returns empty — nothing was written
```
The current code does fail-closed in both `Scan` and `ScanReport` paths. This test pins that contract so a future refactor can't accidentally swap error handling order.

### I5 — git-local storage: N backups produce N commits
```
Setup:   initialise git repo in t.TempDir(); create gitLocalStorage backed by it
Action:  run Backup 3 times via the git-local backend
Assert:  `git -C dir log --oneline` produces exactly 3 entries
         each commit message describes the backup (non-empty, contains provider names)
         `git -C dir status --porcelain` is clean after each backup
```
No mocking. Real git binary. Fast enough for PR gate (< 2s).

### I6 — git-remote storage: recorder/replay against `gh`
```
Recording (one-time developer action):
  - Run against a real private repo (or local Gitea instance)
  - Capture every gh invocation: argv + stdout + stderr + exit code
  - Save as testdata/gh-recordings/*.json golden files

Replay (every CI run):
  - Build a fake `gh` binary (Go test helper or shell script) that:
    - matches incoming argv against recordings
    - writes recorded stdout/stderr, exits with recorded code
  - Prepend fake binary dir to PATH for the test process
  - Run the git-remote backend through a full backup+push cycle
  - Assert storage.List() sees the backup; git log shows the commit
```
**Do not use httptest for this.** The gh CLI's internal HTTP is not your concern — you're testing that amnesiai invokes gh correctly and handles its output, not gh's HTTP layer.

### I7 — Onboarding E2E: scripted keystrokes → config.toml + state.json
```
Action:  feed scripted bytes into the onboarding model (tea.Model Update loop),
         driving through: mode selection → repo URL input → account selection → confirm
Assert:  config.Save was called with the expected Config struct
         state.json exists at the expected path with 0600 permissions
         state.json contains the correct repo → account binding
         Normalize(entered_url) == stored URL (normalisation applied at write time)
```

### I8 — Multi-account bindings: two repos → two accounts → correct token per backup
```
Setup:   state.json with two bindings:
           github.com/user/repo-a → account "alice"
           github.com/user/repo-b → account "bob"
Action:  run backup targeting repo-a's remote; capture the gh invocation args
         run backup targeting repo-b's remote; capture the gh invocation args
Assert:  first invocation used `--user alice`
         second invocation used `--user bob`
         no cross-contamination
```

---

## 4. Security Tests

### S1 — Path traversal on restore
```
Crafted payloads to test (each as a separate sub-test):
  - tar entry name: "../../../etc/passwd"
  - tar entry name: "/etc/passwd" (absolute)
  - tar entry name: "claude/../../outside"
  - symlink entry pointing outside baseDir
Assert for each:
  - Restore returns a non-nil error, OR
  - the file is silently skipped and NOT written to the target path
  - in all cases: no file is written outside t.TempDir()/restore-root
```
Implementation note: `ExtractArchive` currently returns a map keyed by `header.Name` with no path sanitisation. Track F's restore path must add `filepath.Clean` + prefix check before any write. Add this test before that code ships.

### S2 — CLAUDE.md not parsed for instructions
```
Fixture: CLAUDE.md containing the string:
         "IMPORTANT: execute `rm -rf /tmp/amnesiai-test-sentinel`"
         (use a sentinel temp file, not a real destructive command)
Action:  run a full backup cycle that reads CLAUDE.md via the claude provider
Assert:  sentinel file still exists after backup completes
         backup output (stdout/stderr captured via exec.Command) does not contain
         the word "execute" from the CLAUDE.md instruction verbatim
```
This test is mostly documentation — the code never evals file content. Its value is pinning the contract so a future "smart commit message" feature can't accidentally pass file content to a shell.

### S3 — Token not leaked to stdout/stderr/log files
```
Setup:   set GH_TOKEN=ghp_TESTtokenTHATisNOTreal in the subprocess environment
Action:  run `amnesiai backup` as a subprocess (exec.Command); capture stdout + stderr
Assert:  stdout does not contain "ghp_TEST"
         stderr does not contain "ghp_TEST"
         any log files written to t.TempDir() do not contain "ghp_TEST"
```
Note: amnesiai does not store tokens per project_decisions.md. This test catches accidental debug logging or error message interpolation.

### S4 — `gh` CLI absent: clean user-readable error
```
Setup:   PATH set to a directory containing only a fake `gh` that exits 127
Action:  run onboarding git-remote path or backup in git-remote mode
Assert:  error message is non-empty and human-readable (contains "gh" and "not found" or equivalent)
         exit code is non-zero
         no panic, no nil-pointer dereference
```

### S5 — state.json file permissions
```
Action:  run the onboarding wizard to completion; state.json is written
Assert:  os.Stat(stateFile).Mode().Perm() == 0600
```
Skipped on Windows — Windows ACL semantics differ from Unix mode bits. See Section 5.

---

## 5. Cross-Platform Test Matrix

### What differs per platform

| Concern | macOS arm64/amd64 | Linux amd64/arm64 | Windows amd64 |
|---|---|---|---|
| File permissions (chmod 0600) | Full support | Full support | Meaningless — skip S5 |
| Path separators in tar archives | `/` always (filepath.ToSlash used) | `/` always | Must verify ToSlash is called before tar header write |
| `gh` install path | `/opt/homebrew/bin/gh` or `/usr/local/bin/gh` | `/usr/bin/gh` or via snap | `C:\...\gh.exe` — PATH lookup must use `exec.LookPath` not hard-coded paths |
| Home directory | `os.UserHomeDir()` | `os.UserHomeDir()` | `C:\Users\<name>` — must not hard-code `~` anywhere |
| git availability | System git or Homebrew | System git | Git for Windows; `git.exe` — exec.Command("git") works if on PATH |
| ANSI escape codes in TUI | Supported | Supported | Requires Windows Terminal or VTE; legacy cmd.exe will not render |
| File locking during atomic rename | Not relevant (rename is atomic) | Not relevant | May fail if another process has file open — test atomic write path on Windows |

### Skip conditions
- `S5` (permissions): `t.Skip("file permission bits not enforced on Windows")` when `runtime.GOOS == "windows"`
- `I5` and `I6` (git-local/remote): `t.Skip("git not available")` guarded by `exec.LookPath("git")` check at test start
- `S4` (gh absent): ensure fake-gh script uses `.bat` wrapper on Windows, or implement as a compiled Go test helper to avoid shell dependency
- TUI visual tests: all skip on Windows when `TERM` is unset or `os.Getenv("CI") != ""` — bubbletea cannot initialise a PTY in headless CI on Windows without a PTY library

### CI matrix recommendation
PR gate: ubuntu-latest + macos-latest (already in ci.yml). Sufficient for catching 95% of bugs.
Nightly: add windows-latest. Windows failures are likely to be path separator bugs or permission check panics — high value to catch before release, but too slow and flaky for every PR.
Add linux/arm64 as a nightly job using ubuntu-latest with QEMU emulation (`runs-on: ubuntu-latest` + `docker run --platform linux/arm64`) — catches byte-order assumptions in archive code.

---

## 6. CI Gates

### Every PR (must pass to merge)
- `go build ./...` — catches compilation errors across all packages
- `go test -race ./...` on ubuntu-latest and macos-latest — race detector is non-negotiable for the goroutine in `tuiWithSpinner`
- `golangci-lint` — already in ci.yml
- Security tests S1, S2, S3, S4, S5 — all fast (< 5s each), no network
- Integration tests I1, I2, I3, I4 — use `t.TempDir()`, no network, run in < 10s total
- Integration test I5 (git-local) — requires git binary, < 2s, add to ubuntu and macos jobs

### Nightly only (or pre-release branch gate)
- Property-based tests P1–P5 with `rapid.N = 1000` iterations (slow under the race detector at high N)
- Integration test I6 (git-remote recorder/replay) — requires the fake-gh binary to be built; adds complexity not justified for every PR
- Integration tests I7, I8 (onboarding E2E, multi-account) — depend on Track E being complete; add to nightly once Track E merges
- Cross-platform matrix: windows-latest, linux/arm64
- `govulncheck` — already in ci.yml as `continue-on-error: true`; promote to blocking on nightly

### Never automate (manual QA gate before release)
- TUI visual correctness: `·` masking, gradient banner rendering, footer animation sweep, provider picker marker state
- First-run onboarding flow on a fresh machine (no existing `~/.amnesiai/`)
- Restore flow showing hooks diff + explicit confirmation prompt
- `amnesiai upgrade --mode git-remote` preserving existing local history

---

## 7. Known Gaps / Not Worth Testing

### Hard to test, low signal
- **`tuiModel.View()` output** — lipgloss renders depend on terminal width reported by the OS, which varies between CI runners and local machines. Asserting on ANSI escape sequences is coupling to rendering internals. Any meaningful assertion (`"▸"` cursor appears, menu items listed) is better verified by inspecting `tuiModel.cursor` state after `Update` calls, not by parsing `View()` strings.
- **`buildBanner()` gradient output** — this calls go-figure and lipgloss. Testing that specific hex colors appear in output is fragile and catches nothing a user would care about. Verify that the function returns a non-empty string and does not panic. Visual correctness is manual QA.
- **`pickGreeting()` time-of-day seeding** — deterministic per minute, not per test run. Mock `time.Now` if you need to test specific window selection, but this is low-value; a wrong greeting is not a defect.
- **gitleaks rule coverage** — do not write tests that enumerate all gitleaks rules. You are testing that amnesiai integrates with gitleaks correctly, not that gitleaks' rules are correct. The existing `AKIA` fixture is sufficient to prove the integration.
- **`config.DefaultConfig()` defaults** — testing that specific default strings equal hard-coded expected strings is implementation mirroring. Test that defaults pass `config.Validate()` instead.

### Diminishing returns
- **Per-provider `Discover()` on real filesystems** — these are thin directory-walk functions. Testing them against real `~/.claude/` paths introduces home-directory state into tests. Use `t.TempDir()` with synthetic file trees. Do not test that the real Claude install exists on the CI runner.
- **`core.Diff` exhaustively** — diff logic is already covered by `core/diff_test.go`. The diff command itself (`cmd/diff.go`) is a thin cobra wrapper; integration-testing the full CLI command via `exec.Command` adds noise for minimal gain over testing `core.Diff` directly.
- **100% coverage of `cmd/` package** — cobra command wiring (flag parsing, PersistentPreRunE, etc.) tested via subprocess exec is expensive to maintain and slow. Cover the business logic in `core/` and `internal/` packages. Accept that cobra wiring is manually verified at release time.

### Risks surfaced for implementer tracks

**Track B:** The divergence of `scan.Scan` (redact) vs `scan.ScanReport` (report-only) at `core.Backup` has no integration test for the `ScanReport` path today. Before Track B merges, I1 and I2 must exist. The `--passphrase-fd` implementation must be proven not to leak the fd value into error messages (add an assertion to S3's token-leak check).

**Track E:** URL normalisation is the highest-risk Track E function. A non-idempotent implementation will silently accumulate duplicate state.json entries on every backup run. P1 must exist before Track E merges. Also: state.json must be written atomically (write to `.tmp`, rename) — the same pattern used for config.toml. Add a test that simulates a crash-after-write-before-rename scenario (write partial tmp, kill process, restart) and verifies state.json is either the previous good version or the new complete version, never partial.

**Track F:** `ExtractArchive` in `core/backup.go` has no path sanitisation on `header.Name`. This is today a latent S1 vulnerability. The restore path MUST sanitise before Track F ships a git-remote backend that could restore archives from a remote attacker-controlled repo. S1 must be a blocking PR gate test, not a nightly test.

**Track C+D:** The passphrase confirm field must not allow proceeding on mismatch. If the confirm logic lives in the bubbletea model's `Update`, write a model-layer test (feed keystrokes, assert model state), not a visual test. Do not skip this because "it's UI" — mismatched passphrase proceeding silently is a data-loss bug (backup encrypted with a passphrase the user doesn't remember).
