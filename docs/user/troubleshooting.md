# Kad nešto ne stigne: ploča, zdravlje i logovi

Ovaj vodič objašnjava kako pročitati ploču **„Zašto nije stiglo?"**, što znači svaka provjera u
**Sustav → Zdravlje** i kako je riješiti, gdje su logovi i kako Locarr spojiti na nadzornu ploču.

---

## Ploča „Zašto nije stiglo?"

**Aktivnost → Zašto nije stiglo?** prikazuje sve što pratiš, a još nemaš: filmove, epizode koje
su emitirane, izdane albume i datoteke kojima nedostaje titl. Stavke su složene u grupe po
**razlogu** — istom odgovoru koji vidiš u povijesti pojedinog naslova. Grupe s problemom koji
traži tvoju radnju (crveno) su na vrhu, zatim upozorenja (žuto), pa ono što samo čeka (plavo).

Svaka grupa pokazuje koliko je filmova, epizoda, albuma i titlova u njoj, i poveznicu **Popravi**
koja vodi na stranicu gdje se problem rješava. Otvori grupu za popis naslova; označi ih i odaberi
skupnu radnju.

| Razlog | Što znači | Što napraviti |
|---|---|---|
| **Još nije dostupno** | film još nije izašao do minimalne dostupnosti (u kinima / izdano) | ništa — pretraga počinje sama; po potrebi promijeni minimalnu dostupnost filma |
| **Nema indexera za ovu vrstu** | nijedan uključeni indexer nije za automatsku pretragu filmova/serija/glazbe | **Indexeri**: uključi automatsku pretragu ili kategorije te vrste |
| **Nema download klijenta** | nijedan klijent nije uključen | **Download klijenti**: dodaj ili uključi klijent |
| **Još nije traženo** | sljedeća automatska pretraga je zakazana | ništa, ili **Pretraži sada** |
| **Sve odbijeno** | pretraženo N puta; izdanja postoje, ali ih je profil odbio. Ispod su **zbrojeni najčešći razlozi** (npr. „prevelika datoteka", „jezik nije željen") za cijelu grupu | prilagodi profil kvalitete (veličine, jezici, custom formati) ili **Promijeni profil** |
| **Ništa nije nađeno** | indexeri nisu vratili nijedno izdanje | provjeri kategorije indexera, dodaj indexer; za stare ili rijetke naslove to je normalno |
| **Čeka profil kašnjenja** | izdanje je nađeno, ali profil kašnjenja čeka bolje do navedenog vremena | ništa, ili smanji kašnjenje |
| **Preuzima se X %** | preuzimanje je u tijeku | ništa |
| **Zapelo** | torrent ne napreduje i nema seedera | **Blokiraj i ponovi** — preuzimanje se makne iz klijenta (podaci ostaju), izdanje se blokira i traži se drugo |
| **Preuzeto, datoteke nisu vidljive** | klijent je završio, ali Locarr ne vidi tu putanju | popravi mapiranje putanja (vidi [ispod](#remote-path-mapping)), pa **Ponovi uvoz** |
| **Uvoz nije uspio** | preuzimanje je gotovo, uvoz nije. Prikazan je točan razlog, a poveznica vodi na popravak: mapiranje putanja, prava, kategorija klijenta, prostor na disku ili ručni uvoz | popravi uzrok, pa **Ponovi uvoz** |
| **Preuzimanje nije uspjelo** | klijent je javio grešku | **Blokiraj i ponovi** |
| **U biblioteci, ispod granice** | (albumi) dio pjesama postoji, Locarr i dalje traži cijeli album | ništa |
| **Titl nije tražen / nije nađen** | datoteci nedostaje titl na željenom jeziku | **Pretraži sada** ili ručna pretraga na stranici naslova |
| **Nema providera titlova / kvota potrošena** | nijedan provider nije uključen, ili su svi potrošili dnevnu kvotu | **Postavke → Titlovi** |

Skupne radnje:
- **Pretraži sada** — pretraga odmah (preskače čekanje između pretraga).
- **Promijeni profil** — film dobiva novi profil kvalitete; za epizodu se mijenja profil cijele
  serije, za album profil izvođača.
- **Blokiraj i ponovi** — makne zadnje preuzimanje iz klijenta (bez brisanja podataka), blokira
  izdanje i traži sljedeće.
- **Ponovi uvoz** — gotovo preuzimanje uvozi se ponovno; ako datoteke još nisu bile vidljive,
  Locarr odmah ponovno pogleda klijente.
- **Prestani pratiti** — naslov više nije na ploči.

Ploča se cijela preračuna pri pokretanju i svakih 15 minuta, a pojedini naslov nekoliko sekundi
nakon svega što mu se dogodi (pretraga, grab, preuzimanje, uvoz). **Preračunaj** to radi odmah.

## Povijest i statistika

**Aktivnost → Povijest** pokazuje sve što se dogodilo u biblioteci: grabove, uvoze, nadogradnje,
neuspjehe, brisanja u koš, preimenovanja, titlove i obavijesti Streameru, s filtrom po vrsti
događaja, vrsti medija i tekstu. **Aktivnost → Statistika** za svaki indexer i klijent daje broj
grabova, uvezenih, neuspjelih, postotak uspjeha i prosječno vrijeme od graba do uvoza.

---

<a id="health-checks"></a>
## Zdravlje sustava

**Sustav → Zdravlje** se provjerava svakih 5 minuta (i gumbom **Provjeri sada**). Kad se pojavi
novo upozorenje ili greška, zajednički kanali obavijesti dobiju **jednu** poruku „zdravlje sustava"
(vidi [Obavijesti](notifications.md)); sljedeća dolazi tek ako problem nestane i vrati se.
Pragovi su u **Postavke → Nadzor**.

<a id="indexer-failing"></a>
### Indexer ne radi (`indexer_failing`)
Indexer je vratio grešku i Locarr ga privremeno preskače (s rastućim čekanjem). Uzroci: kriv API
ključ, istekao račun, indexer ne radi, Cloudflare. **Indexeri** → otvori indexer → **Test**;
nakon popravka **Poništi zdravlje**.

<a id="no-indexers"></a>
### Nema indexera (`indexers_none`, `indexer_no_kind`)
Nijedan indexer nije uključen, ili nijedan nije za automatsku pretragu vrste za koju imaš root mapu
(filmovi, serije, glazba). Uključi automatsku pretragu i kategorije te vrste u **Indexeri**.

<a id="client-unreachable"></a>
### Download klijent nije dostupan (`client_unreachable`)
Locarr ne može razgovarati s klijentom: kriva adresa ili port, klijent je ugašen, kriva lozinka,
drugi Docker network. **Download klijenti** → **Test** pokazuje točnu grešku. U Docker Composeu
koristi ime servisa (npr. `qbittorrent`), ne `localhost`.

<a id="client-category"></a>
### Nedostaje kategorija (`client_category_missing`)
Klijent nema kategoriju koju Locarr koristi za tu vrstu (npr. `movies`). **Download klijenti** →
**Stvori kategorije** (gdje klijent to dopušta) ili je napravi ručno u klijentu.

<a id="no-clients"></a>
### Nema download klijenta (`clients_none`)
Dodaj ili uključi klijent u **Download klijenti**.

<a id="download-disk-space"></a>
### Malo prostora kod klijenta (`client_low_space`)
Klijent javlja manje slobodnog prostora od praga (zadano 10 GB). Oslobodi prostor ili promijeni
prag u **Postavke → Nadzor**.

<a id="remote-path-mapping"></a>
### Putanja preuzimanja nije vidljiva (`path_not_visible`, `mapping_local_missing`)
Klijent javlja da je preuzimanje u `/downloads/film`, a Locarr tu mapu ne vidi jer klijent i
Locarr imaju različite mape u kontejnerima. Dva rješenja:
1. **Isti pogled (preporučeno):** montiraj isti disk na istu putanju u oba kontejnera (npr. oba
   vide `/data/downloads`). Tada mapiranje ne treba, a uvoz može koristiti hardlinkove.
2. **Mapiranje putanja:** **Download klijenti → Mapiranja udaljenih putanja**: host klijenta,
   putanja kako je vidi klijent (`/downloads`) i kako je vidi Locarr (`/data/downloads`).

`mapping_local_missing` znači da lokalna mapa nekog mapiranja ne postoji — provjeri putanju i
mount. Nakon popravka na ploči odaberi **Ponovi uvoz**.

<a id="root-folders"></a>
### Root mapa nedostaje (`rootfolder_missing`, `rootfolders_none`)
Mapa za medije ne postoji ili se ne može čitati — najčešće disk nije montiran ili je mount u
`docker-compose.yml` promijenjen. Provjeri `volumes:` i pokreni ponovno. Ako još nemaš root mapa,
dodaj ih u **Postavke → Mape za medije** (ili u čarobnjaku za prvo pokretanje).

Locarr vidi mape onako kako su montirane u **njegov kontejner**, pod `/data`. Uz
`- /mnt/media:/data` u `docker-compose.yml` mapa hosta `/mnt/media/movies` u Locarru je
`/data/movies`. Upiši tu putanju (ili je odaberi gumbom **Pregledaj**), ne putanju na hostu;
inače dobiješ „mapa je izvan dopuštenih podatkovnih mapa".

<a id="permissions"></a>
### Nema prava pisanja (`rootfolder_unwritable`, „permission denied")
Locarr radi kao korisnik `PUID:PGID` (zadano 1000:1000) i mora moći pisati u root mape i čitati
preuzimanja. Uz oznaku **Samo za čitanje** Locarr piše i razlog:
- **nema dopuštenja** (`permission denied`): mapa pripada drugom korisniku. Na hostu
  `sudo chown -R 1000:1000 /putanja/do/medija` ili postavi `PUID`/`PGID` na vlasnika mapa. Isti
  korisnik (ili grupa s `UMASK=002`) treba biti i u download klijentu.
- **disk samo za čitanje** (`read-only file system`): mount je samo za čitanje — npr. NFS dijeljenje
  montirano s `ro` (u `/etc/fstab` promijeni u `rw`, a na NFS poslužitelju u `/etc/exports` dopusti
  pisanje, uz `all_squash,anonuid=1000,anongid=1000` ili vlasnika koji odgovara `PUID`-u) ili
  `:ro` na kraju retka u `volumes:` u `docker-compose.yml`.
- **ne postoji**: mapa je nestala (disk odspojen).

<a id="disk-space"></a>
### Malo prostora u root mapi (`rootfolder_low_space`)
Manje slobodnog prostora od praga (zadano 10 GB). Oslobodi prostor (isprazni koš) ili promijeni
prag.

<a id="recycle-bin"></a>
### Koš je velik (`recycle_bin_large`)
Koš za smeće drži više od praga (zadano 50 GB). **Postavke → Upravljanje medijima → Koš**: skrati
rok ili isprazni koš.

<a id="stuck-imports"></a>
### Uvoz zapeo (`import_stuck`)
Preuzimanje je završeno prije više od 60 minuta (prag) i nije uvezeno. Razlog je na ploči
„Zašto nije stiglo?" — najčešće putanja ili prava. Nakon popravka **Ponovi uvoz**.

<a id="metadata-providers"></a>
### Metapodaci (`metadata_no_tmdb_key`, `metadata_invalid_key`, `metadata_rate_limited`, `metadata_unavailable`)
- **Nema TMDb ključa:** bez njega se ne mogu dodavati filmovi i serije. Besplatan ključ je na
  themoviedb.org → Settings → API; upiši ga u **Postavke → Metapodaci**.
- **Ključ odbijen:** TMDb ili TVDB odbija ključ (istekao, kriv, PIN za TVDB). Upiši novi.
- **Ograničenje:** provider traži usporavanje (MusicBrainz dopušta 1 zahtjev u sekundi). Prolazi
  samo; ako traje, provjeri ima li još nešto na istoj IP adresi što šalje mnogo zahtjeva.
- **Nedostupan:** mrežni problem ili provider ne radi.

<a id="subtitle-providers"></a>
### Provideri titlova (`subtitle_provider_quota`, `subtitle_provider_failing`)
Provider je potrošio dnevnu kvotu preuzimanja (vidi vrijeme obnove) ili javlja grešku (ključ,
prijava). **Postavke → Titlovi → Provideri**. Prijava na OpenSubtitles.com račun daje veću kvotu.

<a id="streamero"></a>
### Streamero nije dostupan (`streamero_unreachable`)
Skeniranje biblioteke u Streameru nije uspjelo nakon svih pokušaja. **Postavke → Media server →
Test**. Vidi [Streamero](streamero.md).

<a id="time-skew"></a>
### Sat nije točan (`time_skew`)
Sat poslužitelja razlikuje se više od 2 minute od sata GitHuba. To kvari dvofaktorske kodove i
datume izdanja. Na hostu uključi sinkronizaciju vremena (`timedatectl set-ntp true`). Provjera
radi samo dok je uključena provjera nove verzije.

<a id="database"></a>
### Baza (`database_wal_large`, `database_large`)
Dnevnik baze (`locarr.db-wal`) veći je od 512 MB — obično neki dugi posao drži bazu; restart
pomaže. Velika baza je samo obavijest: skrati zadržavanje povijesti u **Postavke → Nadzor**;
baza se sažima jednom tjedno.

<a id="backups"></a>
### Sigurnosna kopija nije uspjela (`backup_failed`)
Zakazana kopija nije napravljena (najčešće nema prostora u `/config`). Vidi [Backup](backup.md).

<a id="updates"></a>
### Nova verzija (`update_available`)
Na GitHubu postoji novija verzija Locarra. Ažuriranje: `docker compose pull && docker compose up -d`.
Provjera (jednom dnevno, samo javni GitHub API) isključuje se u **Postavke → Nadzor**.

---

## Logovi

**Sustav → Logovi** pokazuje zadnjih 5000 zapisa (filtar po razini, pretraga, **Uživo** za nove
zapise kako nastaju). Datoteke su u `/config/logs` (`locarr.log`, a starije `locarr.1.log` …
`locarr.5.log`, svaka do 10 MB) i mogu se preuzeti s iste stranice. Lozinke, API ključevi i
tokeni se u logu ne vide ni kao atributi ni unutar adresa i poruka grešaka. Više detalja:
`LOCARR_LOG_LEVEL=debug`.

---

## Nadzorne ploče: `/api/stats` i Prometheus

1. **Račun → API ključevi** (administrator) → novi ključ, označi **samo za čitanje**.
2. U nadzornoj ploči: adresa Locarra, putanja `/api/stats`, zaglavlje `X-Api-Key: <ključ>`.

Odgovor je JSON (isto i na `/api/v1/system/stats`), najviše 15 sekundi star. Bez ključa 401, za
druge korisnike 403, uvijek kao JSON `{"code": ..., "message": ...}`.

| Polje | Značenje |
|---|---|
| `version`, `commit`, `generatedAt` | verzija, commit, kada su brojke izračunate |
| `movies`, `moviesHave`, `moviesMissing` | filmovi; s datotekom; praćeni bez datoteke |
| `series`, `episodes`, `episodesHave`, `episodesMissing` | serije; epizode; s datotekom; praćene emitirane bez datoteke |
| `artists`, `albums`, `albumsHave`, `albumsMissing` | izvođači; albumi; s barem jednom pjesmom; praćeni izdani bez ijedne pjesme |
| `queueSize`, `activeDownloads`, `downloadSpeedBytes` | preuzimanja u klijentima; ona koja se sad preuzimaju; ukupna brzina (B/s) |
| `wantedMovies`, `wantedEpisodes`, `wantedAlbums` | praćeno, dostupno, a nema datoteke (kao stranica Nedostaje) |
| `failedImports` | gotova preuzimanja kojima uvoz nije uspio |
| `healthErrors`, `healthWarnings`, `healthInfo` | broj problema zdravlja po težini |
| `indexers`, `indexersHealthy`, `indexersFailing`, `indexersDisabled` | indexeri po stanju |
| `requestsPending` | zahtjevi koji čekaju odobrenje |
| `subtitlesMissing` | datoteke kojima nedostaje barem jedan željeni titl |
| `lastRssSyncAt` | zadnji RSS sync (nema ga prije prvog) |
| `rootFolders` | `[{path, kind, accessible, freeBytes, totalBytes}]` za svaku root mapu |

**Prometheus:** u **Postavke → Nadzor** upiši token za metrike (najmanje 16 znakova). Tada
`GET /metrics` uz `Authorization: Bearer <token>` vraća iste brojke (`locarr_library_items`,
`locarr_wanted_items`, `locarr_queue_size`, `locarr_health_issues`, `locarr_indexers`,
`locarr_rootfolder_free_bytes` …). Bez tokena `/metrics` ne postoji.

```yaml
scrape_configs:
  - job_name: locarr
    authorization: {credentials: "<token>"}
    static_configs: [{targets: ["locarr:8484"]}]
```

## Održavanje

Svakih 6 sati Locarr briše stare zapise po postavkama u **Postavke → Nadzor**: vremensku crtu
starije od 365 dana, zapisnik pretraga stariji od 90, događaje preuzimanja završenih prije 180
dana i viđena RSS izdanja (14 dana); jednom tjedno sažima bazu, a predmemoriju slika drži ispod
1024 MB (brišu se najstarije, ponovno se preuzmu kad zatrebaju).
