import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import {
  api,
  queryKeys,
  unwrap,
  type PathMapping,
  type StreameroLibrary,
  type StreameroLibraryMapping,
  type StreameroSettingsInput,
  type StreameroTestResult,
} from "@/api/client";
import { formatRelative } from "@/components/format";
import { Alert, Badge, Button, Checkbox, EmptyState, ErrorAlert, PageHeader, Panel, Spinner, TextInput } from "@/components/ui";
import { SecretField } from "@/features/notifications/channels";

type Form = {
  enabled: boolean;
  url: string;
  publicUrl: string;
  apiKey?: string;
  pathMappings: PathMapping[];
  libraryMap: StreameroLibraryMapping[];
  signIn: { enabled: boolean; autoCreate: boolean };
};

/** The request body: the key only when typed (or removed). */
export function toStreameroInput(f: Form): StreameroSettingsInput {
  return {
    enabled: f.enabled,
    url: f.url.trim(),
    publicUrl: f.publicUrl.trim(),
    ...(f.apiKey !== undefined ? { apiKey: f.apiKey } : {}),
    pathMappings: f.pathMappings.filter((m) => m.locarrPath.trim() && m.streameroPath.trim())
      .map((m) => ({ locarrPath: m.locarrPath.trim(), streameroPath: m.streameroPath.trim() })),
    libraryMap: f.libraryMap.filter((m) => m.libraryIds.length > 0),
    signIn: f.signIn,
  };
}

