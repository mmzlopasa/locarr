import { memo, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Bookmark, BookmarkX, ChevronDown, ChevronRight, History, ListFilter, Pencil, RefreshCw, Search, Trash2 } from "lucide-react";
import { api, imageUrl, queryKeys, unwrap, type Episode, type SeasonInfo, type SeriesDetail } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Badge, Button, Checkbox, ErrorAlert, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { splitTags } from "@/features/movies/AddMoviePage";
import { Poster, useQualityProfiles } from "@/features/movies/common";
import { HistoryTab } from "@/features/movies/HistoryTab";
import { InteractiveSearch, type ReleaseSource } from "@/features/movies/InteractiveSearch";
import { bySeason, EpisodeStateBadge, episodeCode, formatAir, MONITOR_OPTIONS, SERIES_TYPES, SeriesStatusBadge, useEpisodes, useSeriesDetail } from "./common";
import { Drawer } from "./Drawer";

type Panel = { kind: "history" | "search"; title: string; itemRef: string; source?: ReleaseSource } | null;

function episodeReleases(id: number, seriesId: number): ReleaseSource {
  return {
    key: ["episodes", id, "releases"],
    load: async () => unwrap(await api.GET("/episodes/{id}/releases", { params: { path: { id } } })),
    grab: async (body) => unwrap(await api.POST("/episodes/{id}/releases", { params: { path: { id } }, body })),
    refresh: [queryKeys.seriesEpisodes(seriesId), queryKeys.series(seriesId)],
  };
}

function seasonReleases(seriesId: number, number: number): ReleaseSource {
  return {
    key: ["series", seriesId, "seasons", number, "releases"],
    load: async () => unwrap(await api.GET("/series/{id}/seasons/{number}/releases", { params: { path: { id: seriesId, number } } })),
    grab: async (body) => unwrap(await api.POST("/series/{id}/seasons/{number}/releases", { params: { path: { id: seriesId, number } }, body })),
    refresh: [queryKeys.seriesEpisodes(seriesId), queryKeys.series(seriesId)],
  };
}

export function SeriesDetailPage() {
  const { id } = useParams({ from: "/app/series/$id" });
  const seriesId = Number(id);
  const { t } = useTranslation();
  const me = useCurrentUser();
  const canManage = me.role === "admin" || me.permissions.manageLibrary;
  const series = useSeriesDetail(seriesId);
  const episodes = useEpisodes(seriesId, (series.data?.downloading ?? 0) > 0);
  const [panel, setPanel] = useState<Panel>(null);
  if (series.isLoading) return <Spinner />;
  if (series.error || !series.data) return <ErrorAlert error={series.error} />;
  const s = series.data;
  return (
    <div>
      <Hero s={s} canManage={canManage} onHistory={() => setPanel({ kind: "history", title: t("series.history", { title: s.title }), itemRef: s.itemRef })} />
      {episodes.isLoading ? <Spinner /> : null}
      <ErrorAlert error={episodes.error} />
      {episodes.data ? <Seasons s={s} episodes={episodes.data} canManage={canManage} open={setPanel} /> : null}
      {panel ? (
        <Drawer title={panel.title} onClose={() => setPanel(null)} testId={`drawer-${panel.kind}`}>
          {panel.kind === "history" ? <HistoryTab itemRef={panel.itemRef} /> : null}
          {panel.kind === "search" && panel.source ? <InteractiveSearch source={panel.source} itemRef={panel.itemRef} autoStart /> : null}
        </Drawer>
      ) : null}
    </div>
  );
}

function useRefreshSeries(id: number) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: queryKeys.series(id) });
    void qc.invalidateQueries({ queryKey: queryKeys.seriesEpisodes(id) });
    void qc.invalidateQueries({ queryKey: queryKeys.seriesList });
  };
}

