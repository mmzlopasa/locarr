import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
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
    </div>
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