export function MediaServerPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: queryKeys.streamero, queryFn: async () => unwrap(await api.GET("/mediaserver/streamero")) });
  const roots = useQuery({ queryKey: queryKeys.rootFolders, queryFn: async () => unwrap(await api.GET("/rootfolders")) });
  const configured = !!q.data?.enabled && !!q.data.url && q.data.apiKey.configured;
  const libs = useQuery({
    queryKey: queryKeys.streameroLibraries,
    queryFn: async () => unwrap(await api.GET("/mediaserver/streamero/libraries")),
    enabled: configured,
  });
  const [f, setF] = useState<Form | null>(null);
  const [test, setTest] = useState<StreameroTestResult | null>(null);
  useEffect(() => {
    if (q.data) {
      setF({ enabled: q.data.enabled, url: q.data.url, publicUrl: q.data.publicUrl, pathMappings: q.data.pathMappings,
        libraryMap: q.data.libraryMap, signIn: q.data.signIn });
    }
  }, [q.data]);
  const save = useMutation({
    mutationFn: async (form: Form) => unwrap(await api.PUT("/mediaserver/streamero", { body: toStreameroInput(form) })),
    onSuccess: async (res) => {
      qc.setQueryData(queryKeys.streamero, res);
      setF((cur) => (cur ? { ...cur, apiKey: undefined } : cur));
      await qc.invalidateQueries({ queryKey: queryKeys.streameroLibraries });
      await qc.invalidateQueries({ queryKey: queryKeys.authProviders });
    },
  });
  const runTest = useMutation({
    mutationFn: async (form: Form) =>
      unwrap(await api.POST("/mediaserver/streamero/test", { body: { url: form.url.trim(), ...(form.apiKey ? { apiKey: form.apiKey } : {}) } })),
    onSuccess: setTest,
  });
  const sync = useMutation({
    mutationFn: async () => unwrap(await api.POST("/mediaserver/streamero/sync")),
    onSuccess: () => setTimeout(() => void qc.invalidateQueries({ queryKey: queryKeys.streamero }), 1500),
  });
  if (!f) return q.error ? <ErrorAlert error={q.error} /> : <Spinner />;
  const set = (patch: Partial<Form>) => setF({ ...f, ...patch });
  const libraries: StreameroLibrary[] = libs.data ?? test?.libraries ?? [];
  const st = q.data?.status;

  const toggleLib = (rootId: number, libId: string, on: boolean) => {
    const cur = f.libraryMap.find((m) => m.rootFolderId === rootId)?.libraryIds ?? [];
    const ids = on ? [...cur.filter((x) => x !== libId), libId] : cur.filter((x) => x !== libId);
    set({ libraryMap: [...f.libraryMap.filter((m) => m.rootFolderId !== rootId), { rootFolderId: rootId, libraryIds: ids }] });
  };
  const applySuggestions = () => {
    const map = new Map<number, string[]>();
    for (const l of libraries) for (const r of l.suggestedRootFolderIds) map.set(r, [...(map.get(r) ?? []), l.id]);
    set({ libraryMap: [...map.entries()].map(([rootFolderId, libraryIds]) => ({ rootFolderId, libraryIds })) });
  };

  return (
    <div className="flex flex-col gap-4" data-testid="mediaserver-settings">
      <PageHeader title={t("mediaServer.title")} description={t("mediaServer.subtitle")} />
      <Panel title={t("mediaServer.connection")} description={t("mediaServer.connectionHint")}>
        <div className="flex flex-col gap-3">
          <Checkbox label={t("mediaServer.enabled")} description={t("mediaServer.enabledHint")} checked={f.enabled}
            onChange={(e) => set({ enabled: e.target.checked })} data-testid="streamero-enabled" />
          <div className="grid gap-3 sm:grid-cols-2">
            <TextInput label={t("mediaServer.url")} hint={t("mediaServer.urlHint")} type="url" placeholder="http://streamero:8787"
              value={f.url} onChange={(e) => set({ url: e.target.value })} data-testid="streamero-url" />
            <TextInput label={t("mediaServer.publicUrl")} hint={t("mediaServer.publicUrlHint")} type="url" placeholder="https://media.example.com"
              value={f.publicUrl} onChange={(e) => set({ publicUrl: e.target.value })} data-testid="streamero-public-url" />
            <SecretField label={t("mediaServer.apiKey")} hint={t("mediaServer.apiKeyHint")} name="streamero-key"
              stored={!!q.data?.apiKey.configured} value={f.apiKey} onChange={(v) => set({ apiKey: v })} />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => runTest.mutate(f)} busy={runTest.isPending} disabled={!f.url.trim()} data-testid="streamero-test">
              {t("mediaServer.test")}
            </Button>
          </div>
          <ErrorAlert error={runTest.error} />
          {test ? <TestResult result={test} /> : null}
        </div>
      </Panel>

      <Panel title={t("mediaServer.paths")} description={t("mediaServer.pathsHint")}
        actions={
          <Button variant="ghost" onClick={() => set({ pathMappings: [...f.pathMappings, { locarrPath: "", streameroPath: "" }] })}
            data-testid="add-path-mapping">
            <Plus className="size-4" />
            {t("mediaServer.addMapping")}
          </Button>
        }>
        {f.pathMappings.length === 0 ? <EmptyState>{t("mediaServer.noMappings")}</EmptyState> : null}
        <div className="flex flex-col gap-2">
          {f.pathMappings.map((m, i) => (
            <div key={i} className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_auto]" data-testid={`path-mapping-${i}`}>
              <TextInput label={t("mediaServer.locarrPath")} placeholder="/data/movies" value={m.locarrPath}
                onChange={(e) => set({ pathMappings: f.pathMappings.map((x, j) => (j === i ? { ...x, locarrPath: e.target.value } : x)) })} />
              <TextInput label={t("mediaServer.streameroPath")} placeholder="/media/movies" value={m.streameroPath}
                onChange={(e) => set({ pathMappings: f.pathMappings.map((x, j) => (j === i ? { ...x, streameroPath: e.target.value } : x)) })} />
              <Button variant="ghost" aria-label={t("common.delete")}
                onClick={() => set({ pathMappings: f.pathMappings.filter((_, j) => j !== i) })}>
                <Trash2 className="size-4" />
              </Button>
            </div>
          ))}
        </div>
      </Panel>

      <Panel title={t("mediaServer.libraries")} description={t("mediaServer.librariesHint")}
        actions={libraries.some((l) => l.suggestedRootFolderIds.length) ? (
          <Button variant="ghost" onClick={applySuggestions} data-testid="use-suggestions">{t("mediaServer.useSuggestions")}</Button>
        ) : null}>
        {libs.isFetching ? <Spinner /> : null}
        <ErrorAlert error={libs.error} />
        {libraries.length === 0 && !libs.isFetching ? <EmptyState>{t("mediaServer.noLibraries")}</EmptyState> : null}
        {libraries.length > 0 ? (
          <div className="flex flex-col divide-y divide-line">
            {(roots.data ?? []).map((r) => {
              const chosen = f.libraryMap.find((m) => m.rootFolderId === r.id)?.libraryIds ?? [];
              return (
                <div key={r.id} className="flex flex-col gap-2 py-2.5 sm:flex-row sm:items-start sm:justify-between" data-testid={`root-${r.id}`}>
                  <div className="min-w-0">
                    <p className="truncate font-mono text-[12.5px] text-fg">{r.path}</p>
                    <p className="text-[12px] text-faint">{t(`mediaKinds.${r.kind}`, { defaultValue: r.kind })}</p>
                  </div>
                  <div className="flex flex-wrap gap-x-4 gap-y-1.5">
                    {libraries.map((l) => (
                      <Checkbox key={l.id} label={l.name}
                        description={l.suggestedRootFolderIds.includes(r.id) ? t("mediaServer.suggested") : undefined}
                        checked={chosen.includes(l.id)} data-testid={`map-${r.id}-${l.id}`}
                        onChange={(e) => toggleLib(r.id, l.id, e.target.checked)} />
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        ) : null}
        <p className="mt-2 text-[12px] text-faint">{t("mediaServer.unmappedHint")}</p>
      </Panel>

      <Panel title={t("mediaServer.signIn")} description={t("mediaServer.signInHint")}>
        <div className="flex flex-col gap-3">
          <Checkbox label={t("mediaServer.signInEnabled")} checked={f.signIn.enabled} data-testid="signin-enabled"
            onChange={(e) => set({ signIn: { ...f.signIn, enabled: e.target.checked } })} />
          <Checkbox label={t("mediaServer.autoCreate")} description={t("mediaServer.autoCreateHint")} checked={f.signIn.autoCreate}
            disabled={!f.signIn.enabled} data-testid="signin-autocreate"
            onChange={(e) => set({ signIn: { ...f.signIn, autoCreate: e.target.checked } })} />
          <Alert tone="info">{t("mediaServer.neverAdmin")}</Alert>
        </div>
      </Panel>

      <ErrorAlert error={save.error} />
      {save.isSuccess ? <Alert tone="success">{t("common.saved")}</Alert> : null}
      <div>
        <Button variant="primary" busy={save.isPending} onClick={() => save.mutate(f)} data-testid="save-streamero">{t("common.save")}</Button>
      </div>

      {st ? (
        <Panel title={t("mediaServer.status")} actions={
          <Button variant="ghost" onClick={() => sync.mutate()} busy={sync.isPending} disabled={!configured} data-testid="streamero-sync">
            <RefreshCw className="size-4" />
            {t("mediaServer.refresh")}
          </Button>
        }>
          <dl className="grid gap-x-6 gap-y-2 text-[13px] sm:grid-cols-2" data-testid="streamero-status">
            <div><dt className="text-[12px] text-faint">{t("mediaServer.indexed")}</dt>
              <dd className="text-fg">{t("mediaServer.indexedCount", { count: st.indexedItems })}
                {st.indexedAt ? ` · ${formatRelative(st.indexedAt, i18n.language)}` : ""}</dd></div>
            <div><dt className="text-[12px] text-faint">{t("mediaServer.lastScan")}</dt>
              <dd className="text-fg">{st.lastScanAt ? formatRelative(st.lastScanAt, i18n.language) : t("common.never")}</dd></div>
            <div><dt className="text-[12px] text-faint">{t("mediaServer.pending")}</dt>
              <dd className="text-fg tabular-nums">{st.pendingChanges ?? 0}</dd></div>
            {st.indexError ? <div className="sm:col-span-2 text-danger">{t("mediaServer.indexError", { message: st.indexError })}</div> : null}
            {st.lastScanError ? <div className="sm:col-span-2 text-danger">{t("mediaServer.scanError", { message: st.lastScanError })}</div> : null}
          </dl>
          <ErrorAlert error={sync.error} />
          {sync.isSuccess ? <p className="mt-2 text-[12px] text-muted">{t("mediaServer.syncQueued")}</p> : null}
        </Panel>
      ) : null}
    </div>
  );
}

function TestResult({ result }: { result: StreameroTestResult }) {
  const { t } = useTranslation();
  if (!result.ok) {
    return (
      <Alert>
        <span data-testid="streamero-test-result">
          {t(`mediaServer.errors.${result.errorCode ?? "unknown"}`, { defaultValue: result.message ?? t("mediaServer.errors.unknown") })}
        </span>
      </Alert>
    );
  }
  return (
    <Alert tone="success">
      <div data-testid="streamero-test-result" className="flex flex-col gap-1">
        <span>{t("mediaServer.testOk", { name: result.serverName ?? "Streamero", version: result.version ?? "?" })}</span>
        {result.libraries.length ? (
          <span className="flex flex-wrap gap-1">
            {result.libraries.map((l) => <Badge key={l.id}>{l.name}</Badge>)}
          </span>
        ) : null}
      </div>
    </Alert>
  );
}
