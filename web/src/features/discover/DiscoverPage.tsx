import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { api, imageUrl, queryKeys, unwrap, type DiscoverItem, type MediaRequest } from "@/api/client";
import { Alert, ErrorAlert, Spinner } from "@/components/ui";
import { RequestStatusBadge } from "@/features/requests/common";
import { useCurrentUser } from "@/features/auth/hooks";
import { RequestableArtists } from "@/features/music/discovery";
import { AvailabilityBadge, itemLink, PosterImage, PosterRow } from "./common";

export function useDiscoverHome() {
  return useQuery({ queryKey: queryKeys.discover, queryFn: async () => unwrap(await api.GET("/discover")), staleTime: 60_000 });
}

/** A search box that opens the discover search page. */
export function DiscoverSearchBox({ initial = "", autoFocus }: { initial?: string; autoFocus?: boolean }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [q, setQ] = useState(initial);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (q.trim()) void navigate({ to: "/discover/search", search: { q: q.trim(), type: "all" } });
  };
  return (
    <form onSubmit={submit} role="search" className="relative w-full">
      <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-faint" aria-hidden />
      <input type="search" value={q} onChange={(e) => setQ(e.target.value)} autoFocus={autoFocus}
        aria-label={t("discover.searchLabel")} placeholder={t("discover.searchPlaceholder")} data-testid="discover-search"
        className="h-10 w-full rounded-full border border-line-strong bg-surface-2/90 pl-9 pr-4 text-[14px] text-fg placeholder:text-faint focus:border-info focus:outline-none" />
    </form>
  );
}

function Hero({ item }: { item: DiscoverItem }) {
  const { t } = useTranslation();
  const link = itemLink(item);
  const bg = item.backdrop ? imageUrl(item.backdrop, "w1280") : "";
  return (
    <section className="relative -mx-4 -mt-5 overflow-hidden md:-mx-6" data-testid="discover-hero">
      {bg ? <img src={bg} alt="" className="absolute inset-0 size-full object-cover opacity-45" decoding="async" /> : null}
      <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/80 to-bg/30" />
      <div className="relative flex min-h-[240px] flex-col justify-end gap-3 px-4 pb-6 pt-16 md:min-h-[300px] md:px-6">
        <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-accent">{t("discover.rows.trending")}</p>
        <h1 className="max-w-2xl text-[26px] font-semibold leading-tight tracking-tight text-fg md:text-[34px]">{item.title}</h1>
        {item.overview ? <p className="line-clamp-2 max-w-2xl text-[13.5px] text-muted md:line-clamp-3">{item.overview}</p> : null}
        <div className="flex flex-wrap items-center gap-2">
          {link ? (
            <Link to={link.to as never} params={link.params as never}
              className="inline-flex h-9 items-center rounded-full bg-accent px-4 text-[13px] font-semibold text-accent-ink hover:bg-accent-hover">
              {t("discover.open")}
            </Link>
          ) : null}
          <AvailabilityBadge status={item.status} />
        </div>
        <div className="mt-2 max-w-xl">
          <DiscoverSearchBox />
        </div>
      </div>
    </section>
  );
}

/** What a request maps to on the discover side (for its poster and link). */
export function requestItem(r: MediaRequest): DiscoverItem {
  const mediaType = r.kind === "movie" ? "movie" : r.kind === "series" ? "series" : r.kind === "album" ? "album" : "artist";
  return { mediaType, tmdbId: r.tmdbId, mbid: r.mbid, artistMbid: r.artistMbid, title: r.title, subtitle: r.subtitle, year: r.year,
    poster: r.poster, status: "none", status4k: "none" };
}

