# Compatible API for media servers

Locarr answers the small part of the Seerr, Radarr and Sonarr HTTP APIs that a media server
(Streamero) uses for "discover and request" and "upcoming" (D-079). These routes are **not** part of
`api/openapi.yaml`: they mirror other applications' public API shapes, live outside the generated
server (`server/internal/compat`) and never collide with `/api/v1`.

| Base URL to enter in the client | Acts as |
|---|---|
| `http://<locarr>:8484/compat/seerr` | Seerr |
| `http://<locarr>:8484/compat/radarr` | Radarr |
| `http://<locarr>:8484/compat/sonarr` | Sonarr |

## Authentication

Every API route needs a Locarr API key (`lk_…`) in the `X-Api-Key` header; sessions and cookies are
not accepted. The call runs as the key's owner. A read-only key may only use GET. The Radarr and
Sonarr routes need the owner to have `manageLibrary` (administrators always do). Errors are JSON
`{"code": "...", "message": "..."}`:

| Status | Code | When |
|---|---|---|
| 401 | `unauthenticated` | no key, unknown or revoked key, disabled user |
| 403 | `read_only` | a read-only key on POST |
| 403 | `forbidden` | missing permission (library read, request of that kind) |
| 400 | `validation` | bad parameters or body |
| 404 / 405 | `not_found` / `method_not_allowed` | unknown route or method |
| 429 | `quota_exceeded` | the owner's request quota is used up |
| 502 / 503 | `metadata_unavailable` / `metadata_key_missing` | TMDb unreachable / no TMDb key |

## Seerr (`/compat/seerr/api/v1`)

- `GET /status` → `{"appName": "Locarr", "version", "commitTag"}`.
- `GET /search?query=&page=&language=` and `GET /discover/trending?page=&language=` →
  `{page, totalPages, totalResults, results: [...]}`. Each result: `id` (TMDb id), `mediaType`
  (`movie`, `tv`, `person`), `title` + `releaseDate` (movies) or `name` + `firstAirDate` (series),
  `overview`, `posterPath`, `backdropPath` (TMDb paths with a leading `/`), `voteAverage`, and
  `mediaInfo: {tmdbId, status}` when Locarr knows the title. Search is TMDb only (no music) and has a
  single page. `language` (`hr`, `en-US`) applies to that call.
- `POST /request` with `{mediaType: "movie"|"tv", mediaId, seasons: "all"|[n...], is4k}` → 201
  `{id, status, is4k, createdAt, media: {tmdbId, mediaType, status, status4k}}`. Series with
  `seasons` `"all"` (or omitted) request every season and future ones. A title that is already
  requested or available answers 200 with its state and no `id`.

Media status (`mediaInfo.status`, `media.status`): 1 unknown, 2 pending, 3 processing (also "in the
library without a file"), 4 partially available, 5 available. Request status: 1 pending approval,
2 approved (also processing, partial, available, failed), 3 declined or cancelled.

## Radarr (`/compat/radarr/api/v3`)

- `GET /system/status` → `{appName, version}`.
- `GET /calendar?start=&end=&unmonitored=` (RFC 3339 or `YYYY-MM-DD`; default today + 2 days; at
  most 400 days; monitored movies unless `unmonitored=true`) → movies with an in-cinemas, digital or
  physical date in the range.
- `GET /movie[?tmdbId=]` → library movies.

Movie fields: `id`, `title`, `year`, `titleSlug` (= `id`), `tmdbId`, `imdbId`, `overview`,
`inCinemas`, `digitalRelease`, `physicalRelease` (midnight UTC), `hasFile`, `monitored`,
`images: [{coverType: "poster", remoteUrl}]` (public TMDb address).

## Sonarr (`/compat/sonarr/api/v3`)

- `GET /system/status` → `{appName, version}`.
- `GET /calendar?start=&end=&includeSeries=&unmonitored=` → episodes airing in the range:
  `id`, `seriesId`, `seasonNumber`, `episodeNumber`, `title`, `overview`, `airDate`, `airDateUtc`,
  `hasFile`, `monitored`, and `series` when `includeSeries=true`.
- `GET /series[?tvdbId=]` → library series: `id`, `title`, `year`, `titleSlug` (= `id`), `tvdbId`,
  `tmdbId`, `imdbId`, `network`, `monitored`, `images`.

## Title links

`GET /compat/radarr/movie/{id}` and `GET /compat/sonarr/series/{id}` (no key: browser links built
from the base URL and `titleSlug`) redirect to Locarr's `/movies/{id}` and `/series/{id}`.

## Tests

`server/internal/app/compat_integration_test.go` decodes every answer into the shapes the media
server reads. `scripts/test-streamero-compat.sh` starts the real Streamero image with PostgreSQL and
Valkey on a private Docker network and runs `go test -tags streamero` against it (also in CI).
