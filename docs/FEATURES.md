# Značajke

Oznake: `[ ]` nije gotovo, `[x]` radi end-to-end i testirano. Faza u zagradi.

## Temelji (P0)
- [ ] Prvo pokretanje: postavljanje admin računa, jezik (EN/HR), mapa za medije
- [x] Korisnici i uloge (admin, upravitelj, korisnik koji samo traži), sesije, odjava s uređaja
- [x] API ključevi (puni i samo za čitanje), 2FA (TOTP + kodovi za oporavak)
- [x] Zaštita prijave: ograničenje po adresi, zaključavanje računa, zabrana nakon ponovljenih neuspjeha
- [x] Postavke na jednom mjestu, šifrirane tajne vanjskih servisa (nikad se ne vraćaju klijentu)
- [x] Jedan Docker image (amd64 + arm64), `docker-compose.yml` s jednim servisom, healthcheck, PUID/PGID
- [ ] Backup i vraćanje (automatski i ručno), migracije baze pri pokretanju

## Indexeri (P1)
- [x] Torznab i Newznab: caps, kategorije, pretraga po ID-u (TMDb/TVDB/IMDb/TVmaze) i tekstu, RSS (glazba po izvođaču/albumu: Newznab nema MusicBrainz parametar)
- [x] Prowlarr i Jackett kao izvor (uvoz svih njihovih indexera kao Torznab/Newznab)
- [x] Prioriteti, oznake (tagovi), ograničenja zahtjeva (rate limit), dnevni limiti, backoff i zdravlje
- [x] Paralelna pretraga svih indexera, rezultati čim stignu, ručna (interaktivna) pretraga
- [ ] Proxyji po indexeru (HTTP/SOCKS5), FlareSolverr po izboru (ugrađeno; FlareSolverr testiran s lažnim servisom, proxyji još nisu testirani end-to-end)

## Parser i odluke (P2)
- [x] Parser imena izdanja: naslov, godina, sezona/epizoda (SxxEyy, višestruke, sezonski paketi, apsolutno, dnevno), kvaliteta, izvor, rezolucija, kodek, HDR/DV, audio, jezici, grupa, proper/repack, verzija
- [x] Profili kvalitete (redoslijed, granica nadogradnje), definicije veličina
- [x] Custom formati s bodovima (uvoz/izvoz dijeljenog JSON-a), jezici u profilima, obavezni/zabranjeni izrazi, profili kašnjenja
- [x] Provjera izdanja: sva pročitana polja, custom formati i odluka s razlozima za odabrani profil
- [x] Odluka sa zapisanim razlogom za svako odbijeno izdanje (zapis u vremensku crtu filma od P4)
- [x] Blokirana izdanja, ponovni pokušaj s idućim najboljim izdanjem

## Download klijenti (P3)
- [x] qBittorrent, Transmission, Deluge, rTorrent, aria2 (torrent)
- [x] SABnzbd, NZBGet (usenet)
- [x] Kategorije/oznake po vrsti sadržaja, prioriteti, praćenje napretka i brzine
- [ ] Ciljevi seedanja (omjer, vrijeme) i uklanjanje nakon uvoza po izboru — ciljevi se šalju klijentu i prate; uvoz (P4) javlja „uvezeno", a uklanjanje nakon ispunjenih ciljeva provjereno je samo lažnim klijentom
- [x] Mapiranje udaljenih putanja (klijent na drugom stroju/kontejneru)
- [x] Red preuzimanja uživo (pauza, nastavak, prioritet, uklanjanje s podacima, uklanjanje i blokiranje), nepoznate stavke se ne diraju
- [x] Grab iz pretrage kroz poslužitelj (ključ indexera ostaje na poslužitelju), izbor klijenta po protokolu, oznakama i prioritetu
- [x] Blokirana izdanja: popis, ručno dodavanje i brisanje, provjera za odluke (P2)

