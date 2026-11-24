import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorAlert } from "@/components/ui";
import { Modal } from "@/components/Modal";
import { splitTags } from "@/features/movies/AddMoviePage";
import { ArtistOptionsForm, defaultArtistOptions, type ArtistOptions } from "./AddArtistPage";
import { Cover, musicKeys } from "./common";

type S = components["schemas"];
export type ArtistSuggestion = S["ArtistSuggestion"];
export type ArtistSuggestionList = S["ArtistSuggestionList"];
export type DiscoveryProviderId = S["MusicDiscoveryProviderId"];
export type MusicDiscoverySettings = S["MusicDiscoverySettings"];

export const PROVIDERS: DiscoveryProviderId[] = ["lastfm", "listenbrainz", "musicbrainz", "spotify"];

export type SuggestionQuery = { seed?: string[]; tag?: string; minScore?: number; provider?: DiscoveryProviderId[]; limit?: number };

export const discoveryKeys = {
  all: ["music", "discovery"] as const,
  list: (q: SuggestionQuery) => ["music", "discovery", "list", q] as const,
  similar: (id: number) => ["music", "discovery", "similar", id] as const,
  exclusions: ["music", "discovery", "exclusions"] as const,
  settings: ["settings", "musicdiscovery"] as const,
};

/** Suggestions; polls while the providers are being asked. */
export function useArtistSuggestions(q: SuggestionQuery, enabled = true) {
  return useQuery({
    queryKey: discoveryKeys.list(q),
    queryFn: async () => unwrap(await api.GET("/music/discover/artists", { params: { query: {
      seed: q.seed?.length ? q.seed : undefined, tag: q.tag || undefined, minScore: q.minScore || undefined,
      provider: q.provider?.length ? q.provider : undefined, limit: q.limit ?? 48 } } })),
    enabled,
    refetchInterval: (query) => (query.state.data && (query.state.data.refreshing || query.state.data.pending > 0) ? 5_000 : false),
  });
}

/** "Similar to A, B and 3 more" with the strongest library artists first. */
export function whyLine(t: (k: string, o?: Record<string, unknown>) => string, s: ArtistSuggestion): string {
  const names = s.seeds.slice(0, 2).map((x) => x.name);
  const more = s.seedCount - names.length;
  if (names.length === 0) return "";
  if (more > 0) return t("discovery.similarToMore", { names: names.join(", "), count: more });
  return t(names.length === 2 ? "discovery.similarToTwo" : "discovery.similarTo", { a: names[0], b: names[1] });
}

/** Where the data came from, as the providers' terms ask. */
export function Attribution({ providers }: { providers: DiscoveryProviderId[] }) {
  const { t } = useTranslation();
  const links: Record<DiscoveryProviderId, string> = { lastfm: "https://www.last.fm", listenbrainz: "https://listenbrainz.org",
    musicbrainz: "https://musicbrainz.org", spotify: "https://open.spotify.com" };
  if (providers.length === 0) return null;
  return (
    <p className="text-[11.5px] text-faint" data-testid="discovery-attribution">
      {t("discovery.dataFrom")}{" "}
      {providers.map((p, i) => (
        <span key={p}>
          {i > 0 ? " · " : ""}
          <a href={links[p]} target="_blank" rel="noreferrer noopener" className="underline decoration-line-strong underline-offset-2 hover:text-fg">
            {t(`discovery.providers.${p}`)}
          </a>
        </span>
      ))}
      {" · "}{t("discovery.coversFrom")}
    </p>
  );
}

function ListenLinks({ s }: { s: ArtistSuggestion }) {
  const { t } = useTranslation();
  const list: [string, string | undefined][] = [["lastfm", s.links.lastfm], ["spotify", s.links.spotify], ["listenbrainz", s.links.listenbrainz],
    ["musicbrainz", s.links.musicbrainz]];
  return (
    <div className="flex flex-wrap gap-x-2.5 gap-y-1 text-[11.5px]">
      {list.filter(([, url]) => !!url).map(([p, url]) => (
        <a key={p} href={url} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-0.5 text-muted hover:text-fg"
          aria-label={t("discovery.openAt", { name: s.name, site: t(`discovery.providers.${p}`) })}>
          {t(`discovery.providers.${p}`)} <ExternalLink className="size-3" aria-hidden />
        </a>
      ))}
    </div>
  );
}

