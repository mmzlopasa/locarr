import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type MediaManagementSettings } from "@/api/client";
import { formatBytes, formatDateTime } from "@/components/format";
import { Alert, Button, Checkbox, ConfirmButton, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { SeriesNamingPanel } from "./SeriesNamingPanel";
import { MusicNamingPanel } from "@/features/music/MusicNamingPanel";

/** Waits until v has not changed for ms. */
function useDebounced<T>(v: T, ms: number): T {
  const [d, setD] = useState(v);
  useEffect(() => {
    const id = setTimeout(() => setD(v), ms);
    return () => clearTimeout(id);
  }, [v, ms]);
  return d;
}

export function MediaManagementPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const current = useQuery({ queryKey: queryKeys.mediaManagement, queryFn: async () => unwrap(await api.GET("/settings/mediamanagement")) });
  const [s, setS] = useState<MediaManagementSettings | null>(null);
  useEffect(() => {
    if (current.data && !s) setS(current.data);
  }, [current.data, s]);
  const save = useMutation({
    mutationFn: async (v: MediaManagementSettings) => unwrap(await api.PUT("/settings/mediamanagement", { body: v })),
    onSuccess: (v) => {
      setS(v);
      qc.setQueryData(queryKeys.mediaManagement, v);
      void qc.invalidateQueries({ queryKey: queryKeys.recycleBin });
    },
  });
  const naming = useDebounced(s ? { movieFolderFormat: s.movieFolderFormat, movieFileFormat: s.movieFileFormat, colonReplacement: s.colonReplacement } : null, 250);
  const preview = useQuery({
    queryKey: ["naming-preview", naming],
    queryFn: async () => unwrap(await api.POST("/settings/mediamanagement/preview", { body: naming! })),
    enabled: !!naming,
    placeholderData: (p) => p,
  });
  if (!s) return current.error ? <ErrorAlert error={current.error} /> : <Spinner />;
  const set = <K extends keyof MediaManagementSettings>(k: K, v: MediaManagementSettings[K]) => {
    setS({ ...s, [k]: v });
    save.reset();
  };
  return (
    <div className="space-y-4">
      <Panel title={t("media.naming.title")} description={t("media.naming.description")}>
        <div className="space-y-3">
          <Checkbox label={t("media.naming.rename")} description={t("media.naming.renameHint")} checked={s.renameMovies}
            onChange={(e) => set("renameMovies", e.target.checked)} />
          <TextInput label={t("media.naming.folder")} value={s.movieFolderFormat} onChange={(e) => set("movieFolderFormat", e.target.value)}
            className="font-mono" data-testid="folder-format" />
          <TextInput label={t("media.naming.file")} value={s.movieFileFormat} onChange={(e) => set("movieFileFormat", e.target.value)}
            className="font-mono" data-testid="file-format" />
          <SelectInput label={t("media.naming.colon")} value={s.colonReplacement} onChange={(e) => set("colonReplacement", e.target.value as MediaManagementSettings["colonReplacement"])}>
            {(["dash", "space_dash", "delete"] as const).map((c) => <option key={c} value={c}>{t(`media.naming.colons.${c}`)}</option>)}
          </SelectInput>
          <div className="rounded-ui border border-line bg-surface-2 p-3" aria-live="polite" data-testid="naming-preview">
            <div className="text-[12px] text-faint">{t("media.naming.preview")}</div>
            {preview.data?.folderError ? <p className="text-[12px] text-danger">{preview.data.folderError}</p> : null}
            {preview.data?.fileError ? <p className="text-[12px] text-danger">{preview.data.fileError}</p> : null}
            {preview.data ? (
              <p className="break-all font-mono text-[12px] text-fg">
                {preview.data.folder || "?"}/<span className="text-accent">{preview.data.file || "?"}</span>
              </p>
            ) : null}
          </div>
          <details className="text-[12px] text-muted">
            <summary className="cursor-pointer">{t("media.naming.tokens")}</summary>
            <p className="mt-1">{t("media.naming.tokensHint")}</p>
            <ul className="mt-2 grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
              {(preview.data?.tokens ?? []).map((tk) => (
                <li key={tk.name}><code className="text-fg">{`{${tk.name}}`}</code> <span className="text-faint">{tk.example}</span></li>
              ))}
            </ul>
          </details>
        </div>
      </Panel>
      <SeriesNamingPanel colon={s.colonReplacement} />

      <Panel title={t("media.importing.title")} description={t("media.importing.description")}>
        <div className="grid gap-3 sm:grid-cols-2">
          <SelectInput label={t("media.transferMode")} value={s.transferMode} onChange={(e) => set("transferMode", e.target.value as MediaManagementSettings["transferMode"])}>
            {(["hardlink", "copy", "move"] as const).map((m) => <option key={m} value={m}>{t(`transfer.${m}`)}</option>)}
          </SelectInput>
          <TextInput label={t("media.importing.extensions")} hint={t("media.importing.extensionsHint")} value={s.extraExtensions.join(", ")}
            onChange={(e) => set("extraExtensions", e.target.value.split(",").map((x) => x.trim()).filter(Boolean))} />
          <TextInput label={t("media.importing.delay")} hint={t("media.importing.delayHint")} type="number" value={s.availabilityDelayDays}
            onChange={(e) => set("availabilityDelayDays", Number(e.target.value))} />
          <Checkbox label={t("media.importing.searchOnAdd")} checked={s.searchOnAddDefault} onChange={(e) => set("searchOnAddDefault", e.target.checked)} />
        </div>
        <p className="mt-2 text-[12px] text-faint">{t(`media.transferHints.${s.transferMode}`)}</p>
        {s.transferMode === "move" ? <div className="mt-2"><Alert tone="warning">{t("media.moveWarning")}</Alert></div> : null}
        <div className="mt-3 grid gap-3 sm:grid-cols-3">
          <Checkbox label={t("media.permissions.set")} description={t("media.permissions.hint")} checked={s.setPermissions}
            onChange={(e) => set("setPermissions", e.target.checked)} />
          <TextInput label={t("media.permissions.file")} value={s.fileMode} disabled={!s.setPermissions} onChange={(e) => set("fileMode", e.target.value)} />
          <TextInput label={t("media.permissions.folder")} value={s.folderMode} disabled={!s.setPermissions} onChange={(e) => set("folderMode", e.target.value)} />
        </div>
      </Panel>

      <Panel title={t("recycle.title")} description={t("recycle.description")}>
        <div className="grid gap-3 sm:grid-cols-2">
          <TextInput label={t("recycle.folder")} hint={t("recycle.folderHint")} value={s.recycleBinPath} placeholder="/data/recycle-bin"
            onChange={(e) => set("recycleBinPath", e.target.value)} />
          <TextInput label={t("recycle.retention")} hint={t("recycle.retentionHint")} type="number" min={0} value={s.recycleBinRetentionDays}
            onChange={(e) => set("recycleBinRetentionDays", Number(e.target.value))} />
        </div>
      </Panel>

      <div className="flex items-center gap-2">
        <Button variant="primary" onClick={() => save.mutate(s)} busy={save.isPending} data-testid="save-media">{t("common.save")}</Button>
        {save.isSuccess ? <span role="status" className="text-[13px] text-success">{t("common.saved")}</span> : null}
        <ErrorAlert error={save.error} />
      </div>
      <MusicNamingPanel />
      <RecycleBinPanel />
    </div>
  );
}

