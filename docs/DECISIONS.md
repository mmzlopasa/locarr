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

## D-017 Download klijenti iza jednog sučelja, provjereni istim ugovorom (2026-11-21)
**Kontekst:** sedam klijenata (qBittorrent, Transmission, Deluge, rTorrent, aria2, SABnzbd, NZBGet) imaju različite API-je, stanja i mogućnosti, a ostatak Locarra (praćenje, uvoz) ne smije ovisiti o tome koji je klijent iza.
**Odluka:** `internal/downloaders` definira `Client` (Test, Add, List, Files, Remove, Pause, Resume, SetPriority, Categories, EnsureCategory) i `Capabilities` (kategorije, stvaranje kategorija, kategorije kao podmape, oznake, ciljevi seedanja, redoslijed, prvi/zadnji komad, slobodan prostor, brisanje podataka). Stanja se svode na sedam (queued, downloading, paused, seeding, completed, failed, warning), a izvorno stanje klijenta čuva se uz njih; `Busy` znači da klijent još provjerava, premješta ili raspakirava. ID stavke je info hash (mala slova) za torrente, `nzo_id` / `NZBID` za usenet. Svaki klijent je zaseban paket napisan samo iz javne dokumentacije. Jedan zajednički skup testova (`contracttest`) prolazi protiv stvarnih klijenata u Dockeru (`scripts/test-clients.sh`, oznaka `clients`): torrent test dodaje magnet i .torrent za podatke koje test sam stavi u mapu klijenta, pa klijent provjeri i seeda bez mreže; usenet test (bez news servera) provjerava dodavanje, pauzu, prioritet i brisanje. rTorrent se testira na slici `crazymax/rtorrent-rutorrent` (XML-RPC na `/RPC2`). CI pokreće qBittorrent, Transmission i SABnzbd.
**Posljedice:** klijent koji ne zna obrisati podatke (rTorrent, aria2, NZBGet za završene) javlja `DeletesData=false`, a Locarr tada sam briše sadržaj, samo unutar `LOCARR_DATA_ROOTS`, nikad sam korijen ni mapu biblioteke, i samo na izričit zahtjev korisnika ili uz uključeno „ukloni nakon uvoza". Deluge kategorije traže Label plugin; NZBGet kategorije se ne mogu stvoriti kroz API (samo u njegovoj konfiguraciji).

## D-018 Praćenje preuzimanja je vlastita petlja, ne posao u redu (2026-11-21)
**Kontekst:** klijente treba pitati svakih 30 s, a dok je otvoren red preuzimanja i svakih nekoliko sekundi; posao u redu (D-004) za svako ispitivanje stvarao bi tisuće redaka povijesti dnevno.
**Odluka:** `downloads.Service.Run` je petlja u procesu: interval iz postavke `downloads.pollSeconds` (zadano 30 s, 10–3600), 5 s dok netko gleda red (svaki `GET /queue` produži to na 45 s), odmah nakon grabanja ili ručnog osvježavanja. Klijenti se ispituju paralelno s timeoutom od 30 s; `Test` (verzija, slobodan prostor) svakih 5 min ili nakon greške. Rezultat zadnjeg ispitivanja (stanje veze, greška, nepoznate stavke) drži se u memoriji.
**Posljedice:** ispitivanje nije vidljivo u Sustav → Zadaci; stanje klijenata vidi se na stranici Download klijenti i u redu. Nakon restarta prvo ispitivanje je odmah.

