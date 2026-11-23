# Obavijesti

Uz zvonce u aplikaciji Locarr može slati obavijesti van: na mobitel (ntfy, Gotify, Pushover,
Telegram), u Discord, e-poštom ili vlastitoj usluzi (webhook, Apprise).

Postoje dvije vrste kanala:
- **Zajednički kanali** (administrator, **Postavke → Obavijesti**): dobivaju sve odabrane događaje —
  zahtjeve, prijave problema, biblioteku, titlove i zdravlje sustava.
- **Moji kanali** (svaki korisnik, **Račun → Moje obavijesti**): samo događaji vlastitih zahtjeva i
  prijava (odobreno, dostupno, komentar…).

---

## Prije prvog kanala (administrator)

**Postavke → Obavijesti → Dostava**:
- **Adresa za poveznice**: adresa Locarra kako je vide korisnici, npr.
  `https://locarr.example.com`. Poruke tada imaju poveznicu na zahtjev ili naslov.
- **Poslužitelj pošte** (za e-poštu): host, port (prazno = zadani za odabrano šifriranje: 587 za
  STARTTLS, 465 za TLS, 25 bez), šifriranje, korisnik, lozinka (sprema se šifrirano) i adresa
  pošiljatelja. Svi kanali e-pošte koriste ovaj poslužitelj.
- **Titl nedostaje nakon (dana)**: kad datoteci toliko dana nakon dolaska još nedostaje titl na
  željenom jeziku, šalje se jedna poruka „titl još nedostaje" (zadano 7).

## Vrste kanala

| Vrsta | Što upisati |
|---|---|
| **ntfy** | poslužitelj (prazno = ntfy.sh), tema, po potrebi token ili korisnik i lozinka, prioritet 1–5, oznake |
| **Gotify** | poslužitelj, token aplikacije (šalje se samo u zaglavlju, nikad u adresi), prioritet |
| **Discord** | webhook URL kanala (Postavke kanala → Integracije → Webhookovi), ime pošiljatelja; poruka ima plakat |
| **Telegram** | token bota (od @BotFather) i chat ID |
| **Pushover** | token aplikacije i korisnički ključ, prioritet, zvuk, uređaj |
| **E-pošta** | primatelji (do 20); poruka je i običan tekst i HTML |
| **Webhook** (samo admin) | URL, metoda (POST/PUT), po želji tajna za potpis |
| **Apprise API** (samo admin) | adresa Apprise API-ja (npr. `http://apprise:8000/notify/apprise`) i oznaka |

Tajne (tokeni, ključevi, webhook URL) spremaju se šifrirano i ne prikazuju se više; vidi se samo
„postavljeno". Polje ostavite prazno da ostane staro, ili ga uklonite.

Svaki kanal ima **jezik** poruka (engleski ili hrvatski; za moje kanale zadano je jezik korisnika),
**događaje** i gumb **Testiraj** koji odmah pošalje probnu poruku i pokaže grešku ako ne uspije.

## Događaji

- **Zahtjevi**: novi, odobren, odbijen, dostupan, djelomično dostupan, zapeo, komentar.
- **Problemi**: prijavljen, komentar, riješen, ponovno otvoren.
- **Biblioteka** (samo zajednički): poslano na preuzimanje, uvezeno, nadograđeno, preuzimanje nije
  uspjelo, uvoz nije uspio.
- **Titlovi** (samo zajednički): titl preuzet, titl još nedostaje.
- **Sustav** (samo zajednički): upozorenje kad indexer, download klijent ili Streamero prestane raditi
  (jednom, pri prelasku u grešku).

## Kad nešto ne stigne

Locarr šalje u pozadini i pokuša do pet puta (10 s, 20 s, 40 s, 80 s). Popis kanala pokazuje
**zadnje slanje**, **uzastopne neuspjehe** i **zadnju grešku**. Česti razlozi:
- *address not allowed*: osobni kanali korisnika koji nisu administratori ne smiju na adrese u
  lokalnoj mreži (npr. vlastiti ntfy na `192.168.…`). Takav kanal neka postavi administrator kao
  zajednički, ili koristite javnu adresu.
- *HTTP 401/403*: krivi token.
- *the mail server is not set up*: popunite Poslužitelj pošte.

## Za razvojne programere: webhook

Tijelo je JSON:
```json
{"event": "request.available", "at": "2026-11-23T10:00:00Z", "headline": "Now available: Sintel (2010)",
 "title": "Sintel (2010)", "message": "", "url": "https://locarr.example.com/requests/4",
 "poster": "https://image.tmdb.org/t/p/w342/abc.jpg", "mediaKind": "movie", "requestId": 4,
 "actor": {"id": 1, "username": "admin"}, "params": {}}
```
Zaglavlja: `X-Locarr-Event: <događaj>` i, ako je postavljena tajna,
`X-Locarr-Signature: sha256=<hex HMAC-SHA256 tijela s tajnom>`. Primatelj izračuna HMAC nad točnim
bajtovima tijela i usporedi ga u konstantnom vremenu.