function RecycleBinPanel() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const me = useCurrentUser();
  const bin = useQuery({ queryKey: queryKeys.recycleBin, queryFn: async () => unwrap(await api.GET("/recyclebin")) });
  const done = () => void qc.invalidateQueries({ queryKey: queryKeys.recycleBin });
  const restore = useMutation({ mutationFn: async (id: number) => unwrap(await api.POST("/recyclebin/{id}/restore", { params: { path: { id } } })), onSuccess: done });
  const del = useMutation({ mutationFn: async (id: number) => unwrap(await api.DELETE("/recyclebin/{id}", { params: { path: { id } } })), onSuccess: done });
  const empty = useMutation({ mutationFn: async () => unwrap(await api.DELETE("/recyclebin")), onSuccess: done });
  const entries = bin.data?.entries ?? [];
  return (
    <Panel title={t("recycle.contents")} description={bin.data ? t("recycle.where", { folder: bin.data.folder || "–" }) : undefined}
      actions={me.role === "admin" && entries.length ? (
        <ConfirmButton label={t("recycle.empty")} confirmLabel={t("recycle.emptyConfirm", { count: entries.length })} onConfirm={() => empty.mutate()} busy={empty.isPending} />
      ) : null}>
      <ErrorAlert error={bin.error ?? restore.error ?? del.error ?? empty.error} />
      {bin.data && entries.length === 0 ? <EmptyState>{t("recycle.nothing")}</EmptyState> : null}
      <ul className="divide-y divide-line">
        {entries.map((e) => (
          <li key={e.id} className="flex flex-wrap items-center gap-2 py-2">
            <div className="min-w-0 flex-1">
              <div className="break-all font-mono text-[12px] text-fg">{e.originalPath}</div>
              <div className="text-[12px] text-faint">
                {formatDateTime(e.recycledAt, i18n.language)} · {formatBytes(e.sizeBytes, i18n.language)}{e.reason ? ` · ${e.reason}` : ""}
              </div>
            </div>
            <Button onClick={() => restore.mutate(e.id)} busy={restore.isPending && restore.variables === e.id}>{t("recycle.restore")}</Button>
            {me.role === "admin" ? (
              <ConfirmButton label={t("recycle.deleteNow")} confirmLabel={t("recycle.deleteConfirm")} onConfirm={() => del.mutate(e.id)} />
            ) : null}
          </li>
        ))}
      </ul>
    </Panel>
  );
}
