import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type Indexer, type IndexerCaps, type IndexerCategories, type IndexerDefinition,
  type IndexerInput } from "@/api/client";
import { Alert, Badge, Button, Checkbox, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { CategoryPicker, IndexerForm, TestResult } from "./IndexerForm";

/** The API's defaults; a built-in site replaces the timeout and RSS with its own. */
const defaults = { enabled: true, enableRss: true, enableAutomaticSearch: true, enableInteractiveSearch: true, priority: 25,
  minimumSeeders: 1, timeoutSeconds: 30 };

const num = (s: string): number | undefined => {
  const n = Number(s);
  return s.trim() === "" || !Number.isFinite(n) ? undefined : n;
};

/** The catalog of site definitions and usenet presets (shared cache). */
export function useCatalog() {
  return useQuery({ queryKey: queryKeys.indexerDefinitions, queryFn: async () => unwrap(await api.GET("/indexers/definitions")) });
}

/** A saved indexer as an input for PUT, keeping its stored secrets. */
export function inputFromIndexer(i: Indexer, over: Partial<IndexerInput> = {}): IndexerInput {
  return {
    name: i.name, kind: i.kind, baseUrl: i.baseUrl, apiPath: i.apiPath, enabled: i.enabled, enableRss: i.enableRss,
    enableAutomaticSearch: i.enableAutomaticSearch, enableInteractiveSearch: i.enableInteractiveSearch, priority: i.priority,
    tags: i.tags, categories: i.categories, minimumSeeders: i.minimumSeeders, seedRatio: i.seedRatio, seedTimeMinutes: i.seedTimeMinutes,
    requestLimit: i.requestLimit, requestPeriodSeconds: i.requestPeriodSeconds, grabLimitPerDay: i.grabLimitPerDay,
    timeoutSeconds: i.timeoutSeconds, proxyUrl: i.proxyUrl ?? "", proxyUsername: i.proxyUsername ?? "", flaresolverrUrl: i.flaresolverrUrl ?? "",
    ...(i.kind === "native" ? { definition: i.definition?.id, options: i.options ?? {} } : {}),
    ...over,
  };
}

/** Capabilities of a definition, for the category picker. */
export function definitionCaps(d?: IndexerDefinition): IndexerCaps | undefined {
  return d ? { searching: d.searching, categories: d.categories } : undefined;
}

type Tab = "builtin" | "usenet" | "generic";

/** The "Add indexer" chooser: built-in sites, usenet presets, any Torznab/Newznab feed. */
export function AddIndexer({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<Tab>("builtin");
  const tabs: Tab[] = ["builtin", "usenet", "generic"];
  return (
    <Panel title={t("indexers.chooser.title")} actions={<Button variant="ghost" onClick={onDone}>{t("common.close")}</Button>}>
      <div className="flex flex-col gap-3" data-testid="add-indexer">
        <div role="tablist" aria-label={t("indexers.chooser.title")} className="flex gap-1 border-b border-line">
          {tabs.map((k) => (
            <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}
              className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-[13px] transition-colors ${
                tab === k ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
              {t(`indexers.chooser.tabs.${k}`)}
            </button>
          ))}
        </div>
        <p className="text-[12px] text-muted">{t(`indexers.chooser.${tab}Intro`)}</p>
        <div role="tabpanel">
          {tab === "builtin" ? <BuiltinList /> : null}
          {tab === "usenet" ? <UsenetForm onDone={onDone} /> : null}
          {tab === "generic" ? <IndexerForm onDone={onDone} /> : null}
        </div>
      </div>
    </Panel>
  );
}

function BuiltinList() {
  const { t } = useTranslation();
  const catalog = useCatalog();
  const indexers = useQuery({ queryKey: queryKeys.indexers, queryFn: async () => unwrap(await api.GET("/indexers")) });
  if (catalog.isPending) return <Spinner />;
  if (catalog.error) return <ErrorAlert error={catalog.error} />;
  const defs = catalog.data.definitions;
  if (!defs.length) return <EmptyState>{t("indexers.builtin.none")}</EmptyState>;
  return (
    <ul className="flex flex-col divide-y divide-line rounded-ui border border-line" data-testid="builtin-list">
      {defs.map((d) => (
        <BuiltinRow key={d.id} def={d} indexer={indexers.data?.find((i) => i.kind === "native" && i.definition?.id === d.id)} />
      ))}
    </ul>
  );
}

function BuiltinRow({ def, indexer }: { def: IndexerDefinition; indexer?: Indexer }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const toggle = useMutation({
    mutationFn: async (on: boolean) => {
      if (indexer) {
        return unwrap(await api.PUT("/indexers/{id}", { params: { path: { id: indexer.id } }, body: inputFromIndexer(indexer, { enabled: on }) }));
      }
      return unwrap(await api.POST("/indexers", { body: { ...defaults, name: def.name, kind: "native", definition: def.id } }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.indexers });
    },
  });
  const on = !!indexer?.enabled;
  return (
    <li className="flex flex-wrap items-start justify-between gap-3 px-3 py-2.5" data-testid={`builtin-${def.id}`}>
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-2 text-[13px] text-fg">
          <span className="font-medium">{def.name}</span>
          <span className="font-mono text-[11px] text-faint">{t("indexers.builtin.version", { n: def.version })}</span>
          {!def.builtin ? <Badge tone="info">{t("indexers.builtin.custom")}</Badge> : null}
          {def.overrides ? <Badge tone="info">{t("indexers.builtin.overrides")}</Badge> : null}
          {def.challenge !== "never" ? (
            <Badge tone="warning">{t(`indexers.builtin.flaresolverr.${def.challenge}`)}</Badge>
          ) : null}
          {def.language ? <Badge>{def.language}</Badge> : null}
        </p>
        <p className="mt-0.5 text-[12px] text-muted">{def.description}</p>
        <p className="mt-0.5 text-[11px] text-faint">
          {def.searching.map((s) => t(`indexers.builtin.modes.${s.type}`)).join(" · ")} ·{" "}
          {def.rss ? t("indexers.builtin.rss") : t("indexers.builtin.noRss")}
        </p>
        <details className="mt-1 text-[11px] text-faint">
          <summary className="cursor-pointer select-none">{t("indexers.builtin.verified")}</summary>
          <p className="mt-1 max-w-prose">{def.verified}</p>
        </details>
        <ErrorAlert error={toggle.error} />
      </div>
      <label className="flex items-center gap-2 text-[12px] text-muted">
        <input type="checkbox" role="switch" className="size-4 accent-[var(--lc-accent)]" checked={on} disabled={toggle.isPending}
          aria-label={t("indexers.builtin.use", { name: def.name })} onChange={(e) => toggle.mutate(e.target.checked)} />
      </label>
    </li>
  );
}

function UsenetForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const catalog = useCatalog();
  const presets = catalog.data?.presets ?? [];
  const [presetId, setPresetId] = useState("");
  const [name, setName] = useState("");
  const [apiKey, setApiKey] = useState("");
  const preset = presets.find((p) => p.id === (presetId || presets[0]?.id));
  const body = (): IndexerInput => ({ ...defaults, name: (name.trim() || preset?.name) ?? "", kind: "newznab", baseUrl: preset?.baseUrl ?? "",
    apiPath: preset?.apiPath ?? "/api", apiKey: apiKey.trim() });
  const test = useMutation({ mutationFn: async () => unwrap(await api.POST("/indexers/test", { body: { indexer: body() } })) });
  const save = useMutation({
    mutationFn: async () => unwrap(await api.POST("/indexers", { body: body() })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.indexers });
      onDone();
    },
  });
  if (catalog.isPending) return <Spinner />;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-3" data-testid="usenet-form">
      <div className="grid gap-3 sm:grid-cols-3">
        <SelectInput label={t("indexers.usenet.preset")} value={preset?.id ?? ""} onChange={(e) => setPresetId(e.target.value)}>
          {presets.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </SelectInput>
        <TextInput label={t("indexers.form.name")} value={name} placeholder={preset?.name} onChange={(e) => setName(e.target.value)} />
        <TextInput label={t("indexers.form.apiKey")} type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
      </div>
      {preset ? (
        <p className="text-[12px] text-faint">
          <span className="font-mono">{preset.baseUrl + preset.apiPath}</span> ·{" "}
          <a className="text-info hover:underline" href={preset.siteUrl} target="_blank" rel="noreferrer noopener">
            {t("indexers.usenet.whereKey", { site: preset.siteUrl.replace(/^https?:\/\//, "") })}
          </a>
        </p>
      ) : null}
      {test.data ? <TestResult result={test.data} /> : null}
      <ErrorAlert error={test.error ?? save.error} />
      <div className="flex gap-2">
        <Button type="submit" variant="primary" busy={save.isPending} disabled={!preset || !apiKey.trim()}>
          {t("indexers.usenet.add")}
        </Button>
        <Button busy={test.isPending} disabled={!preset || !apiKey.trim()} onClick={() => test.mutate()}>
          {t("indexers.test.button")}
        </Button>
      </div>
    </form>
  );
}

/** Edit form of a built-in (or custom) site. */
export function NativeIndexerForm({ indexer, onDone }: { indexer: Indexer; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const catalog = useCatalog();
  const def = catalog.data?.definitions.find((d) => d.id === indexer.definition?.id);
  const links = def?.links ?? [];
  const [name, setName] = useState(indexer.name);
  const [address, setAddress] = useState(indexer.baseUrl);
  const [other, setOther] = useState(!links.includes(indexer.baseUrl) && links.length > 0);
  const [options, setOptions] = useState<Record<string, string>>(indexer.options ?? {});
  const [priority, setPriority] = useState(String(indexer.priority));
  const [minSeeders, setMinSeeders] = useState(String(indexer.minimumSeeders));
  const [enabled, setEnabled] = useState(indexer.enabled);
  const [rss, setRss] = useState(indexer.enableRss);
  const [auto, setAuto] = useState(indexer.enableAutomaticSearch);
  const [interactive, setInteractive] = useState(indexer.enableInteractiveSearch);
  const [cats, setCats] = useState<IndexerCategories>(indexer.categories);
  const [reqLimit, setReqLimit] = useState(indexer.requestLimit?.toString() ?? "");
  const [reqPeriod, setReqPeriod] = useState(indexer.requestPeriodSeconds?.toString() ?? "");
  const [timeout, setTimeoutS] = useState(String(indexer.timeoutSeconds));
  const [flaresolverr, setFlaresolverr] = useState(indexer.flaresolverrUrl ?? "");
  const [tags, setTags] = useState(indexer.tags.join(", "));

  const body = (): IndexerInput => inputFromIndexer(indexer, {
    name: name.trim(), baseUrl: address.trim(), options, priority: num(priority) ?? 25, minimumSeeders: num(minSeeders) ?? 1,
    enabled, enableRss: rss, enableAutomaticSearch: auto, enableInteractiveSearch: interactive, categories: cats,
    requestLimit: num(reqLimit), requestPeriodSeconds: num(reqPeriod), timeoutSeconds: num(timeout) ?? 30,
    flaresolverrUrl: flaresolverr.trim(), tags: tags.split(",").map((s) => s.trim()).filter(Boolean),
  });
  const test = useMutation({
    mutationFn: async () => unwrap(await api.POST("/indexers/test", { body: { indexer: body(), indexerId: indexer.id } })),
  });
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/indexers/{id}", { params: { path: { id: indexer.id } }, body: body() })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.indexers });
      onDone();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Panel title={t("indexers.form.editTitle", { name: indexer.name })}>
      <form onSubmit={submit} className="flex flex-col gap-4" data-testid="native-form">
        <div className="grid gap-3 sm:grid-cols-2">
          <TextInput label={t("indexers.form.name")} value={name} onChange={(e) => setName(e.target.value)} />
          {links.length ? (
            <SelectInput label={t("indexers.builtin.address")} value={other ? "" : address}
              onChange={(e) => {
                if (e.target.value === "") {
                  setOther(true);
                } else {
                  setOther(false);
                  setAddress(e.target.value);
                }
              }}>
              {links.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
              <option value="">{t("indexers.builtin.otherAddress")}</option>
            </SelectInput>
          ) : null}
          {other || !links.length ? (
            <TextInput label={t("indexers.builtin.customAddress")} name="native-address" value={address}
              hint={t("indexers.builtin.addressHint")} onChange={(e) => setAddress(e.target.value)} />
          ) : null}
          <TextInput label={t("indexers.form.priority")} type="number" min={1} max={50} value={priority}
            hint={t("indexers.form.priorityHint")} onChange={(e) => setPriority(e.target.value)} />
          <TextInput label={t("indexers.form.minimumSeeders")} type="number" min={0} value={minSeeders}
            onChange={(e) => setMinSeeders(e.target.value)} />
          <TextInput label={t("indexers.form.tags")} value={tags} hint={t("indexers.form.tagsHint")} onChange={(e) => setTags(e.target.value)} />
          <TextInput label={t("indexers.form.flaresolverr")} value={flaresolverr} placeholder="http://flaresolverr:8191"
            hint={t("indexers.builtin.flaresolverrHint")} onChange={(e) => setFlaresolverr(e.target.value)} />
        </div>
        {def?.settings.length ? (
          <div className="flex flex-col gap-2">
            <p className="text-[12px] font-medium text-muted">{t("indexers.builtin.options")}</p>
            <div className="grid gap-3 sm:grid-cols-3">
              {def.settings.map((s) => {
                const v = options[s.name] ?? s.default;
                const set = (val: string) => setOptions({ ...options, [s.name]: val });
                if (s.type === "select") {
                  return (
                    <SelectInput key={s.name} label={s.label} value={v} onChange={(e) => set(e.target.value)}>
                      {s.options.map((o) => (
                        <option key={o.value} value={o.value}>
                          {o.label}
                        </option>
                      ))}
                    </SelectInput>
                  );
                }
                if (s.type === "bool") {
                  return <Checkbox key={s.name} label={s.label} checked={v === "true"} onChange={(e) => set(String(e.target.checked))} />;
                }
                return <TextInput key={s.name} label={s.label} value={v} onChange={(e) => set(e.target.value)} />;
              })}
            </div>
          </div>
        ) : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <Checkbox label={t("indexers.form.enabled")} checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          {def?.rss !== false ? (
            <Checkbox label={t("indexers.form.enableRss")} checked={rss} onChange={(e) => setRss(e.target.checked)} />
          ) : null}
          <Checkbox label={t("indexers.form.enableAutomatic")} checked={auto} onChange={(e) => setAuto(e.target.checked)} />
          <Checkbox label={t("indexers.form.enableInteractive")} checked={interactive} onChange={(e) => setInteractive(e.target.checked)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <p className="text-[12px] font-medium text-muted">{t("indexers.form.categories")}</p>
          <CategoryPicker caps={indexer.caps ?? definitionCaps(def)} value={cats} onChange={setCats} />
        </div>
        <div className="grid gap-3 sm:grid-cols-3">
          <TextInput label={t("indexers.builtin.rateLimit")} type="number" min={1} value={reqLimit}
            hint={t("indexers.builtin.rateLimitHint")} onChange={(e) => setReqLimit(e.target.value)} />
          <TextInput label={t("indexers.form.requestPeriod")} type="number" min={1} value={reqPeriod} onChange={(e) => setReqPeriod(e.target.value)} />
          <TextInput label={t("indexers.form.timeout")} type="number" min={2} max={120} value={timeout} onChange={(e) => setTimeoutS(e.target.value)} />
        </div>
        {test.data ? <TestResult result={test.data} /> : null}
        <ErrorAlert error={test.error ?? save.error} />
        <div className="flex flex-wrap gap-2">
          <Button type="submit" variant="primary" busy={save.isPending} disabled={!name.trim()}>
            {t("common.save")}
          </Button>
          <Button busy={test.isPending} onClick={() => test.mutate()}>
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

/** Custom definition files: what loaded, what failed, reload. */
export function DefinitionsPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const catalog = useCatalog();
  const reload = useMutation({
    mutationFn: async () => unwrap(await api.POST("/indexers/definitions/reload")),
    onSuccess: (c) => {
      qc.setQueryData(queryKeys.indexerDefinitions, c);
      void qc.invalidateQueries({ queryKey: queryKeys.indexers });
    },
  });
  if (!catalog.data) return null;
  const custom = catalog.data.definitions.filter((d) => !d.builtin || d.overrides);
  const errors = catalog.data.errors;
  return (
    <Panel title={t("indexers.definitions.title")}
      description={t("indexers.definitions.subtitle", { dir: catalog.data.customDir || "/config/definitions" })}
      actions={<Button busy={reload.isPending} onClick={() => reload.mutate()}>{t("indexers.definitions.reload")}</Button>}>
      <div className="flex flex-col gap-2" data-testid="definitions-panel">
        {!custom.length && !errors.length ? <p className="text-[12px] text-faint">{t("indexers.definitions.none")}</p> : null}
        {custom.length ? (
          <ul className="flex flex-col gap-1 text-[13px]">
            {custom.map((d) => (
              <li key={d.id} className="flex flex-wrap items-center gap-2">
                <span className="font-medium text-fg">{d.name}</span>
                <span className="font-mono text-[11px] text-faint">{d.file}</span>
                <span className="font-mono text-[11px] text-faint">{t("indexers.builtin.version", { n: d.version })}</span>
                {d.overrides ? <Badge tone="info">{t("indexers.builtin.overrides")}</Badge> : <Badge tone="info">{t("indexers.builtin.custom")}</Badge>}
              </li>
            ))}
          </ul>
        ) : null}
        {errors.length ? (
          <div className="flex flex-col gap-1" data-testid="definition-errors">
            <p className="text-[12px] font-medium text-danger">{t("indexers.definitions.errors")}</p>
            {errors.map((e) => (
              <Alert key={e.file}>
                <span className="font-mono text-[12px]">{e.file}</span>
                <span className="mt-1 block whitespace-pre-wrap font-mono text-[11px] opacity-80">{e.message}</span>
              </Alert>
            ))}
          </div>
        ) : null}
        {reload.isSuccess ? <Alert tone="success">{t("indexers.definitions.reloaded")}</Alert> : null}
        <ErrorAlert error={reload.error} />
      </div>
    </Panel>
  );
}
