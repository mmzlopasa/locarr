import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { InStreameroBadge, WatchInStreamero } from "@/features/mediaserver/streamero";
import { Search } from "lucide-react";
import { api, queryKeys, unwrap, type MonitorOption, type SeriesLookup, type SeriesType } from "@/api/client";
import { Alert, Button, Checkbox, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { splitTags } from "@/features/movies/AddMoviePage";
import { Poster, useQualityProfiles, useRootFolders } from "@/features/movies/common";
import { MONITOR_OPTIONS, SERIES_TYPES } from "./common";

export type SeriesAddDefaults = {
  rootFolderId: number;
  qualityProfileId: number;
  seriesType: SeriesType;
  seasonFolder: boolean;
  monitor: MonitorOption;
  monitorNewSeasons: "all" | "none";
  monitored: boolean;
  tags: string;
  searchOnAdd: boolean;
};

export function useSeriesAddDefaults() {
  const naming = useQuery({ queryKey: queryKeys.seriesNaming, queryFn: async () => unwrap(await api.GET("/settings/mediamanagement/series")), retry: false });
  const [v, setV] = useState<SeriesAddDefaults>({ rootFolderId: 0, qualityProfileId: 0, seriesType: "standard", seasonFolder: true,
    monitor: "all", monitorNewSeasons: "all", monitored: true, tags: "", searchOnAdd: true });
  useEffect(() => {
    if (naming.data) setV((x) => ({ ...x, searchOnAdd: naming.data.searchOnAddDefault }));
  }, [naming.data]);
  return [v, setV] as const;
}

/** Root folder, profile, series type, season folders, monitoring and "search now" for new series. */
export function SeriesOptionsForm({ value, onChange, withMonitor = true }:
  { value: SeriesAddDefaults; onChange: (v: SeriesAddDefaults) => void; withMonitor?: boolean }) {
  const { t } = useTranslation();
  const roots = useRootFolders();
  const profiles = useQualityProfiles();
  const tvRoots = (roots.data ?? []).filter((r) => r.kind === "series");
  const tvProfiles = (profiles.data ?? []).filter((p) => p.kind === "series");
  useEffect(() => {
    const next = { ...value };
    if (!next.rootFolderId && tvRoots[0]) next.rootFolderId = tvRoots[0].id;
    if (!next.qualityProfileId && tvProfiles[0]) next.qualityProfileId = tvProfiles[0].id;
    if (next.rootFolderId !== value.rootFolderId || next.qualityProfileId !== value.qualityProfileId) onChange(next);
  }, [tvRoots, tvProfiles, value, onChange]);
  if (roots.data && tvRoots.length === 0) {
    return (
      <Alert tone="warning">
        {t("addSeries.noRoot")} <Link to="/settings/media" className="underline">{t("settings.tabs.media")}</Link>
      </Alert>
    );
  }
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <SelectInput label={t("addMovie.rootFolder")} value={value.rootFolderId} onChange={(e) => onChange({ ...value, rootFolderId: Number(e.target.value) })}>
        {tvRoots.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
      </SelectInput>
      <SelectInput label={t("addMovie.profile")} value={value.qualityProfileId} onChange={(e) => onChange({ ...value, qualityProfileId: Number(e.target.value) })}>
        {tvProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </SelectInput>
      {withMonitor ? (
        <SelectInput label={t("addSeries.monitor")} value={value.monitor} onChange={(e) => onChange({ ...value, monitor: e.target.value as MonitorOption })}
          data-testid="monitor-option">
          {MONITOR_OPTIONS.map((m) => <option key={m} value={m}>{t(`monitor.${m}`)}</option>)}
        </SelectInput>
      ) : null}
      <SelectInput label={t("addSeries.type")} value={value.seriesType} onChange={(e) => onChange({ ...value, seriesType: e.target.value as SeriesType })}>
        {SERIES_TYPES.map((s) => <option key={s} value={s}>{t(`seriesType.${s}`)}</option>)}
      </SelectInput>
      <SelectInput label={t("addSeries.newSeasons")} value={value.monitorNewSeasons}
        onChange={(e) => onChange({ ...value, monitorNewSeasons: e.target.value as "all" | "none" })}>
        <option value="all">{t("addSeries.newSeasonsAll")}</option>
        <option value="none">{t("addSeries.newSeasonsNone")}</option>
      </SelectInput>
      <TextInput label={t("addMovie.tags")} hint={t("addMovie.tagsHint")} value={value.tags} onChange={(e) => onChange({ ...value, tags: e.target.value })} />
      <Checkbox label={t("addSeries.seasonFolder")} description={t("addSeries.seasonFolderHint")} checked={value.seasonFolder}
        onChange={(e) => onChange({ ...value, seasonFolder: e.target.checked })} />
      <Checkbox label={t("addMovie.monitored")} description={t("addSeries.monitoredHint")} checked={value.monitored}
        onChange={(e) => onChange({ ...value, monitored: e.target.checked })} />
      <Checkbox label={t("addMovie.searchOnAdd")} description={t("addSeries.searchOnAddHint")} checked={value.searchOnAdd}
        onChange={(e) => onChange({ ...value, searchOnAdd: e.target.checked })} />
    </div>
  );
}

export function seriesBody(o: SeriesAddDefaults) {
  return { rootFolderId: o.rootFolderId, qualityProfileId: o.qualityProfileId, seriesType: o.seriesType, seasonFolder: o.seasonFolder,
    monitor: o.monitor, monitorNewSeasons: o.monitorNewSeasons, monitored: o.monitored, tags: splitTags(o.tags), searchOnAdd: o.searchOnAdd };
}

export function AddSeriesPage() {
  const { t } = useTranslation();
  const [term, setTerm] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [picked, setPicked] = useState<SeriesLookup | null>(null);
  const [opts, setOpts] = useSeriesAddDefaults();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const lookup = useQuery({
    queryKey: ["series", "lookup", submitted],
    queryFn: async () => unwrap(await api.GET("/series/lookup", { params: { query: { term: submitted } } })),
    enabled: submitted.length > 0,
  });
  const add = useMutation({
    mutationFn: async (s: SeriesLookup) => unwrap(await api.POST("/series", { body: { ...seriesBody(opts),
      ...(s.tmdbId ? { tmdbId: s.tmdbId } : {}), ...(s.tvdbId ? { tvdbId: s.tvdbId } : {}) } })),
    onSuccess: (s) => {
      void qc.invalidateQueries({ queryKey: queryKeys.seriesList });
      void navigate({ to: "/series/$id", params: { id: String(s.id) } });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setPicked(null);
    setSubmitted(term.trim());
  };
  const pick = (s: SeriesLookup) => {
    // Anime is a guess the user can change.
    setOpts((o) => ({ ...o, seriesType: o.seriesType }));
    setPicked(s);
  };
  return (
    <div>
      <PageHeader title={t("addSeries.title")} description={t("addSeries.description")} />
      <form onSubmit={submit} className="mb-4 flex items-end gap-2" role="search">
        <div className="flex-1">
          <TextInput label={t("addMovie.search")} placeholder={t("addSeries.searchHint")} value={term} onChange={(e) => setTerm(e.target.value)}
            autoFocus data-testid="add-series-term" />
        </div>
        <Button type="submit" variant="primary" disabled={!term.trim()}>
          <Search className="size-3.5" /> {t("addMovie.searchButton")}
        </Button>
      </form>
      {lookup.isFetching ? <Spinner /> : null}
      <ErrorAlert error={lookup.error} />
      {lookup.data && lookup.data.length === 0 ? <p className="text-[13px] text-muted">{t("addMovie.noResults")}</p> : null}
      {picked ? (
        <Panel title={`${picked.title}${picked.year ? ` (${picked.year})` : ""}`} description={t("addSeries.optionsDescription")}
          actions={<Button variant="ghost" onClick={() => setPicked(null)}>{t("common.back")}</Button>}>
          <div className="flex gap-4">
            <Poster file={picked.poster} title={picked.title} size="w185" className="hidden w-28 shrink-0 sm:block" />
            <div className="flex-1">
              <SeriesOptionsForm value={opts} onChange={setOpts} />
              <p className="mt-3 text-[12px] text-faint">{t(`monitorHint.${opts.monitor}`)} {t("addSeries.specialsHint")}</p>
              <div className="mt-4 flex items-center gap-2">
                <Button variant="primary" busy={add.isPending} onClick={() => add.mutate(picked)} disabled={!opts.rootFolderId || !opts.qualityProfileId}
                  data-testid="add-series-confirm">
                  {t("addSeries.add")}
                </Button>
                <ErrorAlert error={add.error} />
              </div>
            </div>
          </div>
        </Panel>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {(lookup.data ?? []).map((s) => (
            <li key={`${s.source}-${s.tmdbId}-${s.tvdbId}`} className="flex gap-3 rounded-ui border border-line bg-surface p-3">
              <Poster file={s.poster} title={s.title} size="w185" className="w-20 shrink-0" />
              <div className="flex min-w-0 flex-1 flex-col">
                <div className="flex items-baseline gap-2">
                  <h2 className="truncate text-[14px] font-semibold text-fg">{s.title}</h2>
                  <span className="text-[12px] tabular-nums text-muted">{s.year || "–"}</span>
                </div>
                <p className="truncate text-[12px] text-faint">
                  {[s.network, s.status ? t(`series.status.${s.status}`, { defaultValue: s.status }) : "", s.originalTitle !== s.title ? s.originalTitle : ""]
                    .filter(Boolean).join(" · ")}
                </p>
                <p className="mt-1 line-clamp-3 text-[12px] text-muted">{s.overview}</p>
                <div className="mt-auto flex flex-wrap items-center gap-2 pt-2">
                  <InStreameroBadge url={s.streameroUrl} testId={`lookup-streamero-${s.tmdbId}`} />
                  <WatchInStreamero url={s.streameroUrl} size="sm" testId={`lookup-watch-${s.tmdbId}`} />
                  {s.seriesId ? (
                    <Link to="/series/$id" params={{ id: String(s.seriesId) }} className="text-[13px] text-accent hover:underline">{t("addMovie.inLibrary")}</Link>
                  ) : (
                    <Button variant="secondary" onClick={() => pick(s)} data-testid={`pick-${s.tmdbId || s.tvdbId}`}>{t("addMovie.choose")}</Button>
                  )}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
