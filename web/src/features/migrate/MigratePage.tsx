import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  api,
  queryKeys,
  unwrap,
  type MigrateItem,
  type MigrateKind,
  type MigratePathCheck,
  type MigratePathMapping,
  type MigrateRun,
  type MigrateSource,
  type MigrateSourceOptions,
  type MigrateTestResult,
} from "@/api/client";
import { formatDuration } from "@/components/format";
import { Alert, Badge, Button, Checkbox, ConfirmButton, EmptyState, ErrorAlert, PageHeader, Panel, SelectInput, Spinner,
  TextInput } from "@/components/ui";

export const KINDS: MigrateKind[] = ["sonarr", "radarr", "lidarr", "prowlarr", "bazarr", "overseerr", "seerr", "qbittorrent", "sabnzbd"];

const DEFAULT_PORTS: Record<MigrateKind, string> = {
  sonarr: "http://sonarr:8989", radarr: "http://radarr:7878", lidarr: "http://lidarr:8686", prowlarr: "http://prowlarr:9696",
  bazarr: "http://bazarr:6767", overseerr: "http://overseerr:5055", seerr: "http://seerr:5055", qbittorrent: "http://qbittorrent:8080",
  sabnzbd: "http://sabnzbd:8080",
};

const STEPS = ["sources", "choose", "paths", "preview", "import"] as const;
type Step = (typeof STEPS)[number];

const ACTIVE = ["analysing", "importing"];

/** The import wizard: sources → what to import → paths → preview → import and report. */
export function MigratePage() {
  const { t } = useTranslation();
  const runs = useQuery({ queryKey: queryKeys.migrateRuns, queryFn: async () => unwrap(await api.GET("/migrate/runs")) });
  const [step, setStep] = useState<Step>("sources");
  const [runId, setRunId] = useState<number | null>(null);
  const run = useQuery({
    queryKey: queryKeys.migrateRun(runId ?? 0),
    enabled: runId !== null,
    queryFn: async () => unwrap(await api.GET("/migrate/runs/{id}", { params: { path: { id: runId ?? 0 } } })),
    refetchInterval: (q) => (q.state.data && (ACTIVE.includes(q.state.data.status) || refreshing(q.state.data)) ? 1000 : false),
  });
  // Pick up a run that is still going (or waiting for its import) after a reload.
  useEffect(() => {
    const last = runs.data?.[0];
    if (runId !== null || !last) return;
    if (last.status === "analysing" || last.status === "planned" || last.status === "analysed") {
      setRunId(last.id);
      setStep(last.status === "analysing" ? "choose" : "preview");
    } else if (last.status === "importing") {
      setRunId(last.id);
      setStep("import");
    }
  }, [runs.data, runId]);
  const current = run.data;
  useEffect(() => {
    if (current?.status === "planned" && step === "choose") setStep("paths");
  }, [current?.status, step]);

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("migrate.title")} description={t("migrate.subtitle")}
        actions={<Link to="/migrate/checklist" className="text-[13px] text-info hover:underline">{t("migrate.checklist.link")}</Link>} />
      <Alert tone="info">{t("migrate.readOnly")}</Alert>
      <ol className="flex flex-wrap gap-1.5" aria-label={t("migrate.steps")}>
        {STEPS.map((s, i) => (
          <li key={s}>
            <button
              type="button"
              disabled={!canGo(s, current)}
              onClick={() => setStep(s)}
              aria-current={step === s ? "step" : undefined}
              className={`rounded-ui border px-2.5 py-1 text-[12px] disabled:opacity-40 ${step === s ? "border-accent bg-accent/10 text-fg" : "border-line text-muted hover:text-fg"}`}
            >
              {i + 1}. {t(`migrate.step.${s}`)}
            </button>
          </li>
        ))}
      </ol>
      {step === "sources" ? <SourcesStep onNext={() => setStep("choose")} /> : null}
      {step === "choose" ? (
        <ChooseStep run={current} onStarted={(r) => { setRunId(r.id); }} />
      ) : null}
      {step === "paths" && current ? <PathsStep run={current} onNext={() => setStep("preview")} /> : null}
      {step === "preview" && current ? <PreviewStep run={current} onImport={() => setStep("import")} /> : null}
      {step === "import" && current ? <ImportStep run={current} onAgain={() => { setRunId(null); setStep("choose"); }} /> : null}
    </div>
  );
}

