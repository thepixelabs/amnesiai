# Upgrading from 1.0.x to 1.1.0

## Re-backup to fix placeholder corruption

Backups created with 1.0.x under encryption may contain `<REDACTED:...>` placeholders where real config values should be. This was a bug: gitleaks redacted bytes _before_ encryption, so restoring those archives overwrote your actual values with placeholders.

**After upgrading, run `amnesiai backup` once.** The new backup captures your current on-disk config without any redaction. Your 1.0.x snapshots remain readable but should be treated as lossy — do not restore from them if a 1.1.0 snapshot is available.

## `--passphrase` flag is gone

The `--passphrase` flag has been removed because it exposed your passphrase in `argv` and shell history.

Switch to one of these alternatives:

```sh
# Environment variable (works everywhere)
export AMNESIAI_PASSPHRASE="your passphrase"
amnesiai backup

# File descriptor (preferred for scripts — never appears in argv)
amnesiai backup --passphrase-fd 3  3<<<$'your passphrase'
```

## `--no-encrypt` now requires `--force-no-encrypt` when secrets are detected

If your scripts passed `--no-encrypt` and gitleaks finds secrets, the command will now refuse to run. Replace `--no-encrypt` with `--force-no-encrypt` only if you have reviewed the detected secrets and accept that they will be written in plaintext.

## First-run wizard

On first launch after upgrade, the wizard runs automatically. It takes about two minutes and sets storage mode, git provider, and encryption defaults. You can re-run it at any time with `amnesiai --settings`.
