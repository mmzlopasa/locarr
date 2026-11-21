import { useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { FolderInput, LayoutGrid, Plus, Rows3 } from "lucide-react";
import { api, queryKeys, unwrap, type Series, type SeriesStatus } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Badge, Button, EmptyState, ErrorAlert, PageHeader, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { Poster, useQualityProfiles } from "@/features/movies/common";
import { columnsFor, pagesFor, useScrollBox, visibleRows } from "@/features/movies/virtual";
import { SeriesStatusBadge } from "./common";

const PAGE = 200;
const STATUSES: SeriesStatus[] = ["continuing", "ended", "upcoming"];
const SORTS = ["title", "year", "added", "network", "rating", "nextAiring", "previousAiring", "missing", "size"] as const;
type Sort = (typeof SORTS)[number];

export type SeriesFilter = {
  q: string;
  status: SeriesStatus | "";
  missing: boolean;
  monitored: "" | "true" | "false";
  profileId: number;
  tag: string;
  sort: Sort;
  desc: boolean;
};

const VIEW_KEY = "locarr.series.view";

function loadView(): "grid" | "table" {
  try {
    return localStorage.getItem(VIEW_KEY) === "table" ? "table" : "grid";
  } catch {
    return "grid";
  }
}

export function seriesQuery(f: SeriesFilter, page: number) {
  return {
    q: f.q || undefined,
    status: f.status || undefined,
    missing: f.missing || undefined,
    monitored: f.monitored === "" ? undefined : f.monitored === "true",
    profileId: f.profileId || undefined,
    tag: f.tag || undefined,
    sort: f.sort,
    desc: f.desc,
    offset: page * PAGE,
    limit: PAGE,
  };
}

function usePagedSeries(f: SeriesFilter, firstItem: number, lastItem: number) {
  const pages = pagesFor(firstItem, lastItem, PAGE);
  const results = useQueries({
    queries: pages.map((p) => ({
      queryKey: [...queryKeys.seriesList, "page", f, p],
      queryFn: async () => unwrap(await api.GET("/series", { params: { query: seriesQuery(f, p) } })),
      staleTime: 15_000,
      placeholderData: (prev: unknown) => prev as never,
    })),
  });
  const items = new Map<number, Series>();
  results.forEach((r, i) => {
    r.data?.items.forEach((s, j) => items.set((pages[i] ?? 0) * PAGE + j, s));
  });
  return { items, error: results.find((r) => r.error)?.error };
}

export function SeriesPage() {
  const { t } = useTranslation();
  const me = useCurrentUser();
  const canManage = me.role === "admin" || me.permissions.manageLibrary;
  const [view, setViewState] = useState<"grid" | "table">(loadView);
  const setView = (v: "grid" | "table") => {
    setViewState(v);
    try {
      localStorage.setItem(VIEW_KEY, v);
    } catch {
      // a remembered view is only a convenience
    }
  };
  const [f, setF] = useState<SeriesFilter>({ q: "", status: "", missing: false, monitored: "", profileId: 0, tag: "", sort: "title", desc: false });
  const set = <K extends keyof SeriesFilter>(k: K, v: SeriesFilter[K]) => setF((x) => ({ ...x, [k]: v }));
  const profiles = useQualityProfiles();
  const seriesProfiles = (profiles.data ?? []).filter((p) => p.kind === "series");
  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        title={t("nav.series")}
        actions={canManage ? (
          <>
            <Link to="/series/import" className="inline-flex h-8 items-center gap-1.5 rounded-ui border border-line-strong bg-surface-3 px-3 text-[13px] hover:bg-line">
              <FolderInput className="size-3.5" /> {t("series.importExisting")}
            </Link>
            <Link to="/series/add" data-testid="add-series"
              className="inline-flex h-8 items-center gap-1.5 rounded-ui bg-accent px-3 text-[13px] font-semibold text-accent-ink hover:bg-accent-hover">
              <Plus className="size-3.5" /> {t("series.add")}
            </Link>
          </>
        ) : null}
      />
      <div className="mb-3 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-7">
        <div className="col-span-2 lg:col-span-2">
          <TextInput label={t("movies.filter.search")} value={f.q} onChange={(e) => set("q", e.target.value)} placeholder={t("series.filter.searchHint")} />
        </div>
        <SelectInput label={t("series.filter.status")} value={f.status} onChange={(e) => set("status", e.target.value as SeriesStatus | "")}>
          <option value="">{t("movies.filter.all")}</option>
          {STATUSES.map((s) => <option key={s} value={s}>{t(`series.status.${s}`)}</option>)}
        </SelectInput>
        <SelectInput label={t("series.filter.episodes")} value={f.missing ? "missing" : ""} onChange={(e) => set("missing", e.target.value === "missing")}>
          <option value="">{t("movies.filter.all")}</option>
          <option value="missing">{t("series.filter.missing")}</option>
        </SelectInput>
        <SelectInput label={t("movies.filter.monitored")} value={f.monitored} onChange={(e) => set("monitored", e.target.value as SeriesFilter["monitored"])}>
          <option value="">{t("movies.filter.all")}</option>
          <option value="true">{t("movies.monitored")}</option>
          <option value="false">{t("movies.unmonitored")}</option>
        </SelectInput>
        <SelectInput label={t("movies.filter.profile")} value={String(f.profileId)} onChange={(e) => set("profileId", Number(e.target.value))}>
          <option value="0">{t("movies.filter.all")}</option>
          {seriesProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <div className="flex items-end gap-1">
          <SelectInput label={t("movies.filter.sort")} value={f.sort} onChange={(e) => set("sort", e.target.value as Sort)}>
            {SORTS.map((s) => <option key={s} value={s}>{t(`series.sort.${s}`)}</option>)}
          </SelectInput>
          <Button variant="ghost" onClick={() => set("desc", !f.desc)} aria-label={t("movies.filter.direction")} title={t("movies.filter.direction")}>
            {f.desc ? "↓" : "↑"}
          </Button>
        </div>
      </div>
      <div className="mb-2 flex items-center justify-end gap-1" role="group" aria-label={t("movies.view.label")}>
        <Button variant={view === "grid" ? "secondary" : "ghost"} onClick={() => setView("grid")} aria-pressed={view === "grid"}>
          <LayoutGrid className="size-3.5" /> {t("movies.view.grid")}
        </Button>
        <Button variant={view === "table" ? "secondary" : "ghost"} onClick={() => setView("table")} aria-pressed={view === "table"}>
          <Rows3 className="size-3.5" /> {t("movies.view.table")}
        </Button>
      </div>
      <Library filter={f} view={view} />
    </div>
  );
}

const MIN_CARD = 150;
const GAP = 12;
const TABLE_ROW = 40;
const COLS = "grid-cols-[1fr_140px_110px_110px_90px_150px_90px]";

function nextAir(s: Series, locale: string): string {
  return s.stats.nextAiring ? new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(s.stats.nextAiring)) : "–";
}

