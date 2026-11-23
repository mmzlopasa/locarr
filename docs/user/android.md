# Locarr za Android

Aplikacija za Android radi s vašim Locarr poslužiteljem: ukućani u njoj traže filmove, serije i
glazbu i dobivaju obavijest kad stignu, a administrator vidi što se preuzima, zašto nešto nije
stiglo i može to popraviti bez računala.

## Instalacija

1. Na stranici izdanja na GitHubu (`github.com/mmzlopasa/locarr/releases`) preuzmite
   `Locarr-vX.Y.Z.apk` na mobitel.
2. Otvorite datoteku. Android će pitati smije li preglednik (ili upravitelj datoteka) instalirati
   aplikacije — dopustite samo za ovu instalaciju.
3. Novu verziju instalirate isto tako, preko postojeće; prijava i poslužitelji ostaju.

Potreban je Android 8.0 ili noviji. Aplikacija radi i na tabletima (izbornik je tada sa strane).

## Spajanje na poslužitelj

1. Upišite adresu na kojoj Locarr otvarate u pregledniku, npr. `http://192.168.1.10:8484` ili
   `https://locarr.mojadomena.hr`. Ako ne upišete `http://`, aplikacija ga doda sama.
2. Prijavite se Locarr računom. Ako ste uključili dvofaktorsku prijavu, aplikacija zatim traži kod
   iz aplikacije za autentifikaciju (ili kod za oporavak).
3. Ako administrator ima povezan Streamero, pojavi se izbor **Streamero**: prijavite se istim
   korisničkim imenom i lozinkom kao u Streameru.

**Kućna mreža i `http://`.** Aplikacija namjerno dopušta obični `http://` jer većina Locarra radi
samo kod kuće. Izvan kuće koristite HTTPS (reverse proxy) ili VPN (npr. WireGuard, Tailscale) — bez
toga lozinka putuje mrežom nešifrirana. Ako za HTTPS kod kuće koristite vlastiti certifikat,
instalirajte svoj CA u **Postavke → Sigurnost → Šifriranje → Instaliraj certifikat**; aplikacija mu vjeruje.

**Više poslužitelja.** U **Više → račun → Poslužitelji** dodajete drugi poslužitelj (npr. kod
roditelja) i prebacujete se između njih. Obavijesti stižu sa svih na kojima ste prijavljeni.

Token prijave spremljen je šifrirano ključem koji ne napušta mobitel i ne ide u backup; nakon
vraćanja mobitela iz kopije prijavite se ponovno.

## Za ukućane

- **Otkrivanje**: što je u trendu, popularno, uskoro, u kinima, nedavno dodano. **Sve** otvara
  cijeli popis.
- **Pretraga**: filmovi, serije, osobe i glazba.
- **Stranica naslova**: opis, glumci, sezone i što je od njih dostupno. **Zatraži** pošalje zahtjev;
  za seriju birate sezone i hoćete li i buduće. **Zatraži 4K** se vidi samo ako vam je dopušteno.
  **Gledaj u Streameru** otvara naslov u Streameru kad je već tamo.
- **Prijavi problem** (na naslovu koji je dostupan): slika, zvuk, titlovi ili drugo, po želji
  sezona i epizoda.
- **Zahtjevi**: vaši zahtjevi sa stanjem („čeka odobrenje", „na putu", „dostupno", „zapelo") i
  rečenicom gdje je zahtjev sada, uz napredak preuzimanja. Zahtjev koji još čeka možete **povući**,
  a u komentarima se dopisivati s administratorom.
- **Obavijesti** (zvono gore desno): odobreno, odbijeno, dostupno, komentari i problemi.

## Za administratora i upravitelje

Izbornik ima **Pregled, Otkrivanje, Biblioteka, Aktivnost i Više**.

- **Pregled**: zdravlje sustava, zahtjevi koji čekaju, problemi, što se preuzima, „Zašto nije
  stiglo?" po razlozima i što dolazi ovaj tjedan.
