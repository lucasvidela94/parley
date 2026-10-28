## Install

```bash
curl -fsSL https://github.com/lucasvidela94/parley/releases/latest/download/parley_linux_amd64.tar.gz \
  | tar -xz -C ~/.local/bin parley
parley setup --write
```

Or `go install github.com/lucasvidela94/parley@latest`.

Existing installs: `parley update`.

## Verify

`checksums.txt` is signed with Sigstore (keyless, GitHub OIDC). To check an archive against it:

```bash
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/lucasvidela94/parley/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check --ignore-missing checksums.txt
```
