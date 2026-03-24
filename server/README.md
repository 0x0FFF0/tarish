# tarish-server

`tarish-server` is the central HTTP API and dashboard for Tarish agents.

The miner-side flow is:

1. Run `tarish server set <url>` on each miner.
2. Run `tarish server agent-key <key>` on each miner if the server requires agent auth.
3. Run `tarish start`.

When mining starts, Tarish launches a background agent daemon that reports to the server every 30 seconds and polls for pending config overrides every 3 seconds.

## What runs on the server

- `tarish-server`: dashboard, API, SQLite storage
- `xmrig-proxy` HTTP API: optional, only needed if you want proxy summary and worker views in the dashboard

## Relevant code

- Server entrypoint: `server/main.go`
- Server routes: `server/api/routes.go`
- SQLite store: `server/store/sqlite.go`
- Agent heartbeat/config polling: `agent/agent.go`

## Build

`tarish-server` is a separate Go module under `server/`.

Requirements:

- Go 1.22+
- Node.js and npm for the web dashboard build
- A working C toolchain because `github.com/mattn/go-sqlite3` uses CGO

Recommended build on Linux ARM64:

```bash
cd server
./build.sh
```

Why native ARM64 builds are preferred:

- The top-level client build uses `CGO_ENABLED=0`, but `tarish-server` cannot.
- `tarish-server` depends on `go-sqlite3`, so cross-compiling to `linux/arm64` from macOS requires a Linux ARM64 cross-C toolchain.
- The simplest path is to build directly on the ARM64 Linux host that will run the service.

If `web/dist` is already prepared and copied into `server/web/dist`, you can build only the Go binary:

```bash
cd server
go build -o tarish-server .
```

## Run

Minimal server:

```bash
./tarish-server \
  --addr 127.0.0.1:8080 \
  --db /var/lib/tarish/tarish.db \
  --agent-key CHANGE_ME
```

With optional `xmrig-proxy` integration:

```bash
./tarish-server \
  --addr 127.0.0.1:8080 \
  --db /var/lib/tarish/tarish.db \
  --agent-key CHANGE_ME \
  --proxy-url http://127.0.0.1:8081 \
  --proxy-api-token PROXY_TOKEN
```

Flags:

- `--addr`: listen address, default `:8080`
- `--db`: SQLite path, default `tarish.db`
- `--agent-key`: shared secret used by miner agents
- `--proxy-url`: optional `xmrig-proxy` HTTP API base URL
- `--proxy-api-token`: optional bearer token for the proxy API
- `--web`: optional external frontend directory instead of embedded assets

## Deploy on Linux ARM64

Suggested layout:

- Binary: `/opt/tarish/server/tarish-server`
- Database: `/var/lib/tarish/tarish.db`
- Systemd unit: `/etc/systemd/system/tarish-server.service`

This repo includes a unit template at `server/tarish-server.service`.

The service file now supports `/etc/tarish-server.env`, so you can keep runtime settings outside the unit file. A matching template is included at `server/tarish-server.env.example`.

## GitHub Actions release bundles

This repo can build `tarish-server` in GitHub Actions and publish ready-to-download archives instead of compiling on the target host.

Manual workflow runs:

- Open the `Build Tarish Server Release` workflow in GitHub Actions
- Run it against the branch or tag you want
- Download the generated artifacts from the workflow run

Tagged releases:

- Push a semver tag such as `v1.0.8`
- The workflow builds `linux/amd64` and `linux/arm64` archives
- The same archives are uploaded to the matching GitHub Release as downloadable assets

Each release archive contains:

- `install.sh`
- `tarish-server`
- `tarish-server.service`
- `tarish-server.env.example`
- `cloudflared.example.yml`
- `README.md`
- `VERSION`

Quick install on the target host after extracting an archive:

```bash
sudo ./install.sh
```

The installer copies the binary to `/opt/tarish/server`, installs the systemd unit to `/etc/systemd/system/tarish-server.service`, creates `/etc/tarish-server.env` if it does not exist yet, and leaves the env file untouched on later upgrades.

GitHub repository settings required:

- `Settings -> Actions -> General -> Workflow permissions -> Read and write`
- GitHub-hosted runners enabled for the repository

No additional secrets are required because the workflow uses the built-in `GITHUB_TOKEN`.

## Cloudflare Tunnel

If you plan to expose the dashboard through Cloudflare Tunnel:

- Bind `tarish-server` to `127.0.0.1:8080`
- Run `cloudflared` on the same host
- Publish a hostname to `http://127.0.0.1:8080`
- Keep port `8080` closed to the public Internet

A sample tunnel config is included at `server/cloudflared.example.yml`.

Suggested `cloudflared` flow:

1. Create a named tunnel in Cloudflare and download its credentials file.
2. Copy `server/cloudflared.example.yml` to your host as `/etc/cloudflared/config.yml` or `$HOME/.cloudflared/config.yml`.
3. Replace the tunnel UUID, credentials path, and hostname.
4. Install `cloudflared` as a service.
5. Start the `cloudflared` service.

Example commands:

```bash
sudo cloudflared --config /etc/cloudflared/config.yml service install
sudo systemctl start cloudflared
sudo systemctl status cloudflared
```

## Security warning

`--agent-key` only protects agent-facing report and config-sync endpoints. The dashboard's config mutation routes are not protected by the same middleware.

If you expose `tarish-server` through Cloudflare Tunnel, put the entire app behind Cloudflare Access or another authentication layer. Do not publish it as an unauthenticated public app.
