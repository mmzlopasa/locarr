# Sadržaj za odrasle

Locarr može voditi i izbornu biblioteku za **legalan sadržaj za odrasle između punoljetnih osoba koje na to pristaju**:
studije (stranice) sa scenama po datumu, filmove i izvođače. Sve je **zadano isključeno** i odvojeno od ostatka biblioteke.

## 1. Uključivanje

1. Postavke → **Sadržaj za odrasle** → **Uključi sadržaj za odrasle**.
2. Potvrdite da je svaka osoba kojoj ćete dati pristup punoljetna. Bez te potvrde uključivanje ne prolazi.

Dok je isključeno, ništa za odrasle se nigdje ne prikazuje, a svaka adresa `/api/v1/adult/...` odgovara „nije pronađeno” (404),
čak i administratoru. Isključivanjem se ništa ne briše; podaci i datoteke ostaju za slučaj da ponovno uključite.

## 2. Dozvole

Uključivanje **nikome ne daje pristup**. Postavke → Korisnici → uredite korisnika → **Vidi sadržaj za odrasle**.

- Dozvola nije zadana ni za jednu ulogu, ni za postojeće upravitelje, ni za administratore: i sebi je morate dodijeliti.
- Tražitelji (npr. dječji računi) je nikad ne mogu imati; poslužitelj je briše i ako je netko pokuša postaviti.
- Pozivnice i prijava preko Streamera je nikad ne daju.
- Odjeljak **Odrasli** u izborniku vide samo računi s tom dozvolom dok je značajka uključena.

## 3. API ključevi

API ključ vidi sadržaj za odrasle samo ako (1) vlasnik ključa ima dozvolu i (2) ključ je stvoren s oznakom
**Uključi sadržaj za odrasle** (Račun → API ključevi). Postojeći ključevi i ključevi bez oznake ne vide ništa, ni postavke.

## 4. Izvori metapodataka

Locarr koristi samo etablirane baze s vlastitom moderacijom i provjerom dobi, i to s **vašim** ključem:

- **StashDB** (zajednička baza; ključ dobivate u svom StashDB profilu) — scene, studiji, izvođači.
- **ThePornDB** (ključ na theporndb.net) — scene, filmovi, stranice, izvođači.

Ključevi se provjeravaju kod izvora prije spremanja i spremaju šifrirano; nikad se ne vraćaju pregledniku.
Unosi koje izvor vodi kao obrisane ili sporne nikad se ne prikazuju ni ne uvoze.

## 5. Mape, indexeri i imena

- **Mape za odrasle** su odvojene od mapa za filmove, serije i glazbu (npr. `/data/media/adult`).
- **Indexeri**: kategorije 6000 (XXX) se ne koriste dok za pojedini indexer ne uključite „Indexeri s kategorijama za odrasle”.
  Izdanja iz tih kategorija nikad se ne pojavljuju u običnim pretragama. Koji javni indexeri rade, vidi odjeljak 11.
- **Imena**: predlošci za mapu i datoteku scene i filma, s oznakama `{Studio}`, `{Date}`, `{Title}`, `{Performers}`, `{Year}`,
  `{Quality}`, `{ReleaseGroup}`. Zadano: `{Studio}/{Studio} - {Date} - {Title} [Izvođači] {Quality}`.

## 6. Dodavanje i praćenje

Odrasli → **Dodaj**: odaberite izvor i vrstu (studio, scena, film, izvođač, galerija) i pretražite.

- **Studio**: dodaju se njegove najnovije scene; praćeni studio nove scene (od dana praćenja) traži sam.
- **Izvođač**: „Prati ovog izvođača” — nove scene s njim postaju tražene.
- Svaka scena i film imaju „Zašto nije ovdje?”, vremensku crtu, ručnu pretragu i „Traži odmah”, kao filmovi.

### Pretraga na Dodaj

- Scene i filmovi dolaze **najnoviji prvi**; Locarr to traži od samog izvora, pa prva stranica stvarno ima najnovija izdanja.
  **Poredak** nudi još Najstarije, Najbolje podudaranje i Naslov; izbor se pamti u vašem pregledniku.
  ThePornDB nema dokumentiran poredak za najstarije i naslov, pa se tada slaže samo ono što je učitano.
- **Učitaj još** dohvaća sljedeću stranicu.
- Svaki rezultat ima malu sličicu (zamućenu po vašoj postavci), studio, datum, izvođače i oznaku **U biblioteci**.
- Označite više rezultata i kliknite **Dodaj odabrano**: svi se dodaju s profilom i mapom odabranima iznad.

### Popisi