function Hero({ s, canManage, onHistory }: { s: SeriesDetail; canManage: boolean; onHistory: () => void }) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const refreshAll = useRefreshSeries(s.id);
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const search = useMutation({ mutationFn: async () => unwrap(await api.POST("/series/{id}/search", { params: { path: { id: s.id } } })), onSuccess: refreshAll });
  const refresh = useMutation({ mutationFn: async () => unwrap(await api.POST("/series/{id}/refresh", { params: { path: { id: s.id } } })), onSuccess: refreshAll });
  const monitor = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/series/{id}", { params: { path: { id: s.id } }, body: { monitored: !s.monitored } })),
    onSuccess: refreshAll,
  });
  const backdrop = imageUrl(s.backdrop, "w1280");
  const st = s.stats;
  return (
    <section className="relative mb-4 overflow-hidden rounded-ui border border-line bg-surface">
      {backdrop ? <img src={backdrop} alt="" className="absolute inset-0 size-full object-cover opacity-20" aria-hidden /> : null}
      <div className="absolute inset-0 bg-gradient-to-r from-bg via-bg/80 to-transparent" aria-hidden />
      <div className="relative flex flex-col gap-4 p-4 sm:flex-row">
        <Poster file={s.poster} title={s.title} size="w342" className="w-32 shrink-0 sm:w-40" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-[22px] font-semibold tracking-tight text-fg" data-testid="series-title">{s.title}</h1>
            <SeriesStatusBadge status={s.status} />
            {s.seriesType !== "standard" ? <Badge tone="info">{t(`seriesType.${s.seriesType}`)}</Badge> : null}
            {!s.monitored ? <Badge>{t("movies.unmonitored")}</Badge> : null}
          </div>
          <p className="mt-0.5 text-[13px] text-muted">
            {[s.year || null, s.network || null, s.runtime ? t("movies.runtime", { minutes: s.runtime }) : null, s.certification || null].filter(Boolean).join(" · ")}
          </p>
          {s.genres.length ? <p className="text-[12px] text-faint">{s.genres.join(", ")}</p> : null}
          <p className="mt-3 max-w-3xl text-[13px] leading-relaxed text-fg/90">{s.overview || t("movie.noOverview")}</p>
          <dl className="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-[12px]" data-testid="series-stats">
            <div><dt className="inline text-faint">{t("series.columns.episodes")}: </dt><dd className="inline tabular-nums text-fg">{st.episodeFileCount}/{st.episodeCount}</dd></div>
            <div><dt className="inline text-faint">{t("series.columns.missing")}: </dt><dd className={`inline tabular-nums ${st.missingCount ? "text-danger" : "text-fg"}`}>{st.missingCount}</dd></div>
            <div><dt className="inline text-faint">{t("movies.columns.size")}: </dt><dd className="inline tabular-nums text-fg">{formatBytes(st.sizeOnDisk, i18n.language)}</dd></div>
            {st.nextAiring ? <div><dt className="inline text-faint">{t("series.columns.nextAiring")}: </dt>
              <dd className="inline text-fg">{new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" }).format(new Date(st.nextAiring))}</dd></div> : null}
            <div className="min-w-0"><dt className="inline text-faint">{t("movie.path")}: </dt><dd className="inline break-all font-mono text-fg">{s.path}</dd></div>
          </dl>
          {canManage ? (
            <div className="mt-4 flex flex-wrap gap-2">
              <Button variant="primary" onClick={() => search.mutate()} busy={search.isPending} data-testid="series-search">
                <Search className="size-3.5" /> {t("series.actions.searchMissing")}
              </Button>
              <Button onClick={() => monitor.mutate()} busy={monitor.isPending}>
                {s.monitored ? <BookmarkX className="size-3.5" /> : <Bookmark className="size-3.5" />}
                {s.monitored ? t("movie.actions.unmonitor") : t("movie.actions.monitor")}
              </Button>
              <Button onClick={() => refresh.mutate()} busy={refresh.isPending}><RefreshCw className="size-3.5" /> {t("movie.actions.refresh")}</Button>
              <Button onClick={onHistory}><History className="size-3.5" /> {t("series.actions.history")}</Button>
              <Button onClick={() => setEditing(!editing)} aria-expanded={editing}><Pencil className="size-3.5" /> {t("movie.actions.edit")}</Button>
              <Button variant="danger" onClick={() => setDeleting(!deleting)} aria-expanded={deleting}><Trash2 className="size-3.5" /> {t("movie.actions.delete")}</Button>
            </div>
          ) : null}
          {search.data ? <p className="mt-2 text-[12px] text-success" role="status">{t("series.searchQueued", { count: search.data.count })}</p> : null}
          <ErrorAlert error={search.error ?? refresh.error ?? monitor.error} />
        </div>
      </div>
      {editing ? <EditPanel s={s} onDone={() => { setEditing(false); refreshAll(); }} /> : null}
      {deleting ? <DeletePanel s={s} onDone={() => { void qc.invalidateQueries({ queryKey: queryKeys.seriesList }); void navigate({ to: "/series" }); }} /> : null}
    </section>
  );
}

function EditPanel({ s, onDone }: { s: SeriesDetail; onDone: () => void }) {
  const { t } = useTranslation();
  const profiles = useQualityProfiles();
  const [profile, setProfile] = useState(s.qualityProfileId);
  const [type, setType] = useState(s.seriesType);
  const [seasonFolder, setSeasonFolder] = useState(s.seasonFolder);
  const [newSeasons, setNewSeasons] = useState(s.monitorNewSeasons);
  const [monitor, setMonitor] = useState<string>("");
  const [tags, setTags] = useState(s.tags.join(", "));
  const [path, setPath] = useState(s.path);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/series/{id}", { params: { path: { id: s.id } }, body: {
      qualityProfileId: profile, seriesType: type, seasonFolder, monitorNewSeasons: newSeasons, tags: splitTags(tags),
      ...(monitor ? { monitor: monitor as (typeof MONITOR_OPTIONS)[number] } : {}), ...(path !== s.path ? { path } : {}) } })),
    onSuccess: onDone,
  });
  const hasFiles = s.stats.episodeFileCount > 0;
  return (
    <div className="relative border-t border-line bg-surface p-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <SelectInput label={t("addMovie.profile")} value={profile} onChange={(e) => setProfile(Number(e.target.value))}>
          {(profiles.data ?? []).filter((p) => p.kind === "series").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("addSeries.type")} value={type} onChange={(e) => setType(e.target.value as typeof type)}>
          {SERIES_TYPES.map((x) => <option key={x} value={x}>{t(`seriesType.${x}`)}</option>)}
        </SelectInput>
        <SelectInput label={t("addSeries.newSeasons")} value={newSeasons} onChange={(e) => setNewSeasons(e.target.value as typeof newSeasons)}>
          <option value="all">{t("addSeries.newSeasonsAll")}</option>
          <option value="none">{t("addSeries.newSeasonsNone")}</option>
        </SelectInput>
        <SelectInput label={t("series.reapplyMonitor")} value={monitor} onChange={(e) => setMonitor(e.target.value)}>
          <option value="">{t("series.keepMonitoring")}</option>
          {MONITOR_OPTIONS.map((m) => <option key={m} value={m}>{t(`monitor.${m}`)}</option>)}
        </SelectInput>
        <TextInput label={t("addMovie.tags")} value={tags} onChange={(e) => setTags(e.target.value)} />
        <div className="lg:col-span-2">
          <TextInput label={t("movie.path")} value={path} onChange={(e) => setPath(e.target.value)} disabled={hasFiles}
            hint={hasFiles ? t("series.pathLocked") : undefined} />
        </div>
        <Checkbox label={t("addSeries.seasonFolder")} checked={seasonFolder} onChange={(e) => setSeasonFolder(e.target.checked)} />
      </div>
      <div className="mt-3 flex gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending}>{t("common.save")}</Button>
        <ErrorAlert error={save.error} />
      </div>
    </div>
  );
}

