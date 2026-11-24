# Instalacija

Locarr je jedan kontejner s jednim portom. Treba vam Docker s Compose pluginom.

## 1. Mape

Locarr vidi dvije mape:

| U kontejneru | Što je | Na hostu (primjer) |
|---|---|---|
| `/config` | baza (`locarr.db`) i ključ za šifriranje (`secret.key`) — **radite backup** | `/srv/locarr/config` |
| `/data` | mediji **i** preuzimanja zajedno | `/srv/data` |

Unutar `/data` preporučeni raspored:

```
/data
├── media
│   ├── movies
│   ├── tv
│   └── music
└── downloads
    ├── torrents
    └── usenet
```

**Zašto jedna montaža?** Kad preuzimanje završi, Locarr ga uvozi u biblioteku *hardlinkom*: datoteka se ne
kopira, ne zauzima dvostruko mjesta, a torrent klijent i dalje može seedati. Hardlink radi samo unutar istog
datotečnog sustava **i iste Docker montaže**. Ako filmove i preuzimanja montirate odvojeno (`/movies`,
`/downloads`), uvoz mora kopirati. Download klijent mora vidjeti preuzimanja na istoj putanji
(`/data/downloads/...`), pa i njemu montirajte isti `/data`.

## 2. Pokretanje

```bash
mkdir -p /srv/locarr && cd /srv/locarr
curl -fsSLO https://raw.githubusercontent.com/mmzlopasa/locarr/main/deploy/docker-compose.yml
curl -fsSL https://raw.githubusercontent.com/mmzlopasa/locarr/main/deploy/.env.example -o .env
# uredite .env: CONFIG_DIR, DATA_DIR, PUID/PGID (id -u, id -g), TZ
docker compose up -d
docker compose logs locarr | grep "setup code"
```

Otvorite `http://<lan-ip-servera>:8484` (na samom serveru `http://127.0.0.1:8484`), upišite setup kod iz loga i napravite administratorski račun.

**Preglednik za zaštićene stranice (neobavezno).** Za stranice iza provjere preglednika (1337x,
ponekad LimeTorrents) Locarr ima vlastiti preglednik u zasebnom kontejneru (Chromium, ~560 MB na
disku, do 1 GB RAM-a dok radi; u mirovanju se gasi). Uključuje se compose profilom:

```bash
echo "LOCARR_BROWSER_TOKEN=$(openssl rand -hex 24)" >> .env
docker compose --profile browser up -d
```

Ubuduće (i kod nadogradnje) dodajte `--profile browser` svakoj `docker compose` naredbi. Provjera:
**Indexeri → Preglednik za zaštićene stranice → Testiraj**. Detalji u
[Indexeri](user/indexers.md#zasticene-stranice).
Setup kod vrijedi dok ne postoji administrator; nakon toga `/setup` više ne radi.

## 3. Kućna mreža i pristup izvana

Zadano Locarr sluša na portu 8484 na svim mrežnim sučeljima servera, pa ga uređaji u kućnoj mreži
(računalo, mobitel, aplikacije) otvaraju na `http://<lan-ip-servera>:8484`. Da sluša samo na kućnoj
mreži, u `.env` postavite `HTTP_PORT=<lan-ip-servera>:8484`. Port 8484 ne otvarajte na routeru.

Ako ipak želite pristup s interneta, postavite `HTTP_PORT=127.0.0.1:8484` i reverse proxy (nginx, Caddy,
Traefik) s HTTPS-om ispred njega i u `.env` postavite `LOCARR_TRUSTED_PROXIES` na adresu proxyja, da bi
ograničenje prijava vidjelo stvarne adrese klijenata.

## 4. Korisnik i prava

Kontejner se pokreće kao root samo toliko da `/config` preda korisniku `PUID:PGID`, a zatim radi kao taj
korisnik. `/data` se nikad ne mijenja — mora pripadati tom korisniku ili njegovoj grupi.

## 5. Zaboravljena lozinka

```bash
docker compose exec locarr /app/locarr reset-admin            # najstariji administrator
docker compose exec locarr /app/locarr reset-admin ivana      # određeni administrator
```

Ispisuje novu lozinku, isključuje dvofaktorsku prijavu tog računa i odjavljuje ga sa svih uređaja.

## 6. Nadogradnja i backup

```bash
docker compose pull && docker compose up -d
# s preglednikom za zaštićene stranice:
docker compose --profile browser pull && docker compose --profile browser up -d
```

Migracije baze rade se same pri pokretanju. Za backup kopirajte cijelu `CONFIG_DIR` mapu (uključujući
`secret.key`; bez njega spremljene lozinke vanjskih servisa ne mogu se dešifrirati).

## 7. Filmovi: TMDb ključ, hardlinkovi i koš

- Za filmove treba vlastiti TMDb API ključ: **Postavke → Metapodaci** (vidi `docs/user/movies.md`).
  Locarr se spaja na `api.themoviedb.org` i `image.tmdb.org`; plakati se čuvaju u `CONFIG_DIR/cache/images`.
- Uvoz radi hardlink kad su preuzimanja i biblioteka na istom datotečnom sustavu i u istoj montaži
  (`/data/downloads` i `/data/media` pod jednim `DATA_DIR`). Download klijent mora vidjeti datoteke pod
  istom putanjom `/data/...` ili treba dodati mapiranje udaljene putanje.
- Koš za smeće je zadano `/data/recycle-bin` (Postavke → Upravljanje medijima); tamo idu zamijenjene i
  obrisane datoteke i brišu se tek nakon roka (zadano 7 dana).