- **Biblioteka**: filmovi, serije i glazba s filtrom. Na stavci: praćenje, **Traži sada**, **Ručna
  pretraga** (sva izdanja s odlukom: prihvaćeno, zadržano, odbijeno i zašto; **Preuzmi** ili **Ipak
  preuzmi**), **Zašto nije ovdje?** s gumbom **Pokušaj sada**, vremenska crta i podaci o datoteci.
  Serija ima sezone i epizode (svaka sa svojom pretragom i objašnjenjem), izvođač albume i pjesme.
- **+** u Biblioteci dodaje film, seriju ili izvođača (mapa za medije, profil kvalitete, što pratiti,
  odmah traži).
- **Aktivnost**: red preuzimanja uživo (pauza, nastavak, na vrh/na kraj, ukloni — po želji s
  podacima i blokiranjem izdanja), **Zašto nije?** (grupe s uputom kako popraviti i radnjama:
  traži, blokiraj i pokušaj iduće, ponovi uvoz, prestani pratiti), Traženo, Kalendar i Povijest.
- **Zahtjevi** (Više → Zahtjevi): odobri, odbij s razlogom, **odobri sve prikazane**, ponovi
  zapeli zahtjev. **Problemi**: riješi (po želji uz traženje zamjene) ili ponovno otvori.
- **Sustav** (samo administrator): Zdravlje (s **Popravi na webu**), Indexeri (vaši, ugrađene
  stranice i usenet popis, **Test**), Download klijenti (**Test**), Mape za medije sa slobodnim
  prostorom.

**Postavke na webu.** Detaljne postavke uređuju se u web sučelju; u **Više → Postavke na webu**
svaka stavka otvara pravu stranicu u pregledniku: opće postavke, korisnici i pozivnice, profili i
definicije kvalitete, custom formati, pravila izdanja, upravljanje medijima i imena, metapodaci i
profili metapodataka, zahtjevi, popisi za uvoz, titlovi, media server, kanali obavijesti, nadzor,
logovi, sigurnosne kopije, zadaci i uvoz iz drugih aplikacija. Dodavanje i uređivanje indexera,
download klijenata i mapa za medije također je na webu (gumb gore desno na tim ekranima). Lozinka,
dvofaktorska prijava i API ključevi: **Više → račun → Lozinka, dvofaktorska prijava, API ključevi**.

## Obavijesti

Locarr ne koristi Googleove push obavijesti. Aplikacija sama pita poslužitelj ima li novih obavijesti:

- pri svakom otvaranju i zatim otprilike **svakih 15 minuta** (najčešće što Android dopušta), samo
  kad postoji mreža;
- nakon prve prijave ne prikazuje stare obavijesti, samo nove;
- obavijesti pročitane na webu ili drugom uređaju se ne ponavljaju;
- ako se skupi puno novih, prikažu se četiri i jedna sažeta („još N obavijesti").

Kanali (Android postavke → Aplikacije → Locarr → Obavijesti): **Moji zahtjevi**, **Novi zahtjevi**
(za one koji odobravaju) i **Problemi** — svaki možete zasebno utišati. Na Androidu 13 i novijem
aplikacija pri prvom otvaranju traži dozvolu za obavijesti; ako ste je odbili, uključite je u
**Više → račun → Obavijesti na mobitelu**.

Ako obavijesti kasne više od pola sata, za Locarr isključite optimizaciju baterije (Postavke →
Aplikacije → Locarr → Baterija → Bez ograničenja). Kad mobitel nije na kućnoj mreži (ni VPN-u),
obavijesti stižu tek kad poslužitelj opet bude dostupan.

## Kad nešto ne radi

- **„Poslužitelj nije dostupan"**: jeste li na istoj mreži (ili VPN-u)? Otvara li se ista adresa u
  pregledniku mobitela?
- **„Na toj adresi nešto odgovara, ali to nije Locarr"**: provjerite port (zadano 8484) i da adresa
  nije adresa routera ili drugog servisa.
- **„Ovaj Locarr još nije postavljen"**: prvo ga otvorite u pregledniku i napravite admin račun.
- **Vraćeni ste na prijavu**: sesija je odjavljena (npr. „odjava sa svih uređaja" na webu) ili je
  račun promijenjen. Prijavite se ponovno.