function DeletePanel({ s, onDone }: { s: SeriesDetail; onDone: () => void }) {
  const { t } = useTranslation();
  const [files, setFiles] = useState(false);
  const del = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/series/{id}", { params: { path: { id: s.id }, query: { deleteFiles: files } } })),
    onSuccess: onDone,
  });
  return (
    <div className="relative border-t border-line bg-danger-bg/40 p-4">
      <p className="mb-3 text-[13px] text-fg">{t("movie.delete.question", { title: s.title })}</p>
      <div className="mb-3"><Checkbox label={t("series.delete.files")} description={t("movie.delete.filesHint")} checked={files} onChange={(e) => setFiles(e.target.checked)} /></div>
      <div className="flex gap-2">
        <Button variant="danger" onClick={() => del.mutate()} busy={del.isPending} data-testid="delete-confirm">{t("movie.delete.confirm")}</Button>
        <ErrorAlert error={del.error} />
      </div>
    </div>
  );
}

function Seasons({ s, episodes, canManage, open }: { s: SeriesDetail; episodes: Episode[]; canManage: boolean; open: (p: Panel) => void }) {
  const { t } = useTranslation();
  const refreshAll = useRefreshSeries(s.id);
  const grouped = useMemo(() => bySeason(episodes), [episodes]);
  const seasons = useMemo(() => [...s.seasons].sort((a, b) => (a.number === 0 ? 1 : b.number === 0 ? -1 : b.number - a.number)), [s.seasons]);
  const latest = seasons.find((x) => x.number > 0)?.number;
  const [expanded, setExpanded] = useState<Set<number>>(() => new Set(latest !== undefined ? [latest] : []));
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const bulk = useMutation({
    mutationFn: async (a: { op: "monitor" | "unmonitor" | "search"; ids: number[] }) =>
      a.op === "search"
        ? unwrap(await api.POST("/episodes/search", { body: { episodeIds: a.ids } }))
        : unwrap(await api.POST("/episodes/monitor", { body: { episodeIds: a.ids, monitored: a.op === "monitor" } })),
    onSuccess: () => { setSelected(new Set()); refreshAll(); },
  });
  const toggleSeason = (n: number) => setExpanded((x) => {
    const next = new Set(x);
    if (next.has(n)) next.delete(n);
    else next.add(n);
    return next;
  });
  const select = (ids: number[], on: boolean) => setSelected((x) => {
    const next = new Set(x);
    for (const id of ids) {
      if (on) next.add(id);
      else next.delete(id);
    }
    return next;
  });
  return (
    <div>
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[15px] font-semibold text-fg">{t("series.seasons")}</h2>
        <div className="flex gap-1">
          <Button variant="ghost" onClick={() => setExpanded(new Set(seasons.map((x) => x.number)))}>{t("series.expandAll")}</Button>
          <Button variant="ghost" onClick={() => setExpanded(new Set())}>{t("series.collapseAll")}</Button>
        </div>
      </div>
      {canManage && selected.size > 0 ? (
        <div className="sticky top-0 z-20 mb-2 flex flex-wrap items-center gap-2 rounded-ui border border-accent/40 bg-surface-2 px-3 py-2 text-[13px]" data-testid="bulk-bar">
          <span className="text-fg">{t("series.selected", { count: selected.size })}</span>
          <Button onClick={() => bulk.mutate({ op: "monitor", ids: [...selected] })} busy={bulk.isPending}><Bookmark className="size-3.5" /> {t("movie.actions.monitor")}</Button>
          <Button onClick={() => bulk.mutate({ op: "unmonitor", ids: [...selected] })} busy={bulk.isPending}><BookmarkX className="size-3.5" /> {t("movie.actions.unmonitor")}</Button>
          <Button variant="primary" onClick={() => bulk.mutate({ op: "search", ids: [...selected] })} busy={bulk.isPending}><Search className="size-3.5" /> {t("movie.actions.search")}</Button>
          <Button variant="ghost" onClick={() => setSelected(new Set())}>{t("series.clearSelection")}</Button>
          <ErrorAlert error={bulk.error} />
        </div>
      ) : null}
      <div className="space-y-2" data-testid="seasons">
        {seasons.map((sn) => (
          <SeasonBlock key={sn.id} s={s} sn={sn} episodes={grouped.get(sn.number) ?? []} open={expanded.has(sn.number)} onToggle={() => toggleSeason(sn.number)}
            canManage={canManage} selected={selected} select={select} openPanel={open} onChanged={refreshAll} />
        ))}
      </div>
    </div>
  );
}

