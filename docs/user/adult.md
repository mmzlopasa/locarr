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
  Izdanja iz tih kategorija nikad se ne pojavljuju u običnim pretragama. Koji javni indexeri rade, vidi odjeljak 9.
- **Imena**: predlošci za mapu i datoteku scene i filma, s oznakama `{Studio}`, `{Date}`, `{Title}`, `{Performers}`, `{Year}`,
  `{Quality}`, `{ReleaseGroup}`. Zadano: `{Studio}/{Studio} - {Date} - {Title} [Izvođači] {Quality}`.

## 6. Dodavanje i praćenje

Odrasli → **Dodaj**: odaberite izvor i vrstu (studio, scena, film, izvođač) i pretražite.

- **Studio**: dodaju se njegove najnovije scene; praćeni studio nove scene (od dana praćenja) traži sam.
- **Izvođač**: „Prati ovog izvođača” — nove scene s njim postaju tražene.
- Svaka scena i film imaju „Zašto nije ovdje?”, vremensku crtu, ručnu pretragu i „Traži odmah”, kao filmovi.

## 7. Blokirani pojmovi

Postavke → Sadržaj za odrasle → **Blokirani pojmovi**. Izdanja (pretraga, RSS, ručni grab), uvozi i unosi metapodataka koji
sadrže neki od pojmova odbijaju se s razlogom `blocked_tag`. Pri prvom uključivanju popis se puni konzervativnim pojmovima
koji se često koriste za nezakonit sadržaj ili sadržaj bez pristanka; možete dodavati svoje. Ako popis ispraznite, i dalje
vrijede zadani pojmovi.

## 8. Privatnost

- Slike se spremaju na poslužitelj (`/config/adult/images`) i poslužuju samo korisnicima s dozvolom, s `Cache-Control: private, no-store`;
  nema javnih adresa slika.
- U webu su slike **zamućene** dok ih ne kliknete; zamućivanje možete isključiti za sebe (Odrasli → „Zamuti slike”).
  Na pregledu i drugim stranicama nema slika za odrasle.
- Sadržaj za odrasle nije u Otkrivanju, zahtjevima, kalendaru i iCal feedu, povijesti, ploči „Zašto nije stiglo?”, statistici
  ni Streamero rutama. U `/api/stats` je samo ukupan broj (`adultItems`) i to samo ako ga uključite.
- Obavijesti: kanal dobiva događaje `adult.*` samo ako ih izričito odaberete; osobni kanali i obavijesti u aplikaciji ih nikad ne dobivaju.
- Streamero se o uvozima obavještava samo ako uključite „Javi Streameru za uvoze” (za zasebnu biblioteku za odrasle u Streameru).
- Mobilne aplikacije zasad ne prikazuju sadržaj za odrasle.
- Dnevnici poslužitelja (vidi ih samo administrator) mogu sadržavati imena izdanja.

## 9. Javni indexeri za odrasle

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

