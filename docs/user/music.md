# Glazba

Locarr za glazbu prati izvođače s MusicBrainza i njihove albume: sam traži albume koji nedostaju, bira
izdanje, šalje ga download klijentu, uparuje preuzete datoteke s pjesmama albuma po tagovima i trajanju,
uvozi ih pod urednim imenima s omotom i sve to zapisuje na vremensku crtu albuma.

## Prije prvog izvođača

1. **Postavke → Mape za medije**: dodajte root mapu za glazbu, npr. `/data/media/music`. Neka preuzimanja
   i glazba budu pod istim `/data`, da uvoz može hardlinkati umjesto kopirati.
2. **Indexeri** i **Download klijenti** (vidi `download-clients.md`). Za glazbu u klijentu možete zadati
   vlastitu kategoriju (npr. `locarr-music`).
3. Po želji **Postavke → Profili metapodataka** i **Profili kvalitete** (zadani glazbeni profili su
   „Lossless" — FLAC 24-bit, FLAC, ALAC — i „Lossy" — MP3 320, V0, AAC, Opus…).
4. Po želji **Postavke → Upravljanje medijima → Imena i uvoz glazbe**: kontakt za MusicBrainz (vaš e-mail ili
   web adresa). MusicBrainz traži da se svaka aplikacija predstavi; bez vlastitog kontakta šalje se stranica
   projekta. Ključ nije potreban.

## Dodavanje izvođača

**Glazba → Dodaj izvođača** traži na MusicBrainzu po imenu ili po ID-ju (`mbid:5a8e07d5-…`). Više izvođača
istog imena razlikuje se po **pojašnjenju** (npr. „electronic duo" i „jazz trio from Zagreb"), vrsti, zemlji
i godinama rada. Nakon odabira birate:

- **Root mapu** — izvođač dobiva mapu po predlošku (zadano `{ArtistName}`).
- **Profil kvalitete** — koje kvalitete smiju stići i do koje se nadograđuje.
- **Profil metapodataka** — koji albumi izvođača ulaze u biblioteku (vidi niže).
- **Prati odmah** — sve albume, samo buduće, samo najnoviji ili ništa.
- **Novi albumi** — što s albumima koji se kasnije pojave na MusicBrainzu: prati ih, prati samo one koji tek
  izlaze, ili ih ne prati.
- **Prati izvođača** i **Traži odmah** — pretraga praćenih albuma koji su već izašli kreće odmah.

Albumi se pojave odmah. Izdanja albuma i popisi pjesama učitavaju se u pozadini, jer MusicBrainz dopušta
jedan zahtjev u sekundi (izvođač s 50 albuma treba oko minute). Stranica albuma učita svoj popis pjesama
odmah ako još nije stigao.

## Profili metapodataka

**Postavke → Profili metapodataka** određuju koje grupe izdanja izvođača Locarr drži kao albume:

- **Glavne vrste**: album, EP, singl, emitiranje, ostalo.
- **Dodatne vrste**: studijski (album bez dodatne vrste), kompilacija, uživo, glazba iz filma, remiks, DJ miks,
  demo… Album prolazi samo ako je odabrana *svaka* dodatna vrsta koju ima — „Uživo + remiks" treba oboje.
- **Statusi izdanja**: službeno, promotivno, neslužbeno (bootleg)… Album bez ijednog izdanja odabranog statusa
  ne ulazi u biblioteku.