function refreshing(r: MigrateRun) {
  const rf = r.summary.refresh;
  return r.status === "done" && !!rf && rf.done < rf.total;
}

function canGo(s: Step, r: MigrateRun | undefined) {
  if (s === "sources" || s === "choose") return true;
  if (!r) return false;
  if (s === "import") return ["importing", "done", "failed", "cancelled"].includes(r.status);
  return r.status === "planned" || r.status === "analysed";
}

// ---- 1. sources ----

function SourcesStep({ onNext }: { onNext: () => void }) {
  const { t } = useTranslation();
  const list = useQuery({ queryKey: queryKeys.migrateSources, queryFn: async () => unwrap(await api.GET("/migrate/sources")) });
  return (
    <div className="flex flex-col gap-4">
      <Panel title={t("migrate.sources.title")} description={t("migrate.sources.description")}
        actions={<Button variant="primary" disabled={!list.data?.length} onClick={onNext}>{t("migrate.next")}</Button>}>
        {list.isPending ? <Spinner /> : null}
        <ErrorAlert error={list.error} />
        {list.data && !list.data.length ? <EmptyState>{t("migrate.sources.empty")}</EmptyState> : null}
        <ul className="flex flex-col gap-2" data-testid="migrate-sources">
          {list.data?.map((s) => <SourceRow key={s.id} source={s} />)}
        </ul>
      </Panel>
      <AddSource />
    </div>
  );
}

function SourceRow({ source }: { source: MigrateSource }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const path = { params: { path: { id: source.id } } };
  const test = useMutation({ mutationFn: async () => unwrap(await api.POST("/migrate/sources/{id}/test", path)),
    onSettled: () => qc.invalidateQueries({ queryKey: queryKeys.migrateSources }) });
  const remove = useMutation({ mutationFn: async () => unwrap(await api.DELETE("/migrate/sources/{id}", path)),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.migrateSources }) });
  return (
    <li className="flex flex-wrap items-center justify-between gap-2 rounded-ui border border-line px-3 py-2" data-testid={`source-${source.kind}`}>
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2 text-[13px] text-fg">
          <span className="font-medium">{source.name}</span>
          <Badge>{t(`migrate.kinds.${source.kind}`)}</Badge>
          {source.version ? <Badge tone="success">{t("migrate.sources.version", { version: source.version })}</Badge> : null}
          {source.secretSet ? <Badge tone="info">{t("migrate.sources.keySaved")}</Badge> : null}
        </p>
        <p className="mt-0.5 break-all font-mono text-[11px] text-faint">{source.url}</p>
        {source.lastError ? <p className="mt-0.5 text-[12px] text-danger">{source.lastError}</p> : null}
        {test.data ? <TestLine result={test.data} /> : null}
      </div>
      <div className="flex items-center gap-1">
        <Button busy={test.isPending} onClick={() => test.mutate()}>{t("migrate.sources.test")}</Button>
        <ConfirmButton label={t("common.remove")} confirmLabel={t("migrate.sources.removeConfirm")} onConfirm={() => remove.mutate()}
          busy={remove.isPending} />
      </div>
    </li>
  );
}

function TestLine({ result }: { result: MigrateTestResult }) {
  const { t } = useTranslation();
  return result.ok ? (
    <p className="mt-0.5 text-[12px] text-success" data-testid="migrate-test-ok">
      {t("migrate.sources.ok", { app: result.appName, version: result.version })}
    </p>
  ) : (
    <p className="mt-0.5 text-[12px] text-danger" data-testid="migrate-test-failed">
      {t(`migrate.sourceErrors.${result.code ?? "bad_response"}`, { defaultValue: result.message ?? "" })} {result.message ? `(${result.message})` : ""}
    </p>
  );
}

