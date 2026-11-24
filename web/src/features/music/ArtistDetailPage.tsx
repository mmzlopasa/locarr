import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Bookmark, BookmarkX, Pencil, RefreshCw, Search, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Badge, Button, Checkbox, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { splitTags } from "@/features/movies/AddMoviePage";
import { useQualityProfiles } from "@/features/movies/common";
import { AlbumStatusBadge, Cover, GROUP_ORDER, MONITOR_NEW, albumGroup, musicKeys, typeKey, useMetadataProfiles, type Album, type Artist,
  type MonitorNewAlbums } from "./common";
import { SimilarArtists } from "./discovery";

export function useArtist(id: number) {
  return useQuery({ queryKey: musicKeys.artist(id), queryFn: async () => unwrap(await api.GET("/music/artists/{id}", { params: { path: { id } } })) });
}

export function useArtistAlbums(id: number) {
  return useQuery({
    queryKey: musicKeys.albums(id),
    queryFn: async () => unwrap(await api.GET("/music/artists/{id}/albums", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data?.some((a) => a.libraryStatus === "downloading") ? 4_000 : 15_000),
  });
}

/** An artist: facts, editing, and the albums grouped by type with monitoring and searches. */
export function ArtistDetailPage() {
  const { id } = useParams({ from: "/app/music/artists/$id" });
  const artistId = Number(id);
  const { t, i18n } = useTranslation();
  const me = useCurrentUser();
  const canManage = me.role === "admin" || me.permissions.manageLibrary;
  const artist = useArtist(artistId);
  const albums = useArtistAlbums(artistId);
  const [editing, setEditing] = useState(false);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const invalidate = () => void qc.invalidateQueries({ queryKey: ["music"] });
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/music/artists/{id}/refresh", { params: { path: { id: artistId } } })),
    onSuccess: invalidate,
  });
  const search = useMutation({ mutationFn: async () => unwrap(await api.POST("/music/artists/{id}/search", { params: { path: { id: artistId } } })) });
  const toggle = useMutation({
    mutationFn: async (monitored: boolean) => unwrap(await api.PATCH("/music/artists/{id}", { params: { path: { id: artistId } }, body: { monitored } })),
    onSuccess: invalidate,
  });
  if (artist.isLoading) return <Spinner />;
  if (artist.error || !artist.data) return <ErrorAlert error={artist.error} />;
  const a = artist.data;
  const groups = new Map<string, Album[]>();
  for (const al of albums.data ?? []) groups.set(albumGroup(al), [...(groups.get(albumGroup(al)) ?? []), al]);
  return (
    <div>
      <div className="mb-5 flex flex-col gap-4 sm:flex-row">
        <Cover file={a.cover} title={a.name} size="250" className="w-36 shrink-0" />
        <div className="min-w-0 flex-1">
          <h1 className="text-[22px] font-semibold text-fg" data-testid="artist-name">{a.name}</h1>
          {a.disambiguation ? <p className="text-[13px] text-muted">{a.disambiguation}</p> : null}
          <p className="mt-1 text-[13px] text-faint">{[a.type, a.country, a.genres.slice(0, 3).join(", ")].filter(Boolean).join(" · ")}</p>
          <div className="mt-2 flex flex-wrap gap-1.5 text-[12px]">
            <Badge tone={a.monitored ? "success" : "neutral"}>{a.monitored ? t("movies.monitored") : t("movies.unmonitored")}</Badge>
            <Badge>{t("music.albumsHave", { have: a.albumsWithFiles, count: a.monitoredAlbums })}</Badge>
            <Badge>{t("music.trackFiles", { count: a.files })}</Badge>
            {a.sizeBytes ? <Badge>{formatBytes(a.sizeBytes, i18n.language)}</Badge> : null}
            <Badge>{t(`addArtist.monitorNewOptions.${a.monitorNewAlbums}`)}</Badge>
          </div>
          <p className="mt-2 truncate font-mono text-[12px] text-faint">{a.path}</p>
          {a.aliases.length ? <p className="mt-1 text-[12px] text-faint">{t("music.aliases")}: {a.aliases.join(", ")}</p> : null}
          {canManage ? (
            <div className="mt-3 flex flex-wrap gap-2">
              <Button onClick={() => search.mutate()} busy={search.isPending} data-testid="search-artist">
                <Search className="size-3.5" /> {t("music.searchWanted")}
              </Button>
              <Button onClick={() => refresh.mutate()} busy={refresh.isPending}><RefreshCw className="size-3.5" /> {t("music.refresh")}</Button>
              <Button onClick={() => toggle.mutate(!a.monitored)} busy={toggle.isPending}>
                {a.monitored ? <BookmarkX className="size-3.5" /> : <Bookmark className="size-3.5" />} {a.monitored ? t("movie.actions.unmonitor") : t("movie.actions.monitor")}
              </Button>
              <Button onClick={() => setEditing(!editing)} aria-expanded={editing}><Pencil className="size-3.5" /> {t("movie.actions.edit")}</Button>
            </div>
          ) : null}
          {search.data ? <p role="status" className="mt-2 text-[13px] text-success">{t("wanted.queued", { count: search.data.count })}</p> : null}
          <ErrorAlert error={refresh.error ?? search.error ?? toggle.error} />
        </div>
      </div>
      {editing ? <EditArtist a={a} onDone={() => setEditing(false)} onDeleted={() => void navigate({ to: "/music" })} /> : null}
      {albums.isLoading ? <Spinner /> : null}
      <ErrorAlert error={albums.error} />
      {albums.data && albums.data.length === 0 ? <p className="text-[13px] text-muted">{t("music.noAlbums")}</p> : null}
      {GROUP_ORDER.filter((g) => groups.has(g)).map((g) => (
        <AlbumGroup key={g} group={g} albums={groups.get(g) ?? []} canManage={canManage} />
      ))}
      <SimilarArtists artistId={a.id} mbid={a.mbid} />
    </div>
  );
}

