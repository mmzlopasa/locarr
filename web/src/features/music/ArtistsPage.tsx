import { useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { FolderInput, LayoutGrid, Plus, Rows3 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Button, EmptyState, ErrorAlert, PageHeader, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { columnsFor, pagesFor, useScrollBox, visibleRows } from "@/features/movies/virtual";
import { Cover, musicKeys, type Artist } from "./common";

const PAGE = 200;
const SORTS = ["name", "added", "albums", "size"] as const;
type Sort = (typeof SORTS)[number];
type Filter = { q: string; monitored: "" | "true" | "false"; sort: Sort; desc: boolean };
const VIEW_KEY = "locarr.music.view";

function loadView(): "grid" | "table" {
  try {
    return localStorage.getItem(VIEW_KEY) === "table" ? "table" : "grid";
  } catch {
    return "grid";
  }
}

function query(f: Filter, page: number) {
  return { q: f.q || undefined, monitored: f.monitored === "" ? undefined : f.monitored === "true", sort: f.sort, desc: f.desc,
    offset: page * PAGE, limit: PAGE };
}

async function fetchPage(f: Filter, page: number) {
  return unwrap(await api.GET("/music/artists", { params: { query: query(f, page) } }));
}

/** The artists of the music library as covers or a table (only visible rows are drawn). */
export function ArtistsPage() {
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
  const [f, setF] = useState<Filter>({ q: "", monitored: "", sort: "name", desc: false });
  const set = <K extends keyof Filter>(k: K, v: Filter[K]) => setF((x) => ({ ...x, [k]: v }));
  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader title={t("nav.music")} description={t("music.description")}
        actions={canManage ? (
          <>
            <Link to="/music/import" className="inline-flex h-8 items-center gap-1.5 rounded-ui border border-line-strong bg-surface-3 px-3 text-[13px] hover:bg-line">
              <FolderInput className="size-3.5" /> {t("music.importExisting")}
            </Link>
            <Link to="/music/add" data-testid="add-artist"
              className="inline-flex h-8 items-center gap-1.5 rounded-ui bg-accent px-3 text-[13px] font-semibold text-accent-ink hover:bg-accent-hover">
              <Plus className="size-3.5" /> {t("music.add")}
            </Link>
          </>
        ) : null} />
      <div className="mb-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <div className="col-span-2">
          <TextInput label={t("music.filter.search")} value={f.q} onChange={(e) => set("q", e.target.value)} placeholder={t("music.filter.searchHint")} />
        </div>
        <SelectInput label={t("music.filter.monitored")} value={f.monitored} onChange={(e) => set("monitored", e.target.value as Filter["monitored"])}>
          <option value="">{t("movies.filter.all")}</option>
          <option value="true">{t("movies.monitored")}</option>
          <option value="false">{t("movies.unmonitored")}</option>
        </SelectInput>
        <div className="flex items-end gap-1">
          <SelectInput label={t("movies.filter.sort")} value={f.sort} onChange={(e) => set("sort", e.target.value as Sort)}>
            {SORTS.map((s) => <option key={s} value={s}>{t(`music.sort.${s}`)}</option>)}
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
const COLS = "grid-cols-[1fr_110px_120px_90px_110px]";

function Library({ filter, view }: { filter: Filter; view: "grid" | "table" }) {
  const { t, i18n } = useTranslation();
  const box = useScrollBox<HTMLDivElement>();
  const cols = view === "grid" ? columnsFor(box.width - 4, MIN_CARD, GAP) : 1;
  const cardW = view === "grid" ? (box.width - 4 - GAP * (cols - 1)) / cols : 0;
  const rowH = view === "grid" ? Math.round(cardW + 52 + GAP) : TABLE_ROW;
  const first = useQuery({ queryKey: [...musicKeys.artists, "page", filter, 0], queryFn: () => fetchPage(filter, 0), staleTime: 15_000 });
  const total = first.data?.total ?? 0;
  const rows = Math.ceil(total / cols);
  const { first: r0, last: r1 } = visibleRows(box.scrollTop, box.height, rowH, rows);
  const pages = pagesFor(r0 * cols, Math.min(total - 1, (r1 + 1) * cols - 1), PAGE);
  const results = useQueries({
    queries: pages.map((p) => ({ queryKey: [...musicKeys.artists, "page", filter, p], queryFn: () => fetchPage(filter, p), staleTime: 15_000 })),
  });
  const items = new Map<number, Artist>();
  results.forEach((r, i) => r.data?.items.forEach((a, j) => items.set((pages[i] ?? 0) * PAGE + j, a)));
  const rowsToDraw = useMemo(() => {
    const out: number[] = [];
    for (let r = r0; r <= r1; r++) out.push(r);
    return out;
  }, [r0, r1]);
  return (
    <div ref={box.ref} className="min-h-[420px] flex-1 overflow-y-auto" data-testid="artist-library" style={{ height: "calc(100vh - 260px)" }}>
      {first.isLoading ? <Spinner /> : null}
      <ErrorAlert error={first.error} />
      {first.data && total === 0 ? <EmptyState>{t("music.empty")}</EmptyState> : null}
      {total > 0 && view === "table" ? (
        <div role="table" aria-label={t("nav.music")} aria-rowcount={total} className="min-w-[640px] text-[13px]">
          <div role="row" className={`sticky top-0 z-10 grid h-8 ${COLS} items-center gap-2 border-b border-line bg-bg px-2 text-[12px] text-muted`}>
            <span role="columnheader">{t("music.columns.name")}</span>
            <span role="columnheader">{t("music.columns.albums")}</span>
            <span role="columnheader">{t("music.columns.tracks")}</span>
            <span role="columnheader">{t("music.columns.size")}</span>
            <span role="columnheader">{t("music.columns.latest")}</span>
          </div>
          <div style={{ height: rows * rowH, position: "relative" }}>
            {rowsToDraw.map((r) => {
              const a = items.get(r);
              return (
                <div key={r} role="row" aria-rowindex={r + 2} style={{ position: "absolute", top: r * rowH, height: rowH, left: 0, right: 0 }}
                  className={`grid ${COLS} items-center gap-2 border-b border-line px-2 hover:bg-surface-2`}>
                  {a ? (
                    <>
                      <Link to="/music/artists/$id" params={{ id: String(a.id) }} className="truncate text-fg hover:text-accent" role="cell">
                        {a.name}{a.disambiguation ? <span className="ml-1 text-faint">({a.disambiguation})</span> : null}
                      </Link>
                      <span role="cell" className="tabular-nums text-muted">{a.albumsWithFiles}/{a.monitoredAlbums}</span>
                      <span role="cell" className="tabular-nums text-muted">{a.files}</span>
                      <span role="cell" className="tabular-nums text-muted">{a.sizeBytes ? formatBytes(a.sizeBytes, i18n.language) : "–"}</span>
                      <span role="cell" className="tabular-nums text-muted">{a.latestRelease || "–"}</span>
                    </>
                  ) : <span className="col-span-5 h-3 w-1/3 animate-pulse rounded bg-surface-3" />}
                </div>
              );
            })}
          </div>
        </div>
      ) : null}
      {total > 0 && view === "grid" ? (
        <ul aria-label={t("nav.music")} style={{ height: rows * rowH, position: "relative" }}>
          {rowsToDraw.flatMap((r) =>
            Array.from({ length: cols }, (_, c) => {
              const idx = r * cols + c;
              if (idx >= total) return null;
              const a = items.get(idx);
              return (
                <li key={idx} style={{ position: "absolute", top: r * rowH, left: c * (cardW + GAP), width: cardW }}>
                  {a ? <Card a={a} /> : <div className="aspect-square animate-pulse rounded-ui bg-surface-2" />}
                </li>
              );
            }),
          )}
        </ul>
      ) : null}
    </div>
  );
}

function Card({ a }: { a: Artist }) {
  const { t } = useTranslation();
  const pct = a.monitoredAlbums ? Math.round((a.albumsWithFiles / a.monitoredAlbums) * 100) : 0;
  return (
    <Link to="/music/artists/$id" params={{ id: String(a.id) }} className="group block" data-testid="artist-card">
      <div className="relative">
        <Cover file={a.cover} title={a.name} size="250" className="transition-[border-color] group-hover:border-line-strong" />
        <div className="absolute inset-x-0 bottom-0 h-1 bg-black/50" aria-hidden>
          <div className={`h-full ${pct >= 100 ? "bg-success" : "bg-info"}`} style={{ width: `${Math.min(pct, 100)}%` }} />
        </div>
        {!a.monitored ? <span className="absolute left-1.5 top-1.5 size-2 rounded-full bg-faint ring-2 ring-bg" aria-hidden /> : null}
      </div>
      <div className="mt-1.5 truncate text-[13px] font-medium text-fg group-hover:text-accent">{a.name}</div>
      <div className="truncate text-[12px] tabular-nums text-muted">
        {t("music.albumsHave", { have: a.albumsWithFiles, count: a.monitoredAlbums })}
      </div>
    </Link>
  );
}
