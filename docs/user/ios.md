# iOS aplikacija

Locarr za iPhone i iPad: članovi obitelji traže filmove, serije i glazbu i prate svoje zahtjeve, a
administratori i upravitelji vide biblioteku, preuzimanja, „Zašto nije stiglo?" i zdravlje sustava.
Aplikacija nije u App Storeu; instalira se kao `.ipa` preko **Feathera** (ili drugog alata koji
potpisuje aplikacije vašim certifikatom). Treba iOS/iPadOS 17 ili noviji.

---

## Instalacija preko Feathera

1. U Featheru otvorite **Sources → +** i upišite adresu izvora:

   ```
   https://raw.githubusercontent.com/mmzlopasa/locarr/main/feather/source.json
   ```

2. U izvoru **Locarr** odaberite **Locarr** i dodirnite **Get** (ili verziju koju želite).
3. Feather preuzme `.ipa` s GitHub izdanja, potpiše ga i instalira. Nove verzije se pojave u
   izvoru čim izađu; ažurirate ih istim gumbom.

Datoteku `Locarr-vX.Y.Z.ipa` možete preuzeti i ručno s
[GitHub izdanja](https://github.com/mmzlopasa/locarr/releases) i uvesti je u Feather.

Aplikacija ne koristi ništa što ponovno potpisivanje pokvari: nema push obavijesti (APNs), iClouda
ni posebnih dopuštenja. Prijava se sprema u Keychain samo na ovom uređaju.

## Spajanje

1. Otvorite aplikaciju i upišite adresu Locarra, npr. `192.168.1.10:8484`, `nas.local:8484` ili
   `https://locarr.example.com`. Adresa bez `http://`/`https://` dobiva `http://` ako je u kućnoj
   mreži, inače `https://`.
2. Prvi put iOS pita smije li Locarr tražiti uređaje u lokalnoj mreži: odaberite **Dopusti**
   (bez toga se aplikacija ne može spojiti na adresu u kućnoj mreži). Ako ste odbili:
   **Postavke → Privatnost i sigurnost → Lokalna mreža → Locarr**.
3. Prijavite se:
   - **Locarr računom** (korisničko ime i lozinka). Ako je uključena dvofaktorska prijava,
     aplikacija zatim traži šifru iz aplikacije za autentifikaciju (ili kod za oporavak).
   - **Prijavom preko Streamera**, ako je administrator to uključio (Postavke → Media server).
     Upišite Streamero korisničko ime i lozinku; ako Streamero traži šifru, aplikacija je zatraži.

### Kućna mreža i HTTPS

Obični `http://` radi samo prema adresama u kućnoj mreži: privatne IP adrese (`10.x`, `172.16–31.x`,
`192.168.x`), Tailscale adrese (`100.64–127.x`, `*.ts.net`) te imena koja završavaju na `.local`,
`.lan`, `.home`, `.home.arpa`, `.internal` ili `.localdomain`. Za pristup izvana Locarr mora biti iza
HTTPS-a (reverse proxy s certifikatom, npr. Caddy, Nginx Proxy Manager ili Traefik). Ako na kućnoj
mreži koristite ime s pravom domenom (npr. `locarr.mojadomena.hr`) bez HTTPS-a, upišite IP adresu.

### Više poslužitelja

**Postavke → Poslužitelji** (ili **Drugi poslužitelj** na zaslonu prijave): dodajte još jedan
Locarr, prebacujte se između njih dodirom, a povlačenjem ulijevo ga zaboravite (briše se samo
prijava na ovom uređaju). Obavijesti se provjeravaju za svaki poslužitelj na koji ste prijavljeni.

Kad sesija istekne ili je odjavite na webu (Račun → Sesije), aplikacija se vrati na prijavu.

## Što je u aplikaciji

**Za sve:** Otkrivanje (u trendu, popularno, uskoro…), pretraga filmova, serija, osoba i glazbe,
stranica naslova s gumbom **Zatraži** (sezone serije, 4K ako vam je dopušten, album ili svi albumi
izvođača), **Moji zahtjevi** s napretkom i odgovorom na pitanje zašto naslov još nije stigao,
komentari, **Prijavi problem** za naslove u biblioteci i centar obavijesti.

**Za administratore i upravitelje:** Pregled (preuzimanja, „Zašto nije stiglo?", zdravlje, sljedećih
7 dana, prostor na diskovima), biblioteke filmova, serija i glazbe s detaljima (datoteke, vremenska
crta, „Zašto nije ovdje?" i **Pokušaj odmah**), dodavanje naslova, ručna pretraga izdanja s odlukom
i razlozima za svako izdanje i ručnim grabom, red preuzimanja (pauza, nastavak, prioritet,
uklanjanje), ploča „Zašto nije stiglo?" sa skupnim radnjama, traženo, kalendar, povijest, odobravanje
zahtjeva, problemi, zdravlje, indexeri (vaši, ugrađene stranice i usenet) i download klijenti s
gumbom **Testiraj** te mape za medije.

Detaljne postavke (profili kvalitete, custom formati, imenovanje, korisnici, dodavanje indexera i
klijenata, kanali obavijesti…) uređuju se u web sučelju: **Više → Postavke na webu** otvara pravu
stranicu u pregledniku.

Na iPadu je izbornik u bočnoj traci. Aplikacija prati veličinu teksta iz postavki iOS-a i radi s
VoiceOverom. Jezik je kao na uređaju, a može se promijeniti u **Postavke → Jezik**.

## Obavijesti

Locarr nema push poslužitelj, pa aplikacija sama pita Locarr ima li novosti:

- **dok je aplikacija otvorena:** odmah i zatim svake minute;
- **u pozadini:** kad iOS dopusti (Background App Refresh). iOS odlučuje kada: obično nekoliko puta
  dnevno za aplikaciju koju često otvarate, a **nikako** nakon što aplikaciju zatvorite povlačenjem
  prema gore, u načinu štednje energije ili kad je osvježavanje u pozadini isključeno.

Zato obavijest u pozadini može kasniti i nekoliko sati. Popis u aplikaciji (zvonce) je uvijek
potpun. **Postavke → Obavijesti** pokazuje je li iOS dopustio obavijesti, je li osvježavanje u
pozadini uključeno i kada je zadnji put radilo, a **Provjeri sada** pita odmah.

Što treba uključiti na iPhoneu:
1. **Postavke → Locarr → Obavijesti → Dopusti obavijesti** (aplikacija to pita nakon prve prijave).
2. **Postavke → Općenito → Osvježavanje aplikacija u pozadini** uključeno, i za Locarr.

Obavijest o novom zahtjevu (za one koji odobravaju) ima radnju **Odobri** (dugi pritisak na
obavijest). Dodir na obavijest otvara zahtjev ili problem, i to na pravom poslužitelju.

Za trenutne obavijesti na mobitel postavite i kanal u webu (**Račun → Moje obavijesti**: ntfy,
Gotify, Pushover ili Telegram); vidi [obavijesti](notifications.md).

## Problemi

| Što se vidi | Što učiniti |
|---|---|
| „Locarr nije dostupan na toj adresi" | Provjerite adresu i port, da je telefon na istoj Wi-Fi mreži i da je dopušten pristup lokalnoj mreži. |
| „Obični HTTP dopušten je samo…" | Adresa nije u kućnoj mreži: koristite `https://` ili IP adresu. |
| „Na toj adresi nešto odgovara, ali to nije Locarr" | Krivi port (npr. port Streamera ili qBittorrenta). Locarr je zadano na 8484. |
| Obavijesti ne stižu u pozadini | Provjerite oba prekidača gore, ne zatvarajte aplikaciju povlačenjem i otvorite je s vremena na vrijeme; za trenutne obavijesti koristite ntfy ili Telegram. |
| Feather javlja grešku pri instalaciji | Potpišite ponovno s važećim certifikatom; aplikacija ne traži posebna dopuštenja. |
