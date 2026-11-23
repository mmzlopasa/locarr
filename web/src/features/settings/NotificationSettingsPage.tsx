import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type NotificationSettingsInput, type SmtpSecurity } from "@/api/client";
import { Alert, Button, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { ChannelList, SecretField } from "@/features/notifications/channels";

type Form = { baseUrl: string; subtitleMissingDays: number; host: string; port: number; security: SmtpSecurity; username: string; from: string; password?: string };

export function NotificationSettingsPage() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-4" data-testid="notification-settings">
      <PageHeader title={t("notificationSettings.title")} description={t("notificationSettings.subtitle")} />
      <Panel title={t("notificationSettings.channels")} description={t("notificationSettings.channelsHint")}>
        <ChannelList personal={false} />
      </Panel>
      <DeliverySettings />
    </div>
  );
}

function DeliverySettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: queryKeys.notificationSettings, queryFn: async () => unwrap(await api.GET("/settings/notifications")) });
  const [f, setF] = useState<Form | null>(null);
  useEffect(() => {
    if (q.data) {
      const s = q.data.smtp;
      setF({ baseUrl: q.data.baseUrl, subtitleMissingDays: q.data.subtitleMissingDays, host: s.host, port: s.port, security: s.security, username: s.username, from: s.from });
    }
  }, [q.data]);
  const save = useMutation({
    mutationFn: async (form: Form) => {
      const body: NotificationSettingsInput = {
        baseUrl: form.baseUrl.trim(),
        subtitleMissingDays: Math.min(90, Math.max(1, form.subtitleMissingDays || 7)),
        smtp: { host: form.host.trim(), port: form.port, security: form.security, username: form.username, from: form.from.trim(),
          ...(form.password !== undefined ? { password: form.password } : {}) },
      };
      return unwrap(await api.PUT("/settings/notifications", { body }));
    },
    onSuccess: (res) => {
      qc.setQueryData(queryKeys.notificationSettings, res);
      setF((cur) => (cur ? { ...cur, password: undefined } : cur));
    },
  });
  if (!f) return q.error ? <ErrorAlert error={q.error} /> : <Spinner />;
  const set = (patch: Partial<Form>) => setF({ ...f, ...patch });
  return (
    <Panel title={t("notificationSettings.delivery")} description={t("notificationSettings.deliveryHint")}>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); save.mutate(f); }}>
        <TextInput label={t("notificationSettings.baseUrl")} hint={t("notificationSettings.baseUrlHint")} type="url"
          placeholder="https://locarr.example.com" value={f.baseUrl} onChange={(e) => set({ baseUrl: e.target.value })} data-testid="base-url" />
        <TextInput label={t("notificationSettings.subtitleMissingDays")} hint={t("notificationSettings.subtitleMissingDaysHint")} type="number"
          min={1} max={90} value={f.subtitleMissingDays || ""} onChange={(e) => set({ subtitleMissingDays: Number(e.target.value) || 0 })}
          data-testid="subtitle-missing-days" />
        <h3 className="text-[13px] font-semibold text-fg">{t("notificationSettings.mailServer")}</h3>
        <div className="grid gap-3 sm:grid-cols-3">
          <TextInput label={t("notificationSettings.host")} value={f.host} onChange={(e) => set({ host: e.target.value })} data-testid="smtp-host" />
          <TextInput label={t("notificationSettings.port")} type="number" min={0} max={65535} value={f.port || ""}
            onChange={(e) => set({ port: Number(e.target.value) || 0 })} hint={t("notificationSettings.portHint")} data-testid="smtp-port" />
          <SelectInput label={t("notificationSettings.security")} value={f.security} onChange={(e) => set({ security: e.target.value as SmtpSecurity })}
            data-testid="smtp-security">
            <option value="starttls">{t("notificationSettings.securities.starttls")}</option>
            <option value="tls">{t("notificationSettings.securities.tls")}</option>
            <option value="none">{t("notificationSettings.securities.none")}</option>
          </SelectInput>
          <TextInput label={t("notificationSettings.username")} autoComplete="off" value={f.username} onChange={(e) => set({ username: e.target.value })} />
          <SecretField label={t("notificationSettings.password")} name="smtp-password" stored={!!q.data?.smtp.password.configured}
            value={f.password} onChange={(v) => set({ password: v })} />
          <TextInput label={t("notificationSettings.from")} placeholder="Locarr <locarr@example.com>" value={f.from}
            onChange={(e) => set({ from: e.target.value })} data-testid="smtp-from" />
        </div>
        <ErrorAlert error={save.error} />
        {save.isSuccess ? <Alert tone="success">{t("common.saved")}</Alert> : null}
        <div>
          <Button type="submit" variant="primary" busy={save.isPending} data-testid="save-notification-settings">{t("common.save")}</Button>
        </div>
      </form>
    </Panel>
  );
}
