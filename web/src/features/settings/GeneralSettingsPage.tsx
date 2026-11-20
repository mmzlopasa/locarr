import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { Alert, Badge, Button, ErrorAlert, PageHeader, Panel, Spinner, TextInput } from "@/components/ui";

export function GeneralSettingsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: queryKeys.settings, queryFn: async () => unwrap(await api.GET("/settings")) });
  const [instanceName, setInstanceName] = useState("");
  const [tmdbKey, setTmdbKey] = useState("");

  useEffect(() => {
    if (settings.data) setInstanceName(settings.data.instanceName);
  }, [settings.data]);

  const save = useMutation({
    mutationFn: async (body: { instanceName?: string; tmdbApiKey?: string }) => unwrap(await api.PATCH("/settings", { body })),
    onSuccess: (s) => {
      qc.setQueryData(queryKeys.settings, s);
      setTmdbKey("");
      void qc.invalidateQueries({ queryKey: queryKeys.info });
    },
  });

  if (settings.isPending) return <Spinner />;
  if (settings.error) return <ErrorAlert error={settings.error} />;
  const s = settings.data;

  const submitGeneral = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ instanceName: instanceName.trim() });
  };

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("settings.general.title")} description={t("settings.general.subtitle")} />
      <ErrorAlert error={save.error} />
      {save.isSuccess ? <Alert tone="success">{t("common.saved")}</Alert> : null}
      <Panel title={t("settings.general.instance")}>
        <form onSubmit={submitGeneral} className="flex flex-wrap items-end gap-3">
          <div className="min-w-56 flex-1">
            <TextInput label={t("settings.general.instanceName")} maxLength={100} value={instanceName}
              hint={t("settings.general.instanceNameHint")} onChange={(e) => setInstanceName(e.target.value)} />
          </div>
          <Button type="submit" variant="primary" busy={save.isPending} disabled={!instanceName.trim() || instanceName === s.instanceName}>
            {t("common.save")}
          </Button>
        </form>
      </Panel>
      <Panel
        title={t("settings.metadata.title")}
        description={t("settings.metadata.subtitle")}
        actions={
          s.tmdbApiKey.configured ? (
            <Badge tone="success">{t("settings.secret.configured")}</Badge>
          ) : (
            <Badge>{t("settings.secret.notConfigured")}</Badge>
          )
        }
      >
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate({ tmdbApiKey: tmdbKey.trim() });
          }}
        >
          <div className="min-w-56 flex-1">
            <TextInput
              label={t("settings.metadata.tmdbKey")}
              type="password"
              autoComplete="off"
              spellCheck={false}
              placeholder={s.tmdbApiKey.configured ? t("settings.secret.keepPlaceholder") : ""}
              hint={t("settings.secret.writeOnly")}
              value={tmdbKey}
              onChange={(e) => setTmdbKey(e.target.value)}
            />
          </div>
          <Button type="submit" variant="primary" busy={save.isPending} disabled={!tmdbKey.trim()}>
            {t("common.save")}
          </Button>
          {s.tmdbApiKey.configured ? (
            <Button variant="ghost" busy={save.isPending} onClick={() => save.mutate({ tmdbApiKey: "" })}>
              {t("settings.secret.remove")}
            </Button>
          ) : null}
        </form>
      </Panel>
    </div>
  );
}
