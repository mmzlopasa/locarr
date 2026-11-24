import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type Indexer, type IndexerSourceItem, type IndexerSourceKind, type IndexerSource } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { Alert, Badge, Button, Checkbox, ConfirmButton, EmptyState, ErrorAlert, Field, PageHeader, Panel, SelectInput, Spinner,
  TextInput } from "@/components/ui";
import { IndexerForm, TestResult } from "./IndexerForm";
import { AddIndexer, DefinitionsPanel, NativeIndexerForm } from "./BuiltinIndexers";
import { BrowserPanel } from "./BrowserPanel";

export function healthTone(s: Indexer["health"]["status"]) {
  return s === "healthy" ? ("success" as const) : s === "failing" ? ("danger" as const) : s === "disabled" ? ("neutral" as const) : ("info" as const);
}

export function IndexersPage() {
  const { t } = useTranslation();
  const [mode, setMode] = useState<"none" | "add">("none");
  const list = useQuery({ queryKey: queryKeys.indexers, queryFn: async () => unwrap(await api.GET("/indexers")), refetchInterval: 30_000 });
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("indexers.title")}
        description={t("indexers.subtitle")}
        actions={
          mode === "none" ? (
            <Button variant="primary" onClick={() => setMode("add")}>
              {t("indexers.add")}
            </Button>
          ) : null
        }
      />
      {mode === "add" ? <AddIndexer onDone={() => setMode("none")} /> : null}
      {list.isPending ? <Spinner /> : null}
      <ErrorAlert error={list.error} />
      {list.data && !list.data.length && mode === "none" ? <EmptyState>{t("indexers.empty")}</EmptyState> : null}
      <div className="flex flex-col gap-2" data-testid="indexer-list">
        {list.data?.map((i) => <IndexerRow key={i.id} indexer={i} />)}
      </div>
      <DefinitionsPanel />
      <BrowserPanel />
      <IndexerSettingsPanel />
      <AdvancedSources />
    </div>
  );
}