Scene, Filmovi, Traženo i popisi na stranici studija i izvođača imaju filtar po naslovu ili studiju, poredak (datum izlaska,
datum dodavanja, naslov), stanje (Traženo, Preuzima se, U biblioteci, Nedostaje), praćenje i prikaz kao mreža ili popis.
Na kartici je oznaka stanja, a za scenu u biblioteci i kvaliteta datoteke. „Traženo” znači praćeno bez datoteke,
„Nedostaje” nepraćeno bez datoteke.

## 7. Uklanjanje i „Ne zanima me”

Uklanjati mogu samo upravitelji biblioteke s dozvolom za sadržaj za odrasle.

- **Scena ili film:** na stranici stavke **Ukloni**. U potvrdi:
  - **Ne zanima me: ne dodaj ponovno** (zadano uključeno) — osvježavanje praćenog studija ili izvođača tu scenu preskače.
  - **Obriši i datoteku** (zadano isključeno) — datoteka ide u **koš za smeće**, odakle se može vratiti. Bez te kvačice
    datoteka ostaje na disku.
- **Studio:** potvrda kaže koliko scena ima i nudi **Zadrži scene** (zadano; ostaju u biblioteci bez stranice studija i više
  se ne osvježavaju) ili **Ukloni i scene** (uz iste izbore „Ne zanima me” i „Obriši i datoteku”).
- **Izvođač:** isto, a zadano se zaboravlja samo izvođač, scene ostaju.
- **Više odjednom:** na popisu kliknite **Odaberi**, označite kartice (ili **Odaberi sve**) i odaberite **Prati**, **Ne prati**
  ili **Ukloni**.

Popis „Ne zanima me” je u Postavke → Sadržaj za odrasle → **Ne zanima me**. **Poništi** uklanja unos; sljedeće osvježavanje
može scenu ponovno dodati. Ako scenu dodate sami preko Dodaj, unos se uklanja i bez toga.

## 7a. Blokirani pojmovi

Postavke → Sadržaj za odrasle → **Blokirani pojmovi**. Izdanja (pretraga, RSS, ručni grab), uvozi i unosi metapodataka koji
sadrže neki od pojmova odbijaju se s razlogom `blocked_tag`.

- **Uvijek blokirano: sadržaj s maloljetnicima.** Mali stalni popis (underage, under age, child, children, preteen,
  pre teen, jailbait, lolita, loli, shota, pthc) vrijedi uvijek, prikazan je kao nepromjenjive oznake i ne može se ukloniti
  ni dodati na vlastiti popis.
- **Vaši pojmovi.** Popis je na početku prazan: sve ostalo je dopušteno dok sami ne dodate pojam. Što uklonite, ostaje
  uklonjeno (ni ponovno uključivanje, ni ponovno pokretanje, ni nadogradnja ništa ne vraćaju).
- **Kako se uspoređuje.** Samo cijele riječi, bez obzira na velika i mala slova; točke, podvlake i crtice broje se kao razmaci,
  a pojam od više riječi hvata se i napisan zajedno (`under.age`, `UnderAge`). „Childs” ili „Lolipop” se ne hvataju.
  Umjetničko ime izvođača koje metapodaci navode za tu scenu (npr. „Lolita Cherry”) ne aktivira stalni popis.
- Kad je izdanje odbijeno, „Zašto nije ovdje?” i odluka na vremenskoj crti kažu koji je pojam pogođen i je li s vašeg popisa
  ili sa stalnog; administrator klikom na oznaku otvara ovaj popis.

Nadogradnja s v0.6.1: ranije zadano upisani pojmovi uklonjeni su s popisa (i „teen”/„teens” koji su hvatali studije poput
„Broken Teens”); pojmovi koje ste dodali sami ostaju.

## 8. Kvaliteta

Javne stranice scene često imenuju bez rezolucije i izvora (`Studio.24.01.15.Izvođač.Naslov.XXX.MP4-GRUPA`), pa je kvaliteta
**Unknown**, a profili uvezeni iz drugih aplikacija (i Locarrov „Any”) Unknown ne dopuštaju. Rezultat je bio „kvaliteta nije
dopuštena” za gotovo svako izdanje.

- Locarr sada čita i `SD` (kao 480p), `FullHD`/`Full HD`/`FHD` (1080p), `UHD`/`4K` (2160p) i dimenzije poput `1920x1080`.
- Profil **Any (Unknown allowed)** dopušta svaku kvalitetu, i Unknown. Zadan je za nove stavke za odrasle, osim ako ste
  pod Zadano sami odabrali drugi profil. Ime je namjerno neutralno jer se vidi na popisu profila svima.
- Postojeće stavke zadržavaju svoj profil. U Postavke → Sadržaj za odrasle → **Profil kvalitete za sadržaj za odrasle**
  vidi se koliko stavki, praćenih studija i izvođača je na drugom profilu, a gumb **„Koristi Any (Unknown allowed) za sve
  stavke za odrasle”** ih prebacuje jednim klikom (mijenja se samo profil).
