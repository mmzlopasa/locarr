import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { api, queryKeys, unwrap, type SubtitleProfile, type SubtitleProvider, type SubtitleWant } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { Alert, Badge, Button, Checkbox, ConfirmButton, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { languageName, useSubtitleProfiles } from "./common";
import { ProfileTranslatorSelect, TranslationSettingsPanel } from "./TranslationSettings";

/** Settings → Subtitles: language profiles, providers with health and quota, options. */
export function SubtitleSettingsPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <PageHeader title={t("subtitles.settings.title")} description={t("subtitles.settings.subtitle")} />
      <Options />
      <Profiles />
      <Providers />
      <TranslationSettingsPanel />
      <ApplyToLibrary />
    </div>
  );
}

function useLanguages() {
  return useQuery({ queryKey: queryKeys.languages, queryFn: async () => unwrap(await api.GET("/languages")) });
}

function Options() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const profiles = useSubtitleProfiles();
  const st = useQuery({ queryKey: queryKeys.subtitleSettings, queryFn: async () => unwrap(await api.GET("/subtitles/settings")) });
  const [form, setForm] = useState(st.data);
  useEffect(() => { if (st.data) setForm(st.data); }, [st.data]);
  const save = useMutation({
    mutationFn: async () => {
      if (!form) return undefined;
      const { syncAvailable: _s, ...body } = form;
      void _s;
      return unwrap(await api.PUT("/subtitles/settings", { body }));
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.subtitleSettings }),
  });
  if (st.isLoading || !form) return <Spinner />;
  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) => setForm({ ...form, [k]: v });
  const num = (v: string) => Math.max(0, Math.round(Number(v) || 0));
  return (
    <Panel title={t("subtitles.settings.options")}>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <SelectInput label={t("subtitles.settings.defaultMovies")} value={form.defaultProfileMovies} data-testid="default-movies"
          onChange={(e) => set("defaultProfileMovies", Number(e.target.value))}>
          <option value={0}>{t("subtitles.settings.noDefault")}</option>
          {(profiles.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("subtitles.settings.defaultSeries")} value={form.defaultProfileSeries} data-testid="default-series"
          onChange={(e) => set("defaultProfileSeries", Number(e.target.value))}>
          <option value={0}>{t("subtitles.settings.noDefault")}</option>
          {(profiles.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <TextInput label={t("subtitles.settings.minMovies")} inputMode="numeric" value={form.minScoreMovies} hint={t("subtitles.settings.minHint")}
          onChange={(e) => set("minScoreMovies", Math.min(num(e.target.value), 100))} />
        <TextInput label={t("subtitles.settings.minEpisodes")} inputMode="numeric" value={form.minScoreEpisodes}
          onChange={(e) => set("minScoreEpisodes", Math.min(num(e.target.value), 100))} />
        <SelectInput label={t("subtitles.settings.format")} value={form.preferredFormat} onChange={(e) => set("preferredFormat", e.target.value as "" | "srt")}>
          <option value="">{t("subtitles.settings.formatKeep")}</option>
          <option value="srt">{t("subtitles.settings.formatSrt")}</option>
        </SelectInput>
        <TextInput label={t("subtitles.settings.upgradeDays")} inputMode="numeric" value={form.upgradeDays} hint={t("subtitles.settings.upgradeHint")}
          onChange={(e) => set("upgradeDays", Math.min(num(e.target.value), 365))} />
        <TextInput label={t("subtitles.settings.perHour")} inputMode="numeric" value={form.searchesPerHour} hint={t("subtitles.settings.perHourHint")}
          onChange={(e) => set("searchesPerHour", Math.min(num(e.target.value), 1000))} />
        <div className="self-center">
          <Checkbox label={t("subtitles.settings.autoSync")} description={form.syncAvailable ? t("subtitles.settings.autoSyncHint") : t("subtitles.sync.unavailable")}
            checked={form.autoSync} disabled={!form.syncAvailable} onChange={(e) => set("autoSync", e.target.checked)} />
        </div>
      </div>
      <div className="mt-3 flex items-center gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending} data-testid="save-subtitle-settings">{t("common.save")}</Button>
        {save.isSuccess ? <span className="text-[12px] text-success" role="status">{t("common.saved")}</span> : null}
        <ErrorAlert error={save.error} />
      </div>
    </Panel>
  );
}

function Profiles() {
  const { t } = useTranslation();
  const profiles = useSubtitleProfiles();
  const [editing, setEditing] = useState<SubtitleProfile | "new" | null>(null);
  return (
    <Panel title={t("subtitles.settings.profiles")} description={t("subtitles.settings.profilesHint")}
      actions={<Button onClick={() => setEditing("new")} data-testid="add-subtitle-profile"><Plus className="size-3.5" /> {t("subtitles.settings.addProfile")}</Button>}>
      {profiles.isLoading ? <Spinner /> : null}
      <ErrorAlert error={profiles.error} />
      <ul className="divide-y divide-line">
        {(profiles.data ?? []).map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-2 py-2" data-testid="subtitle-profile">
            <span className="text-[13px] font-medium text-fg">{p.name}</span>
            <span className="text-[12px] text-muted">{p.languages.map((w) => wantText(t, w)).join(" → ")}</span>
            {p.cutoff ? <Badge>{t("subtitles.settings.cutoffShort")}</Badge> : null}
            {p.translateAfterHours > 0 ? <Badge tone="info">{t("subtitles.settings.translation.title")} · {p.translateAfterHours} h</Badge> : null}
            <Button variant="ghost" className="ml-auto" onClick={() => setEditing(p)}>{t("subtitles.settings.edit")}</Button>
          </li>
        ))}
      </ul>
      {editing ? <ProfileForm profile={editing === "new" ? null : editing} onDone={() => setEditing(null)} /> : null}
    </Panel>
  );
}

