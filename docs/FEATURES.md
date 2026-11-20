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
- [ ] Torznab i Newznab: caps, kategorije, pretraga po ID-u (TMDb/TVDB/IMDb/MusicBrainz) i tekstu, RSS
- [ ] Prowlarr i Jackett kao izvor (uvoz svih njihovih indexera kao Torznab/Newznab)
- [ ] Prioriteti, oznake (tagovi), ograničenja zahtjeva (rate limit), dnevni limiti, backoff i zdravlje
- [ ] Paralelna pretraga svih indexera, rezultati čim stignu, ručna (interaktivna) pretraga
- [ ] Proxyji po indexeru (HTTP/SOCKS5), FlareSolverr po izboru

## Parser i odluke (P2)
- [ ] Parser imena izdanja: naslov, godina, sezona/epizoda (SxxEyy, višestruke, sezonski paketi, apsolutno, dnevno), kvaliteta, izvor, rezolucija, kodek, HDR/DV, audio, jezici, grupa, proper/repack, verzija
- [ ] Profili kvalitete (redoslijed, granica nadogradnje), definicije veličina
- [ ] Custom formati s bodovima, jezični profili, obavezni/zabranjeni izrazi, profili kašnjenja
- [ ] Odluka sa zapisanim razlogom za svako odbijeno izdanje
- [ ] Blokirana izdanja, ponovni pokušaj s idućim najboljim izdanjem

## Download klijenti (P3)
- [ ] qBittorrent, Transmission, Deluge, rTorrent, aria2 (torrent)
- [ ] SABnzbd, NZBGet (usenet)
- [ ] Kategorije/oznake po vrsti sadržaja, prioriteti, praćenje napretka i brzine
- [ ] Ciljevi seedanja (omjer, vrijeme) i uklanjanje nakon uvoza po izboru
- [ ] Mapiranje udaljenih putanja (klijent na drugom stroju/kontejneru)

## Filmovi (P4)
- [ ] TMDb pretraga i dodavanje, kolekcije, popisi (TMDb, Trakt, IMDb liste) za automatsko dodavanje
- [ ] Praćenje, minimalna dostupnost (najavljeno, u kinu, izdano)
- [ ] RSS sync, automatska i ručna pretraga, nadogradnje do granice profila
- [ ] Uvoz: hardlink (zadano kad je moguće), kopija, premještanje; provjera prije brisanja izvora
- [ ] Preimenovanje datoteka i mapa po predlošku, NFO i slike po izboru
- [ ] Koš za smeće (Recycle bin) s rokom, nikad tiho brisanje

## Serije (P5)
- [ ] TMDb (i TVDB po izboru): serije, sezone, epizode, specijali
- [ ] Vrste: standardne, dnevne, anime (apsolutna numeracija, mapiranje scene numeracije)
- [ ] Sezonski paketi, višestruke epizode, praćenje novih sezona
- [ ] Kalendar (web + iCal feed), nadolazeće epizode

## Glazba (P6)
- [ ] MusicBrainz izvođači, albumi, vrste izdanja (album, EP, singl), praćenje novih izdanja
- [ ] Uvoz s prepoznavanjem po tagovima i trajanju, preimenovanje, omoti (Cover Art Archive)

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
- [ ] Vremenska crta po naslovu: pretrage, odbijena izdanja s razlozima, grab, preuzimanje, uvoz, obavijesti
- [ ] „Zašto nije stiglo": jedan ekran koji objašnjava trenutno stanje svakog nedostajućeg naslova
- [ ] Aktivnost (red preuzimanja), povijest, blokirana izdanja, nedostajuće/ispod granice
- [ ] Zdravlje sustava (indexeri, klijenti, disk, putanje, prava), logovi, zadaci
- [ ] `/api/stats` za nadzorne ploče, Prometheus metrike

## Aplikacije (P12)
- [ ] Android i iOS: sve iz weba (biblioteka, dodavanje, pretraga, aktivnost, kalendar, zahtjevi, postavke za admina)
- [ ] Obavijesti bez APNs-a/FCM-a (provjera u pozadini)

## Kvaliteta (P13)
- [ ] Sigurnosni pregled, ciljevi performansi testirani, korisnička dokumentacija, v1.0.0