function AddSource() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [kind, setKind] = useState<MigrateKind>("sonarr");
  const [url, setUrl] = useState(DEFAULT_PORTS.sonarr);
  const [name, setName] = useState("");
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");
  const body = () => ({ kind, url, name: name || undefined, username: kind === "qbittorrent" ? username : undefined, secret });
  const test = useMutation({ mutationFn: async () => unwrap(await api.POST("/migrate/sources/test", { body: body() })) });
  const add = useMutation({
    mutationFn: async () => unwrap(await api.POST("/migrate/sources", { body: body() })),
    onSuccess: () => {
      setSecret("");
      setName("");
      test.reset();
      return qc.invalidateQueries({ queryKey: queryKeys.migrateSources });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    add.mutate();
  };
  return (
    <Panel title={t("migrate.sources.add")}>
      <form onSubmit={submit} className="grid gap-3 sm:grid-cols-2" data-testid="migrate-add-source">
        <SelectInput label={t("migrate.sources.kind")} value={kind} onChange={(e) => {
          const k = e.target.value as MigrateKind;
          setKind(k);
          setUrl(DEFAULT_PORTS[k]);
          test.reset();
        }}>
          {KINDS.map((k) => <option key={k} value={k}>{t(`migrate.kinds.${k}`)}</option>)}
        </SelectInput>
        <TextInput label={t("migrate.sources.name")} value={name} placeholder={t(`migrate.kinds.${kind}`)} onChange={(e) => setName(e.target.value)} />
        <TextInput label={t("migrate.sources.url")} value={url} onChange={(e) => setUrl(e.target.value)} hint={t("migrate.sources.urlHint")} required />
        {kind === "qbittorrent" ? (
          <TextInput label={t("migrate.sources.username")} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
        ) : null}
        <TextInput label={kind === "qbittorrent" ? t("migrate.sources.password") : t("migrate.sources.apiKey")} type="password" value={secret}
          onChange={(e) => setSecret(e.target.value)} autoComplete="new-password" hint={t(`migrate.keyHint.${kind}`)} />
        <div className="flex flex-wrap items-center gap-2 sm:col-span-2">
          <Button busy={test.isPending} onClick={() => test.mutate()}>{t("migrate.sources.test")}</Button>
          <Button type="submit" variant="primary" busy={add.isPending}>{t("migrate.sources.save")}</Button>
          {test.data ? <TestLine result={test.data} /> : null}
        </div>
        <div className="sm:col-span-2">
          <ErrorAlert error={test.error ?? add.error} />
        </div>
      </form>
    </Panel>
  );
}

// ---- 2. choose ----

function ChooseStep({ run, onStarted }: { run: MigrateRun | undefined; onStarted: (r: MigrateRun) => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: queryKeys.migrateSources, queryFn: async () => unwrap(await api.GET("/migrate/sources")) });
  const [opts, setOpts] = useState<Record<number, MigrateSourceOptions>>({});
  const [historyDays, setHistoryDays] = useState(90);
  const sources = list.data ?? [];
  const get = (s: MigrateSource): MigrateSourceOptions =>
    opts[s.id] ?? { id: s.id, settings: s.supports.settings, library: s.supports.library, users: s.supports.users, history: false,
      prowlarrMode: s.kind === "prowlarr" ? "source" : undefined, builtinReplace: s.kind === "prowlarr" ? true : undefined };
  const set = (s: MigrateSource, patch: Partial<MigrateSourceOptions>) => setOpts((o) => ({ ...o, [s.id]: { ...get(s), ...patch } }));
  const start = useMutation({
    mutationFn: async () => unwrap(await api.POST("/migrate/runs", { body: {
      sources: sources.map(get).filter((o) => o.settings || o.library || o.users || o.history), historyDays } })),
    onSuccess: (r) => {
      onStarted(r);
      return qc.invalidateQueries({ queryKey: queryKeys.migrateRuns });
    },
  });
  const analysing = run?.status === "analysing";
  return (
    <Panel title={t("migrate.choose.title")} description={t("migrate.choose.description")}
      actions={<Button variant="primary" busy={start.isPending || analysing} onClick={() => start.mutate()} disabled={!sources.length}
        data-testid="migrate-read">{t("migrate.choose.read")}</Button>}>
      <div className="flex flex-col gap-3">
        {sources.map((s) => {
          const o = get(s);
          return (
            <fieldset key={s.id} className="rounded-ui border border-line px-3 py-2" data-testid={`choose-${s.kind}`}>
              <legend className="px-1 text-[13px] font-medium text-fg">{s.name} <span className="text-faint">({t(`migrate.kinds.${s.kind}`)})</span></legend>
              <div className="flex flex-wrap gap-x-5 gap-y-2">
                {(["settings", "library", "users", "history"] as const).filter((k) => s.supports[k]).map((k) => (
                  <Checkbox key={k} label={t(`migrate.scope.${k}`)} description={t(`migrate.scopeHint.${s.kind}.${k}`, { defaultValue: "" }) || undefined}
                    checked={!!o[k]} onChange={(e) => set(s, { [k]: e.target.checked })} />
                ))}
              </div>
              {s.kind === "prowlarr" ? (
                <div className="mt-2 flex flex-col gap-1" role="radiogroup" aria-label={t("migrate.prowlarr.label")}>
                  {(["source", "direct"] as const).map((m) => (
                    <label key={m} className="flex items-start gap-2 text-[13px] text-fg">
                      <input type="radio" name={`prowlarr-${s.id}`} checked={o.prowlarrMode === m} onChange={() => set(s, { prowlarrMode: m })}
                        className="mt-0.5" />
                      <span>{t(`migrate.prowlarr.${m}`)}<span className="block text-[12px] text-faint">{t(`migrate.prowlarr.${m}Hint`)}</span></span>
                    </label>
                  ))}
                  <Checkbox label={t("migrate.prowlarr.builtin")} description={t("migrate.prowlarr.builtinHint")}
                    checked={o.builtinReplace ?? true} onChange={(e) => set(s, { builtinReplace: e.target.checked })} />
                </div>
              ) : null}
            </fieldset>
          );
        })}
        <TextInput label={t("migrate.choose.historyDays")} type="number" min={1} max={3650} value={historyDays} className="max-w-32"
          onChange={(e) => setHistoryDays(Number(e.target.value) || 90)} hint={t("migrate.choose.historyHint")} />
        {analysing && run ? <ProgressBar run={run} /> : null}
        {run?.status === "failed" ? <Alert>{run.error}</Alert> : null}
        <ErrorAlert error={start.error} />
      </div>
    </Panel>
  );
}