- „Zašto nije ovdje?” sada kaže koja je kvaliteta nađena i koji ju je profil odbio, npr. „Unknown nije dopušteno u HD-1080p”.

## 9. Vremenska crta i brisanje

Jedan ciklus pretrage (svi oblici upita iz odjeljka 11) prikazuje se kao **jedan redak** „Automatska pretraga — rezultata: N”
s brojem po indexeru zbrojenim preko upita i odlukom ispod; strelica otvara upite redom kojim su isprobani i odbijena izdanja.
Upravitelji biblioteke s dozvolom za sadržaj za odrasle mogu obrisati redak ili cijeli ciklus (ikona koša) i **„Očisti
povijest ove stavke”** (potvrda kaže koliko redaka odlazi). Brišu se samo zapisi vremenske crte (pretrage nestaju i iz
dnevnika pretraga); datoteke, blokirani pojmovi, preuzimanja i raspored pretraga ostaju. Bez dozvole za sadržaj za odrasle
ti zapisi se ne vide i ne mogu obrisati, ni preko API-ja.

## 10. Privatnost

- Slike se spremaju na poslužitelj (`/config/adult/images`) i poslužuju samo korisnicima s dozvolom, s `Cache-Control: private, no-store`;
  nema javnih adresa slika.
- U webu su slike **zamućene** dok ih ne kliknete; zamućivanje možete isključiti za sebe (Odrasli → „Zamuti slike”).
  Precrtano oko znači samo „zamućeno”. Kad slike nema (izvor je nema ili je nije bilo moguće dohvatiti), vidi se neutralno
  polje s početnim slovom studija ili imena.
- Slike s ThePornDB-a do v0.6.2 nisu stizale (njihov poslužitelj slika koristi novi Let's Encryptov korijen koji slika
  Locarra nije poznavala). Nedostajuće slike dohvaćaju se pri sljedećem osvježavanju studija ili izvođača (svakih 12 sati ili
  gumbom **Potraži nove scene**).
  Na pregledu i drugim stranicama nema slika za odrasle.
- Sadržaj za odrasle nije u Otkrivanju, zahtjevima, kalendaru i iCal feedu, povijesti, ploči „Zašto nije stiglo?”, statistici
  ni Streamero rutama. U `/api/stats` je samo ukupan broj (`adultItems`) i to samo ako ga uključite.
- Obavijesti: kanal dobiva događaje `adult.*` samo ako ih izričito odaberete; osobni kanali i obavijesti u aplikaciji ih nikad ne dobivaju.
- Streamero se o uvozima obavještava samo ako uključite „Javi Streameru za uvoze” (za zasebnu biblioteku za odrasle u Streameru).
- Mobilne aplikacije zasad ne prikazuju sadržaj za odrasle.
- Dnevnici poslužitelja (vidi ih samo administrator) mogu sadržavati imena izdanja.

## 11. Javni indexeri za odrasle

Pretraga za odrasle radi i samo s Locarrovim ugrađenim javnim stranicama, bez ikakvog računa:

| Indexer | Što ima za odrasle | Napomena |
|---|---|---|
| The Pirate Bay | kategorija Porn (filmovi, isječci, HD, 4K…) | najbolji izvor starijih scena |
| Knaben | kategorija XXX (pretraga preko više trackera) | XXX se traži samo za pretrage za odrasle |
| BitSearch | kategorija XXX | starije scene |
| XXXClub | samo za odrasle, scene po imenu `Studio YY MM DD` | najbolji izvor novih scena; dodaje se kao gotov za odrasle |
| Sukebei | samo za odrasle, uglavnom japanski sadržaj | za zapadne scene rijetko nađe nešto |
| 1337x | kategorija XXX | iza provjere preglednika; s poslužitelja u podatkovnom centru često traži čovjeka |

EZTV, YTS, Nyaa, SubsPlease, Tokyo Toshokan i LimeTorrents nemaju kategoriju za odrasle. Ne nude se u popisu
„Indexeri s kategorijama za odrasle”, nego su navedeni ispod njega kao indexeri koji ne mogu tražiti sadržaj za odrasle,
i pretraga za odrasle ih preskače.

**Što označiti:** Postavke → Sadržaj za odrasle → „Indexeri s kategorijama za odrasle”: The Pirate Bay, Knaben i BitSearch
(i 1337x ako vam radi). U Indexeri → Dodaj indexer → Ugrađeni uključite **XXXClub** i po želji **Sukebei**; one su
odmah označene za odrasle i nikad ne odgovaraju na obične pretrage.