function RequestCard({ r }: { r: MediaRequest }) {
  const item = requestItem(r);
  const link = itemLink(item);
  const body = (
    <>
      <PosterImage item={item} className="border border-line" />
      <div className="mt-1.5 flex flex-col gap-1 px-0.5">
        <p className="truncate text-[12.5px] font-medium text-fg">{r.title}</p>
        <RequestStatusBadge status={r.status} />
      </div>
    </>
  );
  const cls = "block w-[118px] shrink-0 snap-start rounded-ui outline-offset-4 sm:w-[132px]";
  return link ? <Link to={link.to as never} params={link.params as never} className={cls} data-testid={`my-request-${r.id}`}>{body}</Link>
    : <div className={cls}>{body}</div>;
}

export function DiscoverPage() {
  const { t } = useTranslation();
  const home = useDiscoverHome();
  const me = useCurrentUser();
  const facets = useQuery({ queryKey: queryKeys.discoverFacets, queryFn: async () => unwrap(await api.GET("/discover/genres")),
    staleTime: 3_600_000, enabled: home.data?.metadataReady === true });
  const hero = home.data?.rows.find((r) => r.items.some((i) => i.backdrop))?.items.find((i) => i.backdrop);
  return (
    <div className="flex flex-col gap-7" data-testid="discover-home">
      {hero ? <Hero item={hero} /> : (
        <div className="flex flex-col gap-3">
          <h1 className="text-[22px] font-semibold tracking-tight text-fg">{t("discover.title")}</h1>
          <DiscoverSearchBox />
        </div>
      )}
      {home.isPending ? <Spinner /> : null}
      <ErrorAlert error={home.error} />
      {home.data && !home.data.metadataReady ? <Alert tone="info">{t("discover.noMetadata")}</Alert> : null}
      {home.data && home.data.requests.length > 0 ? (
        <section className="flex flex-col gap-2" data-testid="discover-row-my_requests">
          <div className="flex items-end justify-between">
            <h2 className="text-[15px] font-semibold tracking-tight text-fg">{t("discover.rows.my_requests")}</h2>
            <Link to="/requests" className="text-[12px] text-muted hover:text-fg">{t("discover.seeAll")}</Link>
          </div>
          <div className="-mx-1 flex snap-x snap-mandatory gap-3 overflow-x-auto px-1 pb-2">
            {home.data.requests.map((r) => <RequestCard key={r.id} r={r} />)}
          </div>
        </section>
      ) : null}
      {home.data?.rows.map((row) => (
        <PosterRow key={row.key} title={t(`discover.rows.${row.key}`)} items={row.items} testId={`discover-row-${row.key}`}
          action={row.key !== "recently_added" ? (
            <Link to="/discover/list/$key" params={{ key: row.key }} search={{}} className="mr-1 text-[12px] text-muted hover:text-fg">
              {t("discover.seeAll")}
            </Link>
          ) : undefined} />
      ))}
      {home.data?.musicReady && (me.role === "admin" || me.permissions.requestMusic) ? <RequestableArtists /> : null}
      {facets.data ? (
        <section className="flex flex-col gap-4" data-testid="discover-facets">
          <FacetChips title={t("discover.movieGenres")} list="genre_movies" items={facets.data.movieGenres} />
          <FacetChips title={t("discover.seriesGenres")} list="genre_series" items={facets.data.seriesGenres} />
          <FacetChips title={t("discover.studios")} list="studio" items={facets.data.studios} />
          <FacetChips title={t("discover.networks")} list="network" items={facets.data.networks} />
        </section>
      ) : null}
    </div>
  );
}

function FacetChips({ title, list, items }: { title: string; list: "genre_movies" | "genre_series" | "studio" | "network"; items: { id: number; name: string }[] }) {
  if (items.length === 0) return null;
  return (
    <div className="flex flex-col gap-2">
      <h2 className="text-[13px] font-semibold text-muted">{title}</h2>
      <div className="flex flex-wrap gap-1.5">
        {items.map((f) => (
          <Link key={f.id} to="/discover/list/$key" params={{ key: list }} search={{ id: f.id, name: f.name }}
            className="inline-flex h-7 items-center rounded-full border border-line-strong bg-surface-2 px-3 text-[12px] text-fg hover:border-accent hover:text-accent">
            {f.name}
          </Link>
        ))}
      </div>
    </div>
  );
}
