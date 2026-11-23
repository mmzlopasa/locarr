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

## Ograničenja

- Streamero ima samo skeniranje cijele biblioteke (ne jedne mape); to je brzo jer preskače
  nepromijenjene datoteke.
- Prvi popis naslova velike biblioteke traje dulje (jedan zahtjev po naslovu); kasnije samo za nove.
