# Odluke

## D-001 Stack i oblik isporuke (2026-11-20)
**Kontekst:** Locarr zamjenjuje šest aplikacija i mora biti „jedan port, jedna prijava"; pokreće se preko Docker Composea; vlasnik već održava Streamero (Go, React, Kotlin, SwiftUI).
**Odluka:** Go server s OpenAPI-first API-jem (ogen), SQLite u WAL načinu kao jedina baza, vlastiti red poslova u istoj bazi, React web ugrađen u binarij, nativne Android i iOS aplikacije. Jedan Docker image, jedan servis u `docker-compose.yml`.
**Posljedice:** nema Postgresa ni Redisa za održavanje; backup je jedna datoteka (uz WAL checkpoint). Ako se pokaže potreba za više instanci, apstrakcija baze ostaje tanka da se Postgres može dodati kasnije.

## D-002 Clean-room prema postojećim *arr aplikacijama (2026-11-20)
**Kontekst:** aplikacije koje zamjenjujemo uglavnom su GPL-3.0.
**Odluka:** ne čitamo njihov izvorni kod. Ponašanje i formate učimo iz javne API dokumentacije, specifikacija protokola (Torznab/Newznab, API-ji download klijenata) i testiranjem preko njihovih API-ja. Parser imena izdanja pišemo sami s golden testovima.
**Posljedice:** neka ponašanja će se razlikovati; razlike koje korisnici primijete zapisujemo u FEATURES.md.

## D-003 Uvoz postojećeg setupa samo preko API-ja (2026-11-20)
**Kontekst:** korisnici dolaze s gotovim Sonarr/Radarr/… instalacijama.
**Odluka:** uvoz čita podatke isključivo preko javnih API-ja tih aplikacija (URL + API ključ), nikad iz njihovih baza ili konfiguracijskih datoteka, i nikad ništa ne mijenja na izvoru. Prije uvoza prikazuje se pregled (što će se uvesti, što se ne može).
**Posljedice:** uvoz radi i kad su stare aplikacije na drugom stroju; ono što API ne izlaže ne može se uvesti.

## D-004 Red poslova u SQLiteu (2026-11-20)
**Kontekst:** RSS sync, pretrage, praćenje preuzimanja, uvoz i titlovi trebaju pozadinske poslove koji preživljavaju restart, a D-001 isključuje Redis i zaseban servis.
**Odluka:** vlastiti red u `internal/jobs`, u istoj SQLite bazi. Tablica `jobs` (stanja queued/running/succeeded/failed/cancelled) i `job_schedules`. Posao se preuzima jednim `UPDATE … RETURNING` na jedinoj konekciji za pisanje, pa ga dva radnika ne mogu uzeti istovremeno. Jedinstveni ključ je parcijalni unique indeks (`unique_key` dok je posao queued ili running). Ponovni pokušaji s eksponencijalnim čekanjem (10 s × 2ⁿ, ±20 %, najviše 1 h), `Permanent(err)` preskače ponavljanje, panika je trajna greška. Svaki red (queue) ima svoju paralelnost. Otkazivanje: queued odmah, running preko konteksta. Posao prekinut gašenjem vraća se u red bez trošenja pokušaja; posao ostao u running nakon pada vraća se u red pri idućem pokretanju (ili pada ako je to bio zadnji pokušaj). Ponavljajući zadaci imaju ime i interval; novi zadatak prvi put radi odmah nakon pokretanja, a nakon restarta zadržava zapisano vrijeme idućeg pokretanja. Povijest se čisti nakon 14 dana (zadatak `housekeeping`).
**Posljedice:** samo jedan proces smije voditi red (jedan kontejner, D-001). Novo pozivanje ne treba polling dulji od 1 s jer `Enqueue` budi svoj red odmah.