Zadani profili: **Standard** (albumi i EP-ovi, studijski, službeno), **Samo albumi** i **Sve**. Promjena
profila osvježi njegove izvođače: novi albumi se pojave, a oni koji više ne prolaze nestanu — osim albuma koji
već imaju datoteke, oni ostaju (označeni „izvan profila"). Locarr pri tome nikad ne briše datoteke.

## Izvođač i album

Stranica izvođača pokazuje albume grupirane po vrsti (albumi, EP-ovi, singlovi, uživo, kompilacije) s
kvačicom za praćenje, datumom izlaska, brojem pjesama koje imate, kvalitetom i stanjem (preuzeto,
djelomično, nedostaje, preuzima se, nije izašlo, ne prati se). Tu su i **Traži albume koji nedostaju**,
**Osvježi s MusicBrainza**, uređivanje (profili, novi albumi, oznake) i uklanjanje (mapa izvođača po izboru
ide u koš, nikad se ne briše odmah).

Stranica albuma ima:

- **Izdanje** — koje se izdanje (zemlja, format, broj pjesama, deluxe…) koristi za uparivanje. „Automatski"
  znači da bira Locarr: službeno, najčešći broj pjesama (standardno, ne deluxe), digitalno ili CD, vaša
  regija ili svjetsko, najranije. Ručno odabrano izdanje Locarr više ne mijenja.
- **Pjesme** — popis pjesama s datotekom, kvalitetom (npr. FLAC 24-bit, 96 kHz) i veličinom; pjesme bez
  datoteke označene su „nedostaje".
- **Izdanja** — ručna pretraga svih indexera s odlukom i razlozima za svako izdanje; „Preuzmi" ga šalje klijentu.
- **Uvezi mapu** — ručni uvoz bilo koje mape s audio datotekama (vidi niže).
- **Povijest** — ladica s vremenskom crtom albuma i odgovorom na „Zašto nije stiglo?".

## Kako Locarr traži

Album se traži kad su praćeni i album i izvođač i kad je album izašao. Pretraga ide po izvođaču i albumu
(Newznab/Torznab nemaju parametar za MusicBrainz ID, pa se šalju imena), odmah nakon dodavanja, zatim po
rasporedu s odmakom (30 min, 1 h, 2 h… najviše jednom dnevno) i kroz RSS: novo izdanje čiji izvođač (ili drugo
ime izvođača) i naslov albuma odgovaraju praćenom albumu procjenjuje se odmah. Odluka koristi profil
kvalitete albuma: npr. „Lossless" odbija MP3 izdanje s razlogom „kvaliteta nije u profilu". Album kojem
nedostaju pjesme traži se dalje; potpun album traži se samo dok mu najlošija datoteka nije na granici profila.

## Uvoz i uparivanje

Kad preuzimanje završi, Locarr pročita tagove svih audio datoteka (FLAC, MP3 s ID3v2.3/2.4, M4A s AAC ili
ALAC, Ogg Vorbis, Opus) i usporedi ih s pjesmama odabranog izdanja i sličnih izdanja:

- **MusicBrainz ID-jevi** u tagovima (snimka, izdanje) uparuju sigurno.
- Bez njih se uspoređuju **naslovi** (bez dijakritika i dodataka poput „Remastered"), **disk i broj pjesme**
  (iz tagova, mapa „CD2"/„Disc 2" ili imena „03 Naslov") i **trajanje** (do 5 sekundi ili 5 % je isto).
- Rezultat su **bodovi od 0 do 100**. Automatski uvoz treba barem 70 (postavka). Ako drugo izdanje bolje
  odgovara datotekama (npr. deluxe s bonus diskom), album prelazi na to izdanje.
- **Nedostaju pjesme**: uvoz ne uspijeva i na vremenskoj crti piše koliko ih nedostaje — osim ako ste
  uključili „Uvezi albume kojima nedostaju pjesme". Album se tada i dalje traži.
- **Višak datoteka** (skrivene pjesme, bonus koji izdanje nema) se preskače i prijavljuje.
- **Cue + jedna datoteka** (cijeli album kao jedna datoteka s .cue listom) nije podržano: uvoz to javi, a
  album treba prvo razrezati na pjesme.
- Svaka pjesma se uvozi samo ako je nova ili bolje kvalitete; stara datoteka ide u **koš**.

Datoteke se hardlinkaju (kopiraju kad hardlink nije moguć), pa preuzimanje nastavlja seedati. Imena prema
predlošku: mapa albuma `{AlbumTitle} ({ReleaseYear})`, pjesma `{TrackNumber} - {TrackTitle}`, a na albumima s
više diskova `{DiscNumber}-{TrackNumber} - {TrackTitle}` ili, uz „Mapa za svaki disk", podmape `Disc 1`,
`Disc 2`. Omot `cover.jpg` dolazi iz preuzimanja (cover/folder/front.jpg), a ako ga nema, s Cover Art Archivea.

**Ručni uvoz** (album → Uvezi mapu): upišite mapu, „Provjeri podudaranje" pokaže bodove za svako izdanje,
koja je datoteka na kojoj pjesmi, razliku u trajanju, što nedostaje i što je višak. Možete odabrati izdanje,
dopustiti djelomični uvoz ili uvesti i ispod praga („Ipak uvezi").

**Pisanje tagova** („Upiši ispravljene tagove u FLAC datoteke") je zadano isključeno. Kad je uključeno, FLAC
datoteke se kopiraju (nikad hardlinkaju, da se torrent ne pokvari) i u kopiju se upišu naslov, izvođač,
album, brojevi i MusicBrainz ID-jevi izdanja. Ostali formati se ne diraju.

## Uvoz postojeće biblioteke

**Glazba → Uvezi postojeće**: odaberite root mapu i „Pregledaj". Locarr pročita mape `Izvođač/Album/…`,
tagove prve datoteke svakog albuma i za svakog izvođača predloži MusicBrainz izvođača (po ID-ju iz tagova,
inače po imenu — jedan zahtjev po izvođaču, pa velika biblioteka treba vremena). Provjerite i po potrebi
promijenite izbor, odaberite profile i „Uvezi odabrane". Izvođači se dodaju s postojećom mapom, a datoteke se
u pozadini pridružuju albumima **tamo gdje jesu** — ništa se ne premješta ni preimenuje. Povijest svakog
albuma pokazuje koliko je datoteka pronađeno i s kojim bodovima; mape koje ne odgovaraju ni jednom albumu
(npr. jer ih profil metapodataka isključuje) zapisane su na vremenskoj crti izvođača.

## Otkrivanje izvođača

**Glazba → Otkrij izvođače** predlaže izvođače slične onima koje već imate. Locarr za svakog izvođača iz
biblioteke pita nekoliko izvora, spoji odgovore i izostavi ono što već imate i ono što ste odbili.

**Izvori** (Postavke → Metapodaci → Otkrivanje glazbe):

- **Last.fm** — slični izvođači i oznake (žanrovi) prema slušateljima Last.fma. Treba besplatan API ključ:
  prijavite se na last.fm, otvorite <https://www.last.fm/api/account/create>, upišite ime aplikacije (npr.
  „Locarr kod kuće") i kopirajte *API key* u polje „Last.fm API ključ". Ključ se provjeri kod Last.fma prije
  spremanja, sprema se šifrirano i više se ne prikazuje.
- **ListenBrainz** — izvođači koje ljudi slušaju u istim sesijama. Ne treba ključ. Daje i omot najslušanijeg
  albuma, koji se prikazuje kao slika izvođača.
- **MusicBrainz** — veze među izvođačima (članovi benda, suradnje, prateći glazbenici). Ne treba ključ; ima
  najmanju težinu jer suradnja ne znači da zvuče slično.
- **Spotify** — povezani izvođači. Spotify ih od studenog 2024. ne daje novim aplikacijama (ni onima u
  „development" načinu, za koji vlasnik treba i Premium). Ako ipak imate aplikaciju s proširenim pristupom,
  napravite je na <https://developer.spotify.com/dashboard> i upišite *Client ID* i *Client secret*. „Provjeri"
  kaže radi li: „restricted" znači da Spotify odbija povezane izvođače, pa se Spotify koristi samo za poveznice.

Svaki izvor se može isključiti. „Ponovno pitaj nakon" (zadano 7 dana) određuje koliko su stari odgovori
dobri; novi izvođači se pitaju odmah nakon dodavanja, a zadatak „Otkrivanje glazbe" (Sustav → Zadaci) radi
svaki dan. Izvori se pitaju polako i pristojno (ListenBrainz i MusicBrainz jedan zahtjev u sekundi), pa za
veliku biblioteku prvi krug traje: 1000 izvođača ≈ 17 minuta. Prijedlozi se pojavljuju postupno.

**Kako se boduje.** Svaki odgovor ima sličnost (0–1) i pouzdanost da je to pravi izvođač na MusicBrainzu:
Last.fm i ListenBrainz najčešće daju MusicBrainz ID; kad izvor da samo ime, Locarr ga traži na MusicBrainzu i
uzima ga samo ako postoji izvođač točno tog imena — takvi prijedlozi imaju oznaku **„Pronađen po imenu"**
(provjerite da je to taj izvođač prije dodavanja). Težina izvora × sličnost × pouzdanost; više izvora koji se
slažu i više vaših izvođača koji upućuju na isti prijedlog podižu bodove (najviše 100 %). Ispod imena piše
zašto: „Sličan izvođačima A, B i još 3 · Oznake: shoegaze, dream pop".

**Filtri:** „Na temelju" (samo odabrani izvođači iz biblioteke; „Još ovakvih" na stranici izvođača otvara
isto), oznaka, najmanje podudaranje i izvori. **Dodaj** otvara iste opcije kao dodavanje izvođača (root
mapa, profil kvalitete i metapodataka, praćenje, traži odmah); kvačicama na omotima odaberete više izvođača i
dodate ih odjednom. **Ne zanima me** sakrije izvođača zauvijek; popis „Ne zanima me" na dnu stranice ga vraća.

Stranica svakog izvođača ima odjeljak **Slični izvođači** (uključujući one koje već imate, s poveznicom).
Članovi obitelji koji smiju tražiti glazbu vide iste prijedloge na **Otkrivanju** („Izvođači koji bi ti se
mogli svidjeti") s gumbom **Zatraži**; njima se ne prikazuju putanje ni ID-jevi biblioteke.

Podaci dolaze s Last.fma, ListenBrainza, MusicBrainza i Spotifyja prema njihovim uvjetima; svaki prijedlog ima
poveznice natrag na njihove stranice, a slike su samo omoti s Cover Art Archivea.

## Nedostaje i kalendar

**Nedostaje → Albumi** pokazuje praćene albume koji su izašli, a nedostaju im pjesme (ili su ispod granice
kvalitete), s pretragom za odabrane. **Kalendar** prikazuje izlaske albuma uz datume filmova, a iCal feed
ih sadrži također.

## Zašto album nije stigao?

Otvorite **Povijest** na albumu. Gornji okvir daje jedan odgovor: preuzima se, uvoz nije uspio (s razlogom,
npr. „nedostaju 2 od 8 pjesama" ili „bodovi 52 od 100"), ne prati se, još nije izašao, nema indexera ili
klijenta, sva izdanja su odbijena (s najčešćim razlozima), ništa nije nađeno, ili „N od M pjesama je u
biblioteci". Ispod su svi koraci: pretrage po indexerima, odluke s odbijenim izdanjima, slanje klijentu,
napredak, uparivanje s bodovima, uvoz, preimenovanje, omot i nadogradnje.
