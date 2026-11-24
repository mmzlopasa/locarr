import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { components } from "@/api/schema.gen";
import { api, queryKeys, unwrap, type ImportList, type ImportListInput, type MinimumAvailability } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { Badge, Button, Checkbox, ConfirmButton, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { AVAILABILITIES, useQualityProfiles, useRootFolders } from "@/features/movies/common";
import { splitTags } from "@/features/movies/AddMoviePage";

const LANGUAGES = ["en-US", "en-GB", "hr-HR", "de-DE", "fr-FR", "es-ES", "it-IT", "sr-RS", "bs-BA", "sl-SI", "pt-BR", "nl-NL", "pl-PL"];

export function MetadataPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const st = useQuery({ queryKey: queryKeys.metadataSettings, queryFn: async () => unwrap(await api.GET("/settings/metadata")) });
  const [key, setKey] = useState("");
  const [lang, setLang] = useState("");
  const [region, setRegion] = useState("");
  useEffect(() => {
    if (st.data && !lang) {
      setLang(st.data.language);
      setRegion(st.data.region);
    }
  }, [st.data, lang]);
  const save = useMutation({
    mutationFn: async (body: { tmdbApiKey?: string; language?: string; region?: string }) => unwrap(await api.PUT("/settings/metadata", { body })),
    onSuccess: (v) => {
      qc.setQueryData(queryKeys.metadataSettings, v);
      setKey("");
    },
  });
  if (!st.data) return st.error ? <ErrorAlert error={st.error} /> : <Spinner />;
  return (
    <div className="space-y-4">
      <Panel title={t("metadata.title")} description={t("metadata.description")}>
        <div className="grid gap-3 sm:grid-cols-2">
          <TextInput label={t("metadata.key")} type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)}
            placeholder={st.data.tmdbApiKey.configured ? t("metadata.keySet") : t("metadata.keyMissing")} hint={t("metadata.keyHint")}
            data-testid="tmdb-key" />
          <div className="flex items-end">
            {st.data.tmdbApiKey.configured ? <Badge tone="success">{t("metadata.configured")}</Badge> : <Badge tone="warning">{t("metadata.notConfigured")}</Badge>}
          </div>
          <SelectInput label={t("metadata.language")} value={lang} onChange={(e) => setLang(e.target.value)}>
            {LANGUAGES.map((l) => <option key={l} value={l}>{l}</option>)}
          </SelectInput>
          <TextInput label={t("metadata.region")} hint={t("metadata.regionHint")} value={region} maxLength={2}
            onChange={(e) => setRegion(e.target.value.toUpperCase())} />
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button variant="primary" busy={save.isPending} data-testid="save-metadata"
            onClick={() => save.mutate({ ...(key ? { tmdbApiKey: key } : {}), language: lang, region })}>{t("metadata.save")}</Button>
          {st.data.tmdbApiKey.configured ? (
            <ConfirmButton label={t("metadata.removeKey")} confirmLabel={t("metadata.removeKeyConfirm")} onConfirm={() => save.mutate({ tmdbApiKey: "" })} />
          ) : null}
          {save.isSuccess ? <span role="status" className="text-[13px] text-success">{t("common.saved")}</span> : null}
          <ErrorAlert error={save.error} />
        </div>
        <p className="mt-3 text-[12px] text-faint">{t("metadata.attribution")}</p>
      </Panel>
      <TvdbPanel />
      <MusicDiscoveryPanel />
    </div>
  );
}