## Filmovi (P4)
- [x] TMDb pretraga (naslov, TMDb ID, IMDb ID) i dodavanje, kolekcije (dodaj filmove koji nedostaju), TMDb popisi i kolekcije za automatsko dodavanje, isključenja
- [ ] Trakt i IMDb popisi za automatsko dodavanje
- [x] Praćenje, minimalna dostupnost (najavljeno, u kinima, izdano) s odgodom
- [x] RSS sync, automatska (nakon dodavanja i po rasporedu s odmakom) i ručna pretraga s razlozima, profili kašnjenja, ponovni pokušaj nakon neuspjeha, nadogradnje do granice profila
- [x] Uvoz: hardlink (zadano kad je moguće), kopija, premještanje; provjera prije brisanja izvora; titlovi uz video; ručni uvoz s pregledom
- [x] Preimenovanje datoteka i mapa po predlošku (pregled uživo), preimenovanje postojeće biblioteke, uvoz postojeće biblioteke
- [ ] NFO i slike uz film po izboru
- [x] Koš za smeće (Recycle bin) s rokom, vraćanje, nikad tiho brisanje
- [x] Kalendar filmova (mjesec, tjedan, popis) i iCal feed s osobnom poveznicom
- [ ] Podaci o zapisima iz same datoteke (ffprobe); danas iz imena (D-033)

## Serije (P5)
- [x] TMDb (i TVDB po izboru): serije, sezone, epizode, specijali, vrijeme emitiranja u zoni mreže, osvježavanje s prenumeriranjem (D-046, D-050, D-053)
- [x] Vrste: standardne, dnevne, anime (apsolutna numeracija, ručno mapiranje scenske numeracije; javni izvor mapiranja nije uključen, D-047)
- [x] Sezonski paketi, višestruke epizode, praćenje novih sezona, mogućnosti praćenja pri dodavanju (D-049, D-051)
- [x] Uvoz epizoda (sezonski paketi, djelomični paketi, više epizoda u datoteci, mape sezona i specijala, nadogradnje u koš), uvoz postojeće biblioteke, ručni uvoz, preimenovanje (D-048, D-052)
- [x] Kalendar (web + iCal feed), nadolazeće epizode
- [ ] Slike serija dodanih s TVDB-a (samo TMDb slike se prikazuju, D-053)

## Glazba (P6)
- [x] MusicBrainz izvođači (pretraga s pojašnjenjem), albumi, izdanja i pjesme, profili metapodataka (glavne i dodatne vrste, statusi izdanja), praćenje novih izdanja (svi / budući / ništa), osvježavanje koje ne briše datoteke
- [x] Pretraga albuma (automatska, po rasporedu, RSS, ručna s odlukom), profili kvalitete „Lossless" i „Lossy", nadogradnje do granice
- [x] Uvoz s prepoznavanjem po tagovima, brojevima, naslovima i trajanju (bodovi, više diskova, izbor izdanja), djelomični albumi po izboru, preimenovanje, omoti (iz preuzimanja ili Cover Art Archive), zamjena u koš
- [x] Uvoz postojeće glazbene biblioteke (datoteke ostaju na mjestu), kalendar izlazaka albuma, nedostajući albumi
- [ ] Cue slike (jedna datoteka po albumu) i pisanje tagova osim FLAC-a (D-044)

## Titlovi (P7)
- [x] Jezični profili (redoslijed jezika, forsirani, za gluhe, zapisi u videu, granica), zadani po vrsti, po filmu/seriji i za cijelu biblioteku (D-065)
- [x] Provideri: OpenSubtitles.com (hash, ID-evi, prijava, kvota), SubDL, Jimaku; zdravlje, čekanje, kvote; ocjena podudaranja po izdanju (D-063, D-064) — provjereno s lažnim providerima napravljenim iz javne dokumentacije
- [x] Automatska pretraga nakon uvoza, ponovna pretraga za nedostajuće raspoređena kroz dan, nadogradnje, ručni izbor, odbacivanje, skeniranje postojećih titlova
- [x] Sinkronizacija titla po zvuku (vlastita, uz mali ffmpeg u slici), prema ugrađenom ili drugom titlu, pomak, broj sličica, pretvorba u UTF-8 i SRT (D-062, D-066)
- [ ] Provideri bez javnog API-ja (Podnapisi, Addic7ed, Titlovi.com) — nisu uključeni (D-063)