/** One suggested artist: cover, why it is suggested, tags, listen links and actions. */
export function SuggestionCard({ s, actions, selected, onSelect }:
  { s: ArtistSuggestion; actions?: ReactNode; selected?: boolean; onSelect?: (on: boolean) => void }) {
  const { t } = useTranslation();
  const why = whyLine(t, s);
  return (
    <li className={`group flex flex-col overflow-hidden rounded-ui border bg-surface transition-colors ${selected ? "border-accent" : "border-line"}`}
      data-testid="artist-suggestion" data-mbid={s.mbid}>
      <div className="relative">
        <Cover file={s.cover} title={s.name} size="250" className="rounded-none border-0 border-b border-line" />
        <span className="absolute right-2 top-2 rounded-full bg-black/70 px-2 py-0.5 text-[11px] font-semibold tabular-nums text-white"
          title={t("discovery.scoreHint")}>{t("discovery.score", { score: s.score })}</span>
        {onSelect ? (
          <label className="absolute left-2 top-2 flex size-7 cursor-pointer items-center justify-center rounded-full bg-black/60">
            <input type="checkbox" checked={!!selected} onChange={(e) => onSelect(e.target.checked)} className="size-4 accent-[var(--color-accent)]"
              aria-label={t("discovery.select", { name: s.name })} />
          </label>
        ) : null}
      </div>
      <div className="flex flex-1 flex-col gap-1.5 p-3">
        <div className="min-w-0">
          <h3 className="truncate text-[14px] font-semibold text-fg" title={s.name}>{s.name}</h3>
          {s.disambiguation ? <p className="truncate text-[12px] text-muted">{s.disambiguation}</p> : null}
        </div>
        {why ? <p className="line-clamp-2 text-[12px] text-muted" data-testid="suggestion-why">{why}</p> : null}
        {s.tags.length ? <p className="truncate text-[11.5px] text-faint">{t("discovery.tags", { tags: s.tags.slice(0, 3).join(", ") })}</p> : null}
        {s.matchedByName ? <p className="text-[11.5px] text-warning" title={t("discovery.matchedByNameHint")}>{t("discovery.matchedByName")}</p> : null}
        <ListenLinks s={s} />
        {actions ? <div className="mt-auto flex flex-wrap gap-1.5 pt-1.5">{actions}</div> : null}
      </div>
    </li>
  );
}

/** Adds one or more suggested artists with the same options, one after another. */
export function AddSuggestionsDialog({ items, onClose, onAdded }: { items: ArtistSuggestion[]; onClose: () => void; onAdded?: (ids: number[]) => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [opts, setOpts] = useState<ArtistOptions>(defaultArtistOptions);
  const [done, setDone] = useState(0);
  const [failed, setFailed] = useState<string[]>([]);
  const add = useMutation({
    mutationFn: async () => {
      const ids: number[] = [];
      const bad: string[] = [];
      setDone(0);
      for (const s of items) {
        const res = await api.POST("/music/artists", { body: { mbid: s.mbid, rootFolderId: opts.rootFolderId, qualityProfileId: opts.qualityProfileId,
          metadataProfileId: opts.metadataProfileId, monitored: opts.monitored, monitor: opts.monitor, monitorNewAlbums: opts.monitorNewAlbums,
          tags: splitTags(opts.tags), searchOnAdd: opts.searchOnAdd } });
        if (res.response.ok && res.data) ids.push(res.data.id);
        else if (res.response.status !== 409) bad.push(s.name);
        setDone((n) => n + 1);
      }
      setFailed(bad);
      return { ids, bad };
    },
    onSuccess: ({ ids, bad }) => {
      void qc.invalidateQueries({ queryKey: musicKeys.artists });
      void qc.invalidateQueries({ queryKey: discoveryKeys.all });
      onAdded?.(ids);
      if (bad.length === 0) onClose();
    },
  });
  const title = items.length === 1 ? t("discovery.addOne", { name: items[0]?.name ?? "" }) : t("discovery.addMany", { count: items.length });
  return (
    <Modal title={title} onClose={onClose} wide testId="add-suggestions-dialog"
      footer={(
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" busy={add.isPending} onClick={() => add.mutate()} data-testid="add-suggestions-confirm"
            disabled={!opts.rootFolderId || !opts.qualityProfileId || !opts.metadataProfileId}>
            {items.length === 1 ? t("addArtist.add") : t("discovery.addCount", { count: items.length })}
          </Button>
        </>
      )}>
      <ArtistOptionsForm value={opts} onChange={setOpts} />
      {add.isPending && items.length > 1 ? <p role="status" className="mt-3 text-[13px] text-muted">{t("discovery.adding", { done, count: items.length })}</p> : null}
      {failed.length ? <p role="alert" className="mt-3 text-[13px] text-danger">{t("discovery.addFailed", { names: failed.join(", ") })}</p> : null}
      <ErrorAlert error={add.error} />
    </Modal>
  );
}

/** "Not interested": the artist is no longer suggested (it can be undone under Not interested). */
export function useExclude() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (s: ArtistSuggestion) => unwrap(await api.POST("/music/discover/exclusions", { body: { mbid: s.mbid, name: s.name } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: discoveryKeys.all }),
  });
}

