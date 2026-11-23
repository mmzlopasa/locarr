import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  api,
  queryKeys,
  unwrap,
  type KindDefaults,
  type MinimumAvailability,
  type QuotaRule,
  type RequestSettings,
} from "@/api/client";
import { Alert, Button, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useMetadataProfiles } from "@/features/music/common";

type Kind = "movie" | "series" | "music";
const KINDS: Kind[] = ["movie", "series", "music"];
const libraryKind = (k: Kind) => (k === "movie" ? "movies" : k);

export function RequestSettingsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: queryKeys.requestSettings, queryFn: async () => unwrap(await api.GET("/settings/requests")) });
  const profiles = useQuery({ queryKey: queryKeys.qualityProfiles, queryFn: async () => unwrap(await api.GET("/qualityprofiles")) });
  const roots = useQuery({ queryKey: queryKeys.rootFolders, queryFn: async () => unwrap(await api.GET("/rootfolders")) });
  const metaProfiles = useMetadataProfiles();
  const [form, setForm] = useState<RequestSettings | null>(null);
  useEffect(() => {
    if (q.data) setForm(q.data);
  }, [q.data]);
  const save = useMutation({
    mutationFn: async (body: RequestSettings) => unwrap(await api.PUT("/settings/requests", { body })),
    onSuccess: (res) => qc.setQueryData(queryKeys.requestSettings, res),
  });
  if (q.isPending || !form) return q.error ? <ErrorAlert error={q.error} /> : <Spinner />;

  const setKind = (k: Kind, patch: Partial<KindDefaults>) => setForm({ ...form, [k]: { ...form[k], ...patch } });
  const setQuota = (k: Kind, patch: Partial<QuotaRule>) => setForm({ ...form, quotas: { ...form.quotas, [k]: { ...form.quotas[k], ...patch } } });
  const profileOpts = (k: Kind) => profiles.data?.filter((p) => p.kind === libraryKind(k)) ?? [];
  const rootOpts = (k: Kind) => roots.data?.filter((r) => r.kind === libraryKind(k)) ?? [];

  return (
    <div className="flex flex-col gap-4" data-testid="request-settings">
      <PageHeader title={t("requestSettings.title")} description={t("requestSettings.subtitle")} />
      {KINDS.map((k) => {
        const d = form[k];
        return (
          <Panel key={k} title={t(`requestSettings.kinds.${k}`)} description={t(`requestSettings.kindHints.${k}`)}>
            <div className="grid gap-3 sm:grid-cols-2" data-testid={`defaults-${k}`}>
              <SelectInput label={t("requestSettings.profile")} value={d.qualityProfileId}
                onChange={(e) => setKind(k, { qualityProfileId: Number(e.target.value) })} data-testid={`defaults-${k}-profile`}>
                <option value={0}>{t("requestSettings.notChosen")}</option>
                {profileOpts(k).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </SelectInput>
              <SelectInput label={t("requestSettings.rootFolder")} value={d.rootFolderId}
                onChange={(e) => setKind(k, { rootFolderId: Number(e.target.value) })} data-testid={`defaults-${k}-root`}>
                <option value={0}>{t("requestSettings.notChosen")}</option>
                {rootOpts(k).map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
              </SelectInput>
              {k !== "music" ? (
                <>
                  <SelectInput label={t("requestSettings.profile4k")} value={d.qualityProfile4kId}
                    onChange={(e) => setKind(k, { qualityProfile4kId: Number(e.target.value) })}>
                    <option value={0}>{t("requestSettings.no4k")}</option>
                    {profileOpts(k).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </SelectInput>
                  <SelectInput label={t("requestSettings.rootFolder4k")} value={d.rootFolder4kId}
                    onChange={(e) => setKind(k, { rootFolder4kId: Number(e.target.value) })}>
                    <option value={0}>{t("requestSettings.sameFolder")}</option>
                    {rootOpts(k).map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
                  </SelectInput>
                </>
              ) : (
                <SelectInput label={t("requestSettings.metadataProfile")} value={d.metadataProfileId ?? 0}
                  onChange={(e) => setKind(k, { metadataProfileId: Number(e.target.value) })}>
                  <option value={0}>{t("requestSettings.notChosen")}</option>
                  {metaProfiles.data?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </SelectInput>
              )}
              {k === "movie" ? (
                <SelectInput label={t("addMovie.availability")} value={d.minimumAvailability ?? "released"}
                  onChange={(e) => setKind(k, { minimumAvailability: e.target.value as MinimumAvailability })}>
                  {(["announced", "in_cinemas", "released"] as const).map((a) => <option key={a} value={a}>{t(`availability.${a}`)}</option>)}
                </SelectInput>
              ) : null}
              <TextInput label={t("request.tags")} hint={t("request.tagsHint")} value={d.tags.join(", ")}
                onChange={(e) => setKind(k, { tags: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })} />
            </div>
          </Panel>
        );
      })}
      <Panel title={t("requestSettings.quotas")} description={t("requestSettings.quotasHint")}>
        <div className="grid gap-3 sm:grid-cols-3">
          {KINDS.map((k) => (
            <QuotaFields key={k} label={t(`requestSettings.kinds.${k}`)} value={form.quotas[k]} onChange={(p) => setQuota(k, p)} testId={`quota-${k}`} />
          ))}
        </div>
      </Panel>
      <ErrorAlert error={save.error} />
      {save.isSuccess ? <Alert tone="success">{t("common.saved")}</Alert> : null}
      <div>
        <Button variant="primary" busy={save.isPending} onClick={() => save.mutate(form)} data-testid="save-request-settings">{t("common.save")}</Button>
      </div>
    </div>
  );
}

/** "N requests in D days" (N = 0 means unlimited). */
export function QuotaFields({ label, value, onChange, testId }: { label: string; value: QuotaRule; onChange: (p: Partial<QuotaRule>) => void; testId?: string }) {
  const { t } = useTranslation();
  return (
    <fieldset className="flex flex-col gap-2 rounded-ui border border-line p-3" data-testid={testId}>
      <legend className="px-1 text-[12px] font-medium text-muted">{label}</legend>
      <div className="grid grid-cols-2 gap-2">
        <TextInput label={t("requestSettings.limit")} type="number" min={0} max={10000} value={value.limit}
          onChange={(e) => onChange({ limit: Math.max(0, Number(e.target.value) || 0) })} data-testid={testId ? `${testId}-limit` : undefined} />
        <TextInput label={t("requestSettings.days")} type="number" min={1} max={365} value={value.days}
          onChange={(e) => onChange({ days: Math.min(365, Math.max(1, Number(e.target.value) || 1)) })} data-testid={testId ? `${testId}-days` : undefined} />
      </div>
      <p className="text-[11.5px] text-faint">{value.limit === 0 ? t("requests.quota.unlimited") : t("requestSettings.rule", { limit: value.limit, days: value.days })}</p>
    </fieldset>
  );
}
