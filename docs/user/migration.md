# Uvoz postojećeg setupa

Ako već imate Sonarr, Radarr, Lidarr, Prowlarr, Bazarr, Overseerr (ili Seerr/Jellyseerr), qBittorrent
ili SABnzbd, Locarr može preuzeti njihove postavke, biblioteku i korisnike u jednom prolazu.

**Locarr te aplikacije samo čita preko njihova API-ja.** Na njima se ništa ne mijenja (ni postavke, ni
popisi, ni datoteke), a vaše medijske datoteke ostaju točno gdje jesu: Locarr ih samo zabilježi, ne
premješta ih, ne preimenuje i ne kopira. Uvoz možete pokrenuti koliko god puta želite: ono što je već
uvezeno se ne duplicira.

Uvoz pokreće administrator: **Uvoz setupa** u izborniku (ili `/migrate`).

---

## Prije početka

1. **Mape za medije.** Locarr mora vidjeti isti disk na kojem su vaše serije, filmovi i glazba
   (`LOCARR_DATA_ROOTS`, u `docker-compose.yml` je to montaža `/data`). Ako ste dosad imali
   `/data/media/tv`, `/data/media/movies` i `/data/media/music`, Locarr ih vidi pod istim imenom.
2. **Adrese aplikacija.** Locarr mora doći do aplikacija mrežom. Ako rade u Dockeru na istom računalu,
   stavite Locarr u istu Docker mrežu i koristite imena kontejnera (`http://sonarr:8989`).
   Ako koriste URL bazu (npr. `/sonarr` iza reverse proxyja), dodajte je adresi.
3. **API ključevi.** U svakoj *arr aplikaciji: *Settings → General → API Key*. U Bazarru:
   *Settings → General → Security → API Key*. U Overseerru/Seerru: *Settings → General → API Key*.
   Za qBittorrent trebaju korisničko ime i lozinka web sučelja. Za SABnzbd: *Config → General → API Key*.
4. **TMDb ključ** (*Postavke → Metapodaci*) nije nužan za uvoz, ali bez njega se metapodaci uvezenih
   naslova ne mogu dopuniti.

## Korak 1: aplikacije

Za svaku aplikaciju odaberite vrstu, upišite adresu i ključ te kliknite **Provjeri**. Locarr javi
„Spojeno na Sonarr 4.0.11" ili objasni što ne valja:

| Poruka | Što učiniti |
|---|---|
| Locarr je ne može dosegnuti | provjerite adresu, port i je li Locarr u istoj mreži |
| Odbila je API ključ ili lozinku | ključ je pogrešan; kod qBittorrenta i previše pogrešnih prijava privremeno blokira adresu |
| Na ovoj adresi je druga aplikacija | npr. upisali ste Radarrov port za Sonarr |
| Ova je verzija prestara | Sonarr/Radarr trebaju v3 ili noviji, qBittorrent 4.1+, SABnzbd 3+ |
| Na ovoj adresi ništa ne odgovara | nedostaje URL baza |

**Provjeri i dodaj** sprema aplikaciju. Ključ se sprema šifrirano i više se nikad ne prikazuje.

## Korak 2: što uvesti

Za svaku aplikaciju označite što želite:

- **Postavke**: profili kvalitete, veličine, custom formati, profili izdanja i kašnjenja, root mape,
  imena datoteka, indexeri, download klijenti, mapiranja putanja, popisi i isključenja.
- **Biblioteka**: filmovi, serije (s praćenjem po sezoni i epizodi) i izvođači s albumima, zajedno s
  datotekama koje već imate.
- **Korisnici i zahtjevi** (Overseerr/Seerr): korisnici s dozvolama i kvotama, zahtjevi s njihovim
  stanjem, problemi s komentarima.
- **Povijest i blokirana izdanja**: grabovi, uvozi i neuspjela preuzimanja iz zadnjih N dana (zadano
  90) idu na vremensku crtu svakog naslova, pa „Zašto nije stiglo?" zna i što se događalo prije
  prelaska. Blokirana izdanja ostaju blokirana.

**Prowlarr** se može uvesti na dva načina:

- **Zadrži Prowlarr kao izvor indexera** (zadano): Locarr sinkronizira indexere iz Prowlarra; novi se
  pojave sami. Prowlarr mora nastaviti raditi.
- **Dodaj svaki indexer zasebno**: svaki Prowlarrov indexer postaje Locarrov indexer. Običan
  Torznab/Newznab indexer pokazuje izravno na svoju stranicu, ostali i dalje idu kroz Prowlarr.

