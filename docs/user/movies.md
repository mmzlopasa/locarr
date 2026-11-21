# Filmovi

Locarr za svaki film vodi sve od dodavanja do datoteke u biblioteci: traži izdanja, odlučuje koje uzeti
i zašto, šalje ga download klijentu, uvozi datoteku pod urednim imenom i sve to zapisuje na vremensku
crtu filma.

## Prije prvog filma

1. **Postavke → Metapodaci**: zalijepite svoj TMDb API ključ (v3 ključ ili „read access token" s
   themoviedb.org → Settings → API). Locarr ga prije spremanja provjeri kod TMDb-a, sprema ga šifrirano
   i više ga ne prikazuje. Tu birate i jezik naslova i opisa te regiju (datumi izdanja i dobne oznake te zemlje).
2. **Postavke → Mape za medije**: dodajte root mapu za filmove, npr. `/data/media/movies`.
3. **Indexeri** i **Download klijenti** (vidi `download-clients.md`).
4. Po želji **Postavke → Profili kvalitete** (vidi `profiles.md`).

## Dodavanje

**Filmovi → Dodaj film** traži na TMDb-u po naslovu („Big Buck Bunny 2008"), TMDb ID-u (`tmdb:10378`)
ili IMDb ID-u (`tt1254207`). Nakon odabira birate:

- **Root mapu** — film dobiva vlastitu mapu po predlošku (zadano `Naslov (Godina)`).
- **Profil kvalitete** — koje kvalitete smije imati i do koje se nadograđuje.
- **Minimalnu dostupnost** — od kada se traži:
  - *Najavljeno*: odmah.
  - *U kinima*: od prvog kino datuma.
  - *Izdano*: od prvog digitalnog ili fizičkog izdanja; ako TMDb ne zna ni jedno, 90 dana nakon kina.
  U **Postavke → Upravljanje medijima** „odgoda dostupnosti" pomiče taj datum (negativno = ranije).
- **Oznake** — biraju profile izdanja, profile kašnjenja i download klijente s istim oznakama.
- **Prati** — samo praćeni filmovi traže se automatski i nadograđuju.
- **Traži odmah** — pretraga kreće čim je film dodan.

Na stranici kolekcije (poveznica „Kolekcija" na filmu) vidite koji filmovi iz nje nedostaju i dodajete
ih odjednom. **Postavke → Popisi za uvoz** dodaju filmove s TMDb popisa ili kolekcije svakih 6 sati.
Film obrisan uz „ne dodaj ponovno" popisi više ne dodaju (popis isključenja je na istoj stranici).

## Kako Locarr traži

- Nakon dodavanja, a zatim po rasporedu: film koji nije nađen traži se ponovno nakon 30 min, 1 h, 2 h…
  najviše jednom dnevno. Zato ne opterećuje indexere.
- **RSS**: nova izdanja s indexera uspoređuju se s praćenim filmovima (po ID-u, inače po naslovu i godini).
- Svako izdanje prolazi odluku (profil, veličina, jezik, custom formati, blokirana izdanja…). Najbolje
  prihvaćeno ide klijentu; ako ga klijent ili indexer odbije, uzima se sljedeće.
- Izdanje koje profil kašnjenja zadržava čeka i ponovno se procjenjuje kad čekanje istekne.
- Ako preuzimanje ne uspije (greška klijenta, nema seedera), izdanje se blokira i odmah se traži sljedeće najbolje.
- Kad film ima datoteku ispod granice profila, Locarr i dalje traži bolje izdanje.

Na filmu, kartica **Izdanja** pita sve indexere i pokazuje svako izdanje s odlukom i razlozima
odbijanja; bilo koje možete preuzeti i ručno. **Nedostaje** (Wanted) popisuje filmove koji nedostaju ili
su ispod granice i pokreće pretragu za više njih odjednom.

## Uvoz i imena

Kad klijent završi, Locarr pronađe glavnu video datoteku (uzorci, dodaci i manje datoteke se preskaču),
uz nju titlove, i stavi je u mapu filma:

- **Hardlink** (zadano): ne zauzima dodatni prostor, a torrent nastavlja seedati. Ako preuzimanja i
  biblioteka nisu na istom datotečnom sustavu (iste Docker montaže), Locarr kopira (vidi INSTALL.md).
- **Kopija**: uvijek kopira.
- **Premještanje**: samo ako ga izričito odaberete; torrent tada prestaje seedati.

Izvor se nikad ne dira dok odredište nije provjereno. Stara datoteka kod nadogradnje ide u koš.

Imena se namještaju u **Postavke → Upravljanje medijima** uz pregled uživo. Oznake se pišu kao
`{Title}`, `{Year}`, `{QualityFull}`, `{ReleaseGroup}`… a znakovi unutar zagrada oko oznake pojavljuju
se samo kad ona ima vrijednost: `{[Edition]}` daje `[Director's Cut]` ili ništa. Imena se uvijek čiste
tako da rade i na Windowsu (bez `: ? * " < > |`).

Podaci o slici i zvuku (rezolucija, kodek, HDR, zvuk) čitaju se iz imena izdanja i datoteke.

## Postojeća biblioteka

**Filmovi → Uvezi postojeće**:

- **Postojeća biblioteka**: odaberite root mapu i „Pregledaj". Locarr pokaže mape koje još nisu u
  biblioteci i upari ih s TMDb-om (po ID-u u imenu mape, npr. `{tmdb-603}` ili `tt0133093`, inače po
  naslovu i godini). Provjerite parove i dodajte ih; datoteke ostaju gdje jesu, ništa se ne premješta.
- **Ručni uvoz**: upišite mapu ili datoteku, pregledajte što bi se uvezlo i u koji film, i uvezite.

- **Preimenovanje datoteka**: „Pokaži što bi se promijenilo" pa „Preimenuj" primjenjuje predložak na datoteke koje su već u biblioteci (u istoj mapi, titlovi idu s njima, nikad preko postojeće datoteke).

## Koš za smeće

Zamijenjene datoteke i mape obrisanih filmova ne brišu se nego idu u koš (zadano `/data/recycle-bin`).
U **Postavke → Upravljanje medijima** vidite što je u košu, vraćate stavke na izvorno mjesto ili ih
(kao administrator) trajno brišete. Stavke starije od roka (zadano 7 dana, 0 = nikad) briše dnevno čišćenje.

## Vremenska crta i „Zašto nije stiglo?"

Kartica **Povijest** na filmu ima na vrhu jednu rečenicu koja objašnjava trenutno stanje, npr.:

- „Još nije dostupan (minimalna dostupnost). Traženje počinje 12. 3. 2027."
- „Dosadašnjih pretraga: 3. Zadnja je našla izdanja: 5 i sva su odbijena. Najčešći razlog: preveliko."
- „Preuzima se … 42 %."
- „… je preuzet, ali uvoz nije uspio: nije pronađena nijedna video datoteka."

Ispod je sve što se dogodilo, najnovije gore: pretrage (koliko je koji indexer našao), odluke (klik
na strelicu pokazuje odbijena izdanja s razlozima), slanje klijentu, napredak preuzimanja (25, 50, 75 %),
završetak, uvoz (hardlink ili kopija), preimenovanje, nadogradnja, koš i obrisano.

## Kalendar

**Kalendar** pokazuje kino, digitalna i fizička izdanja filmova iz biblioteke (mjesec, tjedan, popis).
„Napravi poveznicu" daje osobni iCal link za aplikaciju kalendara; nova poveznica gasi staru.