function SeasonBlock({ s, sn, episodes, open, onToggle, canManage, selected, select, openPanel, onChanged }: {
  s: SeriesDetail; sn: SeasonInfo; episodes: Episode[]; open: boolean; onToggle: () => void; canManage: boolean;
  selected: Set<number>; select: (ids: number[], on: boolean) => void; openPanel: (p: Panel) => void; onChanged: () => void;
}) {
  const { t, i18n } = useTranslation();
  const monitor = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/series/{id}/seasons/{number}", { params: { path: { id: s.id, number: sn.number } }, body: { monitored: !sn.monitored } })),
    onSuccess: onChanged,
  });
  const search = useMutation({
    mutationFn: async () => unwrap(await api.POST("/series/{id}/seasons/{number}/search", { params: { path: { id: s.id, number: sn.number } } })),
    onSuccess: onChanged,
  });
  const name = sn.number === 0 ? t("series.specials") : t("series.season", { number: sn.number });
  const ids = episodes.map((e) => e.id);
  const allSelected = ids.length > 0 && ids.every((id) => selected.has(id));
  const st = sn.stats;
  return (
    <section className="rounded-ui border border-line bg-surface" data-testid={`season-${sn.number}`}>
      <header className="flex flex-wrap items-center gap-2 px-3 py-2">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 flex-1 items-center gap-2 text-left">
          {open ? <ChevronDown className="size-4 text-muted" /> : <ChevronRight className="size-4 text-muted" />}
          <span className="text-[14px] font-semibold text-fg">{name}</span>
          <span className="text-[12px] tabular-nums text-muted">{st.episodeFileCount}/{st.episodeCount || episodes.length}</span>
          {st.missingCount > 0 ? <Badge tone="danger">{t("series.missingCount", { count: st.missingCount })}</Badge> : null}
          {!sn.monitored ? <Badge>{t("movies.unmonitored")}</Badge> : null}
          {st.sizeOnDisk ? <span className="text-[12px] tabular-nums text-faint">{formatBytes(st.sizeOnDisk, i18n.language)}</span> : null}
        </button>
        {canManage ? (
          <div className="flex gap-1">
            <Button variant="ghost" onClick={() => monitor.mutate()} busy={monitor.isPending} aria-pressed={sn.monitored}
              title={sn.monitored ? t("movie.actions.unmonitor") : t("movie.actions.monitor")} data-testid={`season-monitor-${sn.number}`}>
              {sn.monitored ? <Bookmark className="size-3.5 text-accent" /> : <BookmarkX className="size-3.5" />}
              <span className="sr-only">{sn.monitored ? t("movie.actions.unmonitor") : t("movie.actions.monitor")}</span>
            </Button>
            <Button variant="ghost" onClick={() => search.mutate()} busy={search.isPending} title={t("series.actions.searchSeason")} data-testid={`season-search-${sn.number}`}>
              <Search className="size-3.5" /><span className="sr-only">{t("series.actions.searchSeason")}</span>
            </Button>
            <Button variant="ghost" title={t("series.actions.interactiveSeason")} data-testid={`season-interactive-${sn.number}`}
              onClick={() => openPanel({ kind: "search", title: t("series.releasesFor", { title: `${s.title} · ${name}` }), itemRef: sn.itemRef,
                source: seasonReleases(s.id, sn.number) })}>
              <ListFilter className="size-3.5" /><span className="sr-only">{t("series.actions.interactiveSeason")}</span>
            </Button>
            <Button variant="ghost" title={t("series.actions.history")}
              onClick={() => openPanel({ kind: "history", title: t("series.history", { title: `${s.title} · ${name}` }), itemRef: sn.itemRef })}>
              <History className="size-3.5" /><span className="sr-only">{t("series.actions.history")}</span>
            </Button>
          </div>
        ) : null}
      </header>
      {search.data ? <p className="px-3 pb-2 text-[12px] text-success" role="status">{t("series.seasonQueued")}</p> : null}
      <ErrorAlert error={monitor.error ?? search.error} />
      {open ? (
        <table className="w-full border-t border-line text-[13px]">
          <thead className="text-left text-[12px] text-muted">
            <tr>
              {canManage ? (
                <th className="w-8 px-2 py-1">
                  <input type="checkbox" aria-label={t("series.selectSeason", { name })} checked={allSelected}
                    onChange={(e) => select(ids, e.target.checked)} className="accent-[var(--lc-accent)]" />
                </th>
              ) : null}
              <th className="w-8 px-1 py-1"><span className="sr-only">{t("movies.monitored")}</span></th>
              <th className="w-20 px-2 py-1 font-medium">#</th>
              <th className="px-2 py-1 font-medium">{t("movies.columns.title")}</th>
              <th className="hidden px-2 py-1 font-medium md:table-cell">{t("episode.airs")}</th>
              <th className="px-2 py-1 font-medium">{t("movies.columns.status")}</th>
              <th className="hidden px-2 py-1 font-medium lg:table-cell">{t("movies.columns.quality")}</th>
              <th className="w-28 px-2 py-1" />
            </tr>
          </thead>
          <tbody>
            {[...episodes].reverse().map((e) => (
              <EpisodeRow key={e.id} e={e} s={s} canManage={canManage} selected={selected.has(e.id)} select={select} openPanel={openPanel} onChanged={onChanged} />
            ))}
          </tbody>
        </table>
      ) : null}
    </section>
  );
}

