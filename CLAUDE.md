# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

Two Go modules and one web frontend live in this repo:

- **Client** (`tarish`, root module, `go.mod`, Go 1.21, `CGO_ENABLED=0`) — single binary with `//go:embed bin configs`. Wraps XMRig.
- **Server** (`tarish-server`, `server/go.mod`, Go 1.22, **requires CGO** for `mattn/go-sqlite3`) — dashboard API + SQLite store + embedded React app.
- **Frontend** (`web/`, Vite + React 19 + Tailwind 4) — built into `web/dist`, copied to `server/web/dist` for the server's `//go:embed`.

The two Go modules are intentionally separate. Do not merge them: the client is built CGO-free for portability, the server needs CGO for sqlite3.

## Build, test, run

Client:

```bash
./build.sh            # cross-compile darwin/linux × amd64/arm64 into dist/, write version file
go build -o tarish .  # quick local build
go test ./...         # client tests
```

Server (run on the same arch you're targeting; cross-compile to linux/arm64 needs `aarch64-linux-gnu-gcc`):

```bash
cd server && ./build.sh         # builds web/dist, copies into server/web/dist, then go build
cd server && go test ./...
```

Frontend:

```bash
cd web && npm install
cd web && npm run dev    # vite dev server
cd web && npm run build  # tsc -b && vite build → web/dist
cd web && npm run lint
```

Single Go test: `go test ./xmrig -run TestSelectConfig` (or the equivalent under `server/`).

## Release pipeline

- `./release.sh --version vX.Y.Z` — enforces clean tree, runs `build.sh` with `VERSION` set, then `deploy.sh --no-build`. Use this for client releases shipped via auto-update.
- `./deploy.sh [--dry-run] [--no-build] [--target ...]` — rsyncs `dist/`, `version`, `install.sh` to `nas:/vol1/1000/File/tarish` (override with `TARISH_DEPLOY_TARGET`). The `version` file is what `tarish update` polls via `https://file.aooo.nl/tarish/version` (see `update/update.go`).
- `./build-client-release.sh` + `server/build-release.sh` + `./build-complete-release.sh` — produce GitHub Release archives. Driven by `.github/workflows/server-release.yml` on tag push (`v*`).

## Architecture notes that span files

**Daemonization via hidden subcommands.** `tarish start` calls `exec.Command(self, "_agent-daemon")` and `exec.Command(self, "_update-daemon")` with `Setpgid: true`. Those hidden commands are dispatched in `main.go` before normal command parsing and never appear in help. PID files live in `~/.local/share/tarish/`; daemon logs go to `~/.local/share/tarish/log/`.

**Embedded assets must stay in tree.** `main.go` declares `//go:embed bin configs` — `bin/<version>/xmrig_<os>_<arch>` and `configs/*.json` are part of the build. `embedded/assets.go` extracts them at install or first run. Do not move these directories.

**User-context resolution under sudo.** `userctx.HomeDir()` resolves the *owning* user's home even when invoked as root via sudo (reads `SUDO_USER`/`TARISH_USER`/`TARISH_HOME`). Anything that touches config, PID files, or logs goes through `userctx` or `config.ConfigDir()` — don't call `os.UserHomeDir()` directly for state paths or you'll write to `/root` under sudo.

**Runtime config rewrite.** `xmrig.PrepareRuntimeConfig` reads the selected static config from `configs/`, injects `api.id = "<short-cpu>-0"` and `worker-id = "<local-ip-with-dashes>"`, applies TLS pool settings from `config.IsTLSXmrigProxyEnabled()`, picks an available HTTP API port, and writes the result to `<share>/log/xmrig_runtime.json`. The agent and `tarish status` read port + access-token from that runtime file via `GetHTTPConfigFromRuntime`.

**Pool endpoints are hardcoded.** `xmrig/config.go` defines `TLSPoolURL`, `TLSFingerprint`, `NonTLSPoolURL`. Changing them changes where every miner connects.

**Agent ↔ server protocol.** Agent posts `/api/report` every 30 s and polls `/api/miners/{id}/config/pending` every 3 s for fast dashboard config push. Pending config is PUT into xmrig's local HTTP API (`http://127.0.0.1:<port>/1/config`), then acked back to the server. Auth uses `Authorization: Bearer <agent-key>` plus optional Cloudflare Access service-token headers (`CF-Access-Client-Id` / `CF-Access-Client-Secret`).

**Server auth is partial.** `--agent-key` only protects agent endpoints (`/api/report`, `/api/miners/*/config/pending`, `/api/miners/*/config/ack`). Dashboard mutation routes are unauthenticated — production deploys must front the whole server with Cloudflare Access or equivalent. See `server/README.md` "Security warning".

**Config selection cascade.** `xmrig.SelectConfig` tries `<family>.json`, short alias (`m3pro.json`), base family (`apple_m3.json`), vendor (`apple.json`), arch-default, OS-default, `default.json`, and finally generates `generic_<N>cores.json` on the fly. Adding a new CPU usually means adding a JSON to `configs/` matching one of these names.

**Auto-update opportunism.** When `auto_update` is enabled, every `start`/`stop`/`status`/`info` call also runs `update.AutoUpdate()` inline (not just the daemon). The daemon handles the periodic background path.

## Conventions worth knowing

- Commit messages use Conventional Commits (`feat:`, `fix:`, `chore(xmrig):`, etc.) — match the existing style.
- The `version` file at the repo root is build output, not source. It's regenerated by `build.sh` and consumed by `deploy.sh` and the update endpoint.
- `tarish.db*` files at the repo root are local dev artifacts (gitignored). The server's real DB lives under `/var/lib/tarish/`.
- macOS ships an Apple Silicon dev binary in the working tree (`./tarish`). It's the cross-compile output from `build.sh`, not source-tracked.