function AlbumGroup({ group, albums, canManage }: { group: string; albums: Album[]; canManage: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const monitor = useMutation({
    mutationFn: async (v: { ids: number[]; monitored: boolean }) => unwrap(await api.PUT("/music/albums/monitor", { body: { albumIds: v.ids, monitored: v.monitored } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["music"] }),
  });
  const search = useMutation({ mutationFn: async (id: number) => unwrap(await api.POST("/music/albums/{id}/search", { params: { path: { id } } })) });
  const allOn = albums.every((a) => a.monitored);
  return (
    <section className="mb-5" aria-labelledby={`group-${group}`} data-testid={`album-group-${group}`}>
      <div className="mb-1.5 flex items-center justify-between">
        <h2 id={`group-${group}`} className="text-[13px] font-semibold uppercase tracking-wide text-muted">
          {t(`music.groups.${group}`)} <span className="tabular-nums text-faint">{albums.length}</span>
        </h2>
        {canManage ? (
          <Button variant="ghost" onClick={() => monitor.mutate({ ids: albums.map((a) => a.id), monitored: !allOn })} busy={monitor.isPending}>
            {allOn ? t("music.unmonitorAll") : t("music.monitorAll")}
          </Button>
        ) : null}
      </div>
      <div className="overflow-x-auto rounded-ui border border-line">
        <table className="w-full min-w-[720px] text-[13px]">
          <thead className="bg-surface-2 text-left text-[12px] text-muted">
            <tr>
              <th className="w-10 px-2 py-1.5"><span className="sr-only">{t("movies.monitored")}</span></th>
              <th className="px-2 py-1.5 font-medium">{t("music.columns.album")}</th>
              <th className="px-2 py-1.5 font-medium">{t("music.columns.released")}</th>
              <th className="px-2 py-1.5 font-medium">{t("music.columns.tracks")}</th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.quality")}</th>
              <th className="px-2 py-1.5 font-medium">{t("movies.columns.status")}</th>
              <th className="px-2 py-1.5" />
            </tr>
          </thead>
          <tbody>
            {albums.map((al) => (
              <tr key={al.id} className="border-t border-line" data-testid="album-row">
                <td className="px-2 py-1.5">
                  <input type="checkbox" checked={al.monitored} disabled={!canManage || monitor.isPending} aria-label={t("music.monitorAlbum", { title: al.title })}
                    onChange={(e) => monitor.mutate({ ids: [al.id], monitored: e.target.checked })} className="size-4 accent-[var(--color-accent)]" />
                </td>
                <td className="max-w-[360px] px-2 py-1.5">
                  <Link to="/music/albums/$id" params={{ id: String(al.id) }} className="text-fg hover:text-accent">{al.title}</Link>
                  {al.disambiguation ? <span className="ml-1 text-faint">({al.disambiguation})</span> : null}
                  {al.secondaryTypes.length ? (
                    <span className="ml-1 text-[11px] text-faint">{al.secondaryTypes.map((s) => t(`music.types.${typeKey(s)}`, { defaultValue: s })).join(", ")}</span>
                  ) : null}
                  {al.filtered ? <span className="ml-1 text-[11px] text-warning">{t("music.filtered")}</span> : null}
                </td>
                <td className="whitespace-nowrap px-2 py-1.5 tabular-nums text-muted">{al.releaseDate || "–"}</td>
                <td className="whitespace-nowrap px-2 py-1.5 tabular-nums text-muted">{al.trackCount ? `${al.fileCount}/${al.trackCount}` : "–"}</td>
                <td className="whitespace-nowrap px-2 py-1.5 text-muted">{al.quality?.name ?? "–"}</td>
                <td className="px-2 py-1.5">
                  <AlbumStatusBadge status={al.libraryStatus} />
                  {al.download && al.libraryStatus === "downloading" ? (
                    <span className="ml-1 text-[11px] tabular-nums text-info">{Math.round(al.download.progress * 100)} %</span>
                  ) : null}
                </td>
                <td className="px-2 py-1.5 text-right">
                  {canManage && al.available ? (
                    <Button variant="ghost" onClick={() => search.mutate(al.id)} busy={search.isPending && search.variables === al.id}
                      aria-label={t("music.searchAlbum", { title: al.title })} title={t("music.searchAlbum", { title: al.title })}>
                      <Search className="size-3.5" />
                    </Button>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ErrorAlert error={monitor.error ?? search.error} />
      {search.isSuccess ? <p role="status" className="mt-1 text-[12px] text-success">{t("wanted.queued", { count: 1 })}</p> : null}
    </section>
  );
}

function EditArtist({ a, onDone, onDeleted }: { a: Artist; onDone: () => void; onDeleted: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const profiles = useQualityProfiles();
  const meta = useMetadataProfiles();
  const [v, setV] = useState({ monitorNewAlbums: a.monitorNewAlbums, qualityProfileId: a.qualityProfileId, metadataProfileId: a.metadataProfileId,
    tags: a.tags.join(", ") });
  const [deleteFiles, setDeleteFiles] = useState(false);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/music/artists/{id}", { params: { path: { id: a.id } }, body: { monitorNewAlbums: v.monitorNewAlbums,
      qualityProfileId: v.qualityProfileId, metadataProfileId: v.metadataProfileId, tags: splitTags(v.tags) } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["music"] });
      onDone();
    },
  });
  const del = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/music/artists/{id}", { params: { path: { id: a.id }, query: { deleteFiles } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: musicKeys.artists });
      onDeleted();
    },
  });
  return (
    <Panel title={t("music.editArtist")} className="mb-5">
      <div className="grid gap-3 sm:grid-cols-2">
        <SelectInput label={t("addMovie.profile")} value={v.qualityProfileId} onChange={(e) => setV({ ...v, qualityProfileId: Number(e.target.value) })}>
          {(profiles.data ?? []).filter((p) => p.kind === "music").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("addArtist.metadataProfile")} value={v.metadataProfileId} onChange={(e) => setV({ ...v, metadataProfileId: Number(e.target.value) })}>
          {(meta.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("addArtist.monitorNew")} value={v.monitorNewAlbums} onChange={(e) => setV({ ...v, monitorNewAlbums: e.target.value as MonitorNewAlbums })}>
          {MONITOR_NEW.map((m) => <option key={m} value={m}>{t(`addArtist.monitorNewOptions.${m}`)}</option>)}
        </SelectInput>
        <TextInput label={t("addMovie.tags")} value={v.tags} onChange={(e) => setV({ ...v, tags: e.target.value })} />
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending}>{t("common.save")}</Button>
        <Button variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
      <ErrorAlert error={save.error} />
      <div className="mt-5 border-t border-line pt-3">
        <Checkbox label={t("music.deleteFiles")} description={t("music.deleteFilesHint")} checked={deleteFiles} onChange={(e) => setDeleteFiles(e.target.checked)} />
        <Button variant="danger" className="mt-2" onClick={() => del.mutate()} busy={del.isPending} data-testid="delete-artist">
          <Trash2 className="size-3.5" /> {t("music.deleteArtist")}
        </Button>
        <ErrorAlert error={del.error} />
      </div>
    </Panel>
  );
}