const EpisodeRow = memo(function EpisodeRow({ e, s, canManage, selected, select, openPanel, onChanged }: {
  e: Episode; s: SeriesDetail; canManage: boolean; selected: boolean; select: (ids: number[], on: boolean) => void;
  openPanel: (p: Panel) => void; onChanged: () => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const monitor = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/episodes/{id}", { params: { path: { id: e.id } }, body: { monitored: !e.monitored } })),
    onSuccess: onChanged,
  });
  const search = useMutation({
    mutationFn: async () => unwrap(await api.POST("/episodes/search", { body: { episodeIds: [e.id] } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.timeline(e.itemRef) }),
  });
  const code = s.seriesType === "daily" && e.airDate ? e.airDate : episodeCode(e);
  const label = `${s.title} ${code}`;
  return (
    <tr className={`border-t border-line align-middle ${e.removed ? "opacity-60" : ""}`} data-testid="episode-row" data-code={episodeCode(e)}>
      {canManage ? (
        <td className="px-2 py-1">
          <input type="checkbox" aria-label={label} checked={selected} onChange={(ev) => select([e.id], ev.target.checked)} className="accent-[var(--lc-accent)]" />
        </td>
      ) : null}
      <td className="px-1 py-1">
        <button type="button" disabled={!canManage || monitor.isPending} onClick={() => monitor.mutate()} aria-pressed={e.monitored}
          aria-label={e.monitored ? t("episode.unmonitor", { code }) : t("episode.monitor", { code })} className="text-muted hover:text-fg disabled:opacity-50"
          data-testid="episode-monitor">
          {e.monitored ? <Bookmark className="size-3.5 text-accent" /> : <BookmarkX className="size-3.5" />}
        </button>
      </td>
      <td className="whitespace-nowrap px-2 py-1 font-mono text-[12px] text-muted">
        {code}{s.seriesType === "anime" && e.absoluteNumber ? <span className="ml-1 text-faint">({e.absoluteNumber})</span> : null}
      </td>
      <td className="max-w-[280px] px-2 py-1">
        <div className="truncate text-fg" title={e.overview}>{e.title || t("episode.tba")}</div>
        {e.removed ? <div className="text-[11px] text-warning">{t("episode.removed")}</div> : null}
        {e.sceneSeason !== undefined || e.sceneEpisode !== undefined || e.sceneAbsolute !== undefined ? (
          <div className="text-[11px] text-faint">{t("episode.scene", { code: e.sceneAbsolute !== undefined ? String(e.sceneAbsolute)
            : episodeCode({ seasonNumber: e.sceneSeason ?? e.seasonNumber, episodeNumber: e.sceneEpisode ?? e.episodeNumber }) })}</div>
        ) : null}
      </td>
      <td className="hidden whitespace-nowrap px-2 py-1 tabular-nums text-muted md:table-cell">{formatAir(e, i18n.language) || "–"}</td>
      <td className="px-2 py-1"><EpisodeStateBadge e={e} /></td>
      <td className="hidden truncate px-2 py-1 text-muted lg:table-cell" title={e.file?.path}>
        {e.file ? `${e.file.quality.name} · ${formatBytes(e.file.sizeBytes, i18n.language)}` : "–"}
      </td>
      <td className="whitespace-nowrap px-2 py-1 text-right">
        {canManage ? (
          <>
            <Button variant="ghost" onClick={() => search.mutate()} busy={search.isPending} title={t("episode.search")} data-testid="episode-search">
              <Search className="size-3.5" /><span className="sr-only">{t("episode.search")}</span>
            </Button>
            <Button variant="ghost" title={t("episode.interactive")} data-testid="episode-interactive"
              onClick={() => openPanel({ kind: "search", title: t("series.releasesFor", { title: label }), itemRef: e.itemRef, source: episodeReleases(e.id, s.id) })}>
              <ListFilter className="size-3.5" /><span className="sr-only">{t("episode.interactive")}</span>
            </Button>
            <Button variant="ghost" title={t("episode.why")} data-testid="episode-history"
              onClick={() => openPanel({ kind: "history", title: t("series.history", { title: `${label}${e.title ? ` · ${e.title}` : ""}` }), itemRef: e.itemRef })}>
              <History className="size-3.5" /><span className="sr-only">{t("episode.why")}</span>
            </Button>
          </>
        ) : null}
      </td>
    </tr>
  );
});
