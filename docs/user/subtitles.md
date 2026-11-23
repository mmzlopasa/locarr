# Titlovi

Locarr za svaki film i epizodu u biblioteci zna koje titlove želite, traži ih kod providera čim datoteka
stigne, bira onaj koji odgovara baš vašem izdanju, sprema ga pokraj videa i to zapisuje na vremensku crtu.
Ako titl kasni ili žuri, uskladi ga prema zvuku.

## Prije prvog titla

1. **Postavke → Titlovi → Provideri**: uključite barem jednog providera i upišite njegov API ključ
   (poveznica „Nabavi API ključ" vodi na stranicu gdje se ključ dobiva besplatno):
   - **OpenSubtitles.com** — najveći izbor, i za hrvatski. Ključ dobivate kao „API consumer" na
     opensubtitles.com. Po izboru upišite i korisničko ime i lozinku svog računa: prijavljeni korisnik
     ima veću dnevnu kvotu preuzimanja (bez prijave je nekoliko preuzimanja dnevno).
   - **SubDL** — besplatan ključ u korisničkom panelu na subdl.com.
   - **Jimaku** — samo japanski titlovi za anime.

   Ključevi i lozinke spremaju se šifrirano i više se nikad ne prikazuju (vidi se samo „Postavljeno").
   **Provjeri** pita providera i pokaže preostalu kvotu.
2. **Jezični profili**: **Dodaj profil**, npr. „Hrvatski": jezici po redu (Hrvatski, pa Engleski), za
   svaki jezik izbor *samo forsirani* (titlovi samo za dijelove na stranom jeziku) i *za gluhe*
   (prihvati, prednost, isključi, samo).
   - **Računaj zapise u videu**: ako datoteka već ima titl tog jezika u sebi (npr. MKV s hrvatskim
     zapisom), ništa se ne preuzima.
   - **Stani kad postoji jedan jezik**: dovoljan je prvi pronađeni jezik po redu.
3. **Mogućnosti**: zadani profil za filmove i za serije. Bez zadanog profila Locarr ne traži titlove.

Profil se može promijeniti i za pojedini film ili seriju (uređivanje filma ili serije → **Profil titlova**;
„Bez titlova" ih isključuje), a za sve filmove ili sve serije odjednom u odjeljku **Biblioteka**.
Epizode slijede profil svoje serije.

## Kako Locarr traži

- **Odmah nakon uvoza** filma ili epizode: pročita zapise u videu, pronađe titlove koji su stigli s
  preuzimanjem, pa traži ono što nedostaje.
- **Svaki sat** za stavke kojima nešto nedostaje, najviše „zakazanih pretraga na sat" (zadano 30), da se
  kvote providera rasporede kroz dan. Stavka koja nije nađena traži se ponovno nakon 6 h, 12 h, 24 h…
  najrjeđe jednom tjedno.
- **Nadogradnja**: u prvih N dana (zadano 7) nakon što datoteka stigne Locarr jednom dnevno traži bolji
  titl od onoga koji je preuzeo. Titl koji ste sami stavili na disk automatski se nikad ne mijenja.
- **Dnevno skeniranje** biblioteke pronađe titlove koji su već na disku (npr. `Film (2008).hr.srt`,
  `.hr.forced.srt`, `.en.sdh.srt`) i zapise u videu. Pokreće se i gumbom **Pretraži biblioteku**.

Provideri se pitaju po prioritetu (1 prvi). Provider koji ne odgovara čeka (1 min, pa 5 min i duže, do
12 h); odbijen ključ čeka 12 h; kad provider traži da se uspori, Locarr čeka koliko on kaže; potrošena
kvota znači da se do obnove ne preuzima s tog providera. Sve se vidi na kartici providera (zadnja greška,
preostala kvota i kada se obnavlja, preuzeto u zadnja 24 h), a **Pokušaj odmah** prekida čekanje.

## Bodovi

Svaki pronađeni titl dobiva 0–100 bodova za vašu datoteku:

| Što | Bodovi |
|---|---|
| Napravljen baš za tu datoteku (OpenSubtitles „hash") | 100 |
| Isti naslov (IMDb/TMDb) | 20 |
| Godina (film) / sezona i epizoda (serija) | 10 / 20 |
| Ista grupa izdanja (npr. `-LOCARR`) | 25 |
| Isti izvor (Blu-ray, WEB, HDTV, DVD) | 15 |
| Ista rezolucija / kodek / streaming servis | 8 / 4 / 5 |
| Pouzdan autor / popularan | +5 / +3 |
| Za gluhe kad ih ne tražite / strojni prijevod | −2 / −30 |

Automatski se uzima najbolji titl iznad **najmanje bodova** (zadano 60) koji Locarr za tu stavku još nije
preuzimao i koji niste odbacili. Titl drugog jezika, druge epizode ili pogrešne vrste (forsirani, za gluhe)
se ne uzima.

## Stranica filma i epizode

Film ima karticu **Titlovi**, a epizoda gumb s titlovima u retku epizode. Tu vidite:

- **Tražene jezike** i stanje svakoga: *Postoji*, *U videu* (zapis u datoteci), *Nedostaje*, *Nije potreban*.
- **Datoteke titlova**: jezik, format, odakle je (provider i bodovi ili „pronađen na disku") i alate:
  - **Pregled** prvih redaka s vremenima.
  - **Pomak**: ranije ili kasnije za zadani broj milisekundi.
  - **Broj sličica**: pretvorba titla napravljenog za npr. 25 fps na video od 23,976 fps.
  - **Sinkronizacija**: Locarr uspoređuje kada su reci na ekranu s time kada se u filmu govori (ili s
    ugrađenim titlom ili drugim titlom iste datoteke) i sam nađe pomak i razliku u broju sličica.
    Datoteku mijenja samo kad je podudaranje jasno; inače javi „nema jasnog podudaranja".
  - **Obriši**: titl ide u koš; preuzeti titl se može i odbaciti („nikad više ne preuzimaj"), pa Locarr
    traži drugi.

  Svaka izmjena (pomak, broj sličica, sinkronizacija, zamjena boljim titlom) prethodnu verziju šalje u
  **koš**, odakle se može vratiti.
- **Zapise u videu**: zvuk i titlovi s jezikom i oznakama (čita se iz MKV i MP4 datoteka).
- **Ručnu pretragu**: rezultati svih providera s bodovima i razlozima, oznakama (za gluhe, forsirani,
  već preuzet, odbačen). **Preuzmi** zamjenjuje titl tog jezika.

Na vremenskoj crti filma ili epizode stoji npr. „Titl hrvatski pronađen na opensubtitles, bodova 90,
spremljen kao Film (2008).hr.srt" ili „Još nema titla hrvatski — pretraženo providera: 2; najbolji je imao
45 bodova (najmanje 60)". Okvir **Zašto nije stiglo** ima i redak za titlove: nema profila, nema
uključenog providera, svi provideri čekaju (i zašto), još nije traženo ili koliko je puta traženo i kada
je iduća pretraga.

**Nedostaje → Titlovi** popisuje filmove i epizode kojima nedostaje jezik iz profila, s pretragom
odabranih ili svih te promjenom profila za odabrane.

## Imena datoteka

Titl se sprema pokraj videa pod istim imenom s jezikom:

- `Big Buck Bunny (2008) [Bluray-1080p][x264]-LOCARR.hr.srt`
- `… .hr.forced.srt` (forsirani), `… .en.hi.srt` (za gluhe)

Locarr titl pretvara u UTF-8 (prepoznaje i stare kodne stranice, npr. Windows-1250 za hrvatski,
Windows-1251 za ćirilicu, UTF-16), sređuje prijelome redaka i provjerava je li datoteka ispravan SRT,
ASS/SSA ili VTT. U **Mogućnostima** se može odabrati da se ASS/SSA/VTT uvijek spremaju kao SRT.

## Sinkronizacija i ffmpeg

Slika Locarra ima mali ffmpeg (samo dekoderi zvuka, oko 3,5 MB) za čitanje zvuka. Ako Locarr pokrećete
bez njega, sinkronizacija prema zvuku i ugrađenim titlovima nije dostupna i to piše na stranici; prema
drugom titlu iste datoteke i dalje radi. Drugi ffmpeg postavlja se varijablom `LOCARR_FFMPEG`.
U **Mogućnostima** se može uključiti automatsko usklađivanje svakog preuzetog titla (osim onih
napravljenih baš za tu datoteku).

## Ograničenja

- Podnapisi, Addic7ed i Titlovi.com nisu uključeni: nemaju javni API koji bi Locarr smio koristiti.
- Zapisi se čitaju samo iz MKV/WebM i MP4/MOV datoteka; za AVI i ostale Locarr ne zna što je unutra.
- Sinkronizacija nalazi jedan pomak (i broj sličica) za cijeli titl; titl kojem se pomak mijenja usred
  filma (drugačiji rez) treba ručno popraviti ili zamijeniti.
- RAR i 7z arhive s titlovima se ne raspakiravaju (ZIP da).
