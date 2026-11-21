import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type Indexer, type IndexerCaps, type IndexerCategories, type IndexerInput,
  type IndexerTestResult } from "@/api/client";
import { Alert, Button, Checkbox, ErrorAlert, Panel, SelectInput, TextInput } from "@/components/ui";

const MEDIA = ["movies", "tv", "music", "books"] as const;
type Media = (typeof MEDIA)[number];

const emptyCats = (): IndexerCategories => ({ movies: [], tv: [], music: [], books: [] });

const num = (s: string): number | undefined => {
  const n = Number(s);
  return s.trim() === "" || !Number.isFinite(n) ? undefined : n;
};

/** Category table: one row per category from the capabilities, a column per kind of media. */
export function CategoryPicker({ caps, value, onChange }: { caps?: IndexerCaps; value: IndexerCategories; onChange: (c: IndexerCategories) => void }) {
  const { t } = useTranslation();
  if (!caps?.categories.length) return <p className="text-[12px] text-faint">{t("indexers.form.categoriesNeedTest")}</p>;
  const rows = caps.categories.flatMap((c) => [{ ...c, sub: false }, ...(c.subcategories ?? []).map((s) => ({ ...s, sub: true }))]);
  const toggle = (m: Media, id: number, on: boolean) => {
    const cur = value[m];
    onChange({ ...value, [m]: on ? [...cur, id].sort((a, b) => a - b) : cur.filter((x) => x !== id) });
  };
  return (
    <div className="max-h-72 overflow-y-auto rounded-ui border border-line">
      <table className="w-full text-[12px]" data-testid="category-picker">
        <thead className="sticky top-0 bg-surface-2 text-muted">
          <tr>
            <th className="px-2 py-1 text-left font-medium">{t("indexers.form.category")}</th>
            {MEDIA.map((m) => (
              <th key={m} className="px-2 py-1 font-medium">
                {t(`indexers.media.${m}`)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((c) => (
            <tr key={c.id} className="border-t border-line">
              <td className={`px-2 py-1 ${c.sub ? "pl-6 text-muted" : "text-fg"}`}>
                {c.name} <span className="font-mono text-faint">{c.id}</span>
              </td>
              {MEDIA.map((m) => (
                <td key={m} className="px-2 py-1 text-center">
                  <input type="checkbox" className="size-3.5 accent-[var(--lc-accent)]" checked={value[m].includes(c.id)}
                    aria-label={`${t(`indexers.media.${m}`)}: ${c.name}`} onChange={(e) => toggle(m, c.id, e.target.checked)} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function TestResult({ result }: { result: IndexerTestResult }) {
  const { t } = useTranslation();
  if (result.ok) {
    return (
      <Alert tone="success">
        {t("indexers.test.ok", { n: result.probeResults ?? 0, ms: result.durationMs })}
        {result.caps?.serverTitle ? ` · ${result.caps.serverTitle}` : ""}
      </Alert>
    );
  }
  return (
    <Alert>
      {t(`indexers.errorKinds.${result.errorCode ?? "unknown"}`, { defaultValue: t("indexers.errorKinds.unknown") })}
      {result.error ? <span className="mt-1 block font-mono text-[11px] opacity-80">{result.error}</span> : null}
    </Alert>
  );
}

export function IndexerForm({ indexer, onDone }: { indexer?: Indexer; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [name, setName] = useState(indexer?.name ?? "");
  const [kind, setKind] = useState<IndexerInput["kind"]>(indexer?.kind ?? "torznab");
  const [baseUrl, setBaseUrl] = useState(indexer?.baseUrl ?? "");
  const [apiPath, setApiPath] = useState(indexer?.apiPath ?? "/api");
  const [apiKey, setApiKey] = useState("");
  const [priority, setPriority] = useState(String(indexer?.priority ?? 25));
  const [minSeeders, setMinSeeders] = useState(String(indexer?.minimumSeeders ?? 1));
  const [tags, setTags] = useState((indexer?.tags ?? []).join(", "));
  const [enabled, setEnabled] = useState(indexer?.enabled ?? true);
  const [rss, setRss] = useState(indexer?.enableRss ?? true);
  const [auto, setAuto] = useState(indexer?.enableAutomaticSearch ?? true);
  const [interactive, setInteractive] = useState(indexer?.enableInteractiveSearch ?? true);
  const [cats, setCats] = useState<IndexerCategories>(indexer?.categories ?? emptyCats());
  const [caps, setCaps] = useState<IndexerCaps | undefined>(indexer?.caps);
  const [advanced, setAdvanced] = useState(false);
  const [seedRatio, setSeedRatio] = useState(indexer?.seedRatio?.toString() ?? "");
  const [seedTime, setSeedTime] = useState(indexer?.seedTimeMinutes?.toString() ?? "");
  const [reqLimit, setReqLimit] = useState(indexer?.requestLimit?.toString() ?? "");
  const [reqPeriod, setReqPeriod] = useState(indexer?.requestPeriodSeconds?.toString() ?? "");
  const [grabLimit, setGrabLimit] = useState(indexer?.grabLimitPerDay?.toString() ?? "");
  const [timeout, setTimeoutS] = useState(String(indexer?.timeoutSeconds ?? 30));
  const [proxyUrl, setProxyUrl] = useState(indexer?.proxyUrl ?? "");
  const [proxyUser, setProxyUser] = useState(indexer?.proxyUsername ?? "");
  const [proxyPassword, setProxyPassword] = useState("");
  const [flaresolverr, setFlaresolverr] = useState(indexer?.flaresolverrUrl ?? "");

  const body = (): IndexerInput => {
    const b: IndexerInput = {
      name: name.trim(), kind, baseUrl: baseUrl.trim(), apiPath: apiPath.trim() || "/api", enabled, enableRss: rss,
      enableAutomaticSearch: auto, enableInteractiveSearch: interactive, priority: num(priority) ?? 25,
      minimumSeeders: num(minSeeders) ?? 1, timeoutSeconds: num(timeout) ?? 30,
      tags: tags.split(",").map((s) => s.trim()).filter(Boolean), proxyUrl: proxyUrl.trim(), proxyUsername: proxyUser.trim(),
      flaresolverrUrl: flaresolverr.trim(), seedRatio: num(seedRatio), seedTimeMinutes: num(seedTime),
      requestLimit: num(reqLimit), requestPeriodSeconds: num(reqPeriod), grabLimitPerDay: num(grabLimit),
    };
    if (MEDIA.some((m) => cats[m].length)) b.categories = cats;
    if (apiKey !== "" || !indexer) b.apiKey = apiKey;
    if (proxyPassword !== "") b.proxyPassword = proxyPassword;
    return b;
  };

  const test = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/indexers/test", { body: { indexer: { ...body(), name: name.trim() || "test" }, ...(indexer ? { indexerId: indexer.id } : {}) } })),
    onSuccess: (r) => {
      if (r.caps) setCaps(r.caps);
      if (r.defaultCategories && !MEDIA.some((m) => cats[m].length)) setCats(r.defaultCategories);
    },
  });
  const save = useMutation({
    mutationFn: async () =>
      indexer
        ? unwrap(await api.PUT("/indexers/{id}", { params: { path: { id: indexer.id } }, body: body() }))
        : unwrap(await api.POST("/indexers", { body: body() })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.indexers });
      onDone();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const keyHint = indexer?.apiKey.configured ? t("indexers.form.apiKeyKept") : t("indexers.form.apiKeyHint");

  return (
    <Panel title={indexer ? t("indexers.form.editTitle", { name: indexer.name }) : t("indexers.form.addTitle")}>
      <form onSubmit={submit} className="flex flex-col gap-4" data-testid="indexer-form">
        <div className="grid gap-3 sm:grid-cols-2">
          <TextInput label={t("indexers.form.name")} name="indexer-name" value={name} onChange={(e) => setName(e.target.value)} />
          <SelectInput label={t("indexers.form.kind")} value={kind} onChange={(e) => setKind(e.target.value as IndexerInput["kind"])}>
            <option value="torznab">{t("indexers.kinds.torznab")}</option>
            <option value="newznab">{t("indexers.kinds.newznab")}</option>
          </SelectInput>
          <TextInput label={t("indexers.form.baseUrl")} name="indexer-url" value={baseUrl} placeholder="http://prowlarr:9696/1"
            hint={t("indexers.form.baseUrlHint")} onChange={(e) => setBaseUrl(e.target.value)} />
          <TextInput label={t("indexers.form.apiPath")} name="indexer-api-path" value={apiPath} onChange={(e) => setApiPath(e.target.value)} />
          <TextInput label={t("indexers.form.apiKey")} name="indexer-api-key" type="password" autoComplete="off" value={apiKey}
            hint={keyHint} onChange={(e) => setApiKey(e.target.value)} />
          <TextInput label={t("indexers.form.tags")} name="indexer-tags" value={tags} hint={t("indexers.form.tagsHint")}
            onChange={(e) => setTags(e.target.value)} />
          <TextInput label={t("indexers.form.priority")} type="number" min={1} max={50} value={priority}
            hint={t("indexers.form.priorityHint")} onChange={(e) => setPriority(e.target.value)} />
          <TextInput label={t("indexers.form.minimumSeeders")} type="number" min={0} value={minSeeders}
            onChange={(e) => setMinSeeders(e.target.value)} />
        </div>
        <div className="grid gap-2 sm:grid-cols-2">
          <Checkbox label={t("indexers.form.enabled")} checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          <Checkbox label={t("indexers.form.enableRss")} checked={rss} onChange={(e) => setRss(e.target.checked)} />
          <Checkbox label={t("indexers.form.enableAutomatic")} checked={auto} onChange={(e) => setAuto(e.target.checked)} />
          <Checkbox label={t("indexers.form.enableInteractive")} checked={interactive} onChange={(e) => setInteractive(e.target.checked)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <p className="text-[12px] font-medium text-muted">{t("indexers.form.categories")}</p>
          <CategoryPicker caps={caps} value={cats} onChange={setCats} />
        </div>
        <div>
          <Button variant="ghost" onClick={() => setAdvanced(!advanced)} aria-expanded={advanced}>
            {advanced ? t("indexers.form.hideAdvanced") : t("indexers.form.showAdvanced")}
          </Button>
        </div>
        {advanced ? (
          <div className="grid gap-3 sm:grid-cols-3" data-testid="indexer-advanced">
            <TextInput label={t("indexers.form.seedRatio")} type="number" step="0.1" min={0} value={seedRatio}
              hint={t("indexers.form.seedHint")} onChange={(e) => setSeedRatio(e.target.value)} />
            <TextInput label={t("indexers.form.seedTime")} type="number" min={0} value={seedTime} onChange={(e) => setSeedTime(e.target.value)} />
            <TextInput label={t("indexers.form.timeout")} type="number" min={2} max={120} value={timeout}
              onChange={(e) => setTimeoutS(e.target.value)} />
            <TextInput label={t("indexers.form.requestLimit")} type="number" min={1} value={reqLimit}
              hint={t("indexers.form.requestLimitHint")} onChange={(e) => setReqLimit(e.target.value)} />
            <TextInput label={t("indexers.form.requestPeriod")} type="number" min={1} value={reqPeriod}
              onChange={(e) => setReqPeriod(e.target.value)} />
            <TextInput label={t("indexers.form.grabLimit")} type="number" min={1} value={grabLimit}
              onChange={(e) => setGrabLimit(e.target.value)} />
            <TextInput label={t("indexers.form.proxyUrl")} value={proxyUrl} placeholder="socks5://proxy:1080"
              onChange={(e) => setProxyUrl(e.target.value)} />
            <TextInput label={t("indexers.form.proxyUsername")} value={proxyUser} autoComplete="off" onChange={(e) => setProxyUser(e.target.value)} />
            <TextInput label={t("indexers.form.proxyPassword")} type="password" autoComplete="off" value={proxyPassword}
              hint={indexer?.proxyPassword.configured ? t("indexers.form.apiKeyKept") : undefined}
              onChange={(e) => setProxyPassword(e.target.value)} />
            <div className="sm:col-span-3">
              <TextInput label={t("indexers.form.flaresolverr")} value={flaresolverr} placeholder="http://flaresolverr:8191"
                hint={t("indexers.form.flaresolverrHint")} onChange={(e) => setFlaresolverr(e.target.value)} />
            </div>
          </div>
        ) : null}
        {test.data ? <TestResult result={test.data} /> : null}
        <ErrorAlert error={test.error ?? save.error} />
        <div className="flex flex-wrap gap-2">
          <Button type="submit" variant="primary" busy={save.isPending} disabled={!name.trim() || !baseUrl.trim()}>
            {indexer ? t("common.save") : t("indexers.form.add")}
          </Button>
          <Button busy={test.isPending} disabled={!baseUrl.trim()} onClick={() => test.mutate()}>
            {t("indexers.test.button")}
          </Button>
          <Button variant="ghost" onClick={onDone}>
            {t("common.cancel")}
          </Button>
        </div>
      </form>
    </Panel>
  );
}