Uz oba načina je zadano uključeno **„Gdje može, koristi Locarrove indexere"**: javne stranice koje
Locarr ima ugrađene (Nyaa, The Pirate Bay, YTS, 1337x, LimeTorrents …) postaju Locarrovi ugrađeni
indexeri, a usenet indexeri izravni Newznab indexeri (upiši im ključ i uključi ih). Ti se indexeri
više ne pretražuju kroz Prowlarr, a kad nijedan ne ostane na Prowlarru, Prowlarr se može obrisati
(vidi [indexers.md](indexers.md)).

Kliknite **Pročitaj aplikacije**. Čitanje velike biblioteke traje desetak sekundi do minutu; ništa
se još ne stvara.

## Korak 3: mape

Aplikacije često rade u vlastitim kontejnerima i vide vaše medije pod drugim putanjama (npr. Sonarr
vidi `/tv`, a Locarr `/data/media/tv`). Za svaku root mapu iz aplikacija Locarr pokaže:

> Pronađeno 1234 od 1240 datoteka u /data/media/tv

Ako broj nije dobar, dodajte **mapiranje putanje**: *mapa u aplikaciji* → *mapa u Locarru*.
Locarr sam predloži mapiranje kad pronađe mapu istog imena. Provjera se osvježava dok tipkate;
primjeri datoteka koje nisu pronađene pomažu naći pogrešan dio putanje. Windows putanje
(`D:\Media\TV`) rade isto.

Mapa izvan mapa koje Locarr smije koristiti ne može se mapirati — prvo je dodajte u
`LOCARR_DATA_ROOTS` (montaža u Dockeru).

## Korak 4: pregled

Pregled pokazuje **sve što će uvoz napraviti** prije nego što se išta napravi:

- tablicu po vrsti (root mape, profili, indexeri, filmovi, serije, korisnici…) s brojem stavki koje
  će se **stvoriti**, **spojiti** s postojećima ili **preskočiti**;