function Library({ filter, view }: { filter: SeriesFilter; view: "grid" | "table" }) {
  const { t, i18n } = useTranslation();
  const box = useScrollBox<HTMLDivElement>();
  const cols = view === "grid" ? columnsFor(box.width - 4, MIN_CARD, GAP) : 1;
  const cardW = view === "grid" ? (box.width - 4 - GAP * (cols - 1)) / cols : 0;
  const rowH = view === "grid" ? Math.round(cardW * 1.5 + 64 + GAP) : TABLE_ROW;
  const first = useQuery({
    queryKey: [...queryKeys.seriesList, "page", filter, 0],
    queryFn: async () => unwrap(await api.GET("/series", { params: { query: seriesQuery(filter, 0) } })),
    staleTime: 15_000,
  });
  const total = first.data?.total ?? 0;
  const rows = Math.ceil(total / cols);
  const { first: r0, last: r1 } = visibleRows(box.scrollTop, box.height, rowH, rows);
  const paged = usePagedSeries(filter, r0 * cols, Math.min(total - 1, (r1 + 1) * cols - 1));
  const rowsToDraw = useMemo(() => {
    const out: number[] = [];
    for (let r = r0; r <= r1; r++) out.push(r);
    return out;
  }, [r0, r1]);
  return (
    <div ref={box.ref} className="min-h-[420px] flex-1 overflow-y-auto" data-testid="series-library" style={{ height: "calc(100vh - 260px)" }}>
      {first.isLoading ? <Spinner /> : null}
      <ErrorAlert error={first.error ?? paged.error} />
      {first.data && total === 0 ? <EmptyState>{t("series.empty")}</EmptyState> : null}
      {total > 0 && view === "table" ? (
        <div role="table" aria-label={t("nav.series")} aria-rowcount={total} className="min-w-[860px] text-[13px]">
          <div role="row" className={`sticky top-0 z-10 grid h-8 ${COLS} items-center gap-2 border-b border-line bg-bg px-2 text-[12px] text-muted`}>
            <span role="columnheader">{t("movies.columns.title")}</span>
            <span role="columnheader">{t("series.columns.network")}</span>
            <span role="columnheader">{t("movies.columns.status")}</span>
            <span role="columnheader">{t("series.columns.episodes")}</span>
            <span role="columnheader">{t("series.columns.missing")}</span>
            <span role="columnheader">{t("series.columns.nextAiring")}</span>
            <span role="columnheader">{t("movies.columns.size")}</span>
          </div>
          <div style={{ height: rows * rowH, position: "relative" }}>
            {rowsToDraw.map((r) => {
              const s = paged.items.get(r);
              return (
                <div key={r} role="row" aria-rowindex={r + 2} style={{ position: "absolute", top: r * rowH, height: rowH, left: 0, right: 0 }}
                  className={`grid ${COLS} items-center gap-2 border-b border-line px-2 hover:bg-surface-2`}>
                  {s ? (
                    <>
                      <Link to="/series/$id" params={{ id: String(s.id) }} className="truncate text-fg hover:text-accent" role="cell">
                        {s.title} <span className="text-faint tabular-nums">{s.year || ""}</span>
                      </Link>
                      <span role="cell" className="truncate text-muted">{s.network || "–"}</span>
                      <span role="cell"><SeriesStatusBadge status={s.status} /></span>
                      <span role="cell" className="tabular-nums text-muted">{s.stats.episodeFileCount}/{s.stats.episodeCount}</span>
                      <span role="cell">{s.stats.missingCount > 0 ? <Badge tone="danger">{s.stats.missingCount}</Badge> : <span className="text-faint">0</span>}</span>
                      <span role="cell" className="tabular-nums text-muted">{nextAir(s, i18n.language)}</span>
                      <span role="cell" className="tabular-nums text-muted">{s.stats.sizeOnDisk ? formatBytes(s.stats.sizeOnDisk, i18n.language) : "–"}</span>
                    </>
                  ) : <span className="col-span-7 h-3 w-1/3 animate-pulse rounded bg-surface-3" />}
                </div>
              );
            })}
          </div>
        </div>
      ) : null}
      {total > 0 && view === "grid" ? (
        <ul aria-label={t("nav.series")} style={{ height: rows * rowH, position: "relative" }}>
          {rowsToDraw.flatMap((r) =>
            Array.from({ length: cols }, (_, c) => {
              const idx = r * cols + c;
              if (idx >= total) return null;
              const s = paged.items.get(idx);
              return (
                <li key={idx} style={{ position: "absolute", top: r * rowH, left: c * (cardW + GAP), width: cardW }}>
                  {s ? <Card s={s} /> : <div className="aspect-[2/3] animate-pulse rounded-ui bg-surface-2" />}
                </li>
              );
            }),
          )}
        </ul>
      ) : null}
    </div>
  );
}