**Kako Locarr traži scenu.** Javne stranice istu scenu zovu na više načina (`Studio.20.08.19.Izvođač.Naslov…`,
`[Studio] Izvođač - Naslov (2020)`, `[Studio] Naslov (19.08.2020)`, `Izvođač - Naslov`). Zato Locarr redom pokušava:

1. studio i datum („Broken Teens 20 08 19”),
2. studio napisan zajedno i datum s točkama („BrokenTeens 20.08.19”),
3. izvođač i riječi naslova,
4. sam naslov,

i staje čim nađe izdanje koje može uzeti. Izdanje mora biti ta scena: sa studijem i datumom mora biti isti studio i dan i,
ako ime nakon datuma nešto kaže, izvođač ili većina naslova (studio istog dana objavljuje više scena); bez datuma ime mora
imati naslov uz izvođača ili studio, a godina u imenu mora odgovarati.


## 12. Galerije (skupovi slika)

Osim videa Locarr preuzima i **skupove slika**. Galerija je stavka kao scena: ima studio, datum, naslov i izvođače,
traži se, preuzima, uvozi i ima vremensku crtu i „Zašto nije ovdje?”.

**Kako nastaje galerija.** StashDB i ThePornDB nemaju skupove slika, pa se galerija ne traži na Dodaj nego:

- **iz scene**: na stranici scene **Traži skup fotografija** (ili pri dodavanju scene označite **Traži i skup fotografija**);
  naslov, studio, datum i izvođači preuzimaju se od scene, a galerija je vezana uz nju;
- **iz izvođača**: na stranici izvođača **Dodaj skup fotografija**, a **Traži i skupove fotografija** znači da svaka nova scena
  praćenog izvođača dobiva i svoju galeriju;
- **ručno**: Odrasli → Galerije → **Nova galerija** (ili Dodaj → Vrsta: Galerija): naslov, studio, datum i izvođači.

**Pretraga.** Locarr traži kao za scenu (studio i datum, izvođač i naslov…), prvo s riječi „imageset”. Uzima samo izdanje koje je
skup slika: kategorija XXX/ImageSet (6060) ili ime koje to kaže (`IMAGESET`, `pics`, `photos`, `x150`…). Video (1080p, mp4…)
se za galeriju odbija, a skup slika za scenu. Kvaliteta videa ne vrijedi; vrijedi samo raspon veličine (Postavke → Sadržaj za
odrasle, zadano 1 MB – 20 GB). Blokirani pojmovi vrijede isto kao za scene.

**Indexeri sa skupovima slika:** The Pirate Bay (Porn → Pictures), Knaben (XXX → ImageSet), XXXClub (IMAGESET) i Torznab/Newznab
indexeri s kategorijom 6060. BitSearch ih ima pod XXX, a Locarr ih prepoznaje po imenu.

**Uvoz.** Preuzimanje može imati slike (jpg, jpeg, png, webp, gif, avif, heic) ili zip/rar arhivu sa slikama. Arhiva se
raspakira sigurno: unos s opasnim imenom (npr. `../`) odbija cijelu arhivu, veze i datoteke koje nisu slike se preskaču,
a prevelike arhive (više od 10 000 slika, 200 MB po slici, 32 GB ukupno ili sumnjivo dobro sažete) se ne raspakiraju.
Arhiva ostaje u mapi preuzimanja radi seedanja. Slike idu u **jednu mapu po galeriji** izravno u korijenskoj mapi za odrasle:

```
/data/media/adult/
  Studio A - 2024-01-15 - Naslov scene [Izvođač Jedan]/
    0001.jpg
    0002.jpg
    …
    cover.jpg
```

Ime mape slijedi predložak „Mapa galerije” (zadano `{Studio} - {Date} - {Title}{ [Performers]}`). Slike se preimenuju u
`0001`, `0002`… redom skupa (isključite „Preimenuj slike” za izvorna imena), a `cover` je naslovnica skupa ili prva slika.
Ako postoji mapa istog imena s drugim sadržajem, ne dira se: galerija dobiva ime s „ (2)”. Novi skup iste galerije stari šalje
u koš. Streamero se obavještava kao za scene (samo ako je to uključeno).

**Pregled.** Stranica galerije ima mrežu sličica (zamućenih po vašoj postavci; klik otkriva, drugi klik otvara) i pregled
preko cijelog zaslona: strelice ili tipke ← →, Escape zatvara. Slike se poslužuju samo onima s dozvolom za sadržaj za odrasle.

**Uklanjanje.** Kao za scenu: datoteke samo u koš i samo uz izbor, „Ne zanima me” znači da se skup te scene više ne dodaje sam
(ručno dodavanje to poništava). Skupne radnje rade i na kartici Galerije.
