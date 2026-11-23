# Sigurnosne kopije i vraćanje

Locarr sam radi sigurnosne kopije svojih podataka: baze (biblioteka, povijest, korisnici,
postavke, indexeri, klijenti), tajnog ključa i postavki. **Medijske datoteke nisu u kopiji** —
za filmove, serije i glazbu treba vlastiti backup diska. Vraćanje kopije nikad ne dira medije.

> **Kopija sadrži tajni ključ** (`secret.key`) kojim su šifrirane lozinke i API ključevi
> indexera, klijenata i ostalih servisa. Tko ima kopiju, ima i njih. Čuvaj je kao lozinku:
> ne šalji je e-poštom, ne stavljaj na javna mjesta.

## Automatske kopije

Zadano jednom dnevno; čuva se zadnjih 7 automatskih. Interval (u satima, 0 = isključeno) i broj
kopija su u **Postavke → Nadzor**. Kopije su u `/config/backups` kao
`locarr_backup_<datum>_<vrijeme>_<vrsta>.zip` (samo vlasnik ih može čitati).

Kopija se radi dok Locarr radi i ne zaustavlja ga: baza se kopira kao jedna dosljedna snimka
(SQLite `VACUUM INTO`), pa nema pola zapisanih promjena.

Ako automatska kopija ne uspije, **Sustav → Zdravlje** to pokazuje (i šalje obavijest).

## Ručna kopija i preuzimanje

**Sustav → Sigurnosne kopije → Napravi kopiju sada**, zatim **Preuzmi**. Preporuka: povremeno
preuzmi kopiju na drugo računalo — `/config` je obično na istom disku kao i Locarr.

U zipu su:
| Datoteka | Sadržaj |
|---|---|
| `locarr.db` | cijela baza |
| `secret.key` | ključ za šifrirane tajne |
| `settings.json` | obične postavke u čitljivom obliku (bez tajni), samo za pregled |
| `manifest.json` | verzija Locarra, verzija sheme baze, vrsta i vrijeme kopije |

## Vraćanje

**Sustav → Sigurnosne kopije**: kod kopije **Vrati**, ili **Učitaj i vrati** za zip s računala
(do 2 GB). Za potvrdu upiši `RESTORE`.

Što se događa:
1. Locarr provjeri kopiju: je li ispravan zip, je li baza neoštećena, nije li napravljena
   novijom verzijom Locarra (tada prvo ažuriraj Locarr) i otvara li ključ iz kopije šifrirane
   tajne u njoj.
2. Napravi kopiju trenutnog stanja (vrsta „prije vraćanja"), pa se može vratiti i natrag.
3. Locarr se ponovno pokrene **u istom procesu**: zaustavi poslove, zatvori bazu, postavi bazu i
   ključ iz kopije (stari ostaju pokraj kao `locarr.db.pre-restore` i `secret.key.pre-restore`),
   izvrši migracije ako je kopija iz starije verzije i pokrene se. Kontejner se ne restarta;
   stranica pričeka nekoliko sekundi i sama se osvježi.
4. Svi su odjavljeni osim tebe (ako u kopiji postoji isti administratorski račun; inače se prijavi
   ponovno). API ključevi iz kopije vrijede.

Ako Locarr padne usred vraćanja, pripremljena kopija se primijeni pri idućem pokretanju.

## Vraćanje bez weba (npr. na novom poslužitelju)

1. Zaustavi Locarr: `docker compose stop locarr`.
2. Raspakiraj zip; `locarr.db` i `secret.key` kopiraj u mapu `/config` (preko postojećih),
   obriši `locarr.db-wal` i `locarr.db-shm` ako postoje.
3. Pokreni: `docker compose up -d`. Migracije se izvrše same.

Na novom poslužitelju putanje medija i preuzimanja moraju biti iste kao prije (ili ih promijeni u
postavkama nakon pokretanja), jer ih baza pamti.
