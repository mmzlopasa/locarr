import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { api, queryKeys, unwrap } from "@/api/client";
import { Button, EmptyState, ErrorAlert, PageHeader, Spinner } from "@/components/ui";
import { StatusBadge } from "@/features/movies/common";
import { episodeCode, formatAir } from "@/features/series/common";
import { WantedAlbums } from "@/features/music/WantedAlbums";
import { WantedSubtitles } from "@/features/subtitles/WantedSubtitles";

const PAGE = 100;

type Kind = "missing" | "cutoff_unmet";

/** Monitored movies, episodes and albums that are missing or below the profile's cutoff, with bulk search. */
export function WantedPage() {
  const { t } = useTranslation();
  const [media, setMedia] = useState<"movies" | "episodes" | "albums" | "subtitles">("movies");
  const [kind, setKind] = useState<Kind>("missing");
  return (
    <div>
      <div className="mb-2 flex justify-end gap-1" role="group" aria-label={t("wanted.media")}>
        {(["movies", "episodes", "albums", "subtitles"] as const).map((m) => (
          <Button key={m} variant={media === m ? "secondary" : "ghost"} aria-pressed={media === m} onClick={() => setMedia(m)} data-testid={`wanted-${m}`}>
            {t(`wanted.mediaKinds.${m}`)}
          </Button>
        ))}
      </div>
      {media === "movies" ? <WantedMovies kind={kind} setKind={setKind} /> : null}
      {media === "episodes" ? <WantedEpisodes kind={kind} setKind={setKind} /> : null}
      {media === "albums" ? <WantedAlbums /> : null}
      {media === "subtitles" ? <WantedSubtitles /> : null}
    </div>
  );
}