/** The optional TVDB v4 key and which provider answers series lookups. */
function TvdbPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const st = useQuery({ queryKey: queryKeys.metadataSettings, queryFn: async () => unwrap(await api.GET("/settings/metadata")) });
  const [key, setKey] = useState("");
  const [pin, setPin] = useState("");
  const save = useMutation({
    mutationFn: async (body: { tvdbApiKey?: string; tvdbPin?: string; seriesProvider?: "tmdb" | "tvdb" }) =>
      unwrap(await api.PUT("/settings/metadata", { body })),
    onSuccess: (v) => {
      qc.setQueryData(queryKeys.metadataSettings, v);
      setKey("");
      setPin("");
    },
  });
  if (!st.data) return null;
  const d = st.data;
  return (
    <Panel title={t("metadata.tvdb.title")} description={t("metadata.tvdb.description")}>
      <div className="grid gap-3 sm:grid-cols-3">
        <TextInput label={t("metadata.tvdb.key")} type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)}
          placeholder={d.tvdbApiKey.configured ? t("metadata.keySet") : t("metadata.keyMissing")} />
        <TextInput label={t("metadata.tvdb.pin")} type="password" autoComplete="off" value={pin} onChange={(e) => setPin(e.target.value)}
          placeholder={d.tvdbPin.configured ? t("metadata.keySet") : t("metadata.tvdb.pinHint")} />
        <SelectInput label={t("metadata.tvdb.provider")} value={d.seriesProvider}
          onChange={(e) => save.mutate({ seriesProvider: e.target.value as "tmdb" | "tvdb" })}>
          <option value="tmdb">TMDb</option>
          <option value="tvdb" disabled={!d.tvdbApiKey.configured}>TVDB</option>
        </SelectInput>
      </div>
      <p className="mt-2 text-[12px] text-faint">{t("metadata.tvdb.providerHint")}</p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" busy={save.isPending} disabled={!key && !pin}
          onClick={() => save.mutate({ ...(key ? { tvdbApiKey: key } : {}), ...(pin || key ? { tvdbPin: pin } : {}) })}>{t("metadata.save")}</Button>
        {d.tvdbApiKey.configured ? (
          <ConfirmButton label={t("metadata.removeKey")} confirmLabel={t("metadata.removeKeyConfirm")}
            onConfirm={() => save.mutate({ tvdbApiKey: "", seriesProvider: "tmdb" })} />
        ) : null}
        {d.tvdbApiKey.configured ? <Badge tone="success">{t("metadata.configured")}</Badge> : <Badge>{t("metadata.notConfigured")}</Badge>}
        <ErrorAlert error={save.error} />
      </div>
    </Panel>
  );
}

const blank = (): ImportListInput => ({ name: "", kind: "tmdb_list", listId: "", rootFolderId: 0, qualityProfileId: 0, enabled: true,
  monitored: true, searchOnAdd: false, minimumAvailability: "released", tags: [] });

