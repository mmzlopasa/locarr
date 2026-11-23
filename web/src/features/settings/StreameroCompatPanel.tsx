import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { KeyRound } from "lucide-react";
import { api, queryKeys, unwrap, type CreatedApiKey } from "@/api/client";
import { Alert, Button, ErrorAlert, Panel } from "@/components/ui";
import { CopyButton } from "@/features/account/AccountPage";

/** The values Streamero needs to use Locarr as its Seerr, Radarr and Sonarr (D-079). */
export function compatUrls(origin: string) {
  const base = origin.replace(/\/+$/, "");
  return { seerr: `${base}/compat/seerr`, radarr: `${base}/compat/radarr`, sonarr: `${base}/compat/sonarr` };
}

export function StreameroCompatPanel({ origin = window.location.origin }: { origin?: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/apikeys", { body: { name: "Streamero", readOnly: false } })),
    onSuccess: (k) => {
      setCreated(k);
      void qc.invalidateQueries({ queryKey: queryKeys.apiKeys });
    },
  });
  const urls = compatUrls(origin);
  const rows: { app: string; label: string; value: string }[] = [
    { app: "seerr", label: t("mediaServer.compat.seerrUrl"), value: urls.seerr },
    { app: "radarr", label: t("mediaServer.compat.radarrUrl"), value: urls.radarr },
    { app: "sonarr", label: t("mediaServer.compat.sonarrUrl"), value: urls.sonarr },
  ];
  return (
    <Panel title={t("mediaServer.compat.title")} description={t("mediaServer.compat.intro")}>
      <div className="flex flex-col gap-3" data-testid="streamero-compat">
        <dl className="flex flex-col divide-y divide-line">
          {rows.map((r) => (
            <div key={r.app} className="flex flex-col gap-1.5 py-2 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <dt className="text-[12px] text-faint">{r.label}</dt>
                <dd className="break-all font-mono text-[12.5px] text-fg" data-testid={`compat-url-${r.app}`}>{r.value}</dd>
              </div>
              <CopyButton text={r.value} />
            </div>
          ))}
        </dl>
        <p className="text-[12px] text-faint">{t("mediaServer.compat.hostHint")}</p>
        <div className="flex flex-col gap-2">
          <p className="text-[13px] text-muted">{t("mediaServer.compat.keyHint")}</p>
          <div>
            <Button onClick={() => create.mutate()} busy={create.isPending} data-testid="compat-create-key">
              <KeyRound className="size-4" />
              {t("mediaServer.compat.createKey")}
            </Button>
          </div>
          <ErrorAlert error={create.error} />
          {created ? (
            <div className="flex flex-col gap-2 rounded-ui border border-warning/40 bg-warning-bg p-3" data-testid="compat-created-key">
              <p className="text-[13px] text-warning">{t("account.apiKeys.shownOnce")}</p>
              <code className="break-all rounded bg-bg px-2 py-1 font-mono text-[12px] text-fg">{created.key}</code>
              <div className="flex gap-2">
                <CopyButton text={created.key} />
                <Button variant="ghost" onClick={() => setCreated(null)}>{t("common.done")}</Button>
              </div>
            </div>
          ) : null}
        </div>
        <Alert tone="info">{t("mediaServer.compat.oneUser")}</Alert>
      </div>
    </Panel>
  );
}