- svaku stavku s razlogom („već je u biblioteci", „dolazi iz Prowlarra", „Locarr to ne podržava",
  „nepromijenjeno od zadnjeg uvoza"…);
- oznake **traži pažnju**: ključ ili lozinka koje aplikacija ne otkriva, datoteke koje nisu pronađene,
  uvjeti custom formata koje Locarr ne poznaje, korisnici kojima treba lozinka.

**Sukobi imena.** Locarr već ima npr. profil „HD-1080p". Za takvu stavku birate:

- **Spoji s postojećim** (zadano): postojeći profil dobiva postavke iz aplikacije,
- **Stvori pod drugim imenom**: „HD-1080p (Radarr)",
- **Preskoči**.

Nakon promjene kliknite **Osvježi pregled**, a kad je sve kako treba, **Uvezi sada**.

## Korak 5: uvoz i izvještaj

Uvoz radi u pozadini (stranicu možete zatvoriti); traka pokazuje napredak. 10 000 filmova,
2 000 serija i 1 000 izvođača uvoze se za otprilike minutu.

Izvještaj pokazuje što je napravljeno, što je preskočeno i što nije uspjelo (s razlogom), oznake
„traži pažnju" i **Preuzmi izvještaj (JSON)** za arhivu. Izvještaj ne sadrži nijedan ključ ni lozinku.

Uvezeni naslovi odmah imaju naslov, godinu, ID-jeve, praćenje i datoteke. Ostale metapodatke (sažetke,
glumce, epizode koje aplikacija nije znala, izdanja albuma) Locarr dopunjuje u pozadini; izvještaj
pokazuje koliko je gotovo. Na vremenskoj crti svakog naslova piše odakle je uvezen.

### Što treba napraviti ručno

- **Ključevi i lozinke.** Sonarr, Radarr, Lidarr i Prowlarr u svom API-ju skrivaju ključeve indexera i
  lozinke klijenata (`********`). Takav indexer ili klijent je dodan, ali isključen: upišite ključ u
  *Indexeri* ili *Download klijenti* i uključite ga. SABnzbd i Deluge se bez ključa ne mogu dodati — ako
  ste SABnzbd dodali i kao aplikaciju za uvoz, Locarr koristi ključ koji ste upisali.
- **Korisnici** iz Overseerra/Seerra nemaju lozinku (aplikacija je ne daje). Postavite je u
  *Postavke → Korisnici* ili pošaljite pozivnicu. Korisnik istog imena kao postojeći račun spaja se s
  njim (uloga se ne mijenja).

## Popis za prelazak

**Popis za prelazak** (gumb na vrhu stranice) vodi kroz zadnje korake, da dvije aplikacije ne grabe
ista izdanja. Locarr stare aplikacije **ne mijenja**, pa su koraci na njima upute koje sami označite:

1. Uvezi setup.
2. Isključi automatske pretrage u Sonarru, Radarru i Lidarru (RSS i automatska pretraga) ili zaustavi
   kontejnere.
3. Ukloni stare aplikacije iz Prowlarra (*Settings → Apps*), a kad popis javi da nijedan indexer više
   ne ide kroz Prowlarr, ugasi i Prowlarr.
4. Uputi ljude na Locarr umjesto starog alata za zahtjeve.
5. Zaustavi Bazarr.
6. Provjeri download klijente i kategorije koje Locarr koristi.
7. Uključi RSS sync.
8. Pokreni probnu pretragu.
9. Unesi ključeve koje aplikacije nisu otkrile.

## Što se uvozi iz koje aplikacije

| Aplikacija | Uvozi se | Ne uvozi se |
|---|---|---|
| **Sonarr** (v3, v4) | serije s TVDB/TMDb/IMDb ID-jem, vrstom, mapama sezona, praćenjem serije, sezona i epizoda, datoteke epizoda (kvaliteta, grupa, jezici); profili kvalitete s bodovima formata i jezicima (v3 jezični profili, D-024); veličine; custom formati; profili izdanja (obavezni/zabranjeni izrazi); profili kašnjenja; root mape; imena; oznake; Torznab/Newznab indexeri; download klijenti s kategorijom; mapiranja putanja; povijest; blokirana izdanja | popisi za uvoz i isključenja (Locarr nema popise za serije), preferirani izrazi v3 (koristite custom formate), indexeri koji nisu Torznab/Newznab, obavijesti |
| **Radarr** (v3+) | filmovi s datotekama, praćenjem i minimalnom dostupnošću; kolekcije koje se prate (kao popis kolekcije); TMDb popisi; isključenja; sve postavke kao Sonarr | Trakt, IMDb i drugi popisi, obavijesti |
| **Lidarr** (v1+) | izvođači, albumi s praćenjem, datoteke pjesama (disk, pozicija, snimka); profili metapodataka; profili kvalitete; veličine; root mape; imena; indexeri; klijenti; povijest; blokirana izdanja | popisi za uvoz, isključenja, odabrano izdanje albuma (Locarr ga bira sam nakon osvježavanja) |
| **Prowlarr** (v1+) | indexeri s kategorijama, prioritetom i oznakama — kao izvor ili pojedinačno | aplikacije, proxyji, profili sinkronizacije |
| **Bazarr** (1.x) | jezični profili (jezik, forsirani, za gluhe, granica), provideri OpenSubtitles.com, SubDL i Jimaku (korisnik, lozinka, ključ kad ih Bazarr pokazuje), profil po filmu i seriji | ostali provideri, preuzeti titlovi (Locarr ih nađe na disku sam), postavke sinkronizacije |
| **Overseerr / Seerr** | korisnici s dozvolama i kvotama, zadane kvote, zahtjevi sa stanjem i sezonama, problemi s komentarima | lozinke i prijava preko Plexa/Jellyfina, glasovi, obavijesti |
| **qBittorrent** | veza (s lozinkom koju ste upisali), kategorije kao kategorije za filmove, serije i glazbu, prijedlozi mapiranja putanja | torrenti u klijentu (ostaju gdje jesu) |
| **SABnzbd** | veza (s ključem koji ste upisali), kategorije, prijedlozi mapiranja putanja | poslovi u redu, news serveri |

### Kako se dozvole pretvaraju u uloge

| Overseerr / Seerr | Locarr |
|---|---|
| Admin | administrator (sve) |
| Manage settings / Manage users | upravitelj + upravljanje bibliotekom |
| Manage requests / Manage issues | upravitelj + upravljanje zahtjevima |
| Request (sve) | traži filmove, serije i glazbu |
| Request movies / Request series | traži filmove / serije |
| Request 4K (bilo koji) | traži 4K |
| Auto-approve (sve) | automatsko odobravanje svega |
| Auto-approve movies / series | automatsko odobravanje filmova / serija |
| Auto-approve 4K (bilo koji) | automatsko odobravanje 4K |
| ostalo (glasanje, popis za gledanje…) | ne uvozi se (Locarr nema istovjetno) |

### Stanja zahtjeva

| Overseerr / Seerr | Locarr |
|---|---|
| čeka odobrenje | na čekanju |
| odobren, medij dostupan | dostupno |
| odobren, djelomično dostupan | djelomično |
| odobren, obrađuje se | obrađuje se |
| odobren | odobreno |
| odbijen | odbijeno |
| neuspio | neuspjelo |

Uvezeni zahtjevi zatim prate Locarrovu biblioteku kao i svi drugi.