## Zahtjevi (P8)
- [x] Otkrivanje: u trendu, popularno, nadolazeće, u kinima, najbolje ocijenjeno, po žanru, studiju i mreži, nedavno dodano, pretraga filmova, serija, osoba i glazbe, stranice naslova s glumcima, trailerom i dostupnošću po sezoni (D-061)
- [ ] Preporuke („slični naslovi") na stranici naslova
- [x] Zahtjev za film, seriju (po sezonama, i buduće sezone), album i izvođača; 4K zahtjevi kao zasebna staza (D-054, D-055)
- [x] Odobravanje (pojedinačno, skupno, uz izmjenu opcija), odbijanje s razlogom, automatsko odobravanje po korisniku i vrsti, kvote s iznimkama, komentari, prijave problema s traženjem zamjene (D-054, D-056)
- [x] Lokalni korisnici i pozivnice za obitelj (rok, broj korištenja, uloga, dozvole, kvote; D-059)
- [x] Prijava korisnika preko Streamera (isti računi, 2FA, automatsko stvaranje tražitelja ili povezivanje; D-058, D-068)
- [x] Obavijesti u aplikaciji: odobreno, odbijeno, dostupno, djelomično, zapelo, komentari, problemi; `GET /notifications?since=` za aplikacije (D-057)

## Media server i obavijesti (P9)
- [x] Streamero: veza (URL + API ključ, test, mapiranje putanja i biblioteka), skeniranje samo pogođene biblioteke nakon uvoza/nadogradnje/preimenovanja filma (skupljeno, s ponovnim pokušajima, na vremenskoj crti; D-067)
- [x] Streamero: „U Streameru" i „Gledaj u Streameru" na Otkrivanju, stranicama naslova i u dodavanju (D-069)
- [x] Streamero: prijava korisnika (D-068)
- [x] Obavijesti: webhook (HMAC potpis), ntfy, Gotify, Discord, Telegram, Pushover, e-mail (SMTP), Apprise API; zajednički i osobni kanali, izbor događaja, Test, ponovni pokušaji, stanje neuspjeha (D-070)
- [x] Obavijesti o biblioteci, titlovima i zdravlju sustava (D-071)
- [ ] Streamero: obavijest pri brisanju i preimenovanju serija te brisanju glazbe (danas ih vidi Streamerov nadzor datoteka)

## Uvoz setupa (P10)
- [x] Sonarr, Radarr, Lidarr: biblioteka (datoteke na mjestu, praćenje), profili kvalitete, definicije veličina, custom formati, profili izdanja i kašnjenja, root mape, oznake, imena, indexeri, download klijenti, mapiranja putanja, liste i isključenja (liste samo TMDb za filmove; Locarr nema popise za serije i glazbu), povijest i blokirana izdanja (D-072 – D-076)
- [x] Prowlarr: indexeri (kao izvor ili pojedinačno, D-078); Bazarr: jezični profili, provideri i profil po naslovu; Seerr/Overseerr: korisnici s dozvolama, zahtjevi, problemi, kvote (korisnici i zahtjevi provjereni s lažnim Overseerrom)
- [x] qBittorrent, SABnzbd: veza, kategorije, prijedlozi mapiranja putanja
- [x] Pregled prije uvoza (stvori/spoji/preskoči s razlozima, sukobi), uvoz u pozadini s napretkom, izvještaj (JSON), popis za prelazak; izvori se nikad ne mijenjaju, ponovni uvoz ne duplicira (D-077)

## Vremenska crta i nadzor (P11)
- [ ] Vremenska crta po naslovu: pretrage, odbijena izdanja s razlozima, grab, preuzimanje, uvoz, obavijesti (za filmove od P4, za epizode i sezone od P5, za albume od P6)
- [ ] „Zašto nije stiglo": jedan ekran koji objašnjava trenutno stanje svakog nedostajućeg naslova (za filmove na stranici filma od P4)
- [ ] Aktivnost (red preuzimanja), povijest, blokirana izdanja, nedostajuće/ispod granice
- [ ] Zdravlje sustava (indexeri, klijenti, disk, putanje, prava), logovi, zadaci
- [ ] `/api/stats` za nadzorne ploče, Prometheus metrike

## Aplikacije (P12)
- [ ] Android i iOS: sve iz weba (biblioteka, dodavanje, pretraga, aktivnost, kalendar, zahtjevi, postavke za admina)
- [ ] Obavijesti bez APNs-a/FCM-a (provjera u pozadini)

## Kvaliteta (P13)
- [ ] Sigurnosni pregled, ciljevi performansi testirani, korisnička dokumentacija, v1.0.0
