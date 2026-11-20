# Odluke

## D-001 Stack i oblik isporuke (2026-11-20)
**Kontekst:** Locarr zamjenjuje šest aplikacija i mora biti „jedan port, jedna prijava"; pokreće se preko Docker Composea; vlasnik već održava Streamero (Go, React, Kotlin, SwiftUI).
**Odluka:** Go server s OpenAPI-first API-jem (ogen), SQLite u WAL načinu kao jedina baza, vlastiti red poslova u istoj bazi, React web ugrađen u binarij, nativne Android i iOS aplikacije. Jedan Docker image, jedan servis u `docker-compose.yml`.
**Posljedice:** nema Postgresa ni Redisa za održavanje; backup je jedna datoteka (uz WAL checkpoint). Ako se pokaže potreba za više instanci, apstrakcija baze ostaje tanka da se Postgres može dodati kasnije.

## D-002 Clean-room prema postojećim *arr aplikacijama (2026-11-20)
**Kontekst:** aplikacije koje zamjenjujemo uglavnom su GPL-3.0.
**Odluka:** ne čitamo njihov izvorni kod. Ponašanje i formate učimo iz javne API dokumentacije, specifikacija protokola (Torznab/Newznab, API-ji download klijenata) i testiranjem preko njihovih API-ja. Parser imena izdanja pišemo sami s golden testovima.
**Posljedice:** neka ponašanja će se razlikovati; razlike koje korisnici primijete zapisujemo u FEATURES.md.

## D-003 Uvoz postojećeg setupa samo preko API-ja (2026-11-20)
**Kontekst:** korisnici dolaze s gotovim Sonarr/Radarr/… instalacijama.
**Odluka:** uvoz čita podatke isključivo preko javnih API-ja tih aplikacija (URL + API ključ), nikad iz njihovih baza ili konfiguracijskih datoteka, i nikad ništa ne mijenja na izvoru. Prije uvoza prikazuje se pregled (što će se uvesti, što se ne može).
**Posljedice:** uvoz radi i kad su stare aplikacije na drugom stroju; ono što API ne izlaže ne može se uvesti.
