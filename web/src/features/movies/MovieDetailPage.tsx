import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Bookmark, BookmarkX, Pencil, RefreshCw, Search, Trash2 } from "lucide-react";
import { api, imageUrl, queryKeys, unwrap, type MinimumAvailability, type Movie } from "@/api/client";
import { formatBytes, formatDateTime } from "@/components/format";
import { Badge, Button, Checkbox, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { splitTags } from "./AddMoviePage";
import { AVAILABILITIES, Poster, StatusBadge, movieFacts, useMovie, useQualityProfiles } from "./common";
import { HistoryTab } from "./HistoryTab";
import { InteractiveSearch, movieReleases } from "./InteractiveSearch";

type TabKey = "overview" | "search" | "history";

export function MovieDetailPage() {
  const { id } = useParams({ from: "/app/movies/$id" });
  const movieId = Number(id);
  const { t } = useTranslation();
  const movie = useMovie(movieId);
  const me = useCurrentUser();
  const canManage = me.role === "admin" || me.permissions.manageLibrary;
  const [tab, setTab] = useState<TabKey>(canManage ? "history" : "overview");
  if (movie.isLoading) return <Spinner />;
  if (movie.error || !movie.data) return <ErrorAlert error={movie.error} />;
  const m = movie.data;
  const tabs: TabKey[] = canManage ? ["overview", "search", "history"] : ["overview"];
  return (
    <div>
      <Hero m={m} canManage={canManage} />
      <div role="tablist" aria-label={t("movie.tabs.label")} className="mb-4 flex gap-1 border-b border-line">
        {tabs.map((k) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)} data-testid={`tab-${k}`}
            className={`-mb-px border-b-2 px-3 py-2 text-[13px] ${tab === k ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
            {t(`movie.tabs.${k}`)}
          </button>
        ))}
      </div>
      <div role="tabpanel">
        {tab === "overview" ? <Overview m={m} /> : null}
        {tab === "search" ? <InteractiveSearch source={movieReleases(m.id)} itemRef={m.itemRef ?? `movie:${m.id}`} /> : null}
        {tab === "history" ? <HistoryTab itemRef={m.itemRef ?? `movie:${m.id}`} /> : null}
      </div>
    </div>
  );
}

function Hero({ m, canManage }: { m: Movie; canManage: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const refreshAll = () => {
    void qc.invalidateQueries({ queryKey: queryKeys.movie(m.id) });
    void qc.invalidateQueries({ queryKey: queryKeys.timeline(m.itemRef ?? `movie:${m.id}`) });
    void qc.invalidateQueries({ queryKey: queryKeys.movies });
  };
  const search = useMutation({ mutationFn: async () => unwrap(await api.POST("/movies/{id}/search", { params: { path: { id: m.id } } })), onSuccess: refreshAll });
  const refresh = useMutation({ mutationFn: async () => unwrap(await api.POST("/movies/{id}/refresh", { params: { path: { id: m.id } } })), onSuccess: refreshAll });
  const monitor = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/movies/{id}", { params: { path: { id: m.id } }, body: { monitored: !m.monitored } })),
    onSuccess: refreshAll,
  });
  const backdrop = imageUrl(m.backdrop, "w1280");
  return (
    <section className="relative mb-4 overflow-hidden rounded-ui border border-line bg-surface">
      {backdrop ? (
        <img src={backdrop} alt="" className="absolute inset-0 size-full object-cover opacity-20" aria-hidden />
      ) : null}
      <div className="absolute inset-0 bg-gradient-to-r from-bg via-bg/80 to-transparent" aria-hidden />
      <div className="relative flex flex-col gap-4 p-4 sm:flex-row">
        <Poster file={m.poster} title={m.title} size="w342" className="w-32 shrink-0 sm:w-40" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-[22px] font-semibold tracking-tight text-fg" data-testid="movie-title">{m.title}</h1>
            <StatusBadge status={m.libraryStatus} />
            {!m.monitored ? <Badge>{t("movies.unmonitored")}</Badge> : null}
          </div>
          <p className="mt-0.5 text-[13px] text-muted">{movieFacts(m, t)}{m.studio ? ` · ${m.studio}` : ""}</p>
          {m.genres.length ? <p className="text-[12px] text-faint">{m.genres.join(", ")}</p> : null}
          <p className="mt-3 max-w-3xl text-[13px] leading-relaxed text-fg/90">{m.overview || t("movie.noOverview")}</p>
          {m.download && m.libraryStatus === "downloading" ? (
            <div className="mt-3 max-w-md">
              <div className="mb-1 flex justify-between text-[12px] text-muted">
                <span className="truncate">{m.download.title}</span>
                <span className="tabular-nums">{Math.round(m.download.progress * 100)}%</span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-full bg-surface-3">
                <div className="h-full bg-info transition-[width] duration-500" style={{ width: `${m.download.progress * 100}%` }} />
              </div>
            </div>
          ) : null}
          {canManage ? (
            <div className="mt-4 flex flex-wrap gap-2">
              <Button variant="primary" onClick={() => search.mutate()} busy={search.isPending} data-testid="search-now">
                <Search className="size-3.5" /> {t("movie.actions.search")}
              </Button>
              <Button onClick={() => monitor.mutate()} busy={monitor.isPending}>
                {m.monitored ? <BookmarkX className="size-3.5" /> : <Bookmark className="size-3.5" />}
                {m.monitored ? t("movie.actions.unmonitor") : t("movie.actions.monitor")}
              </Button>
              <Button onClick={() => refresh.mutate()} busy={refresh.isPending}><RefreshCw className="size-3.5" /> {t("movie.actions.refresh")}</Button>
              <Button onClick={() => setEditing(!editing)} aria-expanded={editing}><Pencil className="size-3.5" /> {t("movie.actions.edit")}</Button>
              <Button variant="danger" onClick={() => setDeleting(!deleting)} aria-expanded={deleting}><Trash2 className="size-3.5" /> {t("movie.actions.delete")}</Button>
            </div>
          ) : null}
          {search.isSuccess ? <p className="mt-2 text-[12px] text-success" role="status">{t("movie.searchQueued")}</p> : null}
          <ErrorAlert error={search.error ?? refresh.error ?? monitor.error} />
        </div>
      </div>
      {editing ? <EditPanel m={m} onDone={() => { setEditing(false); refreshAll(); }} /> : null}
      {deleting ? <DeletePanel m={m} onDone={() => { void qc.invalidateQueries({ queryKey: queryKeys.movies }); void navigate({ to: "/movies" }); }} /> : null}
    </section>
  );
}

function EditPanel({ m, onDone }: { m: Movie; onDone: () => void }) {
  const { t } = useTranslation();
  const profiles = useQualityProfiles();
  const [profile, setProfile] = useState(m.qualityProfileId);
  const [avail, setAvail] = useState<MinimumAvailability>(m.minimumAvailability);
  const [tags, setTags] = useState(m.tags.join(", "));
  const [path, setPath] = useState(m.path);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/movies/{id}", { params: { path: { id: m.id } }, body: {
      qualityProfileId: profile, minimumAvailability: avail, tags: splitTags(tags), ...(path !== m.path ? { path } : {}) } })),
    onSuccess: onDone,
  });
  return (
    <div className="relative border-t border-line bg-surface p-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <SelectInput label={t("addMovie.profile")} value={profile} onChange={(e) => setProfile(Number(e.target.value))}>
          {(profiles.data ?? []).filter((p) => p.kind === "movies").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("addMovie.availability")} value={avail} onChange={(e) => setAvail(e.target.value as MinimumAvailability)}>
          {AVAILABILITIES.map((a) => <option key={a} value={a}>{t(`availability.${a}`)}</option>)}
        </SelectInput>
        <TextInput label={t("addMovie.tags")} value={tags} onChange={(e) => setTags(e.target.value)} />
        <TextInput label={t("movie.path")} value={path} onChange={(e) => setPath(e.target.value)} disabled={!!m.file}
          hint={m.file ? t("movie.pathLocked") : undefined} />
      </div>
      <div className="mt-3 flex gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending}>{t("common.save")}</Button>
        <ErrorAlert error={save.error} />
      </div>
    </div>
  );
}

function DeletePanel({ m, onDone }: { m: Movie; onDone: () => void }) {
  const { t } = useTranslation();
  const [files, setFiles] = useState(false);
  const [exclude, setExclude] = useState(false);
  const del = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/movies/{id}", { params: { path: { id: m.id }, query: { deleteFiles: files, exclude } } })),
    onSuccess: onDone,
  });
  return (
    <div className="relative border-t border-line bg-danger-bg/40 p-4">
      <p className="mb-3 text-[13px] text-fg">{t("movie.delete.question", { title: m.title })}</p>
      <div className="mb-3 space-y-2">
        <Checkbox label={t("movie.delete.files")} description={t("movie.delete.filesHint")} checked={files} onChange={(e) => setFiles(e.target.checked)} />
        <Checkbox label={t("movie.delete.exclude")} description={t("movie.delete.excludeHint")} checked={exclude} onChange={(e) => setExclude(e.target.checked)} />
      </div>
      <div className="flex gap-2">
        <Button variant="danger" onClick={() => del.mutate()} busy={del.isPending} data-testid="delete-confirm">{t("movie.delete.confirm")}</Button>
        <ErrorAlert error={del.error} />
      </div>
    </div>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-[12px] text-faint">{label}</dt>
      <dd className="text-[13px] text-fg">{children}</dd>
    </div>
  );
}

function Overview({ m }: { m: Movie }) {
  const { t, i18n } = useTranslation();
  const profiles = useQualityProfiles();
  const profile = profiles.data?.find((p) => p.id === m.qualityProfileId);
  const f = m.file;
  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_1fr]">
      <Panel title={t("movie.details")}>
        <dl className="grid grid-cols-2 gap-3">
          <Fact label={t("movie.inCinemas")}>{m.inCinemas || "–"}</Fact>
          <Fact label={t("movie.digital")}>{m.digitalRelease || "–"}</Fact>
          <Fact label={t("movie.physical")}>{m.physicalRelease || "–"}</Fact>
          <Fact label={t("addMovie.availability")}>
            {t(`availability.${m.minimumAvailability}`)}
            {!m.available && m.availableFrom ? <span className="block text-[12px] text-muted">{t("movie.availableFrom", { date: formatDateTime(m.availableFrom, i18n.language) })}</span> : null}
          </Fact>
          <Fact label={t("addMovie.profile")}>{profile?.name ?? "–"}</Fact>
          <Fact label={t("addMovie.tags")}>{m.tags.length ? m.tags.join(", ") : "–"}</Fact>
          <Fact label={t("movie.ids")}>
            <span className="font-mono text-[12px]">TMDb {m.tmdbId}{m.imdbId ? ` · ${m.imdbId}` : ""}</span>
          </Fact>
          <Fact label={t("movie.path")}><span className="break-all font-mono text-[12px]">{m.path}</span></Fact>
          {m.collectionId ? (
            <Fact label={t("movie.collection")}>
              <Link to="/movies/collections/$id" params={{ id: String(m.collectionId) }} className="text-accent hover:underline">{m.collectionName}</Link>
            </Fact>
          ) : null}
        </dl>
      </Panel>
      <Panel title={t("movie.file")}>
        {f ? (
          <dl className="grid grid-cols-2 gap-3" data-testid="movie-file">
            <div className="col-span-2"><Fact label={t("movie.fileName")}><span className="break-all font-mono text-[12px]">{f.path.split("/").pop()}</span></Fact></div>
            <Fact label={t("movies.columns.quality")}>{f.quality.name}{f.revision > 1 ? ` (v${f.revision})` : ""}</Fact>
            <Fact label={t("movies.columns.size")}>{formatBytes(f.sizeBytes, i18n.language)}</Fact>
            <Fact label={t("movie.formats")}>{f.customFormats.length ? f.customFormats.join(", ") : "–"} {f.score ? `(${f.score})` : ""}</Fact>
            <Fact label={t("movie.group")}>{f.releaseGroup || "–"}</Fact>
            <Fact label={t("movie.video")}>{[f.mediaInfo.resolution ? `${f.mediaInfo.resolution}p` : "", f.mediaInfo.videoCodec, f.mediaInfo.bitDepth ? `${f.mediaInfo.bitDepth}-bit` : "",
              ...(f.mediaInfo.hdr ?? [])].filter(Boolean).join(" · ") || "–"}</Fact>
            <Fact label={t("movie.audio")}>{[f.mediaInfo.audioCodec, f.mediaInfo.audioChannels, f.mediaInfo.atmos ? "Atmos" : ""].filter(Boolean).join(" · ") || "–"}</Fact>
            <Fact label={t("movie.languages")}>{f.languages.length ? f.languages.join(", ").toUpperCase() : "–"}</Fact>
            <Fact label={t("movie.added")}>{formatDateTime(f.addedAt, i18n.language)}</Fact>
            <div className="col-span-2 text-[11px] text-faint">{t("movie.mediaInfoSource")}</div>
          </dl>
        ) : <p className="text-[13px] text-muted">{t("movie.noFile")}</p>}
      </Panel>
    </div>
  );
}
