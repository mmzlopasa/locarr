# Locarr

One app for your whole media pipeline: TV, movies, music, subtitles, indexers and requests.
One login, one port, one place to see why something didn't arrive.

- **Downloaders:** qBittorrent, Transmission, Deluge, rTorrent, aria2, SABnzbd, NZBGet
- **Media server:** Streamero
- **Sources:** any Torznab or Newznab indexer, or Prowlarr / Jackett
- **Imports your setup from:** Sonarr, Radarr, Lidarr, Prowlarr, Bazarr, Seerr, Overseerr, qBittorrent, SABnzbd

Web interface plus Android and iOS apps. Runs with Docker Compose.

> Work in progress: the foundations (accounts, jobs, web shell, container) are done; the media features
> arrive phase by phase. See `docs/PLAN.md` and `docs/PROGRESS.md`.

## Quick start

```bash
mkdir -p /srv/locarr && cd /srv/locarr
curl -fsSLO https://raw.githubusercontent.com/mmzlopasa/locarr/main/deploy/docker-compose.yml
curl -fsSL https://raw.githubusercontent.com/mmzlopasa/locarr/main/deploy/.env.example -o .env
# edit .env: CONFIG_DIR, DATA_DIR, PUID/PGID, TZ
docker compose up -d
docker compose logs locarr | grep "setup code"
```

Open `http://<server-lan-ip>:8484` (on the server itself `http://127.0.0.1:8484`), enter the setup code from the log and create the administrator.

**One `/data` mount for media and downloads.** Put both under one host folder and mount it once:
`/data/media/{movies,tv,music}` and `/data/downloads/{torrents,usenet}`. Imports then use hardlinks
(instant, no extra disk space, seeding keeps working); separate mounts force a copy. Give your download
client the same `/data` mount so paths match.

Locked out? `docker compose exec locarr /app/locarr reset-admin` prints a new password.

Full guide (Croatian): `docs/INSTALL.md`.

## Development

See `CONTRIBUTING.md`. In short: `./scripts/gen-api.sh` after changing `api/openapi.yaml`,
`cd server && go test ./... -race`, `cd web && pnpm test`, and
`docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml up -d --build`.
