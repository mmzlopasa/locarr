# Indexeri

Indexeri su mjesta na kojima Locarr traži izdanja. Locarr ih pretražuje **sam**: za javne torrent
stranice ima ugrađene indexere, a usenet indexere i privatne trackere s Torznabom dodaješ izravno.
**Prowlarr i Jackett više nisu potrebni.**

Indexere dodaješ u **Indexeri → Dodaj indexer**. Prozor ima tri kartice.

## 1. Ugrađene stranice

Javne stranice koje Locarr zna pretraživati bez ikakvog posrednika:

| Stranica | Što ima | Kako je Locarr pretražuje | FlareSolverr |
|---|---|---|---|
| YTS | filmove | službeni API, po IMDb id-u ili naslovu | ne treba |
| EZTV | serije | javni API, po IMDb id-u serije | ne treba |
| The Pirate Bay | sve | API koji koristi i sama stranica, filmovi i po IMDb id-u | ne treba |
| SubsPlease | anime epizode i pakete | API stranice (izbor rezolucije u postavkama) | ne treba |
| Knaben | tražilica preko mnogo trackera | dokumentirani javni API | ne treba |
| Nyaa | anime, glazba, live action | RSS pretraga (filter „samo provjereni" u postavkama) | ne treba |
| Tokyo Toshokan | anime, glazba, raw | RSS | ne treba |
| LimeTorrents | sve | stranice pretrage i RSS zadnjih izdanja (spora stranica) | ponekad |
| 1337x | sve | stranice pretrage; magnet se uzima sa stranice izdanja tek kad se grabi | **uvijek** |
| BitSearch | sve | stranice pretrage | ne treba |

Za uključivanje stranice dovoljan je jedan klik na prekidač „Koristi …". Locarr postavi razumne
zadane vrijednosti: adresu stranice, ograničenje zahtjeva (npr. Nyaa 12 u minuti), vrijeme čekanja,
kategorije i RSS (isključen za stranice koje nemaju popis zadnjih izdanja, npr. 1337x).

**Uredi** na ugrađenom indexeru nudi:
- **Adresu** — glavnu ili poznati mirror, ili vlastitu adresu ako je stranica preselila.
- **Postavke stranice** — npr. rezolucija za SubsPlease, filter za Nyaa.
- **FlareSolverr URL**, **ograničenje zahtjeva**, **kategorije**, **prioritet** i gdje se indexer koristi
  (RSS, automatska, ručna pretraga).

Kod svakog indexera piše verzija definicije, a pod „Kako je provjereno" piše kako je definicija napisana.
Pretraga po ID-u radi gdje je stranica podržava (YTS, EZTV, The Pirate Bay za filmove); za ostale Locarr
sam složi upit (naslov i godina za filmove, naslov i SxxEyy za serije).

Magneti koje Locarr gradi iz info hasha dobivaju javne trackere iz **Postavke indexera → Trackeri za
složene magnete**. Tamo je i **User-Agent** koji Locarr šalje stranicama.

### Zdravlje i razlozi
Ako stranica ne radi, uz indexer piše zašto:
- **Stranica ne odgovara** — pala je ili je spora; Locarr pričeka i pokuša ponovno sam.
- **Ograničenje zahtjeva** — stranica traži pauzu; Locarr je poštuje.
- **Cloudflare provjera** — stranica traži FlareSolverr (vidi niže).
- **Izgled stranice se promijenio** — Locarr ne može pročitati rezultate. Broj nepročitanih redaka
  vidi se uz indexer. Čeka se nova verzija definicije (ili vlastita definicija, vidi niže).

Jednom tjedno Locarr sam testira svaki uključeni ugrađeni indexer i zapiše ishod („Tjedna samoprovjera").
**Testiraj** pokreće isti poznati upit odmah.

### FlareSolverr
Neke stranice (1337x, ponekad LimeTorrents) pred običan zahtjev stave Cloudflare provjeru. Tada pokreni
[FlareSolverr](https://github.com/FlareSolverr/FlareSolverr) uz Locarr, npr. u istom `docker-compose.yml`:

```yaml
  flaresolverr:
    image: ghcr.io/flaresolverr/flaresolverr:latest
    restart: unless-stopped
```

i u indexeru upiši **FlareSolverr URL** `http://flaresolverr:8191`. Locarr ga zove samo kad stranica
stvarno pokaže provjeru i koristi ga samo za kolačiće. Provjera ponekad ne uspije iz prvog puta;
Locarr će pokušati opet kasnije.

## 2. Usenet

Usenet indexere (NZBgeek, DrunkenSlug, NZBPlanet, NZBFinder, DOGnzb, altHUB, Tabula Rasa, NinjaCentral,
Miatrix, Usenet Crawler) dodaješ izravno: odaberi indexer s popisa, upiši **API ključ** (nalazi se na
tvom profilu na stranici indexera — poveznica je uz popis), **Testiraj** i **Dodaj**. Adresa i API putanja
su već upisani.

Indexer kojeg nema na popisu dodaj na kartici **Torznab/Newznab** s njegovom adresom i ključem.

## 3. Torznab / Newznab

Za sve ostalo što govori Torznab ili Newznab: privatne trackere koji nude Torznab, vlastite usenet
indexere i slično. Upiši adresu, API putanju (obično `/api`) i ključ, pa **Testiraj** — test učita
kategorije indexera.

## Vlastite definicije

Stranicu koju Locarr nema (npr. World-torrent ili BigFANGroup) možeš opisati sam: stavi YAML datoteku
u mapu `definitions` unutar konfiguracijske mape (`/config/definitions` u kontejneru) i na stranici
Indexeri u odjeljku **Vlastite definicije stranica** klikni **Ponovno učitaj**. Datoteke s greškama se ne učitaju
i prikazane su s opisom greške. Datoteka s istim `id`-em kao ugrađena definicija je zamjenjuje (korisno
dok se čeka popravak kad stranica promijeni izgled).

Kratki primjer (stranica s JSON pretragom):

```yaml
id: mojastranica
name: Moja stranica
version: 1
links: [https://moja.stranica.example]
verified: napisano prema javnoj stranici pretrage
modes: {search: [q], tv: [q, season, ep], movie: [q]}
categories:
  - {id: "filmovi", cat: 2000, desc: Filmovi}
  - {id: "serije", cat: 5000, desc: Serije}
search:
  requests:
    - if: [keywords]
      path: '/api/search?q={{ .Keywords | urlquery }}&cat={{ first .Categories "" }}'
  response: json        # json, xml (RSS) ili html
  rows: results         # JSON putanja ili CSS selektor
  fields:
    title: {select: name}
    infohash: {select: hash}
    size: {select: size}
    date: {select: added}
    seeders: {select: seeders}
    category: {select: category}
test: {query: ubuntu}
```

Za HTML stranice `rows` i `select` su CSS selektori (`attr` čita atribut, `regex` izvlači dio teksta),
`empty` označava stranicu „nema rezultata", a blok `download` čita magnet sa stranice izdanja kad se grabi.
Ugrađene definicije u izvornom kodu (`server/internal/indexers/native/definitions`) su dobri primjeri.
Prijave na stranice nisu podržane: privatni trackeri idu preko Torznaba.

## Prelazak s Prowlarra

U **Uvoz setupa** (vidi [migration.md](migration.md)) uz Prowlarr je zadano uključeno
**„Gdje može, koristi Locarrove indexere"**:
- Prowlarrovi indexeri za stranice koje Locarr ima ugrađene postaju ugrađeni Locarrovi indexeri
  (isto ime, prioritet, oznake i stanje).
- Usenet indexeri postaju izravni Newznab indexeri. Prowlarr skriva ključeve, pa su dodani **isključeni**
  s oznakom „upiši ključ": upiši ključ i uključi ih.
- Ostali (npr. privatni trackeri bez Torznaba) ostaju na Prowlarru.

Nakon uvoza **Popis za prelazak** pokazuje stavku „Prestani koristiti Prowlarr": kad nijedan indexer više
ne pretražuje kroz Prowlarr, Prowlarr (i Jackett) možeš ugasiti i obrisati. Uvoz iz Prowlarra ili Jacketta
kao izvora i dalje postoji na stranici Indexeri pod **Prikaži uvoz iz Prowlarra i Jacketta**, za one koji ih
žele zadržati.
