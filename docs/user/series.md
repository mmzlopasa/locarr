# Serije

Locarr za svaku seriju vodi sezone i epizode, traži nedostajuće epizode (pojedinačno ili cijelu sezonu
kao paket), šalje izdanja download klijentu, uvozi svaku epizodu pod urednim imenom i sve zapisuje na
vremensku crtu epizode.

## Prije prve serije

1. **Postavke → Metapodaci**: TMDb ključ (isti kao za filmove, vidi `movies.md`). Po želji i **TVDB**:
   zalijepite TVDB v4 projektni ključ (i PIN ako ga TVDB traži) i odaberite „Serije se traže na: TVDB".
   TVDB ima apsolutne brojeve epizoda i vrijeme emitiranja, što pomaže kod animea. Serija zadržava izvor
   s kojim je dodana.
2. **Postavke → Mape za medije**: root mapa za serije, npr. `/data/media/tv`.
3. **Indexeri** i **Download klijenti**; u klijentu po želji kategorija za serije.
4. Po želji **Postavke → Upravljanje medijima → Imena serija** (vidi dolje).

## Dodavanje

**Serije → Dodaj seriju** traži po naslovu („The Expanse"), TMDb ID-u (`tmdb:63639`), TVDB ID-u
(`tvdb:280619`) ili IMDb ID-u (`tt3230854`). Nakon odabira birate:

- **Root mapu** i **profil kvalitete**.
- **Praćenje**: sve epizode, buduće, nedostajuće, postojeće, prvu sezonu, zadnju sezonu, samo pilot ili
  ništa. Specijali (sezona 0) se nikad ne prate sami; uključite ih ručno ako ih želite.
- **Vrstu serije**:
  - *Standardna*: izdanja se zovu `S01E01`.
  - *Dnevna*: talk-showovi i vijesti, izdanja po datumu (`2025.03.14`).
  - *Anime*: izdanja po apsolutnom broju (`- 12`), sezone se ne traže kao paketi po broju sezone.
- **Nove sezone**: prati li Locarr sezone koje izvor kasnije doda.
- **Mape sezona**: epizode idu u `Season 01`, `Season 02`… (specijali u `Specials`).
- **Traži odmah**: pretraga kreće čim je serija dodana.

## Stranica serije

Gore su podaci o seriji, broj epizoda s datotekom, koliko ih nedostaje, veličina i sljedeće emitiranje.
Ispod su sezone (zadnja je otvorena):

- Na sezoni: **praćenje** (uključuje ili isključuje sve epizode sezone), **traži sezonu**, **odaberi
  izdanje za sezonu** (ručna pretraga s odlukom i razlozima za svako izdanje) i **povijest** sezone.
- Na epizodi: praćenje, vrijeme emitiranja u vašoj vremenskoj zoni, stanje (preuzeto, ispod granice,
  nedostaje, preuzima se, nije emitirano), kvaliteta i veličina, **traži**, **odaberi izdanje** i
  **povijest** — ladica s odgovorom na „zašto nije stigla?" i svim koracima (pretrage, odluke s
  odbijenim izdanjima, grab, preuzimanje, uvoz, preimenovanje).
- Označite više epizoda (kvačicom ili cijelu sezonu) za skupno praćenje ili pretragu.
- **Uredi**: profil, vrsta, mape sezona, nove sezone, ponovna primjena praćenja, oznake, mapa (dok
  serija nema datoteka). **Obriši**: po želji mapa serije ide u koš.

## Kako Locarr traži

