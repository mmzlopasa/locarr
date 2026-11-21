# Download klijenti

Locarr šalje izdanja download klijentu, prati preuzimanje i javlja kad je spremno za uvoz.
Podržani su qBittorrent, Transmission, Deluge, rTorrent, aria2 (torrenti) te SABnzbd i NZBGet (usenet).
Klijente dodaješ u **Download klijenti**; red preuzimanja je u **Aktivnost → Red preuzimanja**.

## Prije nego dodaš klijent

### Jedna `/data` montaža
Locarr i svi klijenti moraju vidjeti datoteke **pod istom putanjom**. Preporučeni raspored (D-008):

```
/data/media/movies   /data/media/tv   /data/media/music
/data/downloads/torrents   /data/downloads/usenet
```

U `docker-compose.yml` montiraj istu mapu na hostu kao `/data` i u Locarr i u klijent. Tada uvoz može
napraviti hardlink (bez kopiranja i dvostrukog zauzeća diska), a putanja koju klijent javi odmah postoji i u Locarru.

### Kategorije
Za svaku vrstu sadržaja (filmovi, serije, glazba) klijent može imati svoju kategoriju, npr. `movies`, `tv`, `music`.
Locarr prati **samo** stavke u svojim kategorijama. Stavke koje si u te kategorije dodao ručno ili neki drugi
program prikazuju se u redu kao „Nije dodao Locarr" i Locarr ih nikad ne dira.

Ako kategorija ne postoji, **Testiraj** to javi, a gumb **Stvori kategorije** ih napravi (kad klijent to dopušta).
Kategorije se stvaraju i same pri prvom slanju izdanja.

## Postavljanje po klijentu

| Klijent | Zadani port | Što upisati | Napomene |
|---|---|---|---|
| qBittorrent | 8080 | korisnik i lozinka WebUI-ja | Kategorije imaju svoju mapu. qBittorrent 5 provjerava port u `Host` zaglavlju: ako Docker objavljuje WebUI na drugom portu nego na kojem sluša, prijava pada s „autentikacija nije uspjela". Najjednostavnije je da port bude isti izvana i iznutra (`WEBUI_PORT`). |
| Transmission | 9091 | korisnik i lozinka (ako su uključeni) | URL baza je zadano `/transmission` (Locarr dodaje `/rpc`). Kategorije su oznake (labels) i ne treba ih stvarati. |
| Deluge | 8112 | lozinka web sučelja | Za kategorije uključi plugin **Label** (Preferences → Plugins). Web sučelje se samo spaja na prvi daemon s popisa. Deluge nema vremensko ograničenje seedanja, samo omjer. |
| rTorrent | 8000 | adresa XML-RPC-a (URL baza zadano `/RPC2`), korisnik i lozinka ako ih frontend traži | Kategorija se sprema u `d.custom1` (kao kod ruTorrenta). rTorrent ne briše datoteke kroz API: kad tražiš „ukloni i obriši podatke", Locarr sam briše sadržaj (samo unutar `/data`). |
| aria2 | 6800 | RPC tajna (`--rpc-secret`) i mapa za preuzimanja kako je aria2 vidi | aria2 nema kategorije: svaka kategorija je podmapa mape za preuzimanja (npr. `/data/downloads/torrents/movies`). Ni aria2 ne briše datoteke, to radi Locarr. |
| SABnzbd | 8080 | API ključ (Config → General) | Ako se spajaš preko imena hosta, dodaj ga u `host_whitelist` (Config → Special) ili koristi IP adresu, inače SABnzbd odbija vezu. Mapa preuzimanja ovisi o kategoriji. |
| NZBGet | 6789 | korisnik i lozinka (zadano `nzbget` / `tegbzn6789`) | Kategorije se definiraju u NZBGet postavkama (Categories); Locarr ih ne može stvoriti kroz API. Potreban je NZBGet 18 ili noviji. |

**HTTPS:** uključi „Koristi HTTPS" ako klijent sluša na HTTPS-u. Certifikat se provjerava; za vlastiti
(self-signed) certifikat izričito uključi „Dopusti self-signed certifikat" samo za taj klijent.

**Adrese:** dopuštene su adrese u lokalnoj mreži, Docker imena kontejnera i `localhost`. Link-local adrese
i adrese metapodataka u oblaku (npr. `169.254.169.254`) su zabranjene.

## Mapiranje udaljenih putanja
Ako klijent radi na drugom stroju ili u kontejneru s drugačijim montažama, putanja koju javlja ne postoji u
Locarru. Tada preuzimanje u redu stoji kao gotovo uz poruku da putanja nije vidljiva. Dodaj mapiranje:

- **Host** — kako je klijent upisan u Locarru (npr. `seedbox.lan`)
- **Udaljena putanja** — kako je vidi klijent (npr. `/downloads`)
- **Lokalna putanja** — kako je vidi Locarr (npr. `/data/downloads/torrents`), mora biti unutar `/data`

Najdulja odgovarajuća udaljena putanja ima prednost. Windows putanje (`D:\Torrents`) su podržane.

## Ostale postavke klijenta
- **Prioritet** (1–50): među klijentima istog protokola prvo se koristi onaj s manjim brojem; klijenti istog
  prioriteta se izmjenjuju. Ako klijent odbije izdanje, pokušava se sljedeći.
- **Oznake:** klijent s oznakama prima samo izdanja s barem jednom istom oznakom (npr. s indexera s oznakom `anime`);
  klijent bez oznaka prima sve.
- **Prioritet novih / starijih izdanja:** izdanja mlađa od 14 dana mogu ići na vrh reda, starija na kraj.
- **Početno stanje:** pokreni odmah ili dodaj pauzirano.
- **Ukloni nakon uvoza:** kad uvoz završi, Locarr uklanja preuzimanje iz klijenta (s podacima). Torrent se uklanja
  tek kad su ispunjeni ciljevi seedanja s indexera (omjer ili vrijeme), ili kad ga klijent sam zaustavi po svojim pravilima.
- **Praćenje:** klijenti se provjeravaju svakih 30 s (svakih nekoliko sekundi dok je red otvoren). Torrent bez
  seedera i bez napretka dulje od 24 h označava se kao neuspio (može se promijeniti ili isključiti s 0).

## Red preuzimanja
U redu vidiš naslov, klijent, stanje, napredak, brzinu, preostalo vrijeme, veličinu i omjer. Akcije:
pauza / nastavak, pomak na vrh ili kraj reda (ako klijent to podržava), ukloni, ukloni i obriši podatke
(traži potvrdu) te ukloni i blokiraj (izdanje se više neće automatski grabati). Blokirana izdanja su u
**Aktivnost → Blokirana izdanja**, odakle ih možeš i odblokirati.
