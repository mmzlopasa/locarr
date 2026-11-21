import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { formatBytes, formatDateTime, formatDuration } from "@/components/format";
import { Badge, EmptyState, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";
import { healthTone } from "@/features/indexers/IndexersPage";
import { Link } from "@tanstack/react-router";

export function StatusPage() {
  const { t, i18n } = useTranslation();
  const status = useQuery({
    queryKey: queryKeys.status,
    queryFn: async () => unwrap(await api.GET("/system/status")),
    refetchInterval: 30_000,
  });
  if (status.isPending) return <Spinner />;
  if (status.error) return <ErrorAlert error={status.error} />;
  const s = status.data;
  const rows: [string, string][] = [
    [t("system.status.version"), s.version],
    [t("system.status.commit"), s.commit],
    [t("system.status.started"), formatDateTime(s.startedAt, i18n.language)],
    [t("system.status.uptime"), formatDuration(s.uptimeSeconds, i18n.language)],
    [t("system.status.runtime"), `${s.goVersion} · ${s.os}/${s.arch}`],
    [t("system.status.configDir"), s.configDir],
    [t("system.status.database"), formatBytes(s.databaseSizeBytes, i18n.language)],
    [t("system.status.dataRoots"), s.dataRoots.join(", ") || "–"],
  ];
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("system.status.title")} description={t("system.status.subtitle")} />
      <Panel>
        <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[12rem_1fr]" data-testid="system-status">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-[12px] text-muted">{k}</dt>
              <dd className="break-all font-mono text-[12px] text-fg">{v}</dd>
            </div>
          ))}
        </dl>
      </Panel>
      <IndexerHealthPanel />
    </div>
  );
}

function IndexerHealthPanel() {
  const { t, i18n } = useTranslation();
  const list = useQuery({ queryKey: queryKeys.indexers, queryFn: async () => unwrap(await api.GET("/indexers")), refetchInterval: 30_000 });
  return (
    <Panel title={t("system.status.indexers")} actions={<Link to="/indexers" className="text-[12px] text-info hover:underline">{t("system.status.manageIndexers")}</Link>}>
      <ErrorAlert error={list.error} />
      {list.data && !list.data.length ? <EmptyState>{t("indexers.empty")}</EmptyState> : null}
      <ul className="flex flex-col gap-1.5" data-testid="indexer-health">
        {list.data?.map((i) => (
          <li key={i.id} className="flex flex-wrap items-center gap-2 text-[13px]">
            <span className="text-fg">{i.name}</span>
            <Badge tone={healthTone(i.health.status)}>{t(`indexers.health.${i.health.status}`)}</Badge>
            {i.health.lastError && i.health.status !== "healthy" ? (
              <span className="text-[12px] text-danger">{i.health.lastError}</span>
            ) : null}
            {i.health.retryAt ? (
              <span className="text-[12px] text-faint">{t("indexers.retryAt", { when: formatDateTime(i.health.retryAt, i18n.language), failures: i.health.failures })}</span>
            ) : null}
          </li>
        ))}
      </ul>
    </Panel>
  );
}
