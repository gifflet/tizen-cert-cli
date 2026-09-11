# tizen-cert

Interactive CLI that generates a complete, **valid Samsung Tizen TV certificate profile** (author + distributor certificates) for any number of TVs — end to end, without Tizen Studio's Certificate Manager.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). Cross-platform: Linux, macOS and Windows.

## What it does

1. **Detects TVs** connected via `sdb` (and/or accepts DUIDs typed manually, or a TV IP address to connect on the spot).
2. **Renews expired profiles**: point it at an existing `~/SamsungCertificate/<name>` folder and it reuses the DUIDs from the old distributor certificate.
3. **Signs you into your Samsung account** (browser flow with a local callback, same as Tizen Studio's certificate extension — with a manual token fallback for headless machines).
4. **Issues the certificates** against Samsung's certificate servers: author certificate + distributor certificate bound to the selected DUIDs (Public or Partner privilege).
5. **Writes everything ready to use**: `~/SamsungCertificate/<name>/` with `author.p12`, `distributor.p12`, `.crt`/`.csr` files, and registers the signing profile directly in `~/tizen-studio-data/profile/profiles.xml` — no `tizen` CLI required.

After that, `tizen package -t wgt -s <name> ...` (on any machine with the generated folder + profiles.xml) signs apps installable on the covered TVs.

## Requirements

- A Samsung account (the sign-in happens in your browser).
- `sdb` in the `PATH` **only** if you want automatic TV detection — manual DUIDs work without it.
- TVs in developer mode with the host IP registered (for detection and app installs, not for certificate generation itself).

## Usage

```bash
tizen-cert
```

Follow the wizard:

```
Create a new certificate profile   → select TVs / add DUIDs → privilege →
name/e-mail/password → Samsung sign-in → done
Renew an existing profile          → pick the expired folder → same flow,
DUIDs pre-selected
```

`--version` prints the build version.

## Building

```bash
make build        # local binary ./tizen-cert
make all          # dist/ binaries for linux/darwin (amd64+arm64) and windows (amd64)
```

## Notes

- The distributor certificate covers at most 50 DUIDs (Samsung limit for Public level).
- Partner privilege requires a Samsung account enrolled in the partnership program; issuance fails otherwise.
- Generated `.p12` files use the legacy DES3 encoding on purpose — Tizen Studio's Java signer cannot read modern AES-encrypted PKCS#12 files.
