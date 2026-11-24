import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import { api, queryKeys, unwrap, type SubtitleTranslationSettings, type SubtitleTranslator } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { Alert, Badge, Button, Checkbox, ConfirmButton, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { languageName } from "./common";

type Name = "libretranslate" | "deepl" | "openai" | "anthropic" | "gemini" | "openai-compatible";

export function useTranslators() {
  return useQuery({ queryKey: queryKeys.subtitleTranslators, queryFn: async () => unwrap(await api.GET("/subtitles/translators")) });
}

function useLanguageOptions() {
  const q = useQuery({ queryKey: queryKeys.languages, queryFn: async () => unwrap(await api.GET("/languages")) });
  return (q.data ?? []).filter((l) => !["any", "original", "unknown"].includes(l.code));
}

/** Settings → Subtitles → Translation: services with keys, caps, usage and Test; behaviour options. */
export function TranslationSettingsPanel() {
  const { t } = useTranslation();
  const list = useTranslators();
  return (
    <section id="translation" aria-label={t("subtitles.settings.translation.title")} className="space-y-4" data-testid="translation-settings">
      <Panel title={t("subtitles.settings.translation.title")} description={t("subtitles.settings.translation.hint")}>
        {list.isLoading ? <Spinner /> : null}
        <ErrorAlert error={list.error} />
        <ul className="space-y-3" aria-label={t("subtitles.settings.translation.services")}>
          {(list.data ?? []).map((tr) => <TranslatorCard key={tr.name} tr={tr} />)}
        </ul>
      </Panel>
      <Behaviour translators={list.data ?? []} />
    </section>
  );
}

function amount(n: number, locale: string) {
  return new Intl.NumberFormat(locale).format(n);
}

function TranslatorCard({ tr }: { tr: SubtitleTranslator }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const langs = useLanguageOptions();
  const [enabled, setEnabled] = useState(tr.enabled);
  const [baseUrl, setBaseUrl] = useState(tr.baseUrl);
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState(tr.model);
  const [formality, setFormality] = useState(tr.formality);
  const [glossary, setGlossary] = useState(tr.glossaryId);
  const [cap, setCap] = useState(String(tr.monthlyCap));
  const [sample, setSample] = useState("Where are you going? <i>Wait for me!</i>");
  const [to, setTo] = useState("hr");
  useEffect(() => {
    setEnabled(tr.enabled); setBaseUrl(tr.baseUrl); setModel(tr.model); setFormality(tr.formality); setGlossary(tr.glossaryId); setCap(String(tr.monthlyCap));
  }, [tr.enabled, tr.baseUrl, tr.model, tr.formality, tr.glossaryId, tr.monthlyCap]);
  const name = tr.name as Name;
  const refresh = () => void qc.invalidateQueries({ queryKey: queryKeys.subtitleTranslators });
  const has = (f: SubtitleTranslator["fields"][number]) => tr.fields.includes(f);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/subtitles/translators/{name}", { params: { path: { name } }, body: {
      enabled, monthlyCap: Math.max(0, Math.round(Number(cap) || 0)),
      ...(has("baseUrl") ? { baseUrl } : {}), ...(has("model") ? { model } : {}), ...(apiKey ? { apiKey } : {}),
      ...(has("formality") ? { formality: formality as "" | "default" | "prefer_more" | "prefer_less" } : {}), ...(has("glossaryId") ? { glossaryId: glossary } : {}),
    } })),
    onSuccess: () => { setApiKey(""); refresh(); },
  });
  const clearKey = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/subtitles/translators/{name}", { params: { path: { name } }, body: { apiKey: "", enabled: false } })),
    onSuccess: refresh,
  });
  const test = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/translators/{name}/test", { params: { path: { name } }, body: { text: sample, from: "en", to } })),
    onSettled: refresh,
  });
  const reset = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/translators/{name}/health/reset", { params: { path: { name } } })),
    onSuccess: refresh,
  });
  const unit = t(`subtitles.settings.translation.units_${tr.unit}`);
  const used = tr.unit === "tokens" ? tr.month.inputTokens + tr.month.outputTokens : tr.month.characters;
  const capReached = tr.monthlyCap > 0 && used >= tr.monthlyCap;
  const backoff = tr.retryAt && new Date(tr.retryAt) > new Date();
  const ready = (!tr.needsKey || tr.apiKey.configured) && (!tr.needsUrl || tr.baseUrl);
  const health = !tr.enabled ? "neutral" : capReached || backoff ? "danger" : tr.failures > 0 ? "warning" : "success";
  return (
    <li className="rounded border border-line bg-surface-2 p-3" data-testid="subtitle-translator" data-name={tr.name}>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h3 className="text-[14px] font-semibold text-fg">{t(`subtitles.translatorNames.${tr.name}`, { defaultValue: tr.name })}</h3>
        <Badge tone={health}>{!tr.enabled ? t("subtitles.settings.disabled") : capReached ? t("subtitles.settings.translation.capReached")
          : backoff ? t("subtitles.settings.backoff") : t("subtitles.settings.healthy")}</Badge>
        <Badge tone={tr.local ? "success" : "neutral"}>{tr.local ? t("subtitles.settings.translation.local") : t("subtitles.settings.translation.cloud")}</Badge>
        <a href={tr.website} target="_blank" rel="noreferrer noopener" className="ml-auto text-[12px] text-accent hover:underline">
          {tr.local ? tr.website.replace(/^https:\/\//, "") : t("subtitles.settings.getKey")}
        </a>
      </div>
      <dl className="mb-2 grid gap-x-4 gap-y-1 text-[12px] sm:grid-cols-2" data-testid="translator-usage">
        <div><dt className="inline text-faint">{t("subtitles.settings.translation.usage")}: </dt>
          <dd className="inline text-fg">{t("subtitles.settings.translation.usageValue", { used: amount(used, i18n.language), unit,
            subtitles: tr.month.subtitles, requests: tr.month.requests })}
            {tr.monthlyCap > 0 ? <span className="text-faint"> {t("subtitles.settings.translation.ofCap", { cap: amount(tr.monthlyCap, i18n.language) })}</span> : null}</dd></div>
        <div><dt className="inline text-faint">{t("subtitles.settings.lastOk")}: </dt>
          <dd className="inline text-fg">{tr.lastSuccessAt ? formatDateTime(tr.lastSuccessAt, i18n.language) : t("common.never")}</dd></div>
        {tr.quotaLimit !== undefined && tr.quotaUsed !== undefined ? (
          <div className="sm:col-span-2 text-muted">{t("subtitles.settings.translation.providerQuota", { used: amount(tr.quotaUsed, i18n.language),
            limit: amount(tr.quotaLimit, i18n.language) })}</div>
        ) : null}
      </dl>
      {tr.lastError ? (
        <Alert tone={backoff || capReached ? "danger" : "warning"}>
          {t(`subtitles.errorCodes.${tr.lastErrorCode}`, { defaultValue: tr.lastError })}
          {tr.lastErrorAt ? <span className="text-faint"> · {formatDateTime(tr.lastErrorAt, i18n.language)}</span> : null}
          {backoff && tr.retryAt ? <span> · {t("subtitles.settings.retryAt", { at: formatDateTime(tr.retryAt, i18n.language) })}</span> : null}
        </Alert>
      ) : null}
      <div className="mt-2 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <div className="self-center"><Checkbox label={t("subtitles.settings.enabled")} checked={enabled} onChange={(e) => setEnabled(e.target.checked)} data-testid={`translator-enabled-${tr.name}`} /></div>
        {has("baseUrl") ? (
          <TextInput label={t("subtitles.settings.translation.baseUrl")} value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} inputMode="url"
            hint={t("subtitles.settings.translation.baseUrlHint")} data-testid={`translator-url-${tr.name}`} />
        ) : null}
        {has("apiKey") ? (
          <TextInput label={t("subtitles.settings.apiKey")} type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
            placeholder={tr.apiKey.configured ? t("subtitles.settings.keySet") : ""} data-testid={`translator-key-${tr.name}`}
            hint={tr.apiKey.configured ? undefined : t("subtitles.settings.keyMissing")} />
        ) : null}
        {has("model") ? (
          tr.models.length > 0 ? (
            <SelectInput label={t("subtitles.settings.translation.model")} value={model} onChange={(e) => setModel(e.target.value)} data-testid={`translator-model-${tr.name}`}>
              <option value="">{t("subtitles.settings.translation.modelHint", { model: tr.defaultModel })}</option>
              {[...new Set([...tr.models, ...(model ? [model] : [])])].map((m) => <option key={m} value={m}>{m}</option>)}
            </SelectInput>
          ) : (
            <TextInput label={t("subtitles.settings.translation.model")} value={model} onChange={(e) => setModel(e.target.value)} placeholder="llama3.1:8b"
              data-testid={`translator-model-${tr.name}`} />
          )
        ) : null}
        {has("formality") ? (
          <SelectInput label={t("subtitles.settings.translation.formality")} value={formality} onChange={(e) => setFormality(e.target.value)}>
            <option value="">{t("subtitles.settings.translation.formalityDefault")}</option>
            <option value="prefer_more">{t("subtitles.settings.translation.formalityMore")}</option>
            <option value="prefer_less">{t("subtitles.settings.translation.formalityLess")}</option>
          </SelectInput>
        ) : null}
        {has("glossaryId") ? (
          <TextInput label={t("subtitles.settings.translation.glossary")} value={glossary} onChange={(e) => setGlossary(e.target.value)} autoComplete="off" />
        ) : null}
        <TextInput label={t("subtitles.settings.translation.cap", { unit })} inputMode="numeric" value={cap} onChange={(e) => setCap(e.target.value)}
          hint={t("subtitles.settings.translation.capHint")} data-testid={`translator-cap-${tr.name}`} />
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending} data-testid={`translator-save-${tr.name}`}>{t("common.save")}</Button>
        {tr.failures > 0 || backoff ? <Button variant="ghost" onClick={() => reset.mutate()} busy={reset.isPending}>{t("subtitles.settings.resetHealth")}</Button> : null}
        {tr.apiKey.configured ? <ConfirmButton label={t("subtitles.settings.removeKey")} confirmLabel={t("subtitles.settings.removeKeyConfirm")} onConfirm={() => clearKey.mutate()} /> : null}
      </div>
      <div className="mt-3 flex flex-wrap items-end gap-2 border-t border-line pt-3">
        <div className="min-w-64 flex-1"><TextInput label={t("subtitles.settings.translation.testText")} value={sample} onChange={(e) => setSample(e.target.value)} maxLength={500} /></div>
        <div className="w-44">
          <SelectInput label={t("subtitles.settings.translation.testTo")} value={to} onChange={(e) => setTo(e.target.value)}>
            {langs.filter((l) => l.code !== "en").map((l) => <option key={l.code} value={l.code}>{languageName(t, l.code)}</option>)}
          </SelectInput>
        </div>
        <Button onClick={() => test.mutate()} busy={test.isPending} disabled={!ready} data-testid={`translator-test-${tr.name}`}>{t("subtitles.settings.test")}</Button>
      </div>
      {test.data ? <p className="mt-2 text-[13px] text-success" role="status" data-testid="translator-test-result">{t("subtitles.settings.translation.testResult", { text: test.data.output })}</p> : null}
      <ErrorAlert error={save.error ?? test.error ?? reset.error ?? clearKey.error} />
    </li>
  );
}