function Card({ s }: { s: Series }) {
  const { t } = useTranslation();
  const pct = s.stats.episodeCount ? Math.round((s.stats.episodeFileCount / s.stats.episodeCount) * 100) : 0;
  const bar = s.stats.missingCount > 0 ? "bg-danger" : pct >= 100 ? "bg-success" : "bg-info";
  return (
    <Link to="/series/$id" params={{ id: String(s.id) }} className="group block" data-testid="series-card">
      <div className="relative">
        <Poster file={s.poster} title={s.title} className="transition-[border-color] group-hover:border-line-strong" />
        {!s.monitored ? <span className="absolute left-1.5 top-1.5 size-2 rounded-full bg-faint ring-2 ring-bg" aria-hidden /> : null}
        {s.downloading > 0 ? <span className="absolute right-1.5 top-1.5"><Badge tone="info">{t("series.downloading", { count: s.downloading })}</Badge></span> : null}
      </div>
      <div className="mt-1.5 truncate text-[13px] font-medium text-fg group-hover:text-accent">{s.title}</div>
      <div className="flex items-center justify-between gap-1 text-[12px]">
        <span className="truncate text-muted">{[s.year || null, s.network || null].filter(Boolean).join(" · ") || "–"}</span>
        <span className="shrink-0 tabular-nums text-muted" title={t("series.episodeFiles")}>{s.stats.episodeFileCount}/{s.stats.episodeCount}</span>
      </div>
      <div className="mt-1 h-1 overflow-hidden rounded-full bg-surface-3" aria-hidden>
        <div className={`h-full ${bar}`} style={{ width: `${pct}%` }} />
      </div>
      {s.stats.missingCount > 0 ? <div className="mt-0.5 text-[11px] text-danger">{t("series.missingCount", { count: s.stats.missingCount })}</div> : null}
    </Link>
  );
}
