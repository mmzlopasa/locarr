import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Bookmark, BookmarkX, FolderInput, History, Search, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Badge, Button, Checkbox, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { HistoryTab } from "@/features/movies/HistoryTab";
import { InteractiveSearch } from "@/features/movies/InteractiveSearch";
import { AlbumStatusBadge, Cover, formatLength, musicKeys, typeKey, type AlbumDetail, type AlbumRelease, type MusicImportPreview } from "./common";

export function useAlbum(id: number) {
  return useQuery({
    queryKey: musicKeys.album(id),
    queryFn: async () => unwrap(await api.GET("/music/albums/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data?.album.libraryStatus === "downloading" ? 4_000 : false),
  });
}

/** "2020 · GB · 2×CD · 8 tracks · deluxe edition" */
export function releaseLabel(r: AlbumRelease, t: (k: string, o?: Record<string, unknown>) => string): string {
  return [r.date || null, r.country || null, r.format || null, t("music.tracks", { count: r.trackCount }), r.disambiguation || null,
    r.status && r.status !== "official" ? t(`music.statuses.${typeKey(r.status)}`, { defaultValue: r.status }) : null].filter(Boolean).join(" · ");
}

type TabKey = "tracks" | "search" | "import";

/** One album: the chosen edition, its tracks and files, searches, a folder import with match scores, and the timeline. */
export function AlbumDetailPage() {
  const { id } = useParams({ from: "/app/music/albums/$id" });
  const albumId = Number(id);
  const { t, i18n } = useTranslation();
  const me = useCurrentUser();
  const canManage = me.role === "admin" || me.permissions.manageLibrary;
  const album = useAlbum(albumId);
  const [tab, setTab] = useState<TabKey>("tracks");
  const [drawer, setDrawer] = useState(false);
  const qc = useQueryClient();
  const update = useMutation({
    mutationFn: async (body: { monitored?: boolean; releaseId?: number }) =>
      unwrap(await api.PATCH("/music/albums/{id}", { params: { path: { id: albumId } }, body })),
    onSuccess: (d) => {
      qc.setQueryData(musicKeys.album(albumId), d);
      void qc.invalidateQueries({ queryKey: ["music", "artists"] });
    },
  });
  const search = useMutation({ mutationFn: async () => unwrap(await api.POST("/music/albums/{id}/search", { params: { path: { id: albumId } } })) });
  if (album.isLoading) return <Spinner />;
  if (album.error || !album.data) return <ErrorAlert error={album.error} />;
  const d = album.data;
  const a = d.album;
  const tabs: TabKey[] = canManage ? ["tracks", "search", "import"] : ["tracks"];
  return (
    <div>
      <div className="mb-5 flex flex-col gap-4 sm:flex-row">
        <Cover file={a.cover} title={a.title} size="500" className="w-44 shrink-0" />
        <div className="min-w-0 flex-1">
          <Link to="/music/artists/$id" params={{ id: String(a.artistId) }} className="text-[13px] text-muted hover:text-accent">{a.artistName}</Link>
          <h1 className="text-[22px] font-semibold text-fg" data-testid="album-title">{a.title}</h1>
          <p className="text-[13px] text-faint">
            {[a.releaseDate, t(`music.types.${typeKey(a.primaryType || "other")}`, { defaultValue: a.primaryType }),
              ...a.secondaryTypes.map((s) => t(`music.types.${typeKey(s)}`, { defaultValue: s }))].filter(Boolean).join(" · ")}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <AlbumStatusBadge status={a.libraryStatus} />
            {a.trackCount ? <Badge>{t("music.tracksHave", { have: a.fileCount, count: a.trackCount })}</Badge> : null}
            {a.quality ? <Badge tone={a.cutoffMet ? "success" : "warning"}>{a.quality.name}</Badge> : null}
            {a.sizeBytes ? <Badge>{formatBytes(a.sizeBytes, i18n.language)}</Badge> : null}
            {!a.artistMonitored ? <Badge tone="warning">{t("music.artistUnmonitored")}</Badge> : null}
          </div>
          {a.path ? <p className="mt-2 truncate font-mono text-[12px] text-faint" data-testid="album-path">{a.path}</p> : null}
          <div className="mt-3 flex flex-wrap items-end gap-2">
            {canManage ? (
              <>
                <Button onClick={() => update.mutate({ monitored: !a.monitored })} busy={update.isPending && update.variables?.monitored !== undefined}>
                  {a.monitored ? <BookmarkX className="size-3.5" /> : <Bookmark className="size-3.5" />} {a.monitored ? t("movie.actions.unmonitor") : t("movie.actions.monitor")}
                </Button>
                <Button onClick={() => search.mutate()} busy={search.isPending} disabled={!a.available}><Search className="size-3.5" /> {t("movie.actions.search")}</Button>
              </>
            ) : null}
            <Button onClick={() => setDrawer(true)} data-testid="open-history"><History className="size-3.5" /> {t("music.history")}</Button>
          </div>
          {search.data ? <p role="status" className="mt-2 text-[13px] text-success">{t("wanted.queued", { count: 1 })}</p> : null}
          <ErrorAlert error={update.error ?? search.error} />
        </div>
      </div>
      <ReleasePicker d={d} canManage={canManage} busy={update.isPending} onPick={(releaseId) => update.mutate({ releaseId })} />
      <div role="tablist" aria-label={t("movie.tabs.label")} className="mb-4 flex gap-1 border-b border-line">
        {tabs.map((k) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)} data-testid={`tab-${k}`}
            className={`-mb-px border-b-2 px-3 py-2 text-[13px] ${tab === k ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
            {t(`music.tabs.${k}`)}
          </button>
        ))}
      </div>
      {tab === "tracks" ? <Tracks d={d} /> : null}
      {tab === "search" ? <InteractiveSearch movieId={albumId} itemRef={a.itemRef} kind="album" /> : null}
      {tab === "import" ? <ManualImport albumId={albumId} /> : null}
      {drawer ? (
        <div className="fixed inset-0 z-40 flex justify-end bg-black/50" role="dialog" aria-modal="true" aria-label={t("music.history")}
          onClick={(e) => { if (e.target === e.currentTarget) setDrawer(false); }} onKeyDown={(e) => { if (e.key === "Escape") setDrawer(false); }}>
          <div className="h-full w-full max-w-xl overflow-y-auto border-l border-line bg-bg p-4" data-testid="history-drawer">
            <div className="mb-3 flex items-center justify-between">
              <h2 className="text-[15px] font-semibold text-fg">{t("music.history")}</h2>
              <Button variant="ghost" onClick={() => setDrawer(false)} aria-label={t("nav.close")} autoFocus><X className="size-4" /></Button>
            </div>
            <HistoryTab itemRef={a.itemRef} />
          </div>
        </div>
      ) : null}
    </div>
  );
}

function ReleasePicker({ d, canManage, busy, onPick }: { d: AlbumDetail; canManage: boolean; busy: boolean; onPick: (id: number) => void }) {
  const { t } = useTranslation();
  const r = d.album.release;
  return (
    <Panel title={t("music.edition")} description={d.album.releaseLocked ? t("music.editionLocked") : t("music.editionAuto")} className="mb-4">
      {canManage && d.releases.length > 0 ? (
        <SelectInput label={t("music.chooseEdition")} value={d.album.releaseLocked ? (r?.id ?? 0) : 0} disabled={busy} data-testid="release-picker"
          onChange={(e) => onPick(Number(e.target.value))}>
          <option value={0}>{t("music.editionAutomatic")}{!d.album.releaseLocked && r ? ` — ${releaseLabel(r, t)}` : ""}</option>
          {d.releases.map((x) => <option key={x.id} value={x.id}>{releaseLabel(x, t)}</option>)}
        </SelectInput>
      ) : (
        <p className="text-[13px] text-muted">{r ? releaseLabel(r, t) : t("music.noEdition")}</p>
      )}
      {r?.label ? <p className="mt-1 text-[12px] text-faint">{[r.label, r.catalogNumber].filter(Boolean).join(" · ")}</p> : null}
    </Panel>
  );
}

function Tracks({ d }: { d: AlbumDetail }) {
  const { t, i18n } = useTranslation();
  const discs = new Set(d.tracks.map((x) => x.disc)).size;
  if (d.tracksError && d.tracks.length === 0) return <p className="text-[13px] text-warning">{t("music.tracksUnavailable")}</p>;
  if (d.tracks.length === 0) return <p className="text-[13px] text-muted">{t("music.noTracks")}</p>;
  return (
    <div>
      <div className="overflow-x-auto rounded-ui border border-line">
        <table className="w-full min-w-[640px] text-[13px]" data-testid="tracks">
          <thead className="bg-surface-2 text-left text-[12px] text-muted">
            <tr>
              <th className="w-14 px-2 py-1.5 font-medium">#</th>
              <th className="px-2 py-1.5 font-medium">{t("music.columns.title")}</th>
              <th className="w-16 px-2 py-1.5 text-right font-medium">{t("music.columns.length")}</th>
              <th className="px-2 py-1.5 font-medium">{t("music.columns.file")}</th>
            </tr>
          </thead>
          <tbody>
            {d.tracks.map((tr) => (
              <tr key={tr.id} className="border-t border-line align-top" data-testid="track-row" data-has-file={tr.file ? "true" : "false"}>
                <td className="px-2 py-1.5 tabular-nums text-muted">{discs > 1 ? `${tr.disc}-` : ""}{tr.number || tr.position}</td>
                <td className="px-2 py-1.5 text-fg">
                  {tr.title}
                  {tr.artist && tr.artist !== d.album.artistName ? <div className="text-[11px] text-faint">{tr.artist}</div> : null}
                </td>
                <td className="px-2 py-1.5 text-right tabular-nums text-muted">{formatLength(tr.lengthMs)}</td>
                <td className="max-w-[380px] px-2 py-1.5">
                  {tr.file ? (
                    <>
                      <div className="truncate font-mono text-[12px] text-muted" title={tr.file.path}>{tr.file.path.split("/").pop()}</div>
                      <div className="text-[11px] text-faint">
                        {[tr.file.quality.name, tr.file.bitDepth ? `${tr.file.bitDepth} bit` : null,
                          tr.file.sampleRate ? `${tr.file.sampleRate / 1000} kHz` : null, tr.file.bitrate ? `${tr.file.bitrate} kbps` : null,
                          formatBytes(tr.file.sizeBytes, i18n.language)].filter(Boolean).join(" · ")}
                      </div>
                    </>
                  ) : <span className="text-[12px] text-danger">{t("music.missingTrack")}</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {d.extraFiles.length > 0 ? (
        <p className="mt-2 text-[12px] text-warning">{t("music.extraFiles", { count: d.extraFiles.length })}</p>
      ) : null}
    </div>
  );
}

/** Import a folder into this album: preview the match against each edition (with scores), then import. */
function ManualImport({ albumId }: { albumId: number }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [path, setPath] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [releaseId, setReleaseId] = useState(0);
  const [partial, setPartial] = useState(false);
  const preview = useQuery({
    queryKey: ["music", "import", albumId, submitted],
    queryFn: async () => unwrap(await api.GET("/music/import/manual", { params: { query: { path: submitted, albumId } } })),
    enabled: submitted.length > 0,
    retry: false,
  });
  const run = useMutation({
    mutationFn: async (force: boolean) => unwrap(await api.POST("/music/import/manual", { body: { path: submitted, albumId,
      ...(releaseId ? { releaseId } : {}), allowPartial: partial, force } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["music"] }),
  });
  const p: MusicImportPreview | undefined = preview.data;
  const chosen = p?.matches.find((m) => m.release.id === releaseId) ?? p?.matches[0];
  return (
    <div>
      <form className="mb-3 flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); setReleaseId(0); setSubmitted(path.trim()); }}>
        <div className="flex-1">
          <TextInput label={t("music.importFolder")} hint={t("music.importFolderHint")} value={path} onChange={(e) => setPath(e.target.value)}
            placeholder="/data/downloads/…" data-testid="import-path" />
        </div>
        <Button type="submit" disabled={!path.trim()}><FolderInput className="size-3.5" /> {t("music.preview")}</Button>
      </form>
      {preview.isFetching ? <Spinner /> : null}
      <ErrorAlert error={preview.error} />
      {p ? (
        <div className="space-y-3">
          <p className="text-[13px] text-muted">{t("music.previewSummary", { files: p.files, min: p.minimumScore })}</p>
          {p.rejection ? <p className="text-[13px] text-warning" data-testid="import-rejection">{p.rejection.message}</p> : null}
          <ul className="space-y-1.5">
            {p.matches.map((m) => (
              <li key={m.release.id}>
                <label className="flex cursor-pointer items-center gap-2 rounded-ui border border-line bg-surface px-2 py-1.5 text-[13px]">
                  <input type="radio" name="release" checked={(chosen?.release.id ?? 0) === m.release.id} onChange={() => setReleaseId(m.release.id)} />
                  <span className={`w-10 text-right font-semibold tabular-nums ${m.score >= p.minimumScore ? "text-success" : "text-danger"}`} data-testid="match-score">
                    {m.score}
                  </span>
                  <span className="flex-1 truncate text-fg">{releaseLabel(m.release, t)}</span>
                  <span className="text-[12px] text-muted">{t("music.matched", { count: m.pairs.length, missing: m.missing.length, extra: m.extra.length })}</span>
                </label>
              </li>
            ))}
          </ul>
          {chosen ? (
            <div className="overflow-x-auto rounded-ui border border-line">
              <table className="w-full min-w-[640px] text-[12px]">
                <thead className="bg-surface-2 text-left text-muted">
                  <tr>
                    <th className="px-2 py-1">{t("music.columns.track")}</th>
                    <th className="px-2 py-1">{t("music.columns.file")}</th>
                    <th className="px-2 py-1 text-right">{t("music.columns.fit")}</th>
                    <th className="px-2 py-1 text-right">{t("music.columns.lengthDiff")}</th>
                  </tr>
                </thead>
                <tbody>
                  {chosen.pairs.map((x) => (
                    <tr key={x.path} className="border-t border-line">
                      <td className="px-2 py-1 text-fg">{x.disc}-{x.position} {x.trackTitle}</td>
                      <td className="max-w-[320px] truncate px-2 py-1 font-mono text-muted" title={x.relativePath}>{x.relativePath}</td>
                      <td className="px-2 py-1 text-right tabular-nums text-muted">{x.byId ? "ID" : `${Math.round(x.score * 100)} %`}</td>
                      <td className="px-2 py-1 text-right tabular-nums text-muted">{x.durationDiffMs ? `${(x.durationDiffMs / 1000).toFixed(1)} s` : "–"}</td>
                    </tr>
                  ))}
                  {chosen.missing.map((x) => (
                    <tr key={`m-${x}`} className="border-t border-line text-danger"><td className="px-2 py-1" colSpan={4}>{t("music.missingNamed", { track: x })}</td></tr>
                  ))}
                  {chosen.extra.map((x) => (
                    <tr key={`e-${x}`} className="border-t border-line text-warning"><td className="px-2 py-1" colSpan={4}>{t("music.extraNamed", { file: x })}</td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
          <Checkbox label={t("music.allowPartial")} description={t("music.allowPartialHint")} checked={partial} onChange={(e) => setPartial(e.target.checked)} />
          <div className="flex flex-wrap gap-2">
            <Button variant="primary" onClick={() => run.mutate(false)} busy={run.isPending} disabled={!chosen} data-testid="import-run">{t("music.import")}</Button>
            {chosen && chosen.score < p.minimumScore ? (
              <Button variant="danger" onClick={() => run.mutate(true)} busy={run.isPending}>{t("music.importAnyway")}</Button>
            ) : null}
          </div>
          <ErrorAlert error={run.error} />
          {run.data ? (
            <p role="status" className="text-[13px] text-success">
              {t("music.imported", { count: run.data.imported.length, skipped: run.data.skipped.length, score: run.data.score })}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
