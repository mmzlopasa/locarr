# Profili, custom formati i razlozi odbijanja

Locarr za svako izdanje koje nađe (RSS ili pretraga) pročita ime, odredi kvalitetu, izračuna bodove
custom formata i provjeri ga prema profilu naslova. Ako izdanje ne uzme, uvijek kaže **zašto**.
Sve se namješta u **Postavke**, a **Postavke → Provjera izdanja** pokazuje što bi Locarr učinio s
bilo kojim imenom koje zalijepite.

## Profil kvalitete

Profil kaže koje kvalitete naslov smije imati i kojim redom ih voli.

- **Kvalitete** su poredane od najbolje prema lošijoj. Redak se premješta povlačenjem ili strelicama.
  Samo označene (dopuštene) kvalitete dolaze u obzir.
- **Grupa** spaja kvalitete koje vrijede jednako (npr. „WEB 1080p" = WEB-DL 1080p i WEBRip 1080p).
  Unutar grupe odlučuju bodovi custom formata, a ne kvaliteta.
- **Granica (cutoff)**: kad datoteka u biblioteci dosegne tu kvalitetu, nadogradnje prestaju.
- **Nadograđuj postojeće datoteke**: bez toga Locarr uzima samo prvo izdanje i ništa ne mijenja.
- **Najmanji broj bodova**: izdanja s manje bodova custom formata se odbijaju.
- **Nadogradnja do bodova**: i nakon granice kvalitete nadograđuje se dok datoteka ne skupi toliko bodova (0 = samo kvaliteta).
- **Jezici**: prihvaćeni jezici zvuka. Prazno znači bilo koji. „Izvorni jezik" je jezik u kojem je
  naslov snimljen; izdanja s više jezika (MULTi, Dual Audio) prolaze za izvorni jezik. Ime bez oznake
  jezika smatra se engleskim.

Locarr pri prvom pokretanju napravi profile „Any", „HD-720p/1080p", „HD-1080p" i „UHD" za filmove i
serije te „Lossless", „Lossy" i „Any" za glazbu. Slobodno ih mijenjajte ili brišite.

## Veličine

**Postavke → Veličine** za svaku kvalitetu određuju najmanju, željenu i najveću veličinu, u MB po
minuti trajanja. Pregled pokazuje što to znači za epizodu od 45 minuta, film od 110 minuta ili bilo
koje drugo trajanje. 0 kao najveća vrijednost znači bez granice. Sezonski paket računa sve tražene
epizode te sezone. Među jednako dobrim izdanjima prednost ima ono bliže željenoj veličini.

## Custom formati

Custom format je imenovani skup uvjeta, npr. „x265", „Dolby Vision", „Hrvatski titlovi". Profil
svakom formatu daje bodove (negativni da ga izbjegnete). Vrste uvjeta: naziv izdanja (regularni
izraz), izvor, rezolucija, video kodek, HDR, zvuk, grupa izdanja, jezik, verzija filma (Director's Cut,
IMAX…), oznaka indexera (freeleech…), veličina, streaming servis, dodatak kvalitete (Remux, BR-DISK),
vrsta izdanja (epizoda, više epizoda, sezonski paket) i godina.

Kako se uvjeti zbrajaju: unutar iste vrste uvjeta barem jedan mora odgovarati, a svi **obavezni**
moraju. **Negiraj** obrće uvjet („nije 2160p").

**Uvoz iz vodiča**: Postavke → Custom formati → Uvezi JSON. Zalijepite jedan format ili cijeli popis
u uobičajenom JSON obliku (`name`, `specifications` s `implementation`, `negate`, `required`,
`fields.value`). Odaberite numeriranje: alati za filmove i za serije različito broje izvore. Gumb
**Provjeri** ništa ne sprema. Uvjet čiju vrstu Locarr ne poznaje ostaje u formatu i prijavi se: ako
nije obavezan, zanemaruje se; ako jest, format se nikad ne primjenjuje. **Izvezi** daje isti JSON natrag.

## Pravila izdanja

- **Profil izdanja**: riječi koje ime mora sadržavati (barem jednu) ili ne smije sadržavati (nijednu).
  Običan tekst ne razlikuje velika i mala slova; `/regex/` je regularni izraz. Oznake ograničavaju
  profil na naslove s tim oznakama.
- **Profil kašnjenja**: koliko minuta čekati nakon objave prije preuzimanja (zasebno za usenet i
  torrente), koji protokol se preferira i kada se ne čeka (najbolja kvaliteta profila ili dovoljno
  bodova). Profili se provjeravaju odozgo; vrijedi prvi čije oznake odgovaraju, inače zadani.
  Ručna (interaktivna) pretraga nikad ne čeka.

## Kako čitati razloge odbijanja

Svako odbijeno izdanje ima jedan ili više razloga. Kod (npr. `size_too_large`) je stalan, a tekst
navodi vrijednosti, npr. „18,4 GB > 12 GB najviše za Bluray-1080p (1 h 50 min)". Žuti razlozi su
**privremeni**: izdanje čeka i može proći kasnije.

| Kod | Značenje | Što napraviti |
|---|---|---|
| `parse_failed` | ime je zamaskirano ili nečitljivo | ništa; indexer ne daje pravo ime |
| `wrong_kind`, `wrong_title`, `wrong_year`, `wrong_ids` | nije taj naslov | dodajte alternativni naslov ako je ipak taj |
| `wrong_season`, `wrong_episode` | nije tražena sezona/epizoda | — |
| `quality_not_allowed` | kvaliteta nije dopuštena u profilu | dopustite je u profilu |
| `upgrades_disabled`, `cutoff_met`, `not_an_upgrade` | datoteka u biblioteci je dovoljno dobra ili bolja | podignite granicu ili bodove |
| `custom_format_score_too_low` | premalo bodova | provjerite bodove formata u profilu |
| `size_too_small`, `size_too_large` | izvan granica veličine | Postavke → Veličine |
| `language_not_wanted` | jezik nije prihvaćen | jezici u profilu |
| `must_contain_missing`, `must_not_contain_found` | profil izdanja | Pravila izdanja |
| `blocklisted` | izdanje je ranije blokirano | Aktivnost → Blokirana izdanja |
| `not_enough_seeders` | premalo seedera | postavka indexera |
| `too_old`, `too_new` | usenet: izvan zadržavanja ili još svježe (privremeno) | — |
| `sample`, `hardcoded_subs`, `three_d` | uzorak, utisnuti titlovi, 3D | — |
| `indexer_disabled`, `indexer_limited` | indexer se ne koristi za to ili je na ograničenju (privremeno) | Indexeri |
| `protocol_disabled`, `delayed` | profil kašnjenja (isključen protokol ili čekanje, privremeno) | Pravila izdanja |

Među prihvaćenim izdanjima Locarr bira redom: bolja kvaliteta u profilu, više bodova, novija
revizija (PROPER/REPACK), preferirani protokol, prioritet indexera, više seedera, bliže željenoj
veličini, novije.
