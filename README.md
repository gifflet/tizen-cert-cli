<div align="center">

# tizen-cert

**Generate a complete, valid Samsung Tizen TV certificate profile — author and distributor, for any number of TVs — without Tizen Studio's Certificate Manager.**

[![GitHub Release](https://img.shields.io/github/v/release/gifflet/tizen-cert-cli)](https://github.com/gifflet/tizen-cert-cli/releases)
![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)
![Platforms](https://img.shields.io/badge/macOS%20%7C%20Linux%20%7C%20Windows-4493F8)

### [Documentation →](https://tizen-cert-cli.vercel.app)

<img src="docs/demo/tizen-cert.gif" alt="tizen-cert wizard: adding a DUID, picking a privilege level, filling in certificate details, and signing in to Samsung" width="880">

</div>

## 📥 Install

One command — the same one updates you later.

macOS and Linux:

```bash
curl -fsSL https://tizen-cert-cli.vercel.app/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://tizen-cert-cli.vercel.app/install.ps1 | iex
```

Read them first if you'd rather: [install.sh](https://tizen-cert-cli.vercel.app/install.sh) · [install.ps1](https://tizen-cert-cli.vercel.app/install.ps1). Or grab a binary from the [releases page](https://github.com/gifflet/tizen-cert-cli/releases).

---

`tizen-cert` is an interactive terminal wizard, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), that issues a Samsung Tizen TV signing profile end to end — no `tizen` CLI, no manual DUID juggling, no clicking through a GUI.

The point: everything Tizen Studio's Certificate Manager does through dialogs happens here through a few keystrokes, and the result drops straight into `~/tizen-studio-data/profile/profiles.xml`, ready to sign apps.

## ✨ Highlights

- 📡 **Detects TVs automatically** — via `sdb`, or accept DUIDs typed manually, or a TV IP address to connect on the spot.
- ♻️ **Renews expired profiles** — point it at an existing `~/SamsungCertificate/<name>` folder and it reuses the DUIDs from the old distributor certificate.
- 🔐 **Signs in for you** — the same browser flow as Tizen Studio's certificate extension, with a manual token fallback for headless machines.
- 📜 **Issues both certificates** — author certificate + distributor certificate bound to the selected DUIDs, Public or Partner privilege.
- 📦 **Writes everything ready to use** — `author.p12`, `distributor.p12`, `.crt`/`.csr` files under `~/SamsungCertificate/<name>/`, and registers the profile directly in `profiles.xml` — no `tizen` CLI required.

> After that, `tizen package -t wgt -s <name> ...` — on any machine with the generated folder and `profiles.xml` — signs apps installable on the covered TVs.

## Requirements

- A Samsung account — sign-in happens in your browser.
- `sdb` on your `PATH`, only if you want automatic TV detection (manual DUIDs work without it).
- TVs in developer mode with the host IP registered — needed for detection and app installs, not for certificate generation itself.

## 🚀 Quick Start

**Run**

```bash
tizen-cert
```

<details>
<summary>Building from source instead?</summary>

```bash
make build        # local binary ./tizen-cert
make all          # dist/ binaries for linux/darwin (amd64+arm64) and windows (amd64)
```

</details>

| Mode | What happens |
|---|---|
| **Create a new profile** | Select TVs / add DUIDs → choose privilege → name, e-mail, password → Samsung sign-in → done |
| **Renew an existing profile** | Pick the expired folder → same flow, with DUIDs pre-selected |

`tizen-cert --version` prints the build version.

## 🧩 Good to know

| Topic | Detail |
|---|---|
| **DUID sources** | `sdb shell 0 getduid` (what the tool runs, also when you type a TV IP) or the TV's own menu — `Support > Contact Samsung` → "Unique Device ID". |
| **Headless / SSH servers** | Samsung posts the sign-in result to `localhost:4794`, so forward that port from your local machine first: `ssh -L 4794:localhost:4794 user@server`, then open the sign-in URL in your **local** browser — the flow completes by itself. The wizard detects SSH sessions, prints this command pre-filled, and `c` copies the URL to your local clipboard (OSC 52). |
| **The REST API trap** | The `duid` field from the TV's REST API (`http://<tv>:8001/api/v2/`) is a **different identifier** — a UUID from the SmartThings space — and is **not** valid for certificates. There is no known conversion. |
| **Distributor limit** | Covers at most 50 DUIDs — Samsung's limit for the Public privilege level. |
| **Partner privilege** | Requires a Samsung account enrolled in the partnership program; issuance fails otherwise. |
| **`.p12` encoding** | Generated with legacy DES3 on purpose — Tizen Studio's Java signer can't read modern AES-encrypted PKCS#12 files. |

## 📚 Documentation

- [tizen-cert-cli.vercel.app](https://tizen-cert-cli.vercel.app) — the documentation site: recorded demo, quick start, wizard reference and FAQ (source in `docs-site/`).
- [Samsung Certificate Issuance — Implementation Spec](docs/SAMSUNG-CERT-API-SPEC.md) — the reverse-engineered API contract this tool implements (endpoints, payloads, PKI details), for anyone extending or auditing it.
- The demo recording is reproducible: `docs/demo/record.sh` re-drives the wizard (expect + asciinema + agg) and regenerates the GIF/MP4 assets.

## 🧪 Status

Single Go module, no CGO. Core logic is unit-tested (`internal/pki`, `internal/profile`, `internal/tui`). Dependencies are limited to Bubble Tea/Bubbles/Lip Gloss for the UI and `go-pkcs12` for the certificate bundles.