function IndexerRow({ indexer }: { indexer: Indexer }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: queryKeys.indexers });
  const path = { params: { path: { id: indexer.id } } };
  const test = useMutation({ mutationFn: async () => unwrap(await api.POST("/indexers/{id}/test", path)), onSuccess: () => refresh() });
  const reset = useMutation({ mutationFn: async () => unwrap(await api.POST("/indexers/{id}/health/reset", path)), onSuccess: () => refresh() });
  const remove = useMutation({ mutationFn: async () => unwrap(await api.DELETE("/indexers/{id}", path)), onSuccess: () => refresh() });
  const h = indexer.health;
  if (editing && indexer.kind === "native") return <NativeIndexerForm indexer={indexer} onDone={() => setEditing(false)} />;
  if (editing) return <IndexerForm indexer={indexer} onDone={() => setEditing(false)} />;
  const def = indexer.definition;
  return (
    <div className="rounded-ui border border-line bg-surface" data-testid={`indexer-${indexer.name}`}>
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5">
        <div className="min-w-0">
          <p className="flex flex-wrap items-center gap-2 text-[13px] text-fg">
            <span className="font-medium">{indexer.name}</span>
            <Badge tone={healthTone(h.status)}>{t(`indexers.health.${h.status}`)}</Badge>
            <Badge>{t(`indexers.kinds.${indexer.kind}`)}</Badge>
            {def ? <span className="font-mono text-[11px] text-faint">{t("indexers.builtin.version", { n: def.version })}</span> : null}
            {def && !def.builtin && !def.missing ? <Badge tone="info">{t("indexers.builtin.custom")}</Badge> : null}
            {def?.overrides ? <Badge tone="info">{t("indexers.builtin.overrides")}</Badge> : null}
            {def?.missing ? <Badge tone="danger">{t("indexers.builtin.missing")}</Badge> : null}
            {indexer.source ? <Badge tone="info">{t("indexers.fromSource", { name: indexer.source.name })}</Badge> : null}
            {indexer.tags.map((tag) => (
              <Badge key={tag}>{tag}</Badge>
            ))}
          </p>
          <p className="mt-0.5 break-all font-mono text-[11px] text-faint">
            {indexer.kind === "native" ? indexer.baseUrl : indexer.baseUrl + indexer.apiPath}
          </p>
          <p className="mt-0.5 text-[12px] text-muted">
            {t("indexers.priorityShort", { n: indexer.priority })} ·{" "}
            {[indexer.enableRss && t("indexers.uses.rss"), indexer.enableAutomaticSearch && t("indexers.uses.automatic"),
              indexer.enableInteractiveSearch && t("indexers.uses.interactive")].filter(Boolean).join(", ") || t("indexers.uses.none")}
            {h.lastSuccessAt ? ` · ${t("indexers.lastSuccess", { when: formatRelative(h.lastSuccessAt, i18n.language) })}` : ""}
          </p>
          {indexer.kind === "native" && (h.parseFailures || h.selfTestAt) ? (
            <p className="mt-0.5 text-[12px] text-muted" data-testid="indexer-quality">
              {[h.parseFailures ? t("indexers.builtin.parseFailures", { n: h.parseFailures }) : "",
                h.selfTestAt ? t(h.selfTestOk ? "indexers.builtin.selfTestOk" : "indexers.builtin.selfTestFailed",
                  { when: formatRelative(h.selfTestAt, i18n.language) }) : ""].filter(Boolean).join(" · ")}
            </p>
          ) : null}
          {h.status === "failing" || (h.status === "disabled" && h.lastError) ? (
            <p className="mt-1 text-[12px] text-danger" data-testid="indexer-error">
              {t(`indexers.errorKinds.${h.lastErrorCode ?? "unknown"}`, { defaultValue: t("indexers.errorKinds.unknown") })}
              {h.lastError ? ` (${h.lastError})` : ""}
              {h.retryAt ? ` · ${t("indexers.retryAt", { when: formatDateTime(h.retryAt, i18n.language), failures: h.failures })}` : ""}
            </p>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-1">
          <Button variant="ghost" busy={test.isPending} onClick={() => test.mutate()}>
            {t("indexers.test.button")}
          </Button>
          {h.failures > 0 ? (
            <Button variant="ghost" busy={reset.isPending} onClick={() => reset.mutate()}>
              {t("indexers.resetHealth")}
            </Button>
          ) : null}
          <Button variant="ghost" onClick={() => setEditing(true)}>
            {t("indexers.edit")}
          </Button>
          <ConfirmButton label={t("indexers.delete")} confirmLabel={t("indexers.deleteConfirm", { name: indexer.name })}
            busy={remove.isPending} onConfirm={() => remove.mutate()} />
        </div>
      </div>
      {test.data || test.error || remove.error ? (
        <div className="flex flex-col gap-2 px-4 pb-3">
          {test.data ? <TestResult result={test.data} /> : null}
          <ErrorAlert error={test.error ?? remove.error} />
        </div>
      ) : null}
    </div>
  );
}

function ImportWizard({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [kind, setKind] = useState<IndexerSourceKind>("prowlarr");
  const [url, setUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const preview = useMutation({
    mutationFn: async () => unwrap(await api.POST("/indexers/sources/preview", { body: { kind, url: url.trim(), apiKey: apiKey.trim() } })),
    onSuccess: (p) => setSelected(new Set(p.items.filter((i) => i.action === "add").map((i) => i.ref))),
  });
  const create = useMutation({
    mutationFn: async (items: IndexerSourceItem[]) =>
      unwrap(await api.POST("/indexers/sources", { body: { kind, url: url.trim(), apiKey: apiKey.trim(),
        exclude: items.filter((i) => !selected.has(i.ref)).map((i) => i.ref) } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.indexers });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    preview.mutate();
  };
  const items = preview.data?.items ?? [];
  return (
    <Panel title={t("indexers.import.title")} description={t("indexers.import.subtitle")}>
      <div className="flex flex-col gap-4" data-testid="import-wizard">
        <form onSubmit={submit} className="grid gap-3 sm:grid-cols-[10rem_1fr_1fr_auto] sm:items-end">
          <SelectInput label={t("indexers.import.kind")} value={kind} onChange={(e) => setKind(e.target.value as IndexerSourceKind)}>
            <option value="prowlarr">Prowlarr</option>
            <option value="jackett">Jackett</option>
          </SelectInput>
          <TextInput label={t("indexers.import.url")} value={url} placeholder={kind === "prowlarr" ? "http://prowlarr:9696" : "http://jackett:9117"}
            onChange={(e) => setUrl(e.target.value)} />
          <TextInput label={t("indexers.import.apiKey")} type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
          <Button type="submit" busy={preview.isPending} disabled={!url.trim() || !apiKey.trim()}>
            {t("indexers.import.preview")}
          </Button>
        </form>
        <ErrorAlert error={preview.error ?? create.error} />
        {preview.data ? (
          items.length ? (
            <ul className="flex flex-col gap-1.5" data-testid="import-preview">
              {items.map((it) => (
                <li key={it.ref} className="flex flex-wrap items-center gap-2 text-[13px]">
                  <Checkbox label={it.name} description={it.feedUrl} checked={selected.has(it.ref)} disabled={it.action !== "add"}
                    onChange={(e) => {
                      const next = new Set(selected);
                      if (e.target.checked) next.add(it.ref);
                      else next.delete(it.ref);
                      setSelected(next);
                    }} />
                  <Badge>{t(`indexers.kinds.${it.kind}`)}</Badge>
                  <Badge tone={it.action === "add" ? "success" : "neutral"}>{t(`indexers.import.actions.${it.action}`)}</Badge>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState>{t("indexers.import.none")}</EmptyState>
          )
        ) : null}
        {create.data ? (
          <Alert tone="success">{t("indexers.import.done", { added: create.data.added })}</Alert>
        ) : null}
        <div className="flex gap-2">
          {preview.data && !create.data ? (
            <Button variant="primary" busy={create.isPending} disabled={!selected.size} onClick={() => create.mutate(items)}>
              {t("indexers.import.importN", { n: selected.size })}
            </Button>
          ) : null}
          <Button variant="ghost" onClick={onDone}>
            {create.data ? t("common.close") : t("common.cancel")}
          </Button>
        </div>
      </div>
    </Panel>
  );
}

/** Prowlarr and Jackett import, tucked away: Locarr no longer needs them. */
function AdvancedSources() {
  const { t } = useTranslation();
  const sources = useQuery({ queryKey: queryKeys.indexerSources, queryFn: async () => unwrap(await api.GET("/indexers/sources")) });
  const [open, setOpen] = useState<boolean | undefined>(undefined);
  const [importing, setImporting] = useState(false);
  const shown = open ?? !!sources.data?.length;
  return (
    <div className="flex flex-col gap-3" data-testid="advanced-sources">
      <div>
        <Button variant="ghost" aria-expanded={shown} onClick={() => setOpen(!shown)}>
          {shown ? t("indexers.advanced.hide") : t("indexers.advanced.show")}
        </Button>
      </div>
      {shown ? (
        <>
          <p className="text-[12px] text-muted">{t("indexers.advanced.text")}</p>
          {importing ? <ImportWizard onDone={() => setImporting(false)} /> : (
            <div>
              <Button onClick={() => setImporting(true)}>{t("indexers.import.button")}</Button>
            </div>
          )}
          <SourcesPanel />
        </>
      ) : null}
    </div>
  );
}

function SourcesPanel() {
  const { t } = useTranslation();
  const sources = useQuery({ queryKey: queryKeys.indexerSources, queryFn: async () => unwrap(await api.GET("/indexers/sources")) });
  if (!sources.data?.length) return null;
  return (
    <Panel title={t("indexers.sources.title")} description={t("indexers.sources.subtitle")}>
      <ul className="flex flex-col gap-2" data-testid="source-list">
        {sources.data.map((s) => (
          <SourceRow key={s.id} source={s} />
        ))}
      </ul>
    </Panel>
  );
}

function SourceRow({ source }: { source: IndexerSource }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: queryKeys.indexerSources });
    void qc.invalidateQueries({ queryKey: queryKeys.indexers });
  };
  const path = { params: { path: { id: source.id } } };
  const sync = useMutation({ mutationFn: async () => unwrap(await api.POST("/indexers/sources/{id}/sync", path)), onSuccess: refresh });
  const remove = useMutation({ mutationFn: async () => unwrap(await api.DELETE("/indexers/sources/{id}", path)), onSuccess: refresh });
  return (
    <li className="flex flex-col gap-1.5 border-b border-line pb-2 last:border-b-0 last:pb-0">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-[13px]">
          <span className="font-medium text-fg">{source.name}</span>{" "}
          <span className="font-mono text-[11px] text-faint">{source.url}</span>
          <p className="text-[12px] text-muted">
            {t("indexers.sources.count", { n: source.indexerCount })} ·{" "}
            {source.lastSyncAt ? t("indexers.sources.lastSync", { when: formatRelative(source.lastSyncAt, i18n.language) }) : t("common.never")}
          </p>
          {source.lastError ? <p className="text-[12px] text-danger">{source.lastError}</p> : null}
        </div>
        <div className="flex gap-1">
          <Button variant="ghost" busy={sync.isPending} onClick={() => sync.mutate()}>
            {t("indexers.sources.sync")}
          </Button>
          <ConfirmButton label={t("indexers.sources.forget")} confirmLabel={t("indexers.sources.forgetConfirm")}
            busy={remove.isPending} onConfirm={() => remove.mutate()} />
        </div>
      </div>
      {sync.data ? (
        <Alert tone="success">
          {t("indexers.sources.synced", { added: sync.data.added, updated: sync.data.updated, disabled: sync.data.disabled })}
        </Alert>
      ) : null}
      <ErrorAlert error={sync.error ?? remove.error} />
    </li>
  );
}

function IndexerSettingsPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const st = useQuery({ queryKey: queryKeys.indexerSettings, queryFn: async () => unwrap(await api.GET("/indexers/settings")) });
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [block, setBlock] = useState<boolean | undefined>(undefined);
  const [trackers, setTrackers] = useState<string | undefined>(undefined);
  const save = useMutation({
    mutationFn: async () => {
      const n = (k: string) => (draft[k] !== undefined && draft[k] !== "" ? Number(draft[k]) : undefined);
      return unwrap(await api.PATCH("/indexers/settings", { body: {
        rssSyncIntervalMinutes: n("rss"), releaseRetentionDays: n("retention"), searchTimeoutSeconds: n("timeout"),
        searchCacheSeconds: n("cache"), blockPrivateNetworks: block,
        allowedHosts: draft.hosts !== undefined ? draft.hosts.split(/[\s,]+/).filter(Boolean) : undefined,
        publicTrackers: trackers !== undefined ? trackers.split(/\s+/).filter(Boolean) : undefined,
        userAgent: draft.ua,
      } }));
    },
    onSuccess: (s) => {
      qc.setQueryData(queryKeys.indexerSettings, s);
      setDraft({});
      setBlock(undefined);
      setTrackers(undefined);
    },
  });
  if (!st.data) return null;
  const s = st.data;
  const val = (k: string, cur: string | number) => draft[k] ?? String(cur);
  const set = (k: string) => (e: { target: { value: string } }) => setDraft({ ...draft, [k]: e.target.value });
  return (
    <Panel title={t("indexers.settings.title")} description={t("indexers.settings.subtitle")}>
      <form className="flex flex-col gap-3" data-testid="indexer-settings"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}>
        <div className="grid gap-3 sm:grid-cols-4">
          <TextInput label={t("indexers.settings.rss")} type="number" min={10} max={1440} value={val("rss", s.rssSyncIntervalMinutes)} onChange={set("rss")} />
          <TextInput label={t("indexers.settings.retention")} type="number" min={1} max={365} value={val("retention", s.releaseRetentionDays)}
            onChange={set("retention")} />
          <TextInput label={t("indexers.settings.timeout")} type="number" min={5} max={55} value={val("timeout", s.searchTimeoutSeconds)}
            onChange={set("timeout")} />
          <TextInput label={t("indexers.settings.cache")} type="number" min={0} max={3600} value={val("cache", s.searchCacheSeconds)}
            onChange={set("cache")} />
        </div>
        <TextInput label={t("indexers.settings.allowedHosts")} value={val("hosts", s.allowedHosts.join(", "))}
          hint={t("indexers.settings.allowedHostsHint")} placeholder="127.0.0.1, prowlarr" onChange={set("hosts")} />
        <TextInput label={t("indexers.settings.userAgent")} value={val("ua", s.userAgent)} placeholder="Locarr"
          hint={t("indexers.settings.userAgentHint")} onChange={set("ua")} />
        <Field label={t("indexers.settings.publicTrackers")} hint={t("indexers.settings.publicTrackersHint")} htmlFor="public-trackers">
          <textarea id="public-trackers" rows={4} value={trackers ?? s.publicTrackers.join("\n")} onChange={(e) => setTrackers(e.target.value)}
            className="w-full rounded-ui border border-line-strong bg-surface-2 px-2.5 py-1.5 font-mono text-[12px] text-fg focus:border-info focus:outline-none" />
        </Field>
        <Checkbox label={t("indexers.settings.blockPrivate")} description={t("indexers.settings.blockPrivateHint")}
          checked={block ?? s.blockPrivateNetworks} onChange={(e) => setBlock(e.target.checked)} />
        <ErrorAlert error={save.error} />
        {save.isSuccess ? <Alert tone="success">{t("common.saved")}</Alert> : null}
        <div>
          <Button type="submit" variant="primary" busy={save.isPending}>
            {t("common.save")}
          </Button>
        </div>
      </form>
    </Panel>
  );
}
