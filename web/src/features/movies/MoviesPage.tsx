import { useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { FolderInput, LayoutGrid, Plus, Rows3 } from "lucide-react";
import { api, queryKeys, unwrap, type Movie, type MovieStatus } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Button, EmptyState, ErrorAlert, PageHeader, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { Poster, StatusBadge, useQualityProfiles } from "./common";
import { columnsFor, pagesFor, useScrollBox, visibleRows } from "./virtual";

const PAGE = 200;
const STATUSES: MovieStatus[] = ["missing", "downloading", "downloaded", "cutoff_unmet", "unavailable", "unmonitored"];
const SORTS = ["title", "year", "added", "rating", "popularity", "size", "inCinemas", "digital", "physical"] as const;
type Sort = (typeof SORTS)[number];

export type MovieFilter = {
  q: string;
  status: MovieStatus | "";
  monitored: "" | "true" | "false";
  profileId: number | 0;
  tag: string;
  sort: Sort;
  desc: boolean;
};

const VIEW_KEY = "locarr.movies.view";

function loadView(): "grid" | "table" {
  try {
    return localStorage.getItem(VIEW_KEY) === "table" ? "table" : "grid";
  } catch {
    return "grid";
  }
}

function query(f: MovieFilter, page: number) {
  return {
    q: f.q || undefined,
    status: f.status || undefined,
    monitored: f.monitored === "" ? undefined : f.monitored === "true",
    profileId: f.profileId || undefined,
    tag: f.tag || undefined,
    sort: f.sort,
    desc: f.desc,
    offset: page * PAGE,
    limit: PAGE,
  };
}

/** Loads the pages that hold the visible items (10k movies stay fast). */
function usePagedMovies(f: MovieFilter, firstItem: number, lastItem: number) {
  const pages = pagesFor(firstItem, lastItem, PAGE);
  const results = useQueries({
    queries: pages.map((p) => ({
      queryKey: [...queryKeys.movies, "page", f, p],
      queryFn: async () => unwrap(await api.GET("/movies", { params: { query: query(f, p) } })),
      staleTime: 15_000,
      placeholderData: (prev: unknown) => prev as never,
    })),
  });
  const items = new Map<number, Movie>();
  let total: number | undefined;
  results.forEach((r, i) => {
    if (!r.data) return;
    total = r.data.total;
    r.data.items.forEach((m, j) => items.set((pages[i] ?? 0) * PAGE + j, m));
  });
  return { items, total, loading: results.some((r) => r.isLoading), error: results.find((r) => r.error)?.error };
}

export function MoviesPage() {
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
  const [f, setF] = useState<MovieFilter>({ q: "", status: "", monitored: "", profileId: 0, tag: "", sort: "title", desc: false });
  const set = <K extends keyof MovieFilter>(k: K, v: MovieFilter[K]) => setF((x) => ({ ...x, [k]: v }));
  const profiles = useQualityProfiles();
  const movieProfiles = (profiles.data ?? []).filter((p) => p.kind === "movies");

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        title={t("nav.movies")}
        actions={canManage ? (
          <>
            <Link to="/movies/import" className="inline-flex h-8 items-center gap-1.5 rounded-ui border border-line-strong bg-surface-3 px-3 text-[13px] hover:bg-line">
              <FolderInput className="size-3.5" /> {t("movies.importExisting")}
            </Link>
            <Link to="/movies/add" data-testid="add-movie"
              className="inline-flex h-8 items-center gap-1.5 rounded-ui bg-accent px-3 text-[13px] font-semibold text-accent-ink hover:bg-accent-hover">
              <Plus className="size-3.5" /> {t("movies.add")}
            </Link>
          </>
        ) : null}
      />
      <div className="mb-3 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-7">
        <div className="col-span-2 lg:col-span-2">
          <TextInput label={t("movies.filter.search")} value={f.q} onChange={(e) => set("q", e.target.value)} placeholder={t("movies.filter.searchHint")} />
        </div>
        <SelectInput label={t("movies.filter.status")} value={f.status} onChange={(e) => set("status", e.target.value as MovieStatus | "")}>
          <option value="">{t("movies.filter.all")}</option>
          {STATUSES.map((s) => <option key={s} value={s}>{t(`movies.status.${s}`)}</option>)}
        </SelectInput>
        <SelectInput label={t("movies.filter.monitored")} value={f.monitored} onChange={(e) => set("monitored", e.target.value as MovieFilter["monitored"])}>
          <option value="">{t("movies.filter.all")}</option>
          <option value="true">{t("movies.monitored")}</option>
          <option value="false">{t("movies.unmonitored")}</option>
        </SelectInput>
        <SelectInput label={t("movies.filter.profile")} value={String(f.profileId)} onChange={(e) => set("profileId", Number(e.target.value))}>
          <option value="0">{t("movies.filter.all")}</option>
          {movieProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <TextInput label={t("movies.filter.tag")} value={f.tag} onChange={(e) => set("tag", e.target.value)} />
        <div className="flex items-end gap-1">
          <SelectInput label={t("movies.filter.sort")} value={f.sort} onChange={(e) => set("sort", e.target.value as Sort)}>
            {SORTS.map((s) => <option key={s} value={s}>{t(`movies.sort.${s}`)}</option>)}
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

function Library({ filter, view }: { filter: MovieFilter; view: "grid" | "table" }) {
  const { t, i18n } = useTranslation();
  const box = useScrollBox<HTMLDivElement>();
  const cols = view === "grid" ? columnsFor(box.width - 4, MIN_CARD, GAP) : 1;
  const cardW = view === "grid" ? (box.width - 4 - GAP * (cols - 1)) / cols : 0;
  const rowH = view === "grid" ? Math.round(cardW * 1.5 + 56 + GAP) : TABLE_ROW;
  // Rows are known once the total is; start with a page.
  const first = useQuery({
    queryKey: [...queryKeys.movies, "page", filter, 0],
    queryFn: async () => unwrap(await api.GET("/movies", { params: { query: query(filter, 0) } })),
    staleTime: 15_000,
  });
  const total = first.data?.total ?? 0;
  const rows = Math.ceil(total / cols);
  const { first: r0, last: r1 } = visibleRows(box.scrollTop, box.height, rowH, rows);
  const paged = usePagedMovies(filter, r0 * cols, Math.min(total - 1, (r1 + 1) * cols - 1));
  const rowsToDraw = useMemo(() => {
    const out: number[] = [];
    for (let r = r0; r <= r1; r++) out.push(r);
    return out;
  }, [r0, r1]);

  return (
    <div ref={box.ref} className="min-h-[420px] flex-1 overflow-y-auto" data-testid="movie-library" style={{ height: "calc(100vh - 260px)" }}>
      {first.isLoading ? <Spinner /> : null}
      <ErrorAlert error={first.error ?? paged.error} />
      {first.data && total === 0 ? <EmptyState>{t("movies.empty")}</EmptyState> : null}
      {total > 0 && view === "table" ? (
        <div role="table" aria-label={t("nav.movies")} aria-rowcount={total} className="min-w-[720px] text-[13px]">
          <div role="row" className="sticky top-0 z-10 grid h-8 grid-cols-[1fr_70px_110px_130px_90px_120px] items-center gap-2 border-b border-line bg-bg px-2 text-[12px] text-muted">
            <span role="columnheader">{t("movies.columns.title")}</span>
            <span role="columnheader">{t("movies.columns.year")}</span>
            <span role="columnheader">{t("movies.columns.status")}</span>
            <span role="columnheader">{t("movies.columns.quality")}</span>
            <span role="columnheader">{t("movies.columns.size")}</span>
            <span role="columnheader">{t("movies.columns.digital")}</span>
          </div>
          <div style={{ height: rows * rowH, position: "relative" }}>
            {rowsToDraw.map((r) => {
              const m = paged.items.get(r);
              return (
                <div key={r} role="row" aria-rowindex={r + 2} style={{ position: "absolute", top: r * rowH, height: rowH, left: 0, right: 0 }}
                  className="grid grid-cols-[1fr_70px_110px_130px_90px_120px] items-center gap-2 border-b border-line px-2 hover:bg-surface-2">
                  {m ? (
                    <>
                      <Link to="/movies/$id" params={{ id: String(m.id) }} className="truncate text-fg hover:text-accent" role="cell">{m.title}</Link>
                      <span role="cell" className="tabular-nums text-muted">{m.year || "–"}</span>
                      <span role="cell"><StatusBadge status={m.libraryStatus} /></span>
                      <span role="cell" className="truncate text-muted">{m.file?.quality.name ?? "–"}</span>
                      <span role="cell" className="tabular-nums text-muted">{m.file ? formatBytes(m.file.sizeBytes, i18n.language) : "–"}</span>
                      <span role="cell" className="tabular-nums text-muted">{m.digitalRelease || m.physicalRelease || "–"}</span>
                    </>
                  ) : <span className="col-span-6 h-3 w-1/3 animate-pulse rounded bg-surface-3" />}
                </div>
              );
            })}
          </div>
        </div>
      ) : null}
      {total > 0 && view === "grid" ? (
        <ul aria-label={t("nav.movies")} style={{ height: rows * rowH, position: "relative" }}>
          {rowsToDraw.flatMap((r) =>
            Array.from({ length: cols }, (_, c) => {
              const idx = r * cols + c;
              if (idx >= total) return null;
              const m = paged.items.get(idx);
              return (
                <li key={idx} style={{ position: "absolute", top: r * rowH, left: c * (cardW + GAP), width: cardW }}>
                  {m ? <Card m={m} /> : <div className="aspect-[2/3] animate-pulse rounded-ui bg-surface-2" />}
                </li>
              );
            }),
          )}
        </ul>
      ) : null}
    </div>
  );
}

function Card({ m }: { m: Movie }) {
  const pct = m.download ? Math.round(m.download.progress * 100) : null;
  return (
    <Link to="/movies/$id" params={{ id: String(m.id) }} className="group block" data-testid="movie-card">
      <div className="relative">
        <Poster file={m.poster} title={m.title} className="transition-[border-color] group-hover:border-line-strong" />
        {pct !== null && m.libraryStatus === "downloading" ? (
          <div className="absolute inset-x-0 bottom-0 h-1 bg-black/50">
            <div className="h-full bg-info" style={{ width: `${pct}%` }} />
          </div>
        ) : null}
        {!m.monitored ? <span className="absolute left-1.5 top-1.5 size-2 rounded-full bg-faint ring-2 ring-bg" aria-hidden /> : null}
      </div>
      <div className="mt-1.5 truncate text-[13px] font-medium text-fg group-hover:text-accent">{m.title}</div>
      <div className="flex items-center justify-between gap-1">
        <span className="text-[12px] tabular-nums text-muted">{m.year || "–"}</span>
        <StatusBadge status={m.libraryStatus} />
      </div>
    </Link>
  );
}
