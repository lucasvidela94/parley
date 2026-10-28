# Security

parley runs local agent CLIs and writes to your machine, so the blast radius matters.

## Reporting

Report privately through GitHub's [security advisories](https://github.com/lucasvidela94/parley/security/advisories/new).
Do not open a public issue for a vulnerability. Include the version (`parley version`), the
harness involved and a minimal reproduction.

## What the tool guarantees

- **Read-only by default.** A consulted agent cannot edit files unless the call passes
  `write=true` and its participant config allows `permission: "write"`.
- **No inherited authority.** An incoming message never grants tools or permissions the
  participant did not already have.
- **Bounded recursion.** `PARLEY_CHAIN` and `PARLEY_DEPTH` stop A→B→A loops before tokens
  are spent.
- **Verified updates.** `parley update` downloads from GitHub Releases, checks the sha256
  from `checksums.txt`, and refuses an archive entry larger than 256 MiB. Releases also
  carry a Sigstore signature of `checksums.txt` (`checksums.txt.sigstore.json`) that you can
  check with `cosign verify-blob`; `parley update` does not check it itself (D14, D18).
- **No secrets in config.** Participant config holds harness names, models and paths. API
  keys stay in each harness's own credential store.

## Supported versions

The latest tagged release receives fixes. Older tags are kept for reproducibility and are
not patched.

## Scanning

CI runs `govulncheck` and `gosec` (through golangci-lint) on every push, and Dependabot
opens pull requests for Go modules and GitHub Actions weekly.
