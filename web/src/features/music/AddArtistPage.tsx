import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Checkbox, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { splitTags } from "@/features/movies/AddMoviePage";
import { useQualityProfiles, useRootFolders } from "@/features/movies/common";
import { MONITOR_NEW, musicKeys, useMetadataProfiles, type ArtistLookup, type MonitorNewAlbums } from "./common";

export type ArtistOptions = {
  rootFolderId: number;
  qualityProfileId: number;
  metadataProfileId: number;
  monitored: boolean;
  monitor: "all" | "future" | "latest" | "none";
  monitorNewAlbums: MonitorNewAlbums;
  tags: string;
  searchOnAdd: boolean;
};

export const defaultArtistOptions: ArtistOptions = { rootFolderId: 0, qualityProfileId: 0, metadataProfileId: 0, monitored: true, monitor: "all",
  monitorNewAlbums: "all", tags: "", searchOnAdd: true };

/** Root folder, quality and metadata profiles, monitoring and "search now" for new artists. */
export function ArtistOptionsForm({ value, onChange }: { value: ArtistOptions; onChange: (v: ArtistOptions) => void }) {
  const { t } = useTranslation();
  const roots = useRootFolders();
  const profiles = useQualityProfiles();
  const meta = useMetadataProfiles();
  const musicRoots = (roots.data ?? []).filter((r) => r.kind === "music");
  const musicProfiles = (profiles.data ?? []).filter((p) => p.kind === "music");
  const metaProfiles = meta.data ?? [];
  useEffect(() => {
    const next = { ...value };
    if (!next.rootFolderId && musicRoots[0]) next.rootFolderId = musicRoots[0].id;
    if (!next.qualityProfileId && musicProfiles[0]) next.qualityProfileId = musicProfiles[0].id;
    if (!next.metadataProfileId && metaProfiles[0]) next.metadataProfileId = metaProfiles[0].id;
    if (next.rootFolderId !== value.rootFolderId || next.qualityProfileId !== value.qualityProfileId || next.metadataProfileId !== value.metadataProfileId) {
      onChange(next);
    }
  }, [musicRoots, musicProfiles, metaProfiles, value, onChange]);
  if (roots.data && musicRoots.length === 0) {
    return (
      <Alert tone="warning">
        {t("addArtist.noRoot")} <Link to="/settings/media" className="underline">{t("settings.nav.items.media")}</Link>
      </Alert>
    );
  }
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <SelectInput label={t("addMovie.rootFolder")} value={value.rootFolderId} onChange={(e) => onChange({ ...value, rootFolderId: Number(e.target.value) })}>
        {musicRoots.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
      </SelectInput>
      <SelectInput label={t("addMovie.profile")} value={value.qualityProfileId} onChange={(e) => onChange({ ...value, qualityProfileId: Number(e.target.value) })}>
        {musicProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </SelectInput>
      <SelectInput label={t("addArtist.metadataProfile")} value={value.metadataProfileId}
        onChange={(e) => onChange({ ...value, metadataProfileId: Number(e.target.value) })}>
        {metaProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </SelectInput>
      <SelectInput label={t("addArtist.monitor")} value={value.monitor} onChange={(e) => onChange({ ...value, monitor: e.target.value as ArtistOptions["monitor"] })}>
        {(["all", "future", "latest", "none"] as const).map((m) => <option key={m} value={m}>{t(`addArtist.monitorOptions.${m}`)}</option>)}
      </SelectInput>
      <SelectInput label={t("addArtist.monitorNew")} value={value.monitorNewAlbums}
        onChange={(e) => onChange({ ...value, monitorNewAlbums: e.target.value as MonitorNewAlbums })}>
        {MONITOR_NEW.map((m) => <option key={m} value={m}>{t(`addArtist.monitorNewOptions.${m}`)}</option>)}
      </SelectInput>
      <TextInput label={t("addMovie.tags")} hint={t("addMovie.tagsHint")} value={value.tags} onChange={(e) => onChange({ ...value, tags: e.target.value })} />
      <Checkbox label={t("addArtist.monitored")} description={t("addArtist.monitoredHint")} checked={value.monitored}
        onChange={(e) => onChange({ ...value, monitored: e.target.checked })} />
      <Checkbox label={t("addMovie.searchOnAdd")} description={t("addArtist.searchOnAddHint")} checked={value.searchOnAdd}
        onChange={(e) => onChange({ ...value, searchOnAdd: e.target.checked })} />
    </div>
  );
}

function facts(a: ArtistLookup): string {
  const life = a.begin ? (a.end ? `${a.begin.slice(0, 4)}–${a.end.slice(0, 4)}` : `${a.begin.slice(0, 4)}–`) : "";
  return [a.type, a.country, life].filter(Boolean).join(" · ");
}

export function AddArtistPage() {
  const { t } = useTranslation();
  const [term, setTerm] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [picked, setPicked] = useState<ArtistLookup | null>(null);
  const [opts, setOpts] = useState<ArtistOptions>(defaultArtistOptions);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const lookup = useQuery({
    queryKey: ["music", "lookup", submitted],
    queryFn: async () => unwrap(await api.GET("/music/artists/lookup", { params: { query: { term: submitted } } })),
    enabled: submitted.length > 0,
  });
  const add = useMutation({
    mutationFn: async (a: ArtistLookup) =>
      unwrap(await api.POST("/music/artists", { body: { mbid: a.mbid, rootFolderId: opts.rootFolderId, qualityProfileId: opts.qualityProfileId,
        metadataProfileId: opts.metadataProfileId, monitored: opts.monitored, monitor: opts.monitor, monitorNewAlbums: opts.monitorNewAlbums,
        tags: splitTags(opts.tags), searchOnAdd: opts.searchOnAdd } })),
    onSuccess: (a) => {
      void qc.invalidateQueries({ queryKey: musicKeys.artists });
      void navigate({ to: "/music/artists/$id", params: { id: String(a.id) } });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setPicked(null);
    setSubmitted(term.trim());
  };
  return (
    <div>
      <PageHeader title={t("addArtist.title")} description={t("addArtist.description")} />
      <form onSubmit={submit} className="mb-4 flex items-end gap-2" role="search">
        <div className="flex-1">
          <TextInput label={t("addArtist.search")} placeholder={t("addArtist.searchHint")} value={term} onChange={(e) => setTerm(e.target.value)}
            autoFocus data-testid="add-artist-term" />
        </div>
        <Button type="submit" variant="primary" disabled={!term.trim()}>
          <Search className="size-3.5" /> {t("addMovie.searchButton")}
        </Button>
      </form>
      {lookup.isFetching ? <Spinner /> : null}
      <ErrorAlert error={lookup.error} />
      {lookup.data && lookup.data.length === 0 ? <p className="text-[13px] text-muted">{t("addMovie.noResults")}</p> : null}
      {picked ? (
        <Panel title={picked.name + (picked.disambiguation ? ` (${picked.disambiguation})` : "")} description={t("addArtist.optionsDescription")}
          actions={<Button variant="ghost" onClick={() => setPicked(null)}>{t("common.back")}</Button>}>
          <ArtistOptionsForm value={opts} onChange={setOpts} />
          <div className="mt-4 flex items-center gap-2">
            <Button variant="primary" busy={add.isPending} onClick={() => add.mutate(picked)}
              disabled={!opts.rootFolderId || !opts.qualityProfileId || !opts.metadataProfileId} data-testid="add-artist-confirm">
              {t("addArtist.add")}
            </Button>
            <ErrorAlert error={add.error} />
          </div>
        </Panel>
      ) : (
        <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {(lookup.data ?? []).map((a) => (
            <li key={a.mbid} className="flex flex-col rounded-ui border border-line bg-surface p-3" data-testid="artist-result">
              <div className="flex items-baseline justify-between gap-2">
                <h2 className="truncate text-[14px] font-semibold text-fg">{a.name}</h2>
                <span className="shrink-0 text-[11px] tabular-nums text-faint" title={t("addArtist.score")}>{a.score}</span>
              </div>
              {/* MusicBrainz's disambiguation tells artists of the same name apart. */}
              <p className="text-[12px] text-muted">{a.disambiguation || t("addArtist.noDisambiguation")}</p>
              <p className="text-[12px] text-faint">{facts(a)}</p>
              <div className="mt-auto pt-2">
                {a.artistId ? (
                  <Link to="/music/artists/$id" params={{ id: String(a.artistId) }} className="text-[13px] text-accent hover:underline">
                    {t("addMovie.inLibrary")}
                  </Link>
                ) : (
                  <Button variant="secondary" onClick={() => setPicked(a)} data-testid={`pick-${a.mbid}`}>{t("addMovie.choose")}</Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
