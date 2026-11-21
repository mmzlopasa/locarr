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

## D-010 Locarr radi u kućnoj mreži (2026-11-21)
**Kontekst:** vlasnik pokreće Locarr lokalno i otvara ga s uređaja u kućnoj mreži preko LAN adrese servera; javni demo nije potreban.
**Odluka:** `docker-compose.yml` zadano objavljuje port 8484 na svim sučeljima (`HTTP_PORT=8484`); `.env` može suziti na LAN adresu ili na `127.0.0.1` uz reverse proxy. Dokumentacija kaže da se port ne otvara na routeru. Razvojna instanca na razvojnom hostu i dalje sluša samo na `127.0.0.1`.
**Posljedice:** prijava, ograničenja i zabrana adresa vrijede i na LAN-u (privatne adrese se ne blokiraju, ali se računi zaključavaju nakon neuspjelih pokušaja); bez HTTPS-a u kućnoj mreži lozinke putuju nešifrirano unutar LAN-a, što je opisano u INSTALL.md.

## D-011 Indexeri daju sirova izdanja kroz jedno Go sučelje (2026-11-21)
**Kontekst:** P1 se radi paralelno s parserom i odlukama (P2); izdanja iz indexera trebaju i P2 (odluka), P3 (slanje klijentu) i P4+ (biblioteka), a parser ne smije biti preduvjet za P1.
**Odluka:** `internal/indexers` vraća `indexers.Release` točno onako kako ga je indexer opisao (naslov, GUID, veličina, seederi/leecheri/peerovi, grabovi, datum, kategorije, info hash, magnet, poveznica na detalje, TVDB/TMDb/IMDb/TVmaze/Rage ID, sezona/epizoda, volume factori, minimalni omjer i vrijeme seedanja, oznake poput `freeleech` i sve sirove atribute u `Attrs`). Potrošači koriste `Service.Search` / `Service.SearchStream` (pretrage s `Query.Source` = interactive/automatic i `Query.ItemRef`), `Service.SubscribeRSS` (kanal novih izdanja iz RSS synca, bez blokiranja: spori pretplatnik gubi seriju) uz `Service.SeenSince` za nadoknadu iz `releases_seen`, te `Service.Fetch` za .torrent/.nzb (broji grab i poštuje dnevni limit). Postavke po indexeru (minimalni seederi, omjer/vrijeme seedanja) čitaju se iz `Service.Get`; P1 ništa ne filtrira.
**Posljedice:** parser i odluke ne ovise o formatu feeda; promjene u `Release` su aditivne.

## D-012 Pretraga u stvarnom vremenu preko Server-Sent Events (2026-11-21)
**Kontekst:** ručna pretraga mora pokazati rezultate čim koji indexer odgovori, a API je OpenAPI-first (ogen).
**Odluka:** `GET /api/v1/search/stream` vraća `text/event-stream` s istim parametrima kao `GET /api/v1/search` (JSON koji čeka sve). Događaji: `indexer` (`SearchStreamIndexerEvent`: stanje jednog indexera i njegova još neposlana izdanja) i `done` (`SearchStreamDone`), nakon čega tok završava. SSE (a ne chunked JSON) jer ga preglednici čitaju i preko `EventSource` uz kolačić sesije, radi kroz reverse proxyje (`X-Accel-Buffering: no`) i ima standardni format; web ga čita preko `fetch` radi statusa greške i prekida. U specifikaciji je to binarni odgovor; ruter flusha svaki zapis te jedne putanje. Dupli se uklanjaju po info hashu, inače po GUID-u: u toku pobjeđuje prvi koji stigne, u JSON odgovoru indexer većeg prioriteta. Odgovor svakog indexera na identičan zahtjev pamti se kratko (zadano 120 s). Svaka pretraga (i RSS sync) zapisuje se u `search_log` + `search_log_indexers` sa stabilnom shemom i opcionalnim `item_ref` za kasniju vremensku crtu.
**Posljedice:** cijela pretraga ima gornju granicu (zadano 45 s, najviše 55 s zbog 60-sekundnog ograničenja zahtjeva).

