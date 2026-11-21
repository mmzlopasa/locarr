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
- [ ] TMDb (i TVDB po izboru): serije, sezone, epizode, specijali
- [ ] Vrste: standardne, dnevne, anime (apsolutna numeracija, mapiranje scene numeracije)
- [ ] Sezonski paketi, višestruke epizode, praćenje novih sezona
- [ ] Kalendar (web + iCal feed), nadolazeće epizode

## Glazba (P6)
- [x] MusicBrainz izvođači (pretraga s pojašnjenjem), albumi, izdanja i pjesme, profili metapodataka (glavne i dodatne vrste, statusi izdanja), praćenje novih izdanja (svi / budući / ništa), osvježavanje koje ne briše datoteke
- [x] Pretraga albuma (automatska, po rasporedu, RSS, ručna s odlukom), profili kvalitete „Lossless" i „Lossy", nadogradnje do granice
- [x] Uvoz s prepoznavanjem po tagovima, brojevima, naslovima i trajanju (bodovi, više diskova, izbor izdanja), djelomični albumi po izboru, preimenovanje, omoti (iz preuzimanja ili Cover Art Archive), zamjena u koš
- [x] Uvoz postojeće glazbene biblioteke (datoteke ostaju na mjestu), kalendar izlazaka albuma, nedostajući albumi
- [ ] Cue slike (jedna datoteka po albumu) i pisanje tagova osim FLAC-a (D-044)

## Titlovi (P7)
- [ ] Jezični profili po filmu/seriji, forsirani i za gluhe
- [ ] Provideri: OpenSubtitles.com i drugi javni, ocjena podudaranja po izdanju
- [ ] Automatska pretraga nakon uvoza, ponovna pretraga za nedostajuće, ručni izbor
- [ ] Sinkronizacija titla po zvuku, pretvorba formata

## Zahtjevi (P8)
- [ ] Otkrivanje: trending, popularno, nadolazeće, preporuke, pretraga
- [ ] Zahtjev za film, seriju (po sezonama) i album; 4K zahtjevi
- [ ] Odobravanje, automatsko odobravanje po korisniku, kvote, komentari, prijave problema
- [ ] Prijava korisnika preko Streamera (isti računi), lokalni korisnici
- [ ] Obavijest korisniku kad je zahtjev dostupan

## Media server i obavijesti (P9)
- [ ] Streamero: osvježavanje biblioteke nakon uvoza, „već imaš" na otkrivanju, prijava korisnika
- [ ] Obavijesti: webhook, ntfy, Gotify, Discord, Telegram, e-mail, Apprise-kompatibilno

## Uvoz setupa (P10)
- [ ] Sonarr, Radarr, Lidarr: biblioteka, profili kvalitete, custom formati, root mape, oznake, indexeri, download klijenti, liste
- [ ] Prowlarr: indexeri; Bazarr: jezični profili i provideri; Seerr/Overseerr: korisnici i zahtjevi
- [ ] qBittorrent, SABnzbd: veza i kategorije
- [ ] Pregled prije uvoza, izvori se nikad ne mijenjaju

## Vremenska crta i nadzor (P11)
- [ ] Vremenska crta po naslovu: pretrage, odbijena izdanja s razlozima, grab, preuzimanje, uvoz, obavijesti (za filmove radi od P4, za albume od P6; serije s P5)
- [ ] „Zašto nije stiglo": jedan ekran koji objašnjava trenutno stanje svakog nedostajućeg naslova (za filmove na stranici filma od P4)
- [ ] Aktivnost (red preuzimanja), povijest, blokirana izdanja, nedostajuće/ispod granice
- [ ] Zdravlje sustava (indexeri, klijenti, disk, putanje, prava), logovi, zadaci
- [ ] `/api/stats` za nadzorne ploče, Prometheus metrike

## Aplikacije (P12)
- [ ] Android i iOS: sve iz weba (biblioteka, dodavanje, pretraga, aktivnost, kalendar, zahtjevi, postavke za admina)
- [ ] Obavijesti bez APNs-a/FCM-a (provjera u pozadini)

## Kvaliteta (P13)
- [ ] Sigurnosni pregled, ciljevi performansi testirani, korisnička dokumentacija, v1.0.0
