# Contributing — Locarr

Locarr je jedna aplikacija koja zamjenjuje cijeli „*arr" stack: serije, filmove, glazbu, titlove,
indexere i zahtjeve korisnika. Jedna prijava, jedan port, jedno mjesto za gledanje što se događa.
Web sučelje + mobilne aplikacije (Android, iOS). Pokreće se preko Docker Composea.

Plan: `docs/PLAN.md`. Popis značajki: `docs/FEATURES.md`. Stanje: `docs/PROGRESS.md`. Odluke: `docs/DECISIONS.md`.

## Neprekršiva pravila
- **Clean-room.** Ne otvaraj, ne kopiraj i ne prevodi izvorni kod Sonarra, Radarra, Lidarra, Readarra,
  Bazarra, Prowlarra, Jackett-a, Overseerra, Jellyseerra, Seerra, qBittorrenta, Transmissiona, Delugea,
  rTorrenta, aria2, SABnzbd-a, NZBGeta ni njihovih web sučelja. Dozvoljeno: javna API dokumentacija i
  specifikacije protokola (Torznab, Newznab, qBittorrent WebUI API, Transmission RPC, Deluge JSON-RPC,
  rTorrent XML-RPC, aria2 JSON-RPC, SABnzbd i NZBGet API, TMDb, MusicBrainz, OpenSubtitles), javno
  poznate konvencije imenovanja izdanja i vlastito testiranje ponašanja preko njihovih API-ja.
- Nigdje u UI-ju, ikonama ni identifikatorima nema tuđih imena proizvoda kao brenda. Imena aplikacija
  smiju stajati samo tamo gdje ih korisnik mora prepoznati (uvoz iz X, veza s klijentom Y).
- `api/openapi.yaml` je izvor istine. Promjena API-ja = prvo spec, zatim `scripts/gen-api.sh`, zatim kod.
  Generirani kod se nikad ne uređuje ručno.
- Svaka promjena sheme = nova migracija (nikad ne mijenjaj migraciju koja je već u `main`).
- Kod, komentari, commitovi i API su na engleskom. Svi UI stringovi idu kroz i18n (EN + HR), nikad hardkodirani.
- Tajne nikad u repo. Samo `.env.example`. Ključevi i lozinke vanjskih servisa spremaju se šifrirano
  i nikad se ne vraćaju klijentu (samo „postavljeno").
- **Nikad ne briši korisnikove datoteke** osim kroz eksplicitnu radnju korisnika ili koš za smeće
  (Recycle bin) s rokom. Uvoz nikad ne briše izvor dok odredište nije provjereno.
- Svaka putanja iz zahtjeva prolazi kroz jednu funkciju za sigurne putanje (zaštita od path traversal).
- Svaki odlazni HTTP zahtjev ima timeout, ograničenu veličinu odgovora i ne prati preusmjerenja na drugi host.
- Bez oznaka alata: commitovi, PR-ovi, kod i dokumentacija ne spominju AI ni asistente; nema
  `Co-Authored-By` ni „Generated with" redaka.

## Komande
```bash
# dev stack
docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml up -d --build

# server
cd server && go test ./... -race && golangci-lint run

# API codegen (server + web + mobilne aplikacije)
./scripts/gen-api.sh

# web
cd web && pnpm i && pnpm dev && pnpm typecheck && pnpm test && pnpm e2e

# testni indexeri i download klijenti (lažni servisi za integracijske testove)
./scripts/dev-fakes.sh up
```

## Način rada
1. Na početku sesije pročitaj `docs/PROGRESS.md` i nastavi od zadnjeg otvorenog koraka.
2. Jedna faza = jedan branch `phase/PX-ime`. Mali, logični commitovi (Conventional Commits).
3. Za veće odluke dodaj zapis u `docs/DECISIONS.md` (kontekst → odluka → posljedice).
4. Svaka značajka dolazi s testovima: unit, integracijski (protiv stvarne baze i lažnih indexera/klijenata),
   e2e (Playwright) za korisničke tokove. Parser imena izdanja ima golden testove.
5. Značajka se označava `[x]` u `FEATURES.md` tek kad radi end-to-end i testovi prolaze.
6. Na kraju sesije ažuriraj `PROGRESS.md`.
7. Ako nešto iz plana nije izvedivo ili postoji bolji pristup — zapiši zašto u `DECISIONS.md`.

## Performanse (ciljevi koji se testiraju)
- Pokretanje do `/readyz` < 3 s; jedan kontejner < 250 MB RAM u mirovanju s 5000 serija i 10 000 filmova.
- Lista biblioteke (100 stavki) < 100 ms; pretraga svih indexera paralelno, rezultati čim stignu.
- RSS sync i odluka o izdanju za 1000 izdanja < 1 s.

## Dizajn
Tamni, „operativni" stil: gusto ali čitljivo, sve stanje vidljivo na prvi pogled. Tokeni na CSS
varijablama (web) i u `DesignSystem` (iOS) / temi (Android), da se izgled može mijenjati bez diranja komponenti.
