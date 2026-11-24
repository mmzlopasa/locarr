# Indexeri

Indexeri su mjesta na kojima Locarr traži izdanja. Locarr ih pretražuje **sam**: za javne torrent
stranice ima ugrađene indexere, a usenet indexere i privatne trackere s Torznabom dodaješ izravno.
**Prowlarr i Jackett više nisu potrebni.**

Indexere dodaješ u **Indexeri → Dodaj indexer**. Prozor ima tri kartice.

## 1. Ugrađene stranice

Javne stranice koje Locarr zna pretraživati bez ikakvog posrednika:

| Stranica | Što ima | Kako je Locarr pretražuje | Provjera preglednika |
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
| Sukebei | samo za odrasle | RSS pretraga | ne treba |
| XXXClub | samo za odrasle | stranice pretrage i RSS | ponekad |

Sukebei i XXXClub se nude samo dok je uključena biblioteka za odrasle; vidi [Sadržaj za odrasle](adult.md).

Za uključivanje stranice dovoljan je jedan klik na prekidač „Koristi …". Locarr postavi razumne
zadane vrijednosti: adresu stranice, ograničenje zahtjeva (npr. Nyaa 12 u minuti), vrijeme čekanja,
kategorije i RSS (isključen za stranice koje nemaju popis zadnjih izdanja, npr. 1337x).

**Uredi** na ugrađenom indexeru nudi:
- **Adresu** — glavnu ili poznati mirror, ili vlastitu adresu ako je stranica preselila.
- **Postavke stranice** — npr. rezolucija za SubsPlease, filter za Nyaa.
- **FlareSolverr URL** (samo rezerva, vidi niže), **ograničenje zahtjeva**, **kategorije**, **prioritet** i gdje se indexer koristi
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
- **Provjera preglednika** — stranica pred običan zahtjev stavlja provjeru (Cloudflare, DDoS-Guard).
  Piše i zašto nije prošla: ugrađeni preglednik nije pokrenut, nije uspio ili stranica traži čovjeka
  (vidi niže).
- **Izgled stranice se promijenio** — Locarr ne može pročitati rezultate. Broj nepročitanih redaka
  vidi se uz indexer. Čeka se nova verzija definicije (ili vlastita definicija, vidi niže).

Jednom tjedno Locarr sam testira svaki uključeni ugrađeni indexer i zapiše ishod („Tjedna samoprovjera").
**Testiraj** pokreće isti poznati upit odmah.

<a id="zasticene-stranice"></a>
### Zaštićene stranice bez FlareSolverra
Neke stranice (1337x, ponekad LimeTorrents) pred običan zahtjev stave provjeru preglednika: stranicu
„Just a moment…" ili „Checking your browser" koja pusti dalje tek kad JavaScript u pravom pregledniku
odradi svoje. Locarr za to ima **vlastiti preglednik**: mali dodatni kontejner s Chromiumom
(`ghcr.io/mmzlopasa/locarr-browser`). FlareSolverr više nije potreban.

**Uključivanje.** U `.env` uz `docker-compose.yml` postavi zajednički token (barem 16 znakova):

```bash
echo "LOCARR_BROWSER_TOKEN=$(openssl rand -hex 24)" >> .env
docker compose --profile browser up -d
```

Prva naredba upiše token, druga pokrene i Locarr (s tokenom) i preglednik. Preglednik nema
objavljen port: dostupan je samo Locarru na istoj Docker mreži i samo s tim tokenom. Za
pokretanje ili nadogradnju ubuduće uvijek dodaj `--profile browser`.

**Provjera.** **Indexeri → Preglednik za zaštićene stranice** pokazuje stanje (povezan, verzija,
Chromium, kartice u upotrebi, zadnja stranica). **Testiraj** pusti preglednik kroz njegovu ugrađenu
probnu provjeru (stranica koja JavaScriptom postavi kolačić i ponovno se učita); **Testiraj CAPTCHA
stranicu** provjerava da se stranica koja traži čovjeka ispravno prepozna; **Ili otvori adresu**
otvori bilo koju stranicu i kaže što se dogodilo. Ako preglednik nije pokrenut, ploča pokaže točnu
naredbu. Adresa (zadano `http://locarr-browser:8490`) i token mogu se tu i promijeniti; spremljeni
token ima prednost pred `LOCARR_BROWSER_TOKEN`.

**Kako radi.** Kad stranica odgovori provjerom (ili za stranice koje je uvijek imaju, npr. 1337x),
Locarr zamoli preglednik da otvori tu stranicu. Preglednik pričeka da provjera sama prođe (najviše
minutu) i vrati stranicu, kolačiće i svoj User-Agent. Locarr te kolačiće čuva po stranici i sljedeće
pretrage šalje svojim običnim klijentom s istim User-Agentom, pa se preglednik koristi rijetko (otprilike
jednom dok kolačić ne istekne). Ako stranica kolačiće veže uz sam preglednik i provjera se vrati,
Locarr pola sata šalje zahtjeve te stranice kroz preglednik. Ograničenje zahtjeva i razmak između
zahtjeva vrijede i za preglednik. Chromium se pokreće tek kad zatreba i ugasi se nakon 5 minuta mirovanja.

**Što ne radi i nikad neće.** Preglednik prolazi samo provjere koje pravi preglednik prođe sam
(JavaScript provjera, čekaonica, automatska „managed" provjera). Kad stranica traži da **čovjek**
klikne kvadratić ili riješi CAPTCHA (Turnstile, hCaptcha, reCAPTCHA), Locarr to ne radi i ne koristi
servise za rješavanje CAPTCHA zadataka. Tada uz indexer piše „treba ljudsku provjeru — otvori stranicu
u svom pregledniku" i Locarr pola sata ne pokušava ponovno. Cloudflare takav kvadratić češće pokazuje
poslužiteljima u podatkovnim centrima nego kućnim vezama; pomaže indexerov proxy (preglednik ga
koristi) ili mirror stranice.

**Sigurnost.** Preglednik radi kao običan korisnik (uid 1000), bez dodatnih Linux ovlasti, s
`no-new-privileges`, Dockerovim zadanim seccomp profilom, korijenskim datotečnim sustavom samo za
čitanje i ograničenjem memorije (1 GB) i procesa. Chromiumov vlastiti sandbox traži korisničke
namespaceove koje Docker kontejnerima obično ne daje; tada Chromium radi bez njega (stanje se vidi
na ploči), a granica je sam kontejner. Locarr provjerava adresu prije nego pita preglednik, a
preglednik sve zahtjeve stranice (i slike, skripte, preusmjerenja) šalje kroz svoju provjeru adresa:
loopback, link-local i adrese metapodataka oblaka su zabranjene, privatne mreže samo za adresu
indexera (i ono što je u **Dopuštene lokalne adrese**). Preuzimanja su isključena, `file://` i
`chrome://` adrese odbijene, a jedna stranica smije učitati najviše 64 MB.

**FlareSolverr kao rezerva.** Ako već imaš FlareSolverr i ne želiš drugi kontejner, upiši njegovu adresu
(`http://flaresolverr:8191`) u **FlareSolverr URL** indexera. Locarr ga koristi samo kad ugrađeni
preglednik nije pokrenut.

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
