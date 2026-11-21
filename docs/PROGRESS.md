# Napredak

## Trenutna faza
P3 — download klijenti: gotovo (branch `phase/P3-downloaders`). P0 i P1 gotovi; P2 se radi usporedno.

## Gotovo u P3
- `internal/downloaders`: zajedničko sučelje, normalizirana stanja i mogućnosti (D-017), klijent za odlazne veze s politikom adresa, timeoutima, ograničenjem odgovora i bez preusmjerenja na drugi host (D-020), parser .torrent/magnet, `testkit` (mali torrenti i NZB-ovi), `contracttest`.
- Klijenti, svaki iz javne dokumentacije: qBittorrent (v4 i v5 API, sesija, kategorije sa save pathom, oznake, ciljevi seedanja), Transmission (session-id, oznake kao kategorije), Deluge (Web JSON-RPC, spajanje na daemon, Label plugin), rTorrent (XML-RPC, `d.custom1`, multicall), aria2 (JSON-RPC s tajnom, kategorije kao podmape), SABnzbd (red + povijest, kategorije, prioriteti, poruke o neuspjehu), NZBGet (append, listgroups, history, par/unpack stanje).
- Ugovorni testovi protiv stvarnih klijenata u Dockeru (`scripts/test-clients.sh`, oznaka `clients`): prošli su qBittorrent 5.3, Transmission 4.1, Deluge 2.2, rTorrent 0.16 (`crazymax/rtorrent-rutorrent`), aria2 (alpine), SABnzbd 5.1, NZBGet 26.3. CI posao `clients` pokreće qBittorrent, Transmission i SABnzbd.
- `internal/downloads` (migracija `00003_downloads.sql`): klijenti sa šifriranom lozinkom/API ključem koji se samo pišu, Test s prevedenim greškama i nedostajućim kategorijama, stvaranje kategorija, mapiranje udaljenih putanja, praćenje (D-018, D-019) s događajima u `download_events` i sabirnicom za uvoz, nepoznate stavke prikazane ali netaknute, blokirana izdanja s provjerom za P2 (D-021), grab kroz poslužitelj i kroz indexer (D-020).
- API: `/downloadclients*`, `/remotepathmappings*`, `/queue*` (red, osvježavanje, grab, pauza/nastavak/prioritet/uklanjanje, povijest stavke, blokirana izdanja).
- Web: stranica Download klijenti (popis sa stanjem, verzijom i slobodnim prostorom, obrazac s poljima samo za odabrani klijent, Test, stvaranje kategorija, mapiranja putanja, postavke praćenja), Aktivnost → Red preuzimanja (uživo, filtri, akcije) i Blokirana izdanja, gumb „Preuzmi" u Pretrazi šalje izdanje klijentu. e2e test `web/e2e/downloads.spec.ts` sa stvarnim Transmissionom.
- Korisnička dokumentacija: `docs/user/download-clients.md`.

## Za P4–P6 (uvoz)
- Pretplata: `deps.Downloads.Bus.Subscribe(func(ctx, ev downloads.Event))`; `EventCompleted` nosi `Download.LocalPath` (provjerena lokalna putanja) i `ItemRef`.
- Nakon uvoza: `Downloads.MarkImported(ctx, id, poruka)` ili `MarkImportFailed(ctx, id, poruka)`.
- Grab iz koda: `Downloads.Grab(ctx, downloads.GrabRequest{...})` (s `IndexerID` i `DownloadURL` iz `indexers.Release`); blokiranje: `Downloads.AddBlock`, provjera: `Downloads.IsBlocklisted`.

## Gotovo u P1
- `internal/indexers/znab`: Torznab/Newznab klijent (caps sa supportedParams, kategorijama i limitima; search, tvsearch, movie, music, book; ID parametri; offset/limit), tolerantan parser feedova (imenski prostori atributa, CDATA, HTML entiteti, enclosure ili link, samo magnet, nedostaje veličina, razni formati datuma, gzip bez zaglavlja, 8-bitni charseti) i tipizirane `<error code>` greške.
- `internal/indexers/httpx`: odlazni klijent s politikom adresa (D-013), ograničenjem veličine, bez preusmjerenja na drugi host, HTTP/SOCKS5 proxy, FlareSolverr za Cloudflare provjere, uklanjanje ključeva iz grešaka.
- Indexeri (migracija `00002_indexers.sql`): CRUD s šifriranim ključem i lozinkom proxyja, kategorije po vrsti sadržaja iz capsa, prioritet, oznake, RSS/automatska/ručna pretraga, minimalni seederi, omjer/vrijeme seedanja (za P3), limiti zahtjeva i grabova, timeout, proxy, FlareSolverr, Test (caps + probna pretraga), zdravlje s čekanjem i automatskim ponovnim pokušajem (D-014).
- Pretraga: paralelno na sve indexere, uklanjanje duplikata, kratka predmemorija, JSON i SSE tok (D-012), `search_log` sa stabilnom shemom i `item_ref`.
- RSS sync (zadatak `rss-sync`, zadano 15 min, mijenja se u postavkama bez restarta), `releases_seen` s brisanjem nakon roka, kanal novih izdanja za P2/P4 (D-011).
- Prowlarr i Jackett kao izvori s pregledom i syncom (D-015); preuzimanje .torrent/.nzb preko poslužitelja bez otkrivanja ključa (D-016).
- Web: stranica Indexeri (popis sa zdravljem, dodavanje/uređivanje s testom i izborom kategorija, čarobnjak za uvoz, izvori, postavke), stranica Pretraga (tok rezultata, filtri, sortiranje, poveznice samo za admina/upravitelja, „Preuzmi" isključen do P3), Sustav → Status prikazuje zdravlje indexera.
- Lažni indexeri: `internal/indexers/fake` + `cmd/locarr-fakes`, `scripts/dev-fakes.sh` (kontejner `fake-indexer` na `locarr-net`), e2e test `web/e2e/indexers.spec.ts` u CI-ju.

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
2. P2 parser i odluke (provjera blokiranih: `downloads.BlocklistChecker`); zatim P4 filmovi (uvoz se pretplaćuje na `downloads.Bus`).
3. P1 ostatak: proxyji po indexeru testirani protiv stvarnog HTTP/SOCKS5 proxyja; uvoz Prowlarr/Jackett provjeren samo prema lažnim izvorima.

## Poznati problemi / napomene
- Download klijenti: uklanjanje nakon uvoza i ciljevi seedanja provjereni su samo s lažnim klijentom dok ne stigne uvoz (P4). Usenet klijenti su testirani bez news servera, pa su završena i neuspjela preuzimanja pokrivena samo testovima s fixturama. NZBGet kategorije se ne mogu stvoriti kroz API.
- Ograničenje zahtjeva po indexeru vodi se u memoriji i počinje od nule nakon restarta (D-014).
- Playwright u lokalnom razvoju: lockfile koristi 1.65; slika `mcr.microsoft.com/playwright:v1.64.1` radi uz `PW_CHROMIUM_PATH`.