function wantText(t: ReturnType<typeof useTranslation>["t"], w: SubtitleWant): string {
  const parts = [languageName(t, w.language)];
  if (w.forced) parts.push(t("subtitles.forced"));
  if (w.hearingImpaired !== "include") parts.push(t(`subtitles.hiModes.${w.hearingImpaired}`));
  return parts.join(" · ");
}

function ProfileForm({ profile, onDone }: { profile: SubtitleProfile | null; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const langs = useLanguages();
  const [name, setName] = useState(profile?.name ?? "");
  const [wants, setWants] = useState<SubtitleWant[]>(profile?.languages ?? [{ language: "hr", forced: false, hearingImpaired: "include" }]);
  const [skip, setSkip] = useState(profile?.skipEmbedded ?? true);
  const [cutoff, setCutoff] = useState(profile?.cutoff ?? false);
  const [after, setAfter] = useState(String(profile?.translateAfterHours ?? 0));
  const done = () => { void qc.invalidateQueries({ queryKey: queryKeys.subtitleProfiles }); onDone(); };
  const save = useMutation({
    mutationFn: async () => {
      const body = { name, languages: wants, skipEmbedded: skip, cutoff, translateAfterHours: Math.min(Math.max(0, Math.round(Number(after) || 0)), 8760) };
      return profile
        ? unwrap(await api.PUT("/subtitles/profiles/{id}", { params: { path: { id: profile.id } }, body }))
        : unwrap(await api.POST("/subtitles/profiles", { body }));
    },
    onSuccess: done,
  });
  const del = useMutation({
    mutationFn: async () => profile && unwrap(await api.DELETE("/subtitles/profiles/{id}", { params: { path: { id: profile.id } } })),
    onSuccess: done,
  });
  const update = (i: number, w: Partial<SubtitleWant>) => setWants(wants.map((x, j) => (j === i ? { ...x, ...w } : x)));
  const move = (i: number, d: -1 | 1) => {
    const n = [...wants];
    const a = n[i], b = n[i + d];
    if (!a || !b) return;
    n[i] = b;
    n[i + d] = a;
    setWants(n);
  };
  const options = (langs.data ?? []).filter((l) => !["any", "original", "unknown"].includes(l.code));
  return (
    <div className="mt-3 space-y-3 rounded border border-line bg-surface-2 p-3" data-testid="subtitle-profile-form">
      <div className="max-w-sm"><TextInput label={t("subtitles.settings.profileName")} value={name} onChange={(e) => setName(e.target.value)} data-testid="profile-name" /></div>
      <ol className="space-y-2" aria-label={t("subtitles.settings.languages")}>
        {wants.map((w, i) => (
          <li key={i} className="flex flex-wrap items-end gap-2" data-testid="profile-language">
            <span className="w-5 pb-2 text-[12px] tabular-nums text-faint">{i + 1}.</span>
            <div className="w-48">
              <SelectInput label={t("subtitles.settings.language")} value={w.language} onChange={(e) => update(i, { language: e.target.value })}>
                {options.map((l) => <option key={l.code} value={l.code}>{languageName(t, l.code)}</option>)}
              </SelectInput>
            </div>
            <div className="w-56">
              <SelectInput label={t("subtitles.settings.hearingImpaired")} value={w.hearingImpaired}
                onChange={(e) => update(i, { hearingImpaired: e.target.value as SubtitleWant["hearingImpaired"] })}>
                {(["include", "prefer", "exclude", "only"] as const).map((m) => <option key={m} value={m}>{t(`subtitles.hiModes.${m}`)}</option>)}
              </SelectInput>
            </div>
            <div className="pb-1.5"><Checkbox label={t("subtitles.settings.forcedOnly")} checked={w.forced} onChange={(e) => update(i, { forced: e.target.checked })} /></div>
            <div className="w-48"><ProfileTranslatorSelect value={w.translator ?? ""} onChange={(v) => update(i, { translator: v })} /></div>
            <Button variant="ghost" disabled={i === 0} onClick={() => move(i, -1)} aria-label={t("subtitles.settings.up")}><ArrowUp className="size-3.5" /></Button>
            <Button variant="ghost" disabled={i === wants.length - 1} onClick={() => move(i, 1)} aria-label={t("subtitles.settings.down")}><ArrowDown className="size-3.5" /></Button>
            <Button variant="ghost" disabled={wants.length === 1} onClick={() => setWants(wants.filter((_, j) => j !== i))} aria-label={t("common.delete")}>
              <Trash2 className="size-3.5" />
            </Button>
          </li>
        ))}
      </ol>
      <Button onClick={() => setWants([...wants, { language: "en", forced: false, hearingImpaired: "include" }])} data-testid="profile-add-language">
        <Plus className="size-3.5" /> {t("subtitles.settings.addLanguage")}
      </Button>
      <div className="space-y-2">
        <Checkbox label={t("subtitles.settings.skipEmbedded")} description={t("subtitles.settings.skipEmbeddedHint")} checked={skip} onChange={(e) => setSkip(e.target.checked)} />
        <Checkbox label={t("subtitles.settings.cutoff")} description={t("subtitles.settings.cutoffHint")} checked={cutoff} onChange={(e) => setCutoff(e.target.checked)} />
      </div>
      <div className="max-w-sm">
        <TextInput label={t("subtitles.settings.translation.profileAfter")} inputMode="numeric" value={after} onChange={(e) => setAfter(e.target.value)}
          hint={t("subtitles.settings.translation.profileAfterHint")} data-testid="profile-translate-after" />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending} disabled={!name.trim()} data-testid="profile-save">{t("common.save")}</Button>
        <Button variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
        {profile ? <ConfirmButton label={t("common.delete")} confirmLabel={t("subtitles.settings.deleteProfile")} onConfirm={() => del.mutate()} busy={del.isPending} /> : null}
      </div>
      <ErrorAlert error={save.error ?? del.error} />
    </div>
  );
}