## D-019 Životni ciklus preuzimanja i događaji (2026-11-21)
**Kontekst:** uvoz (P4–P6) i vremenska crta (P11) trebaju znati kada je preuzimanje stvarno spremno, kada nije uspjelo i što se s njim događalo.
**Odluka:** tablica `downloads` veže izdanje (naslov, indexer, protokol, GUID, hash, kategorija, vrsta sadržaja, neprozirni JSON `item_ref` za uvoznike) uz stavku u klijentu, sa statusom `active → completed → imported` ili `failed` / `removed`. „Spremno za uvoz" znači: klijent kaže da je gotovo (seeding/completed, napredak 1), nije `Busy`, i putanja sadržaja nakon mapiranja udaljenih putanja postoji lokalno unutar `LOCARR_DATA_ROOTS` (`fs.SafePath`); inače stavka ostaje aktivna s porukom kako to popraviti. Neuspjeh: klijent javlja grešku ili nedostaju datoteke, ili torrent bez seedera i bez napretka dulje od `downloads.stalledHours` (zadano 24 h, 0 = nikad). Stavka koja nestane iz klijenta u dva uzastopna ispitivanja je uklonjena. Svaka promjena upisuje se u `download_events` (stabilni stupci: type grabbed/state/completed/failed/removed/imported/import_failed/blocklisted, from_state, to_state, message, data), a nakon zapisa objavljuje se na sabirnici u procesu (`downloads.Bus`: Grabbed, Completed, Failed, Removed, Imported). Uvoznik javlja `MarkImported` / `MarkImportFailed`; uz „ukloni nakon uvoza" Locarr uklanja torrent (s podacima) tek kad su ciljevi seedanja ispunjeni (omjer/vrijeme s indexera, ili klijent sam stane), usenet odmah. Stavke u Locarrovim kategorijama koje Locarr nije dodao prikazuju se kao „nepoznate" i nikad se ne diraju.
**Posljedice:** pretplatnici na sabirnici moraju biti brzi (npr. samo staviti posao u red). Događaji se ne gube pri restartu jer stanje je u bazi; sabirnica je samo obavijest.

## D-020 Grab: datoteka ide kroz poslužitelj, klijent se bira po protokolu, oznakama i prioritetu (2026-11-21)
**Kontekst:** poveznice indexera nose ključeve, a download klijent često ne smije ili ne može sam doći do indexera; magnet poveznice nemaju datoteku.
**Odluka:** `POST /queue/grab` (i `downloads.Service.Grab` za P4) uzima poveznicu ili token rezultata pretrage. Uz `indexerId` datoteka se preuzima kroz `indexers.Service` (ključ, dnevni limit, D-016) i od indexera se uzimaju oznake i ciljevi seedanja ako ih zahtjev ne daje; ostale http(s) poveznice preuzima Locarrov klijent (timeout 60 s, najviše 10 MiB za .torrent i 64 MiB za .nzb, bez preusmjerenja na drugi host osim na magnet). Sadržaj se provjerava (.torrent se parsira i iz njega računa info hash, NZB mora biti NZB). Klijent: uključen, istog protokola; klijent s oznakama prima samo izdanja s barem jednom istom oznakom, klijent bez oznaka prima sve; najmanji prioritet prvi, a klijenti istog prioriteta izmjenjuju se; ako klijent odbije, pokušava se sljedeći. Kategorija ovisi o vrsti sadržaja, a nedostajuća se stvara automatski kad klijent to podržava. Isto izdanje (isti hash na istom klijentu) ne šalje se dvaput.
**Posljedice:** veze prema download klijentima idu kroz vlastiti klijent u `downloaders` s politikom adresa: lokalna mreža, Docker i loopback dopušteni (tamo klijenti žive), link-local, multicast i adrese metapodataka u oblaku nikad; provjera je na stvarnoj adresi pri spajanju. TLS se provjerava, osim ako je za taj klijent izričito uključeno „dopusti self-signed".

## D-021 Blokirana izdanja (2026-11-21)
**Kontekst:** izdanje koje nije uspjelo ili ga je korisnik odbio ne smije se ponovno grabati, a odluka o izdanju (P2) prolazi tisuće kandidata.
**Odluka:** tablica `blocklist` (naslov, protokol, indexer, info hash, GUID, razlog, poruka, `item_ref`, preuzimanje). Kandidat je blokiran ako se poklapa hash, ili GUID, ili naslov (bez razlike velikih slova i razmaka) s istog indexera (ili istog protokola kad indexer nije poznat). Provjera ide iz memorije (`BlocklistChecker.IsBlocklisted`), indeks se puni iz baze pri prvoj upotrebi i nakon svake promjene. Akcija „ukloni i blokiraj" u redu radi oboje odjednom.
**Posljedice:** brisanje zapisa iz blokiranih odmah dopušta ponovni grab.