## D-013 Odlazni zahtjevi prema indexerima: politika adresa (2026-11-21)
**Kontekst:** URL indexera upisuje administrator, ali poslužitelj ne smije postati alat za dohvat unutarnjih adresa (SSRF), a Prowlarr/Jackett/FlareSolverr obično su na istoj Docker mreži.
**Odluka:** `internal/indexers/httpx` provjerava svaku razriješenu adresu u trenutku spajanja (otporno na DNS rebinding): zadano blokira loopback, link-local (169.254/16, fe80::/10), metadata adrese oblaka, 0.0.0.0/8, multicast; privatne mreže (RFC 1918, ULA, CGNAT) i Docker DNS imena su dopušteni. Postavka `allowedHosts` (adrese, mreže ili imena) otključava blokirano, a `blockPrivateNetworks` blokira i privatne mreže. Uz to: timeout po indexeru, ograničenje veličine odgovora (16 MiB i nakon gunzipa), preusmjerenja samo na isti host, XML bez proširivanja entiteta, API ključ i lozinke se uklanjaju iz svake poruke greške i loga. Proxy po indexeru (HTTP ili SOCKS5) prolazi istu provjeru; odredište iza proxyja razrješava proxy. FlareSolverr se koristi samo kad odgovor izgleda kao Cloudflare provjera, i to samo za kolačiće.
**Posljedice:** indexer na `localhost` (npr. Prowlarr na hostu) treba izričito dopustiti; poruka greške to kaže.

## D-014 Zdravlje i ograničenja indexera (2026-11-21)
**Kontekst:** indexeri padaju, ograničavaju zahtjeve i zamjeraju previše pretraga.
**Odluka:** svaka greška koja govori nešto o indexeru (ne nepodržan parametar ni naš prekid) povećava broj neuspjeha i stavlja indexer na čekanje: 1 min, zatim 5 min udvostručeno do 12 h (ili dulje ako indexer pošalje Retry-After). Indexer na čekanju se preskače i nakon isteka se automatski opet pokušava; uspjeh briše neuspjehe. Ograničenje „N zahtjeva u razdoblju" vodi se u memoriji (jedan proces, D-001) i ne računa se kao kvar; dnevni limit grabova vodi se u `indexer_grabs`.
**Posljedice:** nakon restarta brojač zahtjeva kreće od nule; čekanje i greške ostaju u bazi.

## D-015 Prowlarr i Jackett kao izvori (2026-11-21)
**Kontekst:** korisnici imaju desetke indexera već postavljenih u Prowlarru ili Jackettu.
**Odluka:** iz dokumentiranih javnih krajnjih točaka čitamo samo popis: Prowlarr `GET /api/v1/indexer` (`X-Api-Key`), Jackett `…/indexers/all/results/torznab/api?t=indexers&configured=true`. Svaki indexer dodaje se kao Torznab/Newznab indexer na proxirani feed (Prowlarr `/{id}/api`, Jackett `/api/v2.0/indexers/{id}/results/torznab/api`) s ključem izvora. Izvor se sprema (ključ šifriran), uvoz ima pregled, a ponovni sync dodaje nove, ažurira promijenjene i isključuje (nikad ne briše) one kojih više nema. Brisanje izvora ostavlja indexere. Ponašanje je provjereno samo prema lažnim izvorima u testovima.
**Posljedice:** ako Jackett zahtijeva admin lozinku za popis, uvoz traži da se indexeri dodaju ručno; P10 može dodati i uvoz ostalih Prowlarr postavki.

## D-016 Poveznice za preuzimanje nikad ne nose ključ do klijenta (2026-11-21)
**Kontekst:** Newznab/Torznab poveznice za .nzb/.torrent sadrže API ključ indexera, a ključevi se nikad ne vraćaju klijentu (D-006).
**Odluka:** pri parsiranju se ključ u poveznicama zamjenjuje oznakom (i takve se spremaju u `releases_seen`); web dobiva `downloadLink` na `/api/v1/indexers/{id}/download?token=…`, gdje je token šifriran (AES-GCM, 24 h) i veže poveznicu uz indexer. Poslužitelj dohvaća datoteku samo s hosta indexera i ključem iz baze. Poveznice i magneti vide samo administratori i upravitelji.
**Posljedice:** passkeyevi privatnih trackera u magnetima ostaju vidljivi upraviteljima (magnet je za klijenta).
