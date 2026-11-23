import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type SystemSettings, type SystemSettingsInput } from "@/api/client";
import { Alert, Badge, Button, Checkbox, ErrorAlert, PageHeader, Panel, Spinner, TextInput } from "@/components/ui";

type NumKey = Exclude<keyof SystemSettings, "updateCheck" | "metricsTokenSet">;
type Form = Omit<SystemSettings, "metricsTokenSet"> & { metricsToken?: string };

/** A random token for /metrics (URL-safe, 32 bytes). */
export function newToken(): string {
  const b = new Uint8Array(32);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** Settings → Monitoring: health thresholds, backups, retention, version check and metrics. */
export function MonitoringSettingsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: queryKeys.systemSettings, queryFn: async () => unwrap(await api.GET("/settings/system")) });
  const [f, setF] = useState<Form | null>(null);
  useEffect(() => {
    if (q.data) {
      const { metricsTokenSet: _ignored, ...rest } = q.data;
      void _ignored;
      setF(rest);
    }
  }, [q.data]);
  const save = useMutation({
    mutationFn: async (form: Form) => {
      const body: SystemSettingsInput = { ...form };
      if (form.metricsToken === undefined) delete body.metricsToken;
      return unwrap(await api.PUT("/settings/system", { body }));
    },
    onSuccess: (res) => {
      qc.setQueryData(queryKeys.systemSettings, res);
      setF((cur) => (cur ? { ...cur, metricsToken: undefined } : cur));
    },
  });
  if (!f) return q.error ? <ErrorAlert error={q.error} /> : <Spinner />;
  const set = (patch: Partial<Form>) => setF({ ...f, ...patch });
  const num = (k: NumKey, min = 0) => (
    <TextInput label={t(`monitoring.fields.${k}`)} hint={t(`monitoring.hints.${k}`)} type="number" min={min} value={String(f[k])}
      onChange={(e) => set({ [k]: Math.max(min, Number(e.target.value) || 0) } as Partial<Form>)} data-testid={`monitoring-${k}`} />
  );
  const tokenSet = !!q.data?.metricsTokenSet;
  return (
    <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); save.mutate(f); }} data-testid="monitoring-settings">
      <PageHeader title={t("monitoring.title")} description={t("monitoring.subtitle")} />
      <Panel title={t("monitoring.health")}>
        <div className="grid gap-3 sm:grid-cols-3">
          {num("minFreeSpaceGb")}
          {num("recycleBinWarnGb")}
          {num("stuckImportMinutes", 5)}
        </div>
        <div className="mt-3">
          <Checkbox label={t("monitoring.updateCheck")} description={t("monitoring.updateCheckHint")} checked={f.updateCheck}
            onChange={(e) => set({ updateCheck: e.target.checked })} data-testid="monitoring-updateCheck" />
        </div>
      </Panel>
      <Panel title={t("monitoring.backups")}>
        <div className="grid gap-3 sm:grid-cols-3">
          {num("backupIntervalHours")}
          {num("backupKeep", 1)}
        </div>
      </Panel>
      <Panel title={t("monitoring.housekeeping")} description={t("monitoring.housekeepingHint")}>
        <div className="grid gap-3 sm:grid-cols-3">
          {num("timelineDays", 7)}
          {num("searchLogDays", 1)}
          {num("downloadEventsDays", 7)}
          {num("releasesSeenDays", 1)}
          {num("vacuumIntervalDays")}
          {num("imageCacheMaxMb", 50)}
        </div>
      </Panel>
      <Panel title={t("monitoring.metrics")} description={t("monitoring.metricsHint")}>
        <div className="flex flex-col gap-3">
          <p className="flex items-center gap-2 text-[13px]">
            {t("monitoring.metricsState")}
            <Badge tone={tokenSet ? "success" : "neutral"}>{tokenSet ? t("monitoring.on") : t("monitoring.off")}</Badge>
          </p>
          <div className="flex flex-wrap items-end gap-2">
            <div className="min-w-72 flex-1">
              <TextInput label={t("monitoring.token")} autoComplete="off" spellCheck={false} className="font-mono"
                placeholder={tokenSet ? t("monitoring.tokenKeep") : ""} value={f.metricsToken ?? ""}
                onChange={(e) => set({ metricsToken: e.target.value })} data-testid="monitoring-token" />
            </div>
            <Button onClick={() => set({ metricsToken: newToken() })} data-testid="monitoring-generate">{t("monitoring.generate")}</Button>
            {tokenSet ? <Button variant="ghost" onClick={() => set({ metricsToken: "" })}>{t("monitoring.turnOff")}</Button> : null}
          </div>
          <pre className="overflow-x-auto rounded-ui border border-line bg-surface-2 p-2 text-[12px] text-muted">
{`curl -H "Authorization: Bearer <token>" ${location.origin}/metrics
curl -H "X-Api-Key: <read-only key>" ${location.origin}/api/stats`}
          </pre>
          <p className="text-[12px] text-faint">{t("monitoring.statsHint")}</p>
        </div>
      </Panel>
      <ErrorAlert error={save.error} />
      {save.isSuccess ? <Alert tone="success">{t("common.saved")}</Alert> : null}
      <div>
        <Button type="submit" variant="primary" busy={save.isPending} data-testid="monitoring-save">{t("common.save")}</Button>
      </div>
    </form>
  );
}