export function ImportListsPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const lists = useQuery({ queryKey: queryKeys.importLists, queryFn: async () => unwrap(await api.GET("/movies/importlists")) });
  const exclusions = useQuery({ queryKey: queryKeys.exclusions, queryFn: async () => unwrap(await api.GET("/movies/exclusions")) });
  const roots = useRootFolders();
  const profiles = useQualityProfiles();
  const [form, setForm] = useState<ImportListInput | null>(null);
  const [editing, setEditing] = useState<number | null>(null);
  const [tags, setTags] = useState("");
  const done = () => void qc.invalidateQueries({ queryKey: queryKeys.importLists });
  const save = useMutation({
    mutationFn: async (v: ImportListInput) => editing
      ? unwrap(await api.PUT("/movies/importlists/{id}", { params: { path: { id: editing } }, body: v }))
      : unwrap(await api.POST("/movies/importlists", { body: v })),
    onSuccess: () => {
      setForm(null);
      setEditing(null);
      done();
    },
  });
  const sync = useMutation({ mutationFn: async (id: number) => unwrap(await api.POST("/movies/importlists/{id}/sync", { params: { path: { id } } })),
    onSuccess: () => { done(); void qc.invalidateQueries({ queryKey: queryKeys.movies }); } });
  const del = useMutation({ mutationFn: async (id: number) => unwrap(await api.DELETE("/movies/importlists/{id}", { params: { path: { id } } })), onSuccess: done });
  const unexclude = useMutation({ mutationFn: async (id: number) => unwrap(await api.DELETE("/movies/exclusions/{tmdbId}", { params: { path: { tmdbId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.exclusions }) });
  const movieRoots = (roots.data ?? []).filter((r) => r.kind === "movies");
  const movieProfiles = (profiles.data ?? []).filter((p) => p.kind === "movies");
  const open = (l?: ImportList) => {
    setEditing(l?.id ?? null);
    setTags(l?.tags.join(", ") ?? "");
    setForm(l ? { name: l.name, kind: l.kind, listId: l.listId, rootFolderId: l.rootFolderId, qualityProfileId: l.qualityProfileId, enabled: l.enabled,
      monitored: l.monitored, searchOnAdd: l.searchOnAdd, minimumAvailability: l.minimumAvailability, tags: l.tags }
      : { ...blank(), rootFolderId: movieRoots[0]?.id ?? 0, qualityProfileId: movieProfiles[0]?.id ?? 0 });
  };
  return (
    <div className="space-y-4">
      <Panel title={t("lists.title")} description={t("lists.description")} actions={<Button onClick={() => open()}>{t("lists.add")}</Button>}>
        <ErrorAlert error={lists.error ?? sync.error ?? del.error} />
        {lists.data && lists.data.length === 0 && !form ? <EmptyState>{t("lists.none")}</EmptyState> : null}
        <ul className="divide-y divide-line">
          {(lists.data ?? []).map((l) => (
            <li key={l.id} className="flex flex-wrap items-center gap-2 py-2">
              <div className="min-w-0 flex-1">
                <div className="text-[13px] text-fg">{l.name} {!l.enabled ? <Badge>{t("lists.disabled")}</Badge> : null}</div>
                <div className="text-[12px] text-faint">
                  {t(`lists.kinds.${l.kind}`)} {l.listId} · {l.lastSyncAt ? t("lists.lastSync", { when: formatDateTime(l.lastSyncAt, i18n.language), count: l.lastAdded }) : t("lists.neverSynced")}
                </div>
                {l.lastError ? <div className="text-[12px] text-danger">{l.lastError}</div> : null}
              </div>
              <Button onClick={() => sync.mutate(l.id)} busy={sync.isPending && sync.variables === l.id}>{t("lists.sync")}</Button>
              <Button variant="ghost" onClick={() => open(l)}>{t("lists.edit")}</Button>
              <ConfirmButton label={t("common.delete")} confirmLabel={t("lists.deleteConfirm")} onConfirm={() => del.mutate(l.id)} />
            </li>
          ))}
        </ul>
        {sync.data ? <p role="status" className="mt-2 text-[13px] text-muted">{t("lists.synced", { found: sync.data.found, added: sync.data.added.length, excluded: sync.data.excluded })}</p> : null}
        {form ? (
          <div className="mt-3 rounded-ui border border-line bg-surface-2 p-3">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <TextInput label={t("lists.name")} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              <SelectInput label={t("lists.kind")} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as ImportListInput["kind"] })}>
                {(["tmdb_list", "tmdb_collection"] as const).map((k) => <option key={k} value={k}>{t(`lists.kinds.${k}`)}</option>)}
              </SelectInput>
              <TextInput label={t("lists.listId")} hint={t(`lists.idHints.${form.kind}`)} value={form.listId} onChange={(e) => setForm({ ...form, listId: e.target.value })} />
              <SelectInput label={t("addMovie.rootFolder")} value={form.rootFolderId} onChange={(e) => setForm({ ...form, rootFolderId: Number(e.target.value) })}>
                {movieRoots.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
              </SelectInput>
              <SelectInput label={t("addMovie.profile")} value={form.qualityProfileId} onChange={(e) => setForm({ ...form, qualityProfileId: Number(e.target.value) })}>
                {movieProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </SelectInput>
              <SelectInput label={t("addMovie.availability")} value={form.minimumAvailability}
                onChange={(e) => setForm({ ...form, minimumAvailability: e.target.value as MinimumAvailability })}>
                {AVAILABILITIES.map((a) => <option key={a} value={a}>{t(`availability.${a}`)}</option>)}
              </SelectInput>
              <TextInput label={t("addMovie.tags")} value={tags} onChange={(e) => setTags(e.target.value)} />
              <Checkbox label={t("lists.enabled")} checked={form.enabled ?? true} onChange={(e) => setForm({ ...form, enabled: e.target.checked })} />
              <Checkbox label={t("addMovie.monitored")} checked={form.monitored ?? true} onChange={(e) => setForm({ ...form, monitored: e.target.checked })} />
              <Checkbox label={t("addMovie.searchOnAdd")} checked={form.searchOnAdd ?? false} onChange={(e) => setForm({ ...form, searchOnAdd: e.target.checked })} />
            </div>
            <div className="mt-3 flex gap-2">
              <Button variant="primary" onClick={() => save.mutate({ ...form, tags: splitTags(tags) })} busy={save.isPending}>{t("common.save")}</Button>
              <Button variant="ghost" onClick={() => setForm(null)}>{t("common.cancel")}</Button>
              <ErrorAlert error={save.error} />
            </div>
          </div>
        ) : null}
      </Panel>
      <Panel title={t("lists.exclusions")} description={t("lists.exclusionsHint")}>
        {exclusions.data && exclusions.data.length === 0 ? <EmptyState>{t("lists.noExclusions")}</EmptyState> : null}
        <ul className="divide-y divide-line">
          {(exclusions.data ?? []).map((x) => (
            <li key={x.tmdbId} className="flex items-center gap-2 py-1.5 text-[13px]">
              <span className="flex-1">{x.title} ({x.year || "?"})</span>
              <Button variant="ghost" onClick={() => unexclude.mutate(x.tmdbId)}>{t("lists.allowAgain")}</Button>
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  );
}

type DiscoveryInput = components["schemas"]["MusicDiscoverySettingsInput"];
type DiscoveryProvider = components["schemas"]["MusicDiscoveryProviderId"];
const REFRESH_DAYS = [1, 3, 7, 14, 30, 60, 90];

/** Music discovery: providers of similar artists, their keys and how often they are asked again. */
function MusicDiscoveryPanel() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const key = ["settings", "musicdiscovery"] as const;
  const st = useQuery({ queryKey: key, queryFn: async () => unwrap(await api.GET("/settings/musicdiscovery")) });
  const [lastfm, setLastfm] = useState("");
  const [spId, setSpId] = useState("");
  const [spSecret, setSpSecret] = useState("");
  const [tests, setTests] = useState<Partial<Record<DiscoveryProvider, { ok: boolean; message: string; code?: string }>>>({});
  const save = useMutation({
    mutationFn: async (body: DiscoveryInput) => unwrap(await api.PUT("/settings/musicdiscovery", { body })),
    onSuccess: (v) => {
      qc.setQueryData(key, v);
      void qc.invalidateQueries({ queryKey: ["music", "discovery"] });
      setLastfm("");
      setSpId("");
      setSpSecret("");
    },
  });
  const test = useMutation({
    mutationFn: async (provider: DiscoveryProvider) => ({ provider,
      res: unwrap(await api.POST("/settings/musicdiscovery/test", { body: { provider } })) }),
    onSuccess: ({ provider, res }) => {
      setTests((cur) => ({ ...cur, [provider]: res }));
      void qc.invalidateQueries({ queryKey: key });
    },
  });
  if (!st.data) return st.error ? <ErrorAlert error={st.error} /> : <Spinner />;
  const d = st.data;
  const status = (p: DiscoveryProvider) => d.providers.find((x) => x.id === p);
  const enabled: Record<DiscoveryProvider, boolean> = { lastfm: d.lastfm, listenbrainz: d.listenbrainz, musicbrainz: d.musicbrainz, spotify: d.spotify };
  const line = (p: DiscoveryProvider) => {
    const s = status(p);
    if (!s) return null;
    const res = tests[p];
    return (
      <div className="mt-1 flex flex-wrap items-center gap-2 text-[12px] text-faint" data-testid={`discovery-status-${p}`}>
        {s.configured ? null : <Badge tone="warning">{t("metadata.notConfigured")}</Badge>}
        <span>{t("discoverySettings.artists", { count: s.artists })}</span>
        {s.lastAt ? <span>· {t("discoverySettings.lastAt", { when: formatDateTime(s.lastAt, i18n.language) })}</span> : null}
        {s.failed ? <span className="text-warning">· {t("discoverySettings.failed", { count: s.failed })}</span> : null}
        {s.configured ? (
          <Button variant="ghost" onClick={() => test.mutate(p)} busy={test.isPending && test.variables === p} data-testid={`discovery-test-${p}`}>
            {t("discoverySettings.test")}
          </Button>
        ) : null}
        {res ? (
          <span role="status" className={res.ok ? "text-success" : "text-danger"}>
            {res.ok ? t("discoverySettings.testOk") : t(`discoverySettings.errors.${res.code ?? "metadata_unavailable"}`)}
          </span>
        ) : null}
      </div>
    );
  };
  return (
    <Panel title={t("discoverySettings.title")} description={t("discoverySettings.description")}>
      <div className="flex flex-col gap-4" data-testid="music-discovery-settings">
        <div>
          <Checkbox label={t("discovery.providers.lastfm")} description={t("discoverySettings.lastfmHint")} checked={enabled.lastfm}
            onChange={(e) => save.mutate({ lastfm: e.target.checked })} />
          <div className="mt-2 grid gap-3 sm:grid-cols-2">
            <TextInput label={t("discoverySettings.lastfmKey")} type="password" autoComplete="off" value={lastfm} onChange={(e) => setLastfm(e.target.value)}
              placeholder={d.lastfmApiKey.configured ? t("metadata.keySet") : t("metadata.keyMissing")} data-testid="lastfm-key"
              hint={<a href="https://www.last.fm/api/account/create" target="_blank" rel="noreferrer noopener" className="underline">{t("discoverySettings.lastfmWhere")}</a>} />
          </div>
          {line("lastfm")}
        </div>
        <div>
          <Checkbox label={t("discovery.providers.listenbrainz")} description={t("discoverySettings.listenbrainzHint")} checked={enabled.listenbrainz}
            onChange={(e) => save.mutate({ listenbrainz: e.target.checked })} />
          {line("listenbrainz")}
        </div>
        <div>
          <Checkbox label={t("discovery.providers.musicbrainz")} description={t("discoverySettings.musicbrainzHint")} checked={enabled.musicbrainz}
            onChange={(e) => save.mutate({ musicbrainz: e.target.checked })} />
          {line("musicbrainz")}
        </div>
        <div>
          <Checkbox label={t("discovery.providers.spotify")} description={t("discoverySettings.spotifyHint")} checked={enabled.spotify}
            onChange={(e) => save.mutate({ spotify: e.target.checked })} />
          <div className="mt-2 grid gap-3 sm:grid-cols-2">
            <TextInput label={t("discoverySettings.spotifyId")} type="password" autoComplete="off" value={spId} onChange={(e) => setSpId(e.target.value)}
              placeholder={d.spotifyClientId.configured ? t("metadata.keySet") : t("metadata.keyMissing")} data-testid="spotify-id"
              hint={<a href="https://developer.spotify.com/dashboard" target="_blank" rel="noreferrer noopener" className="underline">{t("discoverySettings.spotifyWhere")}</a>} />
            <TextInput label={t("discoverySettings.spotifySecret")} type="password" autoComplete="off" value={spSecret}
              onChange={(e) => setSpSecret(e.target.value)} data-testid="spotify-secret"
              placeholder={d.spotifyClientSecret.configured ? t("metadata.keySet") : t("metadata.keyMissing")} />
          </div>
          {status("spotify")?.relatedArtists === "restricted" ? (
            <p className="mt-1 text-[12px] text-warning" data-testid="spotify-restricted">{t("discovery.spotifyRestricted")}</p>
          ) : null}
          {line("spotify")}
        </div>
        <SelectInput label={t("discoverySettings.refreshDays")} value={d.refreshDays} className="max-w-xs"
          onChange={(e) => save.mutate({ refreshDays: Number(e.target.value) })}>
          {REFRESH_DAYS.map((n) => <option key={n} value={n}>{t("discoverySettings.everyDays", { count: n })}</option>)}
        </SelectInput>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" busy={save.isPending} data-testid="save-discovery" disabled={!lastfm && !spId && !spSecret}
          onClick={() => save.mutate({ ...(lastfm ? { lastfmApiKey: lastfm } : {}), ...(spId || spSecret ? { spotifyClientId: spId, spotifyClientSecret: spSecret } : {}) })}>
          {t("discoverySettings.saveKeys")}
        </Button>
        {d.lastfmApiKey.configured ? (
          <ConfirmButton label={t("discoverySettings.removeLastfm")} confirmLabel={t("metadata.removeKeyConfirm")} onConfirm={() => save.mutate({ lastfmApiKey: "" })} />
        ) : null}
        {d.spotifyClientId.configured ? (
          <ConfirmButton label={t("discoverySettings.removeSpotify")} confirmLabel={t("metadata.removeKeyConfirm")} onConfirm={() => save.mutate({ spotifyClientId: "" })} />
        ) : null}
        {save.isSuccess ? <span role="status" className="text-[13px] text-success">{t("common.saved")}</span> : null}
        <ErrorAlert error={save.error ?? test.error} />
      </div>
      <p className="mt-3 text-[12px] text-faint">{t("discoverySettings.attribution")}</p>
    </Panel>
  );
}