## D-022 Parser imena izdanja: tokeni, rječnici i nekoliko čuvanih regexa (2026-11-21)
**Kontekst:** parser se poziva za svako izdanje iz RSS-a i pretrage (cilj: 1000 imena < 50 ms) i mora biti objašnjiv; clean-room (D-002) isključuje tuđe parsere.
**Odluka:** ime se rastavlja na tokene (točka, razmak, podvlaka, zagrade; točka ostaje u „5.1", „H.264", „44.1kHz"), sezone/epizode/datumi/anime brojevi traže se nekolicinom regexa koji se pokreću samo kad brzi pregled znakova pokaže da ima smisla, a sve ostalo su rječnici (pojedinačne riječi i fraze). Naslov završava na prvoj „jakoj" oznaci (rezolucija, izvor, kodek, epizoda…) ili godini prije nje (zadnja godina koja nije prvi token, pa „1917.2019" i „Blade.Runner.2049.2017" rade); višeznačne riječi (Web, Cam, TS, Opus, DV…) računaju se samo velikim slovima ili iza naslova; jezici, verzije i zastavice na kraju naslova se odrežu. Nepoznati dijelovi idu u `unparsed` i smanjuju `confidence`. Ime bez oznake jezika smatra se engleskim. Golden testovi: 1381 ime (generirani skupovi s poznatom istinom iz `testdata/generate.go` + ručno pisani rubni slučajevi), fuzz i test brzine (~35 ms za 1000 imena).
**Posljedice:** novo pravilo = redak u rječniku + golden slučaj. Ime bez izvora a s rezolucijom pogađa se kao HDTV (epizode) ili WEB-DL (ostalo), uz manju pouzdanost.

## D-023 Fiksna ljestvica kvaliteta, veličine u MB po minuti (2026-11-21)
**Kontekst:** profili, custom formati i odluke trebaju stabilne ID-jeve kvaliteta; korisnici misle u GB za film ili epizodu.
**Odluka:** ljestvica je u kodu (`internal/quality`, ID-jevi poput `bluray-1080p`, `remux-2160p`, `flac-24`), video i glazba odvojeno. Definicije veličina (najmanje/željeno/najviše, MB po minuti; 0 = bez granice/bez želje) su po vrsti sadržaja (filmovi, serije, glazba), spremaju se u bazu i vraćaju na zadano. Sezonski paket množi trajanje brojem traženih epizoda te sezone.
**Posljedice:** nova kvaliteta je promjena koda i nova migracija nije potrebna (nedostajuće definicije dodaju se pri pokretanju).

## D-024 Jezici unutar profila kvalitete, ne zasebni jezični profili (2026-11-21)
**Kontekst:** brief dopušta oba; zasebni jezični profili za audio su još jedan izbornik koji se uvijek veže 1:1 s profilom kvalitete.
**Odluka:** profil kvalitete ima popis prihvaćenih jezika (`original`, ISO kodovi; prazno = svi). Multi/dual audio izdanja prolaze za „izvorni jezik". Jezični profili za titlove ostaju za P7.
**Posljedice:** `/api/v1/languages` daje katalog; nema `/languageprofiles`.

## D-025 Custom formati: semantika uvjeta i dijeljeni JSON (2026-11-21)
**Kontekst:** korisnici lijepe formate iz javnih vodiča; regexi u njima koriste lookaround.
**Odluka:** format vrijedi kad za svaku vrstu uvjeta prođu svi obavezni i (ako ih ima) barem jedan neobavezni; negacija obrće pojedini uvjet. Regexi idu kroz .NET-kompatibilan `regexp2` s timeoutom 50 ms. Uvoz prima objekt ili niz, `fields` kao objekt ili stari oblik liste, brojčane vrijednosti prevodi prema „okusu" (numeracija izvora alata za filmove 0–9 ili za serije 0–7); pohranjujemo imena (`webdl`, `remux`, `hr`). Nepoznata implementacija se čuva i prijavljuje: neobavezna se zanemaruje, obavezna sprječava da format ikad vrijedi. Uz javne vrste imamo vlastite: video kodek, HDR, audio, streaming servis.
**Posljedice:** izvoz vraća brojeve u odabranom okusu, pa se formati mogu vratiti u druge alate.

## D-026 Odluka: svi uvjeti uvijek, privremena odbijanja, deterministički poredak (2026-11-21)
**Kontekst:** „zašto nije stiglo" traži svaki razlog, ne samo prvi; P4/P11 spremaju rezultat.
**Odluka:** `decision.Evaluate(ctx, Context, []Candidate) Report` pokreće sve provjere na svakom kandidatu; svako odbijanje ima stabilan kod, englesku poruku s vrijednostima i `params` (web ih prevodi). Odbijanja koja mogu proći kasnije (kašnjenje, minimalna starost, limit indexera) su `temporary` → status `pending`. Poredak prihvaćenih: položaj kvalitete u profilu, bodovi custom formata, revizija, preferirani protokol, prioritet indexera, seederi, blizina željenoj veličini, noviji, pa naziv. `decision.Candidate` je naš tip; P4 piše tanki adapter iz `indexers.Release`, a blocklist je sučelje `Blocked(ctx, *Candidate, *parser.Release)` koje P4 spaja na `downloads.BlocklistChecker`. Interaktivna pretraga ne čeka na profile kašnjenja.
**Posljedice:** oblik `DecisionReport` je u specifikaciji; motor ništa ne sprema.

## D-027 Oznake (tagovi) u profilima kao tekst (2026-11-21)
**Kontekst:** profili izdanja i kašnjenja vežu se uz oznake naslova, a naslovi stižu u P4/P5.
**Odluka:** oznake su mala slova, bez razmaka na rubovima; profil kašnjenja bez oznaka postoji samo jedan (zadani, uvijek zadnji, ne briše se), ostali se provjeravaju redom i prvi koji odgovara vrijedi.
**Posljedice:** kad uvedemo tablicu oznaka, migracija preslikava tekst u ID-jeve.

## D-028 Zajednička jezgra biblioteke i oznaka stavke (2026-11-21)
**Kontekst:** filmovi (P4), serije (P5) i glazba (P6) dijele praćenje, profile, mape, uvoz, vremensku crtu i pretrage; preuzimanje (P3) nosi neprozirni `item_ref`.
**Odluka:** `media_items` drži ono što imaju sve vrste (naslov, godina, praćenje, profil kvalitete, root mapa i putanja, oznake kao tekst po D-027), a svaka vrsta ima svoju tablicu uz isti ID (`movies`; P5 `series`/`episodes`, P6 `artists`/`albums`). Datoteke su u `media_files` (film ima najviše jednu; epizode i pjesme vežu datoteke svojim tablicama). Oznaka stavke je tekst `vrsta:id` (`movie:42`; rezervirano `series`, `episode`, `artist`, `album`) i ista je u `search_log.item_ref`, `timeline_events.item_ref`, `search_state` i `pending_releases`; preuzimanje je nosi kao JSON `{"ref":"movie:42"}` uz indeks na `json_extract(item_ref, '$.ref')` (`library.Ref`, `ParseRef`, `JSON`, `RefFromJSON`).
**Posljedice:** P5/P6 dodaju samo svoje tablice i izvor za pretrage; vremenska crta, objašnjenje, koš i uvoznik rade bez promjena.

## D-029 Metapodaci: TMDb iza sučelja, predmemorija i ograničenje (2026-11-21)
**Kontekst:** filmovi trebaju naslove, datume i plakate; TMDb traži vlastiti ključ i ograničava broj zahtjeva; CSP dopušta slike samo s vlastitog poslužitelja.
**Odluka:** `internal/metadata` definira `MovieProvider` (pretraga, film, IMDb → TMDb, kolekcija, javni popis, provjera ključa) i `ImageSource`; `metadata/tmdb` ga ostvaruje iz javne dokumentacije v3 API-ja (v3 ključ kao `api_key`, v4 token kao bearer), s timeoutom 15 s, odgovorom najviše 4 MiB, preusmjerenjima samo na isti host i ključem uklonjenim iz grešaka. Odgovori se spremaju u `metadata_cache` (pretraga 1 h, film 12 h, kolekcija 24 h); osvježavanje ih zaobilazi. Ograničenje je 20 zahtjeva u sekundi uz poštivanje `Retry-After` kod 429. Jezik i regija su postavke (regija bira datume izdanja i dobnu oznaku). Ključ se prije spremanja provjerava kod TMDb-a (`metadata_key_invalid`) i sprema šifriran (D-006). Slike se preuzimaju jednom u `/config/cache/images` i poslužuju kroz `GET /api/v1/images/tmdb/{size}/{file}` s ETagom i 304. Adresa TMDb-a mijenja se samo varijablom `LOCARR_TMDB_URL` (testovi i lažni servisi), nikad kroz API.
**Posljedice:** bez ključa dodavanje filmova javlja `metadata_key_missing`; P5 dodaje `SeriesProvider`, P6 MusicBrainz istim obrascem.

## D-030 Vremenska crta i „zašto nije stiglo" (2026-11-21)
**Kontekst:** PLAN §2: jedan odgovor na jednom ekranu.
**Odluka:** `timeline_events` drži vlastite događaje (dodano, promijenjeno, osvježeno, odluka sa sažetkom i 10 najboljih odbijenih izdanja s razlozima, zadržano, neuspio grab, ponovni pokušaj, uvezeno, pronađeno, preimenovano, nadograđeno, u košu, obrisano, obavijest), a pretrage (s brojem rezultata po indexeru) i koraci preuzimanja (`download_*`, uključujući 25/50/75 %) čitaju se iz `search_log` i `download_events`, pa se ništa ne piše dvaput. `timeline.Explain` je čista funkcija nad stanjem stavke i činjenicama iz baze, s pravilima ovim redom: u biblioteci i granica dosegnuta → preuzima se → uvoz nije uspio → čeka uvoz → ne prati se → nije dostupno → nema indexera → nema klijenta → zadržano profilom kašnjenja → zadnje preuzimanje nije uspjelo → nije traženo → sve odbijeno (najčešći razlozi) → želi se nadogradnja → ništa nađeno. Web prevodi kod, poruka na engleskom je rezerva.
**Posljedice:** RSS odluke bez prihvaćenih ili zadržanih izdanja ne pišu se na vremensku crtu (inače bi ih bilo tisuće).

## D-031 Dostupnost filma (2026-11-21)
**Kontekst:** „najavljeno / u kinima / izdano" treba datume, a TMDb ih nema uvijek.
**Odluka:** najavljeno je dostupno odmah; u kinima od prvog kino datuma (ili ranijeg digitalnog/fizičkog); izdano od prvog digitalnog ili fizičkog datuma, a bez njih 90 dana nakon kina. Film bez ikakvog datuma kojeg TMDb vodi kao „Released" je dostupan. Postavka „odgoda dostupnosti" (dani, može biti negativna) pomiče sve datume. Datumi su iz regije iz postavki, inače najraniji bilo gdje.
**Posljedice:** 90 dana je procjena; objašnjenje pokazuje datum od kojeg se traži.

## D-032 Orkestracija pretraga (2026-11-21)
**Kontekst:** automatsko traženje mora biti uporno, ali ne smije zatrpati indexere.
**Odluka:** `internal/wanted` radi s „izvorima" po vrsti (`Source`: stavka, popis željenih, uparivač za RSS). Pretraga nakon dodavanja ide kroz red poslova; raspored (zadano svakih 15 min) pretražuje najviše 10 stavki čija je sljedeća pretraga došla, najprije nikad tražene. Nakon pretrage bez grabanja čeka se 30 min, pa 1 h, 2 h… najviše 24 h; nakon grabanja 24 h (nadogradnje). RSS izdanja uparuju se s praćenim filmovima po TMDb/IMDb ID-u, inače po naslovu (i alternativnim naslovima) i godini ±1. Odluka ide kroz `decision.Evaluate`; grab najboljeg prihvaćenog, a ako klijent ili indexer odbije, sljedećeg. Privremeno odbijena izdanja (kašnjenje) spremaju se u `pending_releases` i ponovno procjenjuju svakih 5 min (do 7 dana). Neuspjelo preuzimanje se blokira s razlogom i stavka se odmah ponovno traži (postavka). Stavka s aktivnim preuzimanjem se ne traži.
**Posljedice:** ručna „Traži odmah" radi i za nepraćene i nedostupne filmove; ručni grab iz interaktivne pretrage može uzeti i odbijeno izdanje.

## D-033 Podaci o zapisima iz imena, bez ffprobea (2026-11-21)
**Kontekst:** ffprobe u Debian slici dodaje ~100 MB (FFmpeg biblioteke), a slika je danas ~40 MB.
**Odluka:** rezolucija, kodek, HDR, zvuk i jezici datoteke čitaju se iz imena izdanja i datoteke (parser, D-022); `media_info.source = "name"`, a web to kaže.
**Posljedice:** zamaskirana imena nemaju podatke o zapisima; ffprobe se može dodati uz titlove (P7) ako tada zatreba FFmpeg.

## D-034 Uvoz (2026-11-21)
**Kontekst:** uvoz ne smije izgubiti ni jednu datoteku, a seedanje mora nastaviti.
**Odluka:** zajednički `internal/library/importer` nalazi video datoteke (najveća je glavna; uzorci po imenu ili mapi Sample, dodaci u mapama Featurettes/Extras/…, i datoteke manje od 30 % glavne se preskaču; strukture diska i slike se ne uvoze automatski), uz video nosi titlove po popisu ekstenzija (zadržava jezične oznake, npr. `.hr.srt`). Prijenos (`library/fsops`): hardlink zadano, kopija kad hardlink nije moguć (drugi datotečni sustav), premještanje samo ako je korisnik to odabrao; kopija ide u privremenu datoteku i preimenuje se tek nakon provjere veličine; izvor se nikad ne briše prije provjere. Nova datoteka stiže pokraj stare (`.locarr-new`), stara ide u koš, nova dobiva konačno ime. Nadogradnja mora biti bolja po položaju u profilu, reviziji ili bodovima. Sve putanje prolaze `fs.SafePath`. Na kraju `MarkImported` ili `MarkImportFailed` s razlogom. `mediaserver.Notifier` (zadano bez učinka) zove se nakon uvoza, nadogradnje i preimenovanja.
**Posljedice:** višedijelni filmovi (CD1/CD2) prepoznaju se, ali se uvozi samo glavni dio; P5/P6 koriste isti `Discover`/`Place` s vlastitim uparivanjem.

## D-035 Predlošci imena (2026-11-21)
**Kontekst:** korisnici žele imena po vlastitom ukusu; datoteke se često čitaju s Windowsa preko dijeljenja.
**Odluka:** vlastita sintaksa `{Oznaka}`, a znakovi oko imena unutar zagrada (`{[Edition]}`, `{-ReleaseGroup}`) pišu se samo kad oznaka ima vrijednost; prazne zagrade i dvostruki razmaci se uklanjaju. Imena se uvijek čiste za Windows: bez `/ \ : * ? " < > |` i kontrolnih znakova (dvotočka po postavci), bez točke ili razmaka na kraju, bez rezerviranih imena (CON, NUL…), najviše 255 bajtova uz očuvanu ekstenziju. Predložak se provjerava pri spremanju; pregled uživo koristi primjer filma.
**Posljedice:** preimenovanje postojeće biblioteke je pregled pa primjena, unutar iste mape, nikad preko postojeće datoteke.

## D-036 Koš za smeće (2026-11-21)
**Kontekst:** pravilo „nikad ne briši korisnikove datoteke" (CONTRIBUTING).
**Odluka:** zamijenjene datoteke i mape obrisanih filmova idu u koš (zadano `recycle-bin` u prvoj mapi s podacima, mijenja se samo unutar `LOCARR_DATA_ROOTS`), svaka u vlastitu podmapu s vremenom; zapis u `recycle_bin` omogućuje vraćanje na izvorno mjesto (ako je slobodno). Brisanje pojedinačno i pražnjenje su radnje administratora; dnevni posao briše stavke starije od roka (zadano 7 dana, 0 = nikad). Brisanje se izvodi samo nad putanjama unutar mape koša.
**Posljedice:** koš na drugom datotečnom sustavu znači kopiju pri premještanju; zato je zadano unutar /data.

## D-037 Kalendar, popisi i isključenja (2026-11-21)
**Kontekst:** kalendar se čita iz aplikacija koje ne znaju za prijavu; popisi dodaju filmove sami.
**Odluka:** iCal feed (`/calendar/feed.ics?token=…`) koristi osobni token po korisniku spremljen kao SHA-256 (prikazan jednom, nova poveznica gasi staru, isključeni korisnik gubi feed). Popisi za uvoz su za sada TMDb javni popisi i kolekcije (sync svakih 6 h); film obrisan uz „ne dodaj ponovno" ide u `import_exclusions`. Mapa filma može se promijeniti samo dok film nema datoteku.
**Posljedice:** Trakt i IMDb popisi dolaze kasnije, istim sučeljem.