/** The link to an artist in the library. */
export function InLibraryLink({ id }: { id: number }) {
  const { t } = useTranslation();
  return (
    <Link to="/music/artists/$id" params={{ id: String(id) }} className="inline-flex h-7 items-center text-[12.5px] text-accent hover:underline">
      {t("addMovie.inLibrary")}
    </Link>
  );
}

/** An artist's similar artists (the artist page). */
export function SimilarArtists({ artistId, mbid }: { artistId: number; mbid: string }) {
  const { t } = useTranslation();
  const [adding, setAdding] = useState<ArtistSuggestion[] | null>(null);
  const exclude = useExclude();
  const list = useQuery({
    queryKey: discoveryKeys.similar(artistId),
    queryFn: async () => unwrap(await api.GET("/music/artists/{id}/similar", { params: { path: { id: artistId }, query: { limit: 12 } } })),
    refetchInterval: (q) => (q.state.data && (q.state.data.refreshing || q.state.data.pending > 0) ? 5_000 : false),
  });
  const data = list.data;
  if (!data || (data.items.length === 0 && !data.refreshing && data.pending === 0)) return null;
  const used = PROVIDERS.filter((p) => data.items.some((s) => s.providers.includes(p)));
  return (
    <section className="mt-6" aria-labelledby="similar-artists" data-testid="similar-artists">
      <div className="mb-2 flex items-end justify-between gap-2">
        <h2 id="similar-artists" className="text-[13px] font-semibold uppercase tracking-wide text-muted">{t("discovery.similarTitle")}</h2>
        <Link to="/music/discover" search={{ seed: [mbid] }} className="text-[12px] text-muted hover:text-fg">{t("discovery.moreLikeThis")}</Link>
      </div>
      {data.items.length === 0 ? <p role="status" className="text-[13px] text-muted">{t("discovery.working", { count: data.pending })}</p> : (
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-7">
          {data.items.map((s) => (
            <SuggestionCard key={s.mbid} s={s} actions={s.artistId ? <InLibraryLink id={s.artistId} /> : (
              <>
                <Button variant="primary" onClick={() => setAdding([s])} data-testid={`add-${s.mbid}`}>{t("discovery.add")}</Button>
                <Button variant="ghost" onClick={() => exclude.mutate(s)}>{t("discovery.notInterested")}</Button>
              </>
            )} />
          ))}
        </ul>
      )}
      <div className="mt-2"><Attribution providers={used} /></div>
      {adding ? <AddSuggestionsDialog items={adding} onClose={() => setAdding(null)} /> : null}
    </section>
  );
}

/** Discover's "Artists you might like": the same suggestions with Request instead of Add. */
export function RequestableArtists() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useArtistSuggestions({ limit: 12 });
  const request = useMutation({
    mutationFn: async (s: ArtistSuggestion) => unwrap(await api.POST("/requests", { body: { kind: "artist", mbid: s.mbid } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: discoveryKeys.all });
      void qc.invalidateQueries({ queryKey: ["requests"] });
    },
  });
  const data = list.data;
  if (!data || !data.canRequest || data.items.length === 0) return null;
  const used = PROVIDERS.filter((p) => data.items.some((s) => s.providers.includes(p)));
  return (
    <section className="flex flex-col gap-2" data-testid="discover-row-artist_suggestions">
      <h2 className="text-[15px] font-semibold tracking-tight text-fg">{t("discovery.youMightLike")}</h2>
      <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-7">
        {data.items.map((s) => (
          <SuggestionCard key={s.mbid} s={s} actions={s.status === "none" ? (
            <Button variant="primary" onClick={() => request.mutate(s)} busy={request.isPending && request.variables?.mbid === s.mbid}
              data-testid={`request-${s.mbid}`}>{t("discovery.request")}</Button>
          ) : (
            <span className="text-[12.5px] text-muted" data-testid={`status-${s.mbid}`}>{t(`discovery.status.${s.status}`)}</span>
          )} />
        ))}
      </ul>
      <ErrorAlert error={request.error} />
      <Attribution providers={used} />
    </section>
  );
}
