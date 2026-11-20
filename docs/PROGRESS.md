# Napredak

## Trenutna faza
P0 — temelji: gotovo (branch `phase/P0-foundations`).

## Gotovo u P0
- Monorepo prema PLAN §4: `api/openapi.yaml` kao izvor istine, `scripts/gen-api.sh` (ogen → `server/internal/api/gen`, openapi-typescript → `web/src/api/schema.gen.ts`), CI provjerava da je generirani kod svjež.
- Server (`cmd/locarr`: `serve`, `migrate`, `healthcheck`, `version`, `reset-admin`): konfiguracija iz `LOCARR_*`, slog JSON s maskiranjem tajni, SQLite (WAL, busy timeout, strani ključevi, jedna konekcija za pisanje + čitači), goose migracije ugrađene u binarij, `/healthz`, `/readyz` (spreman za ~40 ms), `/api/v1/system/info`, uredno gašenje.
- Red poslova (`internal/jobs`, D-004) sa zadacima `ping` i `housekeeping`; povijest i ručno pokretanje preko API-ja i weba.
- Prijava (D-005): postavljanje admina sa setup kodom iz loga, uloge i dozvole, sesije (kolačić + bearer), uređaji i odjava s uređaja, API ključevi (puni / samo čitanje), TOTP s kodovima za oporavak, zaključavanje računa, ograničenje i zabrana po adresi, povjerljivi proxyji, zaštita od CSRF-a, promjena lozinke odjavljuje ostale sesije, upravljanje korisnicima sa zaštitom zadnjeg admina.
- Tajne (D-006): `secret.key`, AES-GCM, tipizirane postavke s tajnim poljima koja se samo pišu.
- Sigurne putanje: `fs.SafePath`, root mape s prostorom na disku i provjerom pisanja, preglednik mapa.
- HTTP: sigurnosna zaglavlja i CSP, HSTS na HTTPS-u, gzip, ograničenje tijela 1 MiB, timeouti, bez CORS-a, `robots.txt`, 404 za putanje skenera, JSON greške pod `/api`.
- Web: React 19 + Vite + TanStack + Tailwind v4, EN/HR, tamni „operativni" izgled, postavljanje, prijava s 2FA, račun, korisnici, postavke (općenito, mape za medije), sustav (status, zadaci); ostale stranice pošteno označene „stiže u fazi PX".
- Isporuka: `deploy/Dockerfile` (amd64 + arm64, ~37 MB), `docker-compose.yml` s jednim servisom, `compose.dev.yml`, `.env.example`, `docs/INSTALL.md`, CI (`ci.yml`, `image.yml` s cosign potpisom).
- Dev instanca na ovom hostu: `scripts/deploy-dev.sh` (projekt `locarr`, `127.0.0.1:8484`, podaci u `~/locarr-data`).

## Sljedeći koraci
1. P0 ostatak:
   - Prvo pokretanje: korak za odabir mape za medije u čarobnjaku (danas se root mape dodaju u Postavke → Mape za medije odmah nakon postavljanja).
   - Backup i vraćanje (automatski i ručno); migracije pri pokretanju već rade.
2. Paralelno: P1 indexeri (Torznab/Newznab klijent, lažni indexer za testove, `scripts/dev-fakes.sh`), P2 parser i odluke, P3 download klijenti.

## Poznati problemi / napomene
- Playwright u lokalnom razvoju: lockfile koristi 1.65; slika `mcr.microsoft.com/playwright:v1.64.1` radi uz `PW_CHROMIUM_PATH`.
