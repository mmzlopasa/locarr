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
```

Migracije baze rade se same pri pokretanju. Za backup kopirajte cijelu `CONFIG_DIR` mapu (uključujući
`secret.key`; bez njega spremljene lozinke vanjskih servisa ne mogu se dešifrirati).