## D-005 Prijava, uloge i dozvole (2026-11-20)
**Kontekst:** jedna prijava za admina, upravitelja i korisnike koji samo traže sadržaj; web, mobilne aplikacije i skripte.
**Odluka:** uloge `admin`, `manager`, `requester` plus bitset dozvola (`permissions INTEGER`): zahtjev za film/seriju/glazbu, automatsko odobravanje, 4K, upravljanje bibliotekom, upravljanje zahtjevima. Bitovi se nikad ne prenumeriraju. Lozinke argon2id; sesijski tokeni i API ključevi (`lk_…`, puni ili samo za čitanje) su 256-bitni slučajni nizovi spremljeni kao SHA-256. Web koristi HttpOnly `SameSite=Strict` kolačić `locarr_session` (Secure kad je veza HTTPS), aplikacije bearer token. CSRF: SameSite=Strict, samo JSON tijela, bez CORS-a, i dodatno odbijanje zahtjeva koji mijenjaju stanje kad `Sec-Fetch-Site`/`Origin` pokazuju drugo podrijetlo. TOTP (RFC 6238, svaki kod jednom) s 10 jednokratnih kodova za oporavak; tajna šifrirana (D-006). Zaključavanje računa nakon 5 neuspjeha na 15 min; ograničenje po adresi (zadano 10/min) i zabrana na 1 h nakon 15 odbijenih pokušaja u 10 min (IPv6 po /64, lokalne mreže izuzete). Pogrešna trenutna lozinka kod osjetljivih promjena vraća 403 (ne 401), da klijent to ne shvati kao istek sesije. `locarr reset-admin` u kontejneru je put natrag za vlasnika koji se zaključao.
**Posljedice:** prijava preko Streamera (P8) dodat će se kao drugi način prijave na iste račune.

## D-006 Šifriranje tajni vanjskih servisa (2026-11-20)
**Kontekst:** API ključevi indexera, lozinke klijenata i tokeni ne smiju biti čitljivi iz kopije baze ni vraćeni klijentu.
**Odluka:** pri prvom pokretanju generira se 256-bitni ključ u `/config/secret.key` (0600). Tajne se šifriraju AES-256-GCM s nasumičnim nonceom i AAD-om koji veže šifrat uz njegovo mjesto (npr. `setting:metadata.tmdbApiKey`), uz bajt verzije formata. API tajna polja samo prima; čitanjem vraća `{configured: true|false}`.
**Posljedice:** backup mora uključiti `secret.key` uz bazu; bez njega su spremljene tajne izgubljene (ostali podaci ne).

## D-007 Zadani port 8484 (2026-11-20)
**Kontekst:** korisnici često uz Locarr još neko vrijeme drže stare aplikacije (7878, 8989, 8686, 6767, 9696, 5055).
**Odluka:** Locarr sluša na 8484; compose ga zadano objavljuje samo na `127.0.0.1:8484` (javni pristup preko reverse proxyja).
**Posljedice:** može raditi usporedno sa starim stackom tijekom prelaska.

## D-008 Raspored podataka: jedna `/data` montaža (2026-11-20)
**Kontekst:** uvoz treba hardlinkove (bez kopiranja i dvostruke potrošnje diska), a hardlink radi samo unutar istog datotečnog sustava i iste Docker montaže.
**Odluka:** kontejner ima samo `/config` (baza, ključ) i `/data` (mediji i preuzimanja zajedno: `/data/media/{movies,tv,music}`, `/data/downloads/{torrents,usenet}`). Download klijenti moraju vidjeti istu strukturu pod istom putanjom. Putanje iz zahtjeva smiju pokazivati samo unutar `LOCARR_DATA_ROOTS` (zadano `/data`) i prolaze kroz `fs.SafePath` (čišćenje, praćenje simboličkih veza, provjera korijena).
**Posljedice:** zasebne montaže za filmove i preuzimanja rade, ali uvoz tada kopira; dokumentirano u `docs/INSTALL.md`.

## D-009 Kontejner: PUID/PGID bez trajnog roota (2026-11-20)
**Kontekst:** mediji pripadaju korisniku na hostu, a slika mora raditi i na NAS-ovima gdje to nije uid 1000.
**Odluka:** entrypoint (kao root) samo predaje `/config` korisniku `PUID:PGID` ako mu već ne pripada, zatim `setpriv` trajno spušta prava (bez dodatnih grupa, `no_new_privs`). `/data` se nikad ne dira. Compose: `cap_drop: ALL` uz samo CHOWN/SETUID/SETGID, `no-new-privileges`, read-only root filesystem, tmpfs `/tmp`. Pokretanje s `user:` u composeu radi izravno bez roota.
**Posljedice:** ffmpeg i slično dodat će se u sliku tek kad zatreba (P7).