function ProgressBar({ run }: { run: MigrateRun }) {
  const { t } = useTranslation();
  const p = run.progress;
  const pct = p.total > 0 ? Math.round((p.done / p.total) * 100) : 0;
  return (
    <div className="flex flex-col gap-1" data-testid="migrate-progress">
      <div className="flex justify-between text-[12px] text-muted">
        <span>{t(`migrate.phase.${p.phase}`, { defaultValue: p.phase })}{p.message ? ` · ${p.message}` : ""}</span>
        {p.total > 0 ? <span>{p.done} / {p.total}</span> : null}
      </div>
      <div className="h-1.5 overflow-hidden rounded bg-surface-3" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}>
        <div className="h-full bg-accent transition-[width] duration-300" style={{ width: `${p.total > 0 ? pct : 15}%` }} />
      </div>
    </div>
  );
}

// ---- 3. paths ----

function PathsStep({ run, onNext }: { run: MigrateRun; onNext: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [mappings, setMappings] = useState<MigratePathMapping[]>(run.options.pathMappings);
  const [checks, setChecks] = useState<MigratePathCheck[]>(run.summary.pathChecks);
  const sources = run.summary.sources;
  const check = useMutation({
    mutationFn: async (m: MigratePathMapping[]) =>
      unwrap(await api.POST("/migrate/runs/{id}/paths", { params: { path: { id: run.id } }, body: { pathMappings: m } })),
    onSuccess: setChecks,
  });
  // Live validation while the mappings are edited.
  useEffect(() => {
    const h = setTimeout(() => check.mutate(mappings.filter((m) => m.from && m.to)), 400);
    return () => clearTimeout(h);
  }, [JSON.stringify(mappings)]);
  const plan = useMutation({
    mutationFn: async () => unwrap(await api.POST("/migrate/runs/{id}/plan", { params: { path: { id: run.id } },
      body: { pathMappings: mappings.filter((m) => m.from && m.to) } })),
    onSuccess: (r) => {
      qc.setQueryData(queryKeys.migrateRun(run.id), r);
      onNext();
    },
  });
  const update = (i: number, patch: Partial<MigratePathMapping>) => setMappings((l) => l.map((m, j) => (j === i ? { ...m, ...patch } : m)));
  const name = (id: number) => sources.find((s) => s.id === id)?.name ?? t("migrate.paths.allSources");
  return (
    <Panel title={t("migrate.paths.title")} description={t("migrate.paths.description")}
      actions={<Button variant="primary" busy={plan.isPending} onClick={() => plan.mutate()} data-testid="migrate-to-preview">{t("migrate.paths.preview")}</Button>}>
      <div className="flex flex-col gap-4">
        <ul className="flex flex-col gap-2" data-testid="migrate-path-checks">
          {checks.map((c) => {
            const ok = c.files > 0 ? c.found === c.files : c.exists;
            return (
              <li key={`${c.sourceId}:${c.sourcePath}`} className="rounded-ui border border-line px-3 py-2 text-[13px]">
                <p className="flex flex-wrap items-center gap-2">
                  <Badge tone={ok ? "success" : c.found > 0 ? "warning" : "danger"}>{ok ? t("migrate.paths.ok") : t("migrate.paths.problem")}</Badge>
                  <span className="text-muted">{name(c.sourceId)}</span>
                  <span className="font-mono text-[12px]">{c.sourcePath}</span>
                  <span className="text-faint">→</span>
                  <span className="font-mono text-[12px]">{c.localPath}</span>
                </p>
                <p className="mt-1 text-[12px] text-muted">
                  {c.files > 0 ? t("migrate.paths.found", { found: c.found, files: c.files, path: c.localPath })
                    : c.exists ? t("migrate.paths.folderOk") : !c.inside ? t("migrate.paths.outside") : t("migrate.paths.missing")}
                </p>
                {c.missing.length > 0 && c.found < c.files ? (
                  <details className="mt-1 text-[12px] text-faint">
                    <summary>{t("migrate.paths.examples")}</summary>
                    <ul className="ml-4 list-disc font-mono">{c.missing.map((m) => <li key={m} className="break-all">{m}</li>)}</ul>
                  </details>
                ) : null}
              </li>
            );
          })}
        </ul>
        <div className="flex flex-col gap-2">
          <h3 className="text-[13px] font-semibold text-fg">{t("migrate.paths.mappings")}</h3>
          <p className="text-[12px] text-muted">{t("migrate.paths.mappingsHint")}</p>
          {mappings.map((m, i) => (
            <div key={i} className="grid gap-2 sm:grid-cols-[10rem_1fr_1fr_auto] sm:items-end" data-testid="migrate-mapping">
              <SelectInput label={t("migrate.paths.source")} value={m.sourceId} onChange={(e) => update(i, { sourceId: Number(e.target.value) })}>
                <option value={0}>{t("migrate.paths.allSources")}</option>
                {sources.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </SelectInput>
              <TextInput label={t("migrate.paths.from")} value={m.from} onChange={(e) => update(i, { from: e.target.value })} placeholder="/tv" />
              <TextInput label={t("migrate.paths.to")} value={m.to} onChange={(e) => update(i, { to: e.target.value })} placeholder="/data/media/tv" />
              <Button variant="ghost" onClick={() => setMappings((l) => l.filter((_, j) => j !== i))}>{t("common.remove")}</Button>
            </div>
          ))}
          <div>
            <Button onClick={() => setMappings((l) => [...l, { sourceId: 0, from: "", to: "" }])}>{t("migrate.paths.add")}</Button>
          </div>
        </div>
        <ErrorAlert error={check.error ?? plan.error} />
      </div>
    </Panel>
  );
}

// ---- 4. preview ----

const CATEGORY_ORDER = ["rootfolder", "qualitydefinitions", "customformat", "qualityprofile", "metadataprofile", "delayprofile", "naming",
  "indexersource", "indexer", "releaseprofile", "downloadclient", "pathmapping", "importlist", "exclusions", "movie", "series", "artist",
  "user", "requestdefaults", "request", "issue", "subtitleprofile", "subtitleprovider", "subtitleassignments", "blocklist", "history"];

function sortedCategories(counts: Record<string, Record<string, number>>) {
  return Object.keys(counts).sort((a, b) => CATEGORY_ORDER.indexOf(a) - CATEGORY_ORDER.indexOf(b));
}

function PreviewStep({ run, onImport }: { run: MigrateRun; onImport: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [resolutions, setResolutions] = useState<Record<string, "merge" | "rename" | "skip">>({});
  const replan = useMutation({
    mutationFn: async () => unwrap(await api.POST("/migrate/runs/{id}/plan", { params: { path: { id: run.id } }, body: { resolutions:
      { ...(run.options.resolutions as Record<string, "merge" | "rename" | "skip">), ...resolutions } } })),
    onSuccess: (r) => {
      setResolutions({});
      qc.setQueryData(queryKeys.migrateRun(run.id), r);
      return qc.invalidateQueries({ queryKey: ["migrate", "runs", run.id, "items"] });
    },
  });
  const start = useMutation({
    mutationFn: async () => unwrap(await api.POST("/migrate/runs/{id}/import", { params: { path: { id: run.id } } })),
    onSuccess: (r) => {
      qc.setQueryData(queryKeys.migrateRun(run.id), r);
      onImport();
    },
  });
  const pending = Object.keys(resolutions).length > 0;
  return (
    <div className="flex flex-col gap-4">
      <Panel title={t("migrate.preview.title")} description={t("migrate.preview.description")}
        actions={
          <>
            {pending ? <Button busy={replan.isPending} onClick={() => replan.mutate()} data-testid="migrate-replan">{t("migrate.preview.apply")}</Button> : null}
            <Button variant="primary" busy={start.isPending} disabled={pending} onClick={() => start.mutate()} data-testid="migrate-import">
              {t("migrate.preview.import")}
            </Button>
          </>
        }>
        <CountsTable run={run} />
        <AttentionSummary run={run} />
        {run.summary.warnings.length ? (
          <details className="mt-3 text-[12px] text-muted">
            <summary>{t("migrate.preview.warnings", { count: run.summary.warnings.length })}</summary>
            <ul className="ml-4 list-disc">{run.summary.warnings.map((w) => <li key={w}>{w}</li>)}</ul>
          </details>
        ) : null}
        <ErrorAlert error={replan.error ?? start.error} />
      </Panel>
      <ItemsPanel run={run} resolutions={resolutions} onResolve={(key, v) => setResolutions((r) => ({ ...r, [key]: v }))} />
    </div>
  );
}

function CountsTable({ run, result }: { run: MigrateRun; result?: boolean }) {
  const { t } = useTranslation();
  const counts = run.summary.counts;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[13px]" data-testid="migrate-counts">
        <thead>
          <tr className="text-left text-[12px] text-faint">
            <th className="py-1 pr-3 font-medium">{t("migrate.preview.category")}</th>
            <th className="py-1 pr-3 text-right font-medium">{t("migrate.action.create")}</th>
            <th className="py-1 pr-3 text-right font-medium">{t("migrate.action.update")}</th>
            <th className="py-1 text-right font-medium">{t("migrate.action.skip")}</th>
          </tr>
        </thead>
        <tbody>
          {sortedCategories(counts).map((c) => (
            <tr key={c} className="border-t border-line" data-testid={`count-${c}`}>
              <td className="py-1 pr-3 text-fg">{t(`migrate.category.${c}`, { defaultValue: c })}</td>
              <td className="py-1 pr-3 text-right tabular-nums text-success">{counts[c]?.create ?? 0}</td>
              <td className="py-1 pr-3 text-right tabular-nums text-info">{counts[c]?.update ?? 0}</td>
              <td className="py-1 text-right tabular-nums text-muted">{counts[c]?.skip ?? 0}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {result ? null : <p className="mt-2 text-[12px] text-faint">{t("migrate.preview.countsHint")}</p>}
    </div>
  );
}

function AttentionSummary({ run }: { run: MigrateRun }) {
  const { t } = useTranslation();
  const att = Object.entries(run.summary.attention);
  if (!att.length && !run.summary.conflicts) return null;
  return (
    <ul className="mt-3 flex flex-wrap gap-2" data-testid="migrate-attention">
      {run.summary.conflicts ? <li><Badge tone="warning">{t("migrate.preview.conflicts", { count: run.summary.conflicts })}</Badge></li> : null}
      {att.map(([k, n]) => (
        <li key={k}><Badge tone={k === "needs_secret" || k === "file_not_found" ? "warning" : "info"}>{t(`migrate.attention.${k}`, { count: n })}</Badge></li>
      ))}
    </ul>
  );
}

const PAGE = 100;

function ItemsPanel({ run, resolutions, onResolve, result }: { run: MigrateRun; resolutions?: Record<string, string>;
  onResolve?: (key: string, v: "merge" | "rename" | "skip") => void; result?: boolean }) {
  const { t } = useTranslation();
  const [category, setCategory] = useState("");
  const [action, setAction] = useState<"" | "create" | "update" | "skip">("");
  const [status, setStatus] = useState<"" | "done" | "failed" | "skipped">("");
  const [attention, setAttention] = useState(false);
  const [conflicts, setConflicts] = useState(false);
  const [q, setQ] = useState("");
  const [offset, setOffset] = useState(0);
  const filter = { category, action, status, attention, conflicts, q, offset, at: run.updatedAt };
  const items = useQuery({
    queryKey: queryKeys.migrateItems(run.id, filter),
    queryFn: async () => unwrap(await api.GET("/migrate/runs/{id}/items", { params: { path: { id: run.id }, query: {
      category: category || undefined, action: action || undefined, status: status || undefined, attention: attention || undefined,
      conflicts: conflicts || undefined, q: q || undefined, offset, limit: PAGE } } })),
    placeholderData: (prev) => prev,
  });
  const cats = useMemo(() => sortedCategories(run.summary.counts), [run.summary.counts]);
  const reset = () => setOffset(0);
  return (
    <Panel title={result ? t("migrate.report.items") : t("migrate.preview.items")}>
      <div className="mb-3 grid gap-2 sm:grid-cols-4">
        <SelectInput label={t("migrate.preview.category")} value={category} onChange={(e) => { setCategory(e.target.value); reset(); }}>
          <option value="">{t("migrate.preview.all")}</option>
          {cats.map((c) => <option key={c} value={c}>{t(`migrate.category.${c}`, { defaultValue: c })}</option>)}
        </SelectInput>
        {result ? (
          <SelectInput label={t("migrate.report.status")} value={status} onChange={(e) => { setStatus(e.target.value as typeof status); reset(); }}>
            <option value="">{t("migrate.preview.all")}</option>
            {(["done", "failed", "skipped"] as const).map((s) => <option key={s} value={s}>{t(`migrate.status.${s}`)}</option>)}
          </SelectInput>
        ) : (
          <SelectInput label={t("migrate.preview.action")} value={action} onChange={(e) => { setAction(e.target.value as typeof action); reset(); }}>
            <option value="">{t("migrate.preview.all")}</option>
            {(["create", "update", "skip"] as const).map((a) => <option key={a} value={a}>{t(`migrate.action.${a}`)}</option>)}
          </SelectInput>
        )}
        <TextInput label={t("migrate.preview.search")} value={q} onChange={(e) => { setQ(e.target.value); reset(); }} />
        <div className="flex flex-col justify-end gap-1">
          <Checkbox label={t("migrate.preview.attentionOnly")} checked={attention} onChange={(e) => { setAttention(e.target.checked); reset(); }} />
          {result ? null : <Checkbox label={t("migrate.preview.conflictsOnly")} checked={conflicts} onChange={(e) => { setConflicts(e.target.checked); reset(); }} />}
        </div>
      </div>
      <ErrorAlert error={items.error} />
      {items.data && !items.data.items.length ? <EmptyState>{t("migrate.preview.none")}</EmptyState> : null}
      <ul className="flex flex-col divide-y divide-line" data-testid="migrate-items">
        {items.data?.items.map((it) => (
          <ItemRow key={it.key} item={it} result={result} chosen={resolutions?.[it.key]} onResolve={onResolve} />
        ))}
      </ul>
      {items.data && items.data.total > PAGE ? (
        <div className="mt-3 flex items-center justify-between text-[12px] text-muted">
          <span>{t("migrate.preview.page", { from: offset + 1, to: Math.min(offset + PAGE, items.data.total), total: items.data.total })}</span>
          <span className="flex gap-1">
            <Button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>{t("migrate.preview.prev")}</Button>
            <Button disabled={offset + PAGE >= items.data.total} onClick={() => setOffset(offset + PAGE)}>{t("migrate.preview.nextPage")}</Button>
          </span>
        </div>
      ) : null}
    </Panel>
  );
}

const actionTone = { create: "success", update: "info", skip: "neutral" } as const;
const statusTone = { planned: "neutral", done: "success", failed: "danger", skipped: "neutral" } as const;

function ItemRow({ item, result, chosen, onResolve }: { item: MigrateItem; result?: boolean; chosen?: string;
  onResolve?: (key: string, v: "merge" | "rename" | "skip") => void }) {
  const { t } = useTranslation();
  return (
    <li className="flex flex-wrap items-start justify-between gap-2 py-2 text-[13px]" data-testid={`item-${item.category}`}>
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-2">
          <Badge tone={result ? statusTone[item.status] : actionTone[item.action]}>
            {result ? t(`migrate.status.${item.status}`) : t(`migrate.action.${item.action}`)}
          </Badge>
          <span className="text-faint">{t(`migrate.category.${item.category}`, { defaultValue: item.category })}</span>
          <span className="break-all text-fg">{item.name}</span>
          {item.attention.map((a) => <Badge key={a} tone="warning">{t(`migrate.attentionShort.${a}`, { defaultValue: a })}</Badge>)}
        </p>
        {item.reason ? <p className="mt-0.5 text-[12px] text-muted">{t(`migrate.reason.${item.reason}`, { defaultValue: item.reason })}</p> : null}
        {item.message ? <p className="mt-0.5 break-words text-[12px] text-faint">{item.message}</p> : null}
        {item.error ? <p className="mt-0.5 text-[12px] text-danger">{item.error}</p> : null}
      </div>
      {item.conflict && !result && onResolve ? (
        <div className="w-44">
          <SelectInput label={t("migrate.preview.conflict", { name: item.conflict.existing })} value={chosen ?? item.conflict.chosen}
            onChange={(e) => onResolve(item.key, e.target.value as "merge" | "rename" | "skip")} data-testid="migrate-resolution">
            {item.conflict.options.map((o) => <option key={o} value={o}>{t(`migrate.resolution.${o}`)}</option>)}
          </SelectInput>
        </div>
      ) : null}
    </li>
  );
}

// ---- 5. import and report ----

function ImportStep({ run, onAgain }: { run: MigrateRun; onAgain: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const cancel = useMutation({ mutationFn: async () => unwrap(await api.POST("/migrate/runs/{id}/cancel", { params: { path: { id: run.id } } })),
    onSuccess: (r) => qc.setQueryData(queryKeys.migrateRun(run.id), r) });
  const download = useMutation({
    mutationFn: async () => {
      const rep = unwrap(await api.GET("/migrate/runs/{id}/report", { params: { path: { id: run.id } } }));
      const url = URL.createObjectURL(new Blob([JSON.stringify(rep, null, 2)], { type: "application/json" }));
      const a = document.createElement("a");
      a.href = url;
      a.download = `locarr-import-${run.id}.json`;
      a.click();
      URL.revokeObjectURL(url);
    },
  });
  // What the import created is new everywhere else (library, settings, users).
  useEffect(() => {
    if (run.status === "done") void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "migrate" });
  }, [run.status, qc]);
  const st = run.summary.statuses;
  const rf = run.summary.refresh;
  return (
    <div className="flex flex-col gap-4">
      <Panel title={t(`migrate.report.title.${run.status}`, { defaultValue: t("migrate.report.title.done") })}
        actions={run.status === "importing"
          ? <Button variant="danger" busy={cancel.isPending} onClick={() => cancel.mutate()}>{t("migrate.report.cancel")}</Button>
          : <>
              <Button busy={download.isPending} onClick={() => download.mutate()} data-testid="migrate-download">{t("migrate.report.download")}</Button>
              <Button onClick={onAgain}>{t("migrate.report.again")}</Button>
            </>}>
        {run.status === "importing" ? <ProgressBar run={run} /> : null}
        {run.status === "failed" || run.status === "cancelled" ? <Alert>{run.error}</Alert> : null}
        {run.status === "done" ? (
          <div className="flex flex-col gap-3" data-testid="migrate-report">
            <p className="text-[13px] text-fg">
              {t("migrate.report.summary", { done: st.done ?? 0, skipped: st.skipped ?? 0, failed: st.failed ?? 0 })}
              {run.summary.durationMs ? ` · ${formatDuration(Math.max(1, Math.round(run.summary.durationMs / 1000)), i18n.language)}` : ""}
            </p>
            {st.failed ? <Alert tone="warning">{t("migrate.report.failedHint", { count: st.failed })}</Alert> : null}
            {rf ? (
              <p className="text-[12px] text-muted" data-testid="migrate-refresh">
                {rf.done < rf.total ? t("migrate.report.refreshing", { done: rf.done, total: rf.total }) : t("migrate.report.refreshed", { total: rf.total, failed: rf.failed })}
              </p>
            ) : null}
            <CountsTable run={run} result />
            <AttentionSummary run={run} />
            <p className="text-[13px]">
              <Link to="/migrate/checklist" className="text-info hover:underline" data-testid="migrate-checklist-link">{t("migrate.report.next")}</Link>
            </p>
          </div>
        ) : null}
        <ErrorAlert error={cancel.error ?? download.error} />
      </Panel>
      {run.status === "done" ? <ItemsPanel run={run} result /> : null}
    </div>
  );
}