function Providers() {
  const { t } = useTranslation();
  const list = useQuery({ queryKey: queryKeys.subtitleProviders, queryFn: async () => unwrap(await api.GET("/subtitles/providers")), refetchInterval: 15_000 });
  return (
    <Panel title={t("subtitles.settings.providers")} description={t("subtitles.settings.providersHint")}>
      {list.isLoading ? <Spinner /> : null}
      <ErrorAlert error={list.error} />
      <ul className="space-y-3">
        {(list.data ?? []).map((p) => <ProviderCard key={p.name} p={p} />)}
      </ul>
    </Panel>
  );
}

function ProviderCard({ p }: { p: SubtitleProvider }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [enabled, setEnabled] = useState(p.enabled);
  const [priority, setPriority] = useState(String(p.priority));
  const [username, setUsername] = useState(p.username);
  const [apiKey, setApiKey] = useState("");
  const [password, setPassword] = useState("");
  useEffect(() => { setEnabled(p.enabled); setPriority(String(p.priority)); setUsername(p.username); }, [p.enabled, p.priority, p.username]);
  const refresh = () => void qc.invalidateQueries({ queryKey: queryKeys.subtitleProviders });
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/subtitles/providers/{name}", { params: { path: { name: p.name as "opensubtitles" } }, body: {
      enabled, priority: Math.min(Math.max(Math.round(Number(priority) || 25), 1), 100), ...(p.fields.includes("username") ? { username } : {}),
      ...(apiKey ? { apiKey } : {}), ...(password ? { password } : {}) } })),
    onSuccess: () => { setApiKey(""); setPassword(""); refresh(); },
  });
  const clearKey = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/subtitles/providers/{name}", { params: { path: { name: p.name as "opensubtitles" } }, body: { apiKey: "", enabled: false } })),
    onSuccess: refresh,
  });
  const test = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/providers/{name}/test", { params: { path: { name: p.name as "opensubtitles" } } })),
    onSettled: refresh,
  });
  const reset = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/providers/{name}/health/reset", { params: { path: { name: p.name as "opensubtitles" } } })),
    onSuccess: refresh,
  });
  const backoff = p.retryAt && new Date(p.retryAt) > new Date();
  const quotaOut = p.quotaRemaining === 0 && (!p.quotaResetAt || new Date(p.quotaResetAt) > new Date());
  const health = !p.enabled ? "neutral" : backoff || quotaOut ? "danger" : p.failures > 0 ? "warning" : "success";
  return (
    <li className="rounded border border-line bg-surface-2 p-3" data-testid="subtitle-provider" data-name={p.name}>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h3 className="text-[14px] font-semibold text-fg">{t(`subtitles.providerNames.${p.name}`, { defaultValue: p.name })}</h3>
        <Badge tone={health}>{!p.enabled ? t("subtitles.settings.disabled") : backoff ? t("subtitles.settings.backoff") : quotaOut ? t("subtitles.settings.quotaOut")
          : t("subtitles.settings.healthy")}</Badge>
        {p.languages.length ? <span className="text-[12px] text-muted">{t("subtitles.settings.onlyLanguages", { languages: p.languages.map((l) => languageName(t, l)).join(", ") })}</span> : null}
        <a href={p.website} target="_blank" rel="noreferrer noopener" className="ml-auto text-[12px] text-accent hover:underline">{t("subtitles.settings.getKey")}</a>
      </div>
      <dl className="mb-2 grid gap-x-4 gap-y-1 text-[12px] sm:grid-cols-3" data-testid="provider-health">
        <div><dt className="inline text-faint">{t("subtitles.settings.quota")}: </dt>
          <dd className="inline text-fg">{p.quotaRemaining !== undefined ? t("subtitles.settings.quotaLeft", { left: p.quotaRemaining, limit: p.quotaLimit ?? "?" }) : t("subtitles.settings.quotaUnknown")}
            {p.quotaResetAt ? <span className="text-faint"> · {t("subtitles.settings.quotaReset", { at: formatDateTime(p.quotaResetAt, i18n.language) })}</span> : null}</dd></div>
        <div><dt className="inline text-faint">{t("subtitles.settings.today")}: </dt><dd className="inline text-fg">{p.downloadsToday}</dd></div>
        <div><dt className="inline text-faint">{t("subtitles.settings.lastOk")}: </dt>
          <dd className="inline text-fg">{p.lastSuccessAt ? formatDateTime(p.lastSuccessAt, i18n.language) : t("common.never")}</dd></div>
        {p.account ? <div className="sm:col-span-3"><dt className="inline text-faint">{t("subtitles.settings.account")}: </dt><dd className="inline text-fg">{p.account}</dd></div> : null}
      </dl>
      {p.lastError ? (
        <Alert tone={backoff ? "danger" : "warning"}>
          {t(`subtitles.errorCodes.${p.lastErrorCode}`, { defaultValue: p.lastError })}
          {p.lastErrorAt ? <span className="text-faint"> · {formatDateTime(p.lastErrorAt, i18n.language)}</span> : null}
          {backoff && p.retryAt ? <span> · {t("subtitles.settings.retryAt", { at: formatDateTime(p.retryAt, i18n.language) })}</span> : null}
        </Alert>
      ) : null}
      <div className="mt-2 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div className="self-center"><Checkbox label={t("subtitles.settings.enabled")} checked={enabled} onChange={(e) => setEnabled(e.target.checked)} data-testid={`provider-enabled-${p.name}`} /></div>
        <TextInput label={t("subtitles.settings.priority")} inputMode="numeric" value={priority} onChange={(e) => setPriority(e.target.value)} hint={t("subtitles.settings.priorityHint")} />
        {p.fields.includes("apiKey") ? (
          <TextInput label={t("subtitles.settings.apiKey")} type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
            placeholder={p.apiKey.configured ? t("subtitles.settings.keySet") : ""} data-testid={`provider-key-${p.name}`}
            hint={p.apiKey.configured ? undefined : t("subtitles.settings.keyMissing")} />
        ) : null}
        {p.fields.includes("username") ? (
          <TextInput label={t("subtitles.settings.username")} autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} hint={t("subtitles.settings.usernameHint")} />
        ) : null}
        {p.fields.includes("password") ? (
          <TextInput label={t("subtitles.settings.password")} type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)}
            placeholder={p.password.configured ? t("subtitles.settings.keySet") : ""} />
        ) : null}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending} data-testid={`provider-save-${p.name}`}>{t("common.save")}</Button>
        <Button onClick={() => test.mutate()} busy={test.isPending} disabled={!p.apiKey.configured}>{t("subtitles.settings.test")}</Button>
        {p.failures > 0 || backoff ? <Button variant="ghost" onClick={() => reset.mutate()} busy={reset.isPending}>{t("subtitles.settings.resetHealth")}</Button> : null}
        {p.apiKey.configured ? <ConfirmButton label={t("subtitles.settings.removeKey")} confirmLabel={t("subtitles.settings.removeKeyConfirm")} onConfirm={() => clearKey.mutate()} /> : null}
        {test.isSuccess ? <span className="text-[12px] text-success" role="status">{t("subtitles.settings.testOk")}</span> : null}
      </div>
      <ErrorAlert error={save.error ?? test.error ?? reset.error ?? clearKey.error} />
    </li>
  );
}