function Behaviour({ translators }: { translators: SubtitleTranslator[] }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const langs = useLanguageOptions();
  const st = useQuery({ queryKey: queryKeys.subtitleTranslation, queryFn: async () => unwrap(await api.GET("/subtitles/translation")) });
  const [form, setForm] = useState<SubtitleTranslationSettings | undefined>(st.data);
  const [sources, setSources] = useState("");
  useEffect(() => { if (st.data) { setForm(st.data); setSources(st.data.sourceLanguages.join(", ")); } }, [st.data]);
  const save = useMutation({
    mutationFn: async () => form && unwrap(await api.PUT("/subtitles/translation", { body: { ...form,
      sourceLanguages: sources.split(/[,\s]+/).map((s) => s.trim().toLowerCase()).filter(Boolean) } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.subtitleTranslation }),
  });
  if (st.isLoading || !form) return st.error ? <ErrorAlert error={st.error} /> : <Spinner />;
  const set = <K extends keyof SubtitleTranslationSettings>(k: K, v: SubtitleTranslationSettings[K]) => setForm({ ...form, [k]: v });
  const perLang = Object.entries(form.languageTranslators);
  const setLang = (entries: [string, string][]) => set("languageTranslators", Object.fromEntries(entries));
  const services = (
    <>
      {translators.map((x) => <option key={x.name} value={x.name}>{t(`subtitles.translatorNames.${x.name}`, { defaultValue: x.name })}{x.enabled ? "" : ` (${t("subtitles.settings.disabled")})`}</option>)}
    </>
  );
  return (
    <Panel title={t("subtitles.settings.translation.behaviour")}>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <SelectInput label={t("subtitles.settings.translation.defaultTranslator")} value={form.defaultTranslator} onChange={(e) => set("defaultTranslator", e.target.value)}
          data-testid="translation-default">
          <option value="">{t("subtitles.settings.translation.firstEnabled")}</option>
          {services}
        </SelectInput>
        <TextInput label={t("subtitles.settings.translation.suffix")} value={form.suffix} maxLength={20}
          onChange={(e) => set("suffix", e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
          hint={t("subtitles.settings.translation.suffixHint", { suffix: form.suffix || "…" })} data-testid="translation-suffix" />
        <TextInput label={t("subtitles.settings.translation.sources")} value={sources} onChange={(e) => setSources(e.target.value)}
          hint={t("subtitles.settings.translation.sourcesHint")} />
        <TextInput label={t("subtitles.settings.translation.perHour")} inputMode="numeric" value={form.perHour}
          onChange={(e) => set("perHour", Math.min(Math.max(0, Math.round(Number(e.target.value) || 0)), 500))} />
        <div className="self-center sm:col-span-2">
          <Checkbox label={t("subtitles.settings.translation.autoSync")} description={t("subtitles.settings.translation.autoSyncHint")} checked={form.autoSync}
            onChange={(e) => set("autoSync", e.target.checked)} />
        </div>
      </div>
      <div className="mt-4">
        <h3 className="text-[13px] font-medium text-fg">{t("subtitles.settings.translation.perLanguage")}</h3>
        <p className="mb-2 text-[12px] text-muted">{t("subtitles.settings.translation.perLanguageHint")}</p>
        <ul className="space-y-2">
          {perLang.map(([lang, svc], i) => (
            <li key={i} className="flex flex-wrap items-end gap-2" data-testid="translation-language">
              <div className="w-48">
                <SelectInput label={t("subtitles.settings.language")} value={lang}
                  onChange={(e) => setLang(perLang.map((p, j) => (j === i ? [e.target.value, p[1]] : p)))}>
                  {langs.map((l) => <option key={l.code} value={l.code}>{languageName(t, l.code)}</option>)}
                </SelectInput>
              </div>
              <div className="w-56">
                <SelectInput label={t("subtitles.settings.translation.profileService")} value={svc}
                  onChange={(e) => setLang(perLang.map((p, j) => (j === i ? [p[0], e.target.value] : p)))}>
                  {services}
                  <option value="none">{t("subtitles.settings.translation.never")}</option>
                </SelectInput>
              </div>
              <Button variant="ghost" onClick={() => setLang(perLang.filter((_, j) => j !== i))} aria-label={t("common.delete")}><Trash2 className="size-3.5" /></Button>
            </li>
          ))}
        </ul>
        <Button className="mt-2" onClick={() => {
          const free = langs.find((l) => !(l.code in form.languageTranslators))?.code ?? "hr";
          setLang([...perLang, [free, translators.find((x) => x.enabled)?.name ?? translators[0]?.name ?? "libretranslate"]]);
        }}><Plus className="size-3.5" /> {t("subtitles.settings.translation.addLanguage")}</Button>
      </div>
      <div className="mt-3 flex items-center gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending} data-testid="save-translation-settings">{t("common.save")}</Button>
        {save.isSuccess ? <span className="text-[12px] text-success" role="status">{t("common.saved")}</span> : null}
        <ErrorAlert error={save.error} />
      </div>
    </Panel>
  );
}

/** The translation choice of a profile language. */
export function ProfileTranslatorSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation();
  const list = useTranslators();
  return (
    <SelectInput label={t("subtitles.settings.translation.profileService")} value={value} onChange={(e) => onChange(e.target.value)} data-testid="profile-translator">
      <option value="">{t("subtitles.settings.translation.profileDefault")}</option>
      {(list.data ?? []).map((x) => <option key={x.name} value={x.name}>{t(`subtitles.translatorNames.${x.name}`, { defaultValue: x.name })}</option>)}
      <option value="none">{t("subtitles.settings.translation.never")}</option>
    </SelectInput>
  );
}
