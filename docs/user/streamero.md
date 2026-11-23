# Streamero

Locarr i Streamero rade zajedno: kad Locarr uveze novi film ili epizodu, Streamero ga odmah pokaže;
na Otkrivanju se vidi što se u Streameru već može gledati; a ukućani se u Locarr prijavljuju istim
korisničkim imenom i lozinkom kao u Streamero.

Potreban je Streamero **0.11.4 ili noviji** (trenutni `main`). Locarr koristi samo Streamerov
običan API (`/api/v1`), ništa ne treba instalirati u Streameru.

---

## 1. Povezivanje

1. U Streameru kao administrator otvorite **Postavke → API ključevi** i napravite **novi ključ samo za
   Locarr** (ne „samo za čitanje": Locarr pokreće skeniranje biblioteka). Ključ vrijedi s pravima
   administratora koji ga je napravio; ako ga ikad zatrebate opozvati, opozovete samo taj ključ.
2. U Locarru: **Postavke → Media server**.
   - **Adresa Streamera**: kako Locarr dolazi do Streamera, npr. `http://streamero:8787` kad su u istoj
     Docker mreži, ili `https://media.example.com`.
   - **Javna adresa** (nije obavezno): adresa koju vide preglednici, za gumbe „Gledaj u Streameru".
     Ako je prazna, koristi se adresa iznad.
   - **API ključ**: zalijepite ključ. Spremljen je šifrirano i nikad se ne prikazuje (vidi se samo
     „postavljeno").
3. Kliknite **Testiraj**. Locarr provjeri da na adresi odgovara Streamero, da ključ radi i pripada
   administratoru te pokaže biblioteke. Česte poruke:
   - *Streamero nije dostupan*: krivi host ili port, ili kontejneri nisu u istoj mreži.
   - *Ključ ne vrijedi* ili *ključ ne pripada administratoru*.
   - *Adresa nije dopuštena*: Locarr po zadanom ne ide na `127.0.0.1`/`localhost`. Dodajte adresu u
     **Indexeri → Postavke → Dopuštene adrese** ili koristite ime kontejnera.
4. Uključite **Streamero je uključen** i **Spremi**.

## 2. Putanje i biblioteke

Locarr i Streamero obično rade u različitim kontejnerima, pa istu mapu vide pod različitim imenima
(Locarr `/data/media/movies`, Streamero `/media/movies`).

- **Mapiranje putanja**: dodajte red *Locarr putanja* → *Streamero putanja*, npr. `/data/media` →
  `/media`. Vrijedi najduže poklapanje.
- **Biblioteke po mapama za medije**: za svaku Locarrovu mapu za medije odaberite Streamero
  biblioteku (ili više njih). **Koristi prijedloge** odabere biblioteke čija se mapa poklapa s
  mapom za medije nakon mapiranja putanja.
- Ako za mapu ništa ne odaberete, Locarr traži biblioteku čija mapa sadrži datoteku.

## 3. Što se događa nakon uvoza

Nakon uvoza, nadogradnje, preimenovanja ili brisanja (s premještanjem u koš) Locarr čeka ~30 sekundi
da skupi sve promjene, pa zatraži **skeniranje samo pogođenih biblioteka**, svake jednom. Uvoz nikad
ne čeka na Streamero. Ako Streamero nije dostupan, Locarr pokuša ponovno nekoliko puta (10 s, 20 s,
40 s …).

Na vremenskoj crti naslova (kartica **Povijest**) vidi se ishod:
- *notified Streamero: scanning Movies* – Streamero je zamoljen da skenira biblioteku „Movies".
- *could not notify Streamero: …* – razlog, npr. nijedna biblioteka ne sadrži putanju (provjerite
  mapiranje) ili Streamero nije odgovorio ni nakon ponovnih pokušaja.

Stanje (zadnje skeniranje, greška, promjene koje čekaju) je u Postavke → Media server.

## 4. „U Streameru" i „Gledaj u Streameru"

Locarr vodi popis naslova koje Streamero već ima (filmovi i serije, po TMDb, IMDb i TVDB ID-u).
Osvježava ga svakih 6 sati, dvije minute nakon svakog skeniranja i na gumb **Osvježi sada**.

- Na **Otkrivanju** kartice takvih naslova imaju oznaku **U Streameru**.
- Na stranici naslova i u **Dodaj film / Dodaj seriju** pojavi se gumb **Gledaj u Streameru**, koji
  otvori naslov u Streamerovu web klijentu. Gumb Zatraži ostaje, ali u drugom planu: nema smisla
  tražiti nešto što se već može gledati.

Naslov se prepoznaje po ID-jevima koje Streamero ima za njega; naslov bez metapodataka u Streameru
(npr. nema TMDb ključa ni `[tmdbid-…]` u imenu mape) neće biti prepoznat.

## 5. Prijava preko Streamera

U **Postavke → Media server → Prijava**:
- **Ponudi „Prijava preko Streamera"**: na stranici za prijavu pojavi se izbor *Locarr račun /
  Streamero*.
- **Automatski stvori tražitelje**: tko se prvi put prijavi Streamero računom dobije Locarr račun s
  ulogom *tražitelj*, zadanim dozvolama i zadanim kvotama (Postavke → Zahtjevi). Bez ove opcije
  prijava uspije samo za račune koje je administrator povezao.

Važno:
- Locarr **ne sprema** Streamero lozinku ni token. Provjeri ih tako da se prijavi u Streamero i odmah
  odjavi tu sesiju (u Streameru se nakratko vidi uređaj „Locarr sign-in check").
- Ako Streamero traži dvofaktorski kod, Locarr ga zatraži u drugom koraku.
- Streamero administrator **nikad** ne postaje administrator u Locarru.
- Onemogućen korisnik (u Locarru ili Streameru) se ne može prijaviti.
- Pogrešne lozinke broje se kao i kod obične prijave (ograničenje i privremena zabrana adrese).

**Povezivanje postojećih korisnika**: **Postavke → Korisnici**, kod korisnika odaberite Streamero
račun s popisa. Korisnik može i sam: **Račun → Povezani računi → Poveži Streamero** (upiše svoju
Streamero lozinku). Povezivanje se može ukloniti na istim mjestima.

## 6. Otkrivanje u Streameru (Discover in Streamero)

Streamero ima vlastito **Otkrivanje** (pretraga i trendovi s gumbom *Zatraži*) i popis **Uskoro**.
Obično ih pokreću Seerr, Radarr i Sonarr; Locarr može glumiti sva tri, pa ukućani traže filmove i
serije izravno u Streameru, a zahtjevi završe u Locarru. U Streameru ne treba ništa instalirati.

### Korak po korak

1. **Locarr: TMDb i zadane vrijednosti zahtjeva.** U **Postavke → Metapodaci** mora biti TMDb ključ, a
   u **Postavke → Zahtjevi** profil kvalitete i mapa za medije za filmove i serije (bez toga zahtjev
   čeka odobrenje, vidi [Zahtjevi](requests.md)).
2. **Locarr: adrese.** Otvorite **Postavke → Media server**, karticu **Otkrivanje u Streameru**. Tu su
   tri adrese s gumbom **Kopiraj**:
   - Seerr: `http://<locarr>:8484/compat/seerr`
   - Radarr: `http://<locarr>:8484/compat/radarr`
   - Sonarr: `http://<locarr>:8484/compat/sonarr`

   `<locarr>` je adresa na kojoj Streamero vidi Locarr. Kad su u istoj Docker mreži, to je ime
   kontejnera (npr. `http://locarr:8484/compat/seerr`); inače IP ili ime računala
   (npr. `http://192.168.1.10:8484/compat/seerr`). Kartica pokazuje adresu na kojoj ste otvorili Locarr.
3. **Locarr: API ključ.** Najjednostavnije: na istoj kartici kliknite **Napravi ključ za Streamero**
   i kopirajte ga (prikazuje se **samo jednom**). Ključ pripada vama (administratoru), pa se zahtjevi
   iz Streamera **odobravaju automatski**.

   Ako želite sami odobravati i imati kvote:
   - u **Postavke → Korisnici** napravite korisnika, npr. `streamero`, s ulogom *tražitelj* i
     dozvolama za filmove i serije (bez automatskog odobravanja, ili s njim ako ga želite samo za
     neke vrste) i po želji s kvotama;
   - prijavite se kao taj korisnik i u **Račun → API ključevi** napravite ključ (ne „samo za čitanje");
     taj ključ ide u polje **Seerr**;
   - kao administrator napravite još jedan ključ **samo za čitanje** za polja **Radarr** i **Sonarr**
     (kalendar i poveznice čitaju biblioteku, što tražitelj ne smije).
4. **Streamero:** kao administrator otvorite **Postavke → Zahtjevi** i upišite:

   | Polje u Streameru | Vrijednost |
   |---|---|
   | Adresa Seerr | `http://<locarr>:8484/compat/seerr` |
   | API ključ Seerr | Locarr ključ (`lk_…`) |
   | Adresa Radarr | `http://<locarr>:8484/compat/radarr` |
   | API ključ Radarr | isti ključ (ili administratorov samo za čitanje) |
   | Adresa Sonarr | `http://<locarr>:8484/compat/sonarr` |
   | API ključ Sonarr | isti ključ (ili administratorov samo za čitanje) |

   Uključite **Svi mogu slati zahtjeve** ako zahtjeve smiju slati i obični korisnici Streamera.
   Bazarr, Lidarr i Prowlarr ostavite prazne.
5. **Streamero: Provjeri veze.** Uz Seerr, Radarr i Sonarr treba pisati *povezano* s Locarrovom
   verzijom. Česte greške:
   - *HTTP 401*: ključ nije upisan ili ne vrijedi (opozvan, korisnik onemogućen).
   - *HTTP 403* kod Radarra/Sonarra: vlasnik ključa nema pravo vidjeti biblioteku; koristite
     administratorov ključ.
   - *ne može se spojiti*: kriva adresa ili kontejneri nisu u istoj mreži.

### Kako to izgleda

- **Otkrivanje** u Streameru pokazuje trendove i rezultate pretrage iz TMDb-a (na jeziku korisnika
  Streamera), s istim stanjem kao Locarrovo Otkrivanje: *zatraženo*, *obrađuje se*, *djelomično*,
  *dostupno*.
- **Zatraži** napravi zahtjev u Locarru. Film se traži sam; serija se traži **cijela, sa svim
  sezonama i onima koje tek dolaze**. S administratorovim ključem naslov odmah kreće (*obrađuje se*),
  inače čeka odobrenje u **Zahtjevi → Svi zahtjevi** (*zatraženo*). Ako je naslov već zatražen ili
  dostupan, Streamero pokaže to stanje umjesto greške.
- Ako je kvota potrošena, Streamero javi grešku *Seerr answered HTTP 429*; nedozvoljena vrsta
  (npr. serije) daje *HTTP 403*.
- **Uskoro** pokazuje filmove (kino, digitalno ili fizičko izdanje) i epizode iz Locarrova kalendara,
  samo za naslove koji se prate.
- Poveznice **Radarr/Sonarr** na Streamerovoj stranici naslova (za administratore) otvaraju stranicu
  tog naslova u Locarru.

### Ograničenja

- Streamero ne javlja tko je kliknuo *Zatraži*, pa su **svi zahtjevi iz Streamera u Locarru zapisani
  kao vlasnik ključa** (npr. `streamero`). Kvote vrijede za sve ukućane zajedno.
- Streamero uvijek traži seriju cijelu; pojedinačne sezone tražite u Locarru.
- Pretraga ima jednu stranicu rezultata; glazba se iz Streamera ne može tražiti.
- Titlovi preko Bazarra u Streameru ne idu preko Locarra (Locarr titlove radi sam, vidi
  [Titlovi](subtitles.md)).

## Ograničenja

- Streamero ima samo skeniranje cijele biblioteke (ne jedne mape); to je brzo jer preskače
  nepromijenjene datoteke.
- Prvi popis naslova velike biblioteke traje dulje (jedan zahtjev po naslovu); kasnije samo za nove.