- Epizoda se traži kad je emitirana (vrijeme emitiranja u zoni mreže; kad ga izvor ne zna, 20:00).
- **Sezonski paket**: kad sezoni nedostaje barem 3 epizode (mijenja se u Imenima serija: „Sezonski paket
  od") ili je sezona završila, a nedostaju barem dvije, Locarr traži cijelu sezonu. Paket koji pokriva sve
  tražene epizode ima prednost; ako ga nema, uzima pojedinačne epizode iz iste pretrage. Nikad ne šalje
  dva izdanja za istu epizodu.
- Inače se svaka epizoda traži zasebno; pri tome se sezonski paketi odbijaju s razlogom.
- **RSS**: nova izdanja uparuju se sa serijom po TVDB/IMDb ID-u, naslovu ili alternativnom naslovu, pa
  s epizodama (ili sezonom kad je paket).
- Nadogradnje do granice profila traže se po epizodi. Ostalo (profili kašnjenja, blokirana izdanja,
  ponovni pokušaj s idućim izdanjem nakon neuspjeha) radi kao kod filmova.
- **Nedostaje** (Wanted) → **Epizode** popisuje nedostajuće epizode i one ispod granice i pokreće
  pretragu za više njih odjednom.

## Numeracija i scensko mapiranje

Ako izdanja numeriraju epizodu drukčije od TMDb-a ili TVDB-a, upišite scensku numeraciju na epizodi
(`PATCH /api/v1/episodes/{id}` s `sceneSeason`/`sceneEpisode` ili `sceneAbsolute`; `clearScene` je
briše). Locarr tada traži i uparuje po scenskim brojevima. Javni izvor mapiranja se ne koristi (licenca).

## Uvoz

Kad preuzimanje završi, Locarr pronađe sve video datoteke (uzorci, dodaci i NFO se preskaču) i svaku
upari s epizodama iz imena (`S01E01`, `S01E01E02`, datum, apsolutni broj):

- Svaka datoteka ide u mapu serije / mapu sezone pod imenom iz predloška, **hardlinkom** kad je moguće
  (torrent nastavlja seedati), inače kopijom; premještanje samo ako ga odaberete.
- Titlovi istog imena idu s epizodom.
- Datoteka s više epizoda dobiva jedno ime za sve (npr. `S01E01-E02`).
- Datoteka koja nije bolja od postojeće ostaje gdje jest; stara datoteka kod nadogradnje ide u koš.
- **Djelomičan paket**: uvozi se ono što se može upariti, ostatak je zapisan na vremenskoj crti, a
  epizode kojih nije bilo u paketu traže se ponovno.

## Postojeća biblioteka i ručni uvoz

**Serije → Uvezi postojeće**:

- **Postojeća biblioteka**: odaberite root mapu i „Pregledaj". Mape koje još nisu u biblioteci uparuju se
  s izvorom (po `{tvdb-…}`, `{tmdb-…}` ili IMDb ID-u u imenu mape, inače po naslovu i godini). Nakon
  dodavanja Locarr pronađe datoteke epizoda u mapi i poveže ih; ništa se ne premješta.
- **Ručni uvoz**: mapa ili datoteka, pregled serije i epizoda za svaku datoteku, pa uvoz.
- **Preimenovanje datoteka**: pregled pa primjena predložaka na epizode koje su već u biblioteci.

## Imena

**Postavke → Upravljanje medijima → Imena serija** ima pregled uživo za standardnu epizodu, dvije epizode
u jednoj datoteci, dnevnu emisiju i anime. Najčešće oznake: `{SeriesTitle}`, `{SeriesTitleYear}`,
`{Season}`, `{Episode}`, `{EpisodeCode}`, `{Absolute}`, `{AirDate}`, `{EpisodeTitle}`, `{QualityFull}`,
`{ReleaseGroup}`. Zagrade oko oznake (`{[QualityFull]}`, `{-ReleaseGroup}`) pojavljuju se samo kad
oznaka ima vrijednost. Stil više epizoda bira oblik `S01E01-E03`, `S01E01-03`, `S01E01E02E03`…

## Kalendar

**Kalendar** prikazuje epizode (s vremenom emitiranja) uz datume filmova, po mjesecu, tjednu ili kao
popis. iCal poveznica (dolje na stranici) uključuje i epizode s vremenom emitiranja i mrežom.
