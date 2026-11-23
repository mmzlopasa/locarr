# Zahtjevi

Ukućani i prijatelji sami biraju što žele gledati i slušati: pregledavaju **Otkrivanje**, kliknu
**Zatraži**, a Locarr javi kad je naslov stigao. Administrator odlučuje što se odobrava i koliko tko
smije tražiti.

Prvi dio je za administratora, drugi za članove obitelji.

---

## Za administratora

### Prije prvog zahtjeva

1. **Postavke → Metapodaci**: TMDb ključ (bez njega Otkrivanje pokazuje samo nedavno dodano).
   Za glazbu postavite kontakt za MusicBrainz.
2. **Postavke → Zahtjevi**: za svaku vrstu (filmovi, serije, glazba) odaberite **profil kvalitete** i
   **mapu za medije** u koje idu odobreni zahtjevi. Bez toga se zahtjev ne može odobriti
   (poruka „nisu postavljene zadane vrijednosti").
   - **4K**: za filmove i serije možete odabrati poseban profil (npr. „UHD") i mapu. 4K zahtjev je
     zasebna „staza" istog naslova: ako naslov već postoji, odobreni 4K zahtjev mu promijeni profil u
     4K profil. Locarr drži jednu kopiju naslova (D-055).
   - **Glazba**: profil metapodataka može ostati „Nije odabrano"; tada Locarr koristi „Everything",
     pa je svaki traženi album na popisu izvođača, a prati se samo taj album.
   - **Oznake** se dodaju naslovima iz zahtjeva (npr. `zahtjev`, da ih lakše nađete).
3. **Zadane kvote** na istoj stranici: koliko zahtjeva po vrsti smije poslati jedan korisnik u
   određenom broju dana (npr. 5 filmova u 7 dana). 0 = bez ograničenja. Album i izvođač dijele
   glazbenu kvotu. Povučeni i odbijeni zahtjevi se ne broje. Administratori i korisnici koji smiju
   upravljati zahtjevima nemaju kvotu.

### Korisnici i pozivnice

**Postavke → Korisnici**:

- **Novi korisnik**: upišete korisničko ime i lozinku i odaberete ulogu. *Tražitelj* (requester) vidi
  samo Otkrivanje, svoje zahtjeve i svoje prijave problema. *Upravitelj* (manager) uz to može voditi
  biblioteku i odobravati zahtjeve, ovisno o dozvolama.
- **Dozvole** po korisniku: smije li tražiti filmove, serije i glazbu, smije li tražiti 4K, i što se
  **odobrava automatski** (sve, ili samo filmovi / serije / glazba, posebno 4K).
- **Kvote** po korisniku: vlastito pravilo za vrstu zamijeni zadano. Vidi se i koliko je potrošeno.
- **Pozivnice**: napravite pozivnicu (naziv, uloga, dozvole, kvote, broj korištenja, rok — zadano
  7 dana) i pošaljite poveznicu `https://vaš-locarr/join#…`. Poveznica se prikazuje **samo jednom**.
  Tko je otvori, sam odabere korisničko ime i lozinku i odmah je prijavljen. Iskorištena, istekla ili
  opozvana pozivnica više ne radi; računi koji su s njom napravljeni ostaju.

Prijava preko Streamera (isti računi kao na media serveru) dolazi u fazi P9.

### Odobravanje

**Zahtjevi → Svi zahtjevi** prikazuje zahtjeve svih korisnika (zadano one koji čekaju).

- **Odobri**: naslov se dodaje u biblioteku (ili se počne pratiti ako već postoji) i odmah se traži.
  Za seriju se prate samo tražene sezone; ako je tražitelj označio „i buduće sezone", prate se i nove.
- **Uredi…**: prije odobravanja promijenite profil, mapu, oznake ili sezone.
- **Odbij**: upišite razlog — tražitelj ga vidi u obavijesti.
- Označite više zahtjeva i **Odobri označene**; ako koji ne prođe (npr. već je odbijen), Locarr
  kaže koji i zašto.
- **Traži ponovno** kod zahtjeva koji je zapeo.

Stanja zahtjeva:

| Stanje | Značenje |
|---|---|
| Čeka odobrenje | nitko ga još nije odobrio |
| Odobreno / Obrađuje se | u biblioteci je i Locarr ga traži ili preuzima (vidi se napredak preuzimanja) |
| Djelomično dostupno | stigao je dio (npr. jedna od dvije sezone, ili dio pjesama) |
| Dostupno | sve što je traženo je u biblioteci |
| Zapelo | Locarr ga trenutno ne može nabaviti; razlog je isti kao „Zašto nije stiglo?" na stranici naslova (ništa nađeno, sve odbijeno, uvoz nije uspio…) |
| Odbijeno / Povučeno | završeno |

„Zapelo" nije konačno: ako naslov kasnije ipak stigne (RSS, nova pretraga), zahtjev postaje dostupan i
tražitelj dobiva obavijest. Na vremenskoj crti naslova vidi se tko ga je zatražio i tko je odobrio.

### Problemi

**Problemi** su prijave korisnika za naslove koji su već u biblioteci (slika, zvuk, titlovi, nešto
drugo; za serije i sezona/epizoda). Možete odgovoriti komentarom, a **Riješi** zatvara prijavu.
Uz **Potraži zamjenu** trenutna datoteka (epizoda, cijela sezona ili album) ide u **koš za smeće**
(ništa se ne briše), njezino izdanje se blokira i Locarr traži drugo. Tražitelj dobiva obavijest i
može prijavu ponovno otvoriti.

### Obavijesti

Zvonce (dolje lijevo, na mobitelu gore) pokazuje broj nepročitanih. Administratori i upravitelji
dobivaju obavijest za svaki novi zahtjev, prijavu problema, komentar i zahtjev koji je zapeo.
Obavijesti na e-mail, Discord, Telegram, ntfy i ostale kanale dolaze u fazi P9.

---

## Za članove obitelji

### Prijava

Otvorite poveznicu pozivnice koju ste dobili, odaberite korisničko ime i lozinku (barem 8 znakova) i
kliknite **Napravi račun**. Ubuduće se prijavljujete na istoj adresi s tim imenom i lozinkom.

### Otkrivanje

Na **Otkrivanju** su redovi: nedavno dodano, u trendu, popularni filmovi i serije, uskoro, sada u
kinima, najbolje ocijenjeno, te žanrovi, studiji i mreže za pregledavanje. Gore je tražilica za
filmove, serije, osobe i glazbu (kartica **Glazba** traži izvođače i albume).

Svaki plakat ima oznaku:

- **Dostupno** — već je u biblioteci, možete gledati.
- **Djelomično tu** — dio je tu (npr. neke sezone).
- **Zatraženo** — netko ga je zatražio, čeka odobrenje.
- **Na putu** — odobreno, Locarr ga nabavlja.
- **U biblioteci** — dodano, ali još nema datoteke.

### Kako zatražiti

1. Otvorite naslov (opis, glumci, trailer, a kod serija sezone s oznakom za svaku).
2. **Zatraži**. Kod serije označite sezone (ili „Sve sezone", i po želji „i buduće sezone").
   Ako imate pravo na 4K, možete odabrati 4K. Možete dodati i poruku.
3. Ispod gumba piše koliko vam je zahtjeva ostalo u kvoti.

Ako je vaš račun postavljen za automatsko odobravanje, naslov odmah kreće; inače čeka administratora.
Isti naslov ne možete zatražiti dvaput (ni ako ga je već zatražio netko drugi), niti ako je već dostupan.

### Moji zahtjevi

**Zahtjevi** pokazuju sve što ste zatražili i dokle je stiglo, s napretkom preuzimanja. Dok čeka
odobrenje, zahtjev možete **povući**. U zahtjevu se možete dopisivati s administratorom.

### Obavijesti

Zvonce javlja kad je zahtjev odobren ili odbijen (s razlogom), kad je naslov **dostupan** (ili dio
njega) i kad je zapeo. **Označi sve pročitanim** čisti brojač.

### Nešto ne valja?

Na stranici dostupnog naslova kliknite **Prijavi problem**, odaberite što ne valja (slika, zvuk,
titlovi, nešto drugo), za seriju sezonu i epizodu, i opišite problem. Dobit ćete obavijest kad je
riješen.
