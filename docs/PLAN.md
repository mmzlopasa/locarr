# Locarr — plan

> Jedna aplikacija umjesto šest: serije, filmovi, glazba, titlovi, indexeri i zahtjevi.
> Jedna prijava, jedan port, jedan kontejner, jedno mjesto za vidjeti zašto nešto nije stiglo.
> Web sučelje + Android i iOS aplikacija. Pokretanje: Docker Compose. Repo: github.com/mmzlopasa/locarr.

## 1. Što zamjenjuje

| Danas | U Locarru |
|---|---|
| Sonarr (serije) | Serije: praćenje, epizode, sezonski paketi, anime numeracija, kalendar |
| Radarr (filmovi) | Filmovi: praćenje, dostupnost, kolekcije |
| Lidarr (glazba) | Glazba: izvođači, albumi, izdanja |
| Bazarr (titlovi) | Titlovi: jezični profili, provideri, automatsko traženje i sinkronizacija |
| Prowlarr / Jackett (indexeri) | Indexeri: Torznab, Newznab, ili Prowlarr/Jackett kao izvor |
| Seerr / Overseerr (zahtjevi) | Zahtjevi: korisnici traže, admin odobrava, automatsko odobravanje, kvote |

Download klijenti: qBittorrent, Transmission, Deluge, rTorrent, aria2, SABnzbd, NZBGet.
Media server: Streamero (obavijest o novom sadržaju, osvježavanje biblioteke, prijava korisnika za zahtjeve).
Uvoz postojećeg setupa: Sonarr, Radarr, Lidarr, Prowlarr, Bazarr, Seerr, Overseerr, qBittorrent, SABnzbd.

## 2. Ključna ideja: jedna vremenska crta po naslovu

Za svaki film, epizodu i album Locarr vodi **povijest odluka**: koje je indexere pitao, što je našao,
zašto je koje izdanje odbio (kvaliteta, veličina, jezik, custom format, blokirano, nema seedera…),
što je poslao kojem klijentu, kako napreduje preuzimanje, zašto uvoz nije uspio (putanja, prava, nedostaje
datoteka, nepoznata epizoda) i kada je obaviješten media server. Pitanje „zašto serija nije stigla" ima
jedan odgovor na jednom ekranu.

## 3. Stack

| Dio | Izbor | Zašto |
|---|---|---|
| Server | Go 1.26, chi, ogen (OpenAPI-first) | jedan binarij, mala potrošnja, isti alati kao Streamero |
| Baza | SQLite (WAL, `modernc.org/sqlite`, bez CGO) | jedan kontejner, jedna datoteka, backup = kopija; dovoljno za stotine tisuća stavki |
| Poslovi | vlastiti red poslova u SQLiteu + raspored (RSS sync, pretrage, praćenje preuzimanja, uvoz, titlovi) | bez dodatnih servisa |
| Web | React 19, Vite, TypeScript, TanStack Router/Query, Tailwind v4, i18n EN/HR | ugrađen u Go binarij |
| Android | Kotlin, Jetpack Compose | nativno, isti obrasci kao Streamero |
| iOS | Swift 6, SwiftUI, XcodeGen, .ipa za Feather | bez APNs-a i entitlementa koji ne prežive resign |
| Metapodaci | TMDb (filmovi, serije), TVDB po izboru, MusicBrainz + Cover Art Archive (glazba) | javni API-ji |
| Titlovi | OpenSubtitles.com + dodatni javni provideri, sinkronizacija po zvuku (ffsubsync-stil, vlastita implementacija) | |
| Isporuka | jedan Docker image (amd64 + arm64) na GHCR, `docker-compose.yml` s jednim servisom | „jedan port" |

## 4. Struktura repoa
```
api/openapi.yaml          izvor istine za API
server/                   Go: cmd/locarr, internal/{app,api,auth,db,jobs,indexers,downloaders,parser,
                          decision,library/{movies,series,music},importer,subtitles,requests,metadata,
                          mediaserver,migrate(uvoz iz drugih aplikacija),notify,timeline,fs}
web/                      React klijent
android/  ios/            mobilne aplikacije
deploy/                   Dockerfile, docker-compose.yml, compose.dev.yml, .env.example
scripts/                  gen-api.sh, dev-fakes.sh, gen-fixtures.sh
docs/                     PLAN, FEATURES, DECISIONS, PROGRESS, korisnička dokumentacija
```

## 5. Faze

| Faza | Sadržaj | Prihvaćanje |
|---|---|---|
| P0 temelji | repo, server skeleton, SQLite + migracije, korisnici/sesije/API ključevi/2FA, web ljuska, i18n, CI, Docker image, compose | `docker compose up` → postavljanje admina → prijava; CI zelen |
| P1 indexeri | Torznab/Newznab klijent (caps, kategorije, pretraga, RSS), Prowlarr/Jackett kao izvor, zdravlje i backoff, ručna pretraga | lažni indexer u testovima; ručna pretraga vraća rezultate paralelno |
| P2 parser i odluke | parser imena izdanja (golden testovi, 1000+ imena), kvalitete, profili kvalitete, custom formati, jezici, veličine, odluka s razlozima | svaka odbijena opcija ima razlog |
| P3 download klijenti | 7 klijenata, kategorije, praćenje napretka, završetak, seed ciljevi, blokiranje | lažni klijenti u testovima |
| P4 filmovi | TMDb, dodavanje, praćenje, dostupnost, RSS i pretraga, grab, uvoz (hardlink/kopija/premještanje), preimenovanje, koš | film od dodavanja do datoteke u biblioteci end-to-end |
| P5 serije | TMDb/TVDB, sezone, epizode, sezonski paketi, anime/apsolutna numeracija, dnevne emisije, kalendar | serija end-to-end |
| P6 glazba | MusicBrainz, izvođači, albumi, izdanja, uvoz s tagovima | album end-to-end |
| P7 titlovi | jezični profili, provideri, automatska pretraga, ručni izbor, sinkronizacija | titl stiže za uvezeni film |
| P8 zahtjevi | korisnici (lokalni + prijava preko Streamera), otkrivanje (trending, popularno), zahtjevi, odobravanje, kvote, obavijesti | korisnik zatraži film, on stigne i korisnik je obaviješten |
| P9 media server i obavijesti | Streamero (osvježavanje, prijava), webhookovi, ntfy, Gotify, Discord, Telegram, e-mail | |
| P10 uvoz setupa | iz Sonarra, Radarra, Lidarra, Prowlarra, Bazarra, Seerra/Overseerra, qBittorrenta, SABnzbd-a | probni uvoz s lažnim servisima; ništa se ne mijenja na izvoru |
| P11 vremenska crta i nadzor | timeline po naslovu, aktivnost, „zašto nije stiglo", zdravlje sustava, logovi, backup/restore, `/api/stats` | |
| P12 mobilne aplikacije | Android i iOS: sve iz weba, push-free obavijesti (polling) | .apk i .ipa iz CI-ja |
| P13 kvaliteta | sigurnosni pregled, performanse (ciljevi iz CONTRIBUTING), dokumentacija, prvo izdanje v1.0.0 | |

Faze P1–P3 mogu ići paralelno nakon P0; P4–P8 nakon P1–P3.