function KindTabs({ kind, setKind, reset }: { kind: Kind; setKind: (k: Kind) => void; reset: () => void }) {
  const { t } = useTranslation();
  return (
    <div role="tablist" aria-label={t("nav.wanted")} className="mb-3 flex gap-1 border-b border-line">
      {(["missing", "cutoff_unmet"] as const).map((k) => (
        <button key={k} type="button" role="tab" aria-selected={kind === k} onClick={() => { setKind(k); reset(); }}
          className={`-mb-px border-b-2 px-3 py-2 text-[13px] ${kind === k ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
          {t(`wanted.tabs.${k}`)}
        </button>
      ))}
    </div>
  );
}

function WantedEpisodes({ kind, setKind }: { kind: Kind; setKind: (k: Kind) => void }) {
  const { t, i18n } = useTranslation();
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const list = useQuery({
    queryKey: queryKeys.wantedEpisodes(kind, page),
    queryFn: async () => unwrap(await api.GET("/episodes/wanted", { params: { query: { kind, offset: page * PAGE, limit: PAGE } } })),
  });
  const search = useMutation({
    mutationFn: async (ids: number[]) => unwrap(await api.POST("/episodes/search", { body: { episodeIds: ids } })),
    onSuccess: () => setSelected(new Set()),
  });
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;
  const toggle = (id: number) => setSelected((s) => {
    const n = new Set(s);
    if (n.has(id)) n.delete(id);
    else n.add(id);
    return n;
  });
  return (
    <div>
      <PageHeader title={t("nav.wanted")} description={t("wanted.episodesDescription")}
        actions={
          <>
            <Button onClick={() => search.mutate([...selected])} disabled={selected.size === 0} busy={search.isPending}>
              <Search className="size-3.5" /> {t("wanted.searchSelected", { count: selected.size })}
            </Button>
            <Button variant="primary" onClick={() => search.mutate(items.map((e) => e.id))} disabled={items.length === 0} busy={search.isPending}>
              <Search className="size-3.5" /> {t("wanted.searchPage")}
            </Button>
          </>
        } />
      <KindTabs kind={kind} setKind={setKind} reset={() => { setPage(0); setSelected(new Set()); }} />
      {search.data ? <p role="status" className="mb-2 text-[13px] text-success">{t("wanted.queued", { count: search.data.count })}</p> : null}
      <ErrorAlert error={list.error ?? search.error} />
      {list.isLoading ? <Spinner /> : null}
      {list.data && items.length === 0 ? <EmptyState>{t("wanted.emptyEpisodes")}</EmptyState> : null}
      {items.length > 0 ? (
        <table className="w-full text-[13px]" data-testid="wanted-episodes">
          <thead className="text-left text-[12px] text-muted">
            <tr className="border-b border-line">
              <th className="w-8 px-2 py-1.5">
                <input type="checkbox" aria-label={t("wanted.selectAll")} checked={selected.size === items.length}
                  onChange={(e) => setSelected(e.target.checked ? new Set(items.map((x) => x.id)) : new Set())} className="accent-[var(--lc-accent)]" />
              </th>
              <th className="px-2 py-1.5 font-medium">{t("nav.series")}</th>
              <th className="px-2 py-1.5 font-medium">#</th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.title")}</th>
              <th className="px-2 py-1.5 font-medium">{t("episode.airs")}</th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.quality")}</th>
            </tr>
          </thead>
          <tbody>
            {items.map((e) => (
              <tr key={e.id} className="border-b border-line hover:bg-surface-2">
                <td className="px-2 py-1.5">
                  <input type="checkbox" aria-label={`${e.seriesTitle} ${episodeCode(e)}`} checked={selected.has(e.id)} onChange={() => toggle(e.id)}
                    className="accent-[var(--lc-accent)]" />
                </td>
                <td className="px-2 py-1.5"><Link to="/series/$id" params={{ id: String(e.seriesId) }} className="text-fg hover:text-accent">{e.seriesTitle}</Link></td>
                <td className="px-2 py-1.5 font-mono text-[12px] text-muted">{e.seriesType === "daily" ? e.airDate : episodeCode(e)}</td>
                <td className="px-2 py-1.5 text-fg">{e.title || "–"}</td>
                <td className="px-2 py-1.5 tabular-nums text-muted">{formatAir(e, i18n.language) || "–"}</td>
                <td className="px-2 py-1.5 text-muted">{e.file?.quality.name ?? "–"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {total > PAGE ? (
        <div className="mt-3 flex items-center justify-end gap-2 text-[13px] text-muted">
          <Button variant="ghost" disabled={page === 0} onClick={() => setPage(page - 1)}>{t("wanted.previous")}</Button>
          <span>{t("wanted.page", { page: page + 1, pages: Math.ceil(total / PAGE) })}</span>
          <Button variant="ghost" disabled={(page + 1) * PAGE >= total} onClick={() => setPage(page + 1)}>{t("wanted.next")}</Button>
        </div>
      ) : null}
    </div>
  );
}

function WantedMovies({ kind, setKind }: { kind: Kind; setKind: (k: Kind) => void }) {
  const { t } = useTranslation();
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const list = useQuery({
    queryKey: ["wanted", kind, page],
    queryFn: async () => unwrap(await api.GET("/movies", { params: { query: { status: kind, sort: "digital", desc: true, offset: page * PAGE, limit: PAGE } } })),
  });
  const search = useMutation({
    mutationFn: async (ids: number[]) => unwrap(await api.POST("/movies/search", { body: { movieIds: ids } })),
    onSuccess: () => setSelected(new Set()),
  });
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;
  const toggle = (id: number) => setSelected((s) => {
    const n = new Set(s);
    if (n.has(id)) n.delete(id);
    else n.add(id);
    return n;
  });
  return (
    <div>
      <PageHeader title={t("nav.wanted")} description={t("wanted.description")}
        actions={
          <>
            <Button onClick={() => search.mutate([...selected])} disabled={selected.size === 0} busy={search.isPending}>
              <Search className="size-3.5" /> {t("wanted.searchSelected", { count: selected.size })}
            </Button>
            <Button variant="primary" onClick={() => search.mutate(items.map((m) => m.id))} disabled={items.length === 0} busy={search.isPending}>
              <Search className="size-3.5" /> {t("wanted.searchPage")}
            </Button>
          </>
        } />
      <KindTabs kind={kind} setKind={setKind} reset={() => { setPage(0); setSelected(new Set()); }} />
      {search.data ? <p role="status" className="mb-2 text-[13px] text-success">{t("wanted.queued", { count: search.data.count })}</p> : null}
      <ErrorAlert error={list.error ?? search.error} />
      {list.isLoading ? <Spinner /> : null}
      {list.data && items.length === 0 ? <EmptyState>{t("wanted.empty")}</EmptyState> : null}
      {items.length > 0 ? (
        <table className="w-full text-[13px]">
          <thead className="text-left text-[12px] text-muted">
            <tr className="border-b border-line">
              <th className="w-8 px-2 py-1.5">
                <input type="checkbox" aria-label={t("wanted.selectAll")} checked={selected.size === items.length}
                  onChange={(e) => setSelected(e.target.checked ? new Set(items.map((m) => m.id)) : new Set())} className="accent-[var(--lc-accent)]" />
              </th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.title")}</th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.status")}</th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.quality")}</th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.digital")}</th>
            </tr>
          </thead>
          <tbody>
            {items.map((m) => (
              <tr key={m.id} className="border-b border-line hover:bg-surface-2">
                <td className="px-2 py-1.5">
                  <input type="checkbox" aria-label={m.title} checked={selected.has(m.id)} onChange={() => toggle(m.id)} className="accent-[var(--lc-accent)]" />
                </td>
                <td className="px-2 py-1.5">
                  <Link to="/movies/$id" params={{ id: String(m.id) }} className="text-fg hover:text-accent">{m.title}</Link>
                  <span className="ml-1.5 text-[12px] text-faint">{m.year || ""}</span>
                </td>
                <td className="px-2 py-1.5"><StatusBadge status={m.libraryStatus} /></td>
                <td className="px-2 py-1.5 text-muted">{m.file?.quality.name ?? "–"}</td>
                <td className="px-2 py-1.5 tabular-nums text-muted">{m.digitalRelease || m.physicalRelease || m.inCinemas || "–"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {total > PAGE ? (
        <div className="mt-3 flex items-center justify-end gap-2 text-[13px] text-muted">
          <Button variant="ghost" disabled={page === 0} onClick={() => setPage(page - 1)}>{t("wanted.previous")}</Button>
          <span>{t("wanted.page", { page: page + 1, pages: Math.ceil(total / PAGE) })}</span>
          <Button variant="ghost" disabled={(page + 1) * PAGE >= total} onClick={() => setPage(page + 1)}>{t("wanted.next")}</Button>
        </div>
      ) : null}
    </div>
  );
}