function ApplyToLibrary() {
  const { t } = useTranslation();
  const profiles = useSubtitleProfiles();
  const [kind, setKind] = useState<"movies" | "series">("movies");
  const [profile, setProfile] = useState(0);
  const apply = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/profiles/assign", { body: { all: kind, profileId: profile } })),
  });
  const scan = useMutation({ mutationFn: async () => unwrap(await api.POST("/subtitles/scan")) });
  return (
    <Panel title={t("subtitles.settings.bulk")} description={t("subtitles.settings.bulkHint")}>
      <div className="flex flex-wrap items-end gap-2">
        <div className="w-44">
          <SelectInput label={t("subtitles.settings.bulkKind")} value={kind} onChange={(e) => setKind(e.target.value as "movies" | "series")}>
            <option value="movies">{t("nav.movies")}</option>
            <option value="series">{t("nav.series")}</option>
          </SelectInput>
        </div>
        <div className="w-56">
          <ProfileOptions label={t("subtitles.settings.bulkProfile")} value={profile} onChange={setProfile} profiles={profiles.data ?? []} />
        </div>
        <ConfirmButton label={t("subtitles.settings.bulkApply")} confirmLabel={t("subtitles.settings.bulkConfirm")} onConfirm={() => apply.mutate()} busy={apply.isPending} testId="bulk-apply" />
        <Button onClick={() => scan.mutate()} busy={scan.isPending} data-testid="subtitle-scan">{t("subtitles.settings.scan")}</Button>
      </div>
      {apply.data ? <p className="mt-2 text-[12px] text-success" role="status">{t("subtitles.settings.bulkDone", { count: apply.data.count })}</p> : null}
      {scan.isSuccess ? <p className="mt-2 text-[12px] text-success" role="status">{t("subtitles.settings.scanQueued")}</p> : null}
      <ErrorAlert error={apply.error ?? scan.error} />
    </Panel>
  );
}

/** A profile select with "default" (0) and "none" (-1). */
export function ProfileOptions({ label, value, onChange, profiles, keep }: { label: string; value: number | ""; onChange: (v: number) => void;
  profiles: SubtitleProfile[]; keep?: boolean }) {
  const { t } = useTranslation();
  return (
    <SelectInput label={label} value={value} onChange={(e) => onChange(e.target.value === "" ? Number.NaN : Number(e.target.value))} data-testid="subtitle-profile-select">
      {keep ? <option value="">{t("subtitles.picker.keep")}</option> : null}
      <option value={0}>{t("subtitles.picker.default")}</option>
      <option value={-1}>{t("subtitles.picker.none")}</option>
      {profiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
    </SelectInput>
  );
}
