import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CheckCircle2, ExternalLink, RefreshCw } from "lucide-react";
import { api, queryKeys, unwrap, type HealthIssue } from "@/api/client";
import { formatRelative } from "@/components/format";
import { Badge, Button, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";
import { DOCS_BASE } from "@/features/board/board";
import { AddMappingButton } from "@/features/downloaders/PathSuggestion";
import { describeHealth, severityTone } from "./health";

const ORDER = { error: 0, warning: 1, info: 2 } as const;

/** System → Health: every current problem with how to fix it. */
export function HealthPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const health = useQuery({
    queryKey: queryKeys.health,
    queryFn: async () => unwrap(await api.GET("/system/health")),
    refetchInterval: 60_000,
  });
  const run = useMutation({
    mutationFn: async () => unwrap(await api.POST("/system/health/run")),
    onSuccess: (report) => qc.setQueryData(queryKeys.health, report),
  });
  const issues = [...(health.data?.issues ?? [])].sort((a, b) => ORDER[a.severity] - ORDER[b.severity]);
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("health.title")}
        description={health.data?.checkedAt ? t("health.checked", { when: formatRelative(health.data.checkedAt, i18n.language) }) : t("health.subtitle")}
        actions={
          <Button busy={run.isPending} onClick={() => run.mutate()} data-testid="health-run">
            <RefreshCw className="size-3.5" />
            {t("health.runNow")}
          </Button>
        }
      />
      {health.isPending ? <Spinner /> : null}
      <ErrorAlert error={health.error ?? run.error} />
      {health.data && issues.length === 0 ? (
        <Panel>
          <p className="flex items-center gap-2 text-[13px] text-success" data-testid="health-ok">
            <CheckCircle2 className="size-4" />
            {t("health.allGood")}
          </p>
        </Panel>
      ) : null}
      {issues.length > 0 ? (
        <Panel>
          <ul className="divide-y divide-line" data-testid="health-issues">
            {issues.map((i) => <IssueRow key={i.key} issue={i} />)}
          </ul>
        </Panel>
      ) : null}
    </div>
  );
}

export function IssueRow({ issue, compact = false }: { issue: HealthIssue; compact?: boolean }) {
  const { t, i18n } = useTranslation();
  return (
    <li className="flex flex-wrap items-start justify-between gap-2 py-2" data-testid={`health-issue-${issue.code}`}>
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-2 text-[13px]">
          <Badge tone={severityTone[issue.severity]}>{t(`health.severity.${issue.severity}`)}</Badge>
          <span className="text-fg">{describeHealth(t, issue, i18n.language)}</span>
        </p>
        {!compact ? (
          <p className="mt-0.5 text-[12px] text-faint">{t("health.since", { when: formatRelative(issue.since, i18n.language) })}</p>
        ) : null}
      </div>
      <span className="flex items-center gap-3 text-[12px]">
        {issue.code === "mapping_suggested" ? <SuggestedMapping params={issue.params} /> : null}
        {issue.fixRoute ? <Link to={issue.fixRoute} className="text-info hover:underline">{t("health.fixHere")}</Link> : null}
        {!compact && issue.docs ? (
          <a href={`${DOCS_BASE}#${issue.docs}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-muted hover:text-fg">
            {t("health.howToFix")}
            <ExternalLink className="size-3" />
          </a>
        ) : null}
      </span>
    </li>
  );
}

/** The one-click add for a mapping_suggested issue. */
function SuggestedMapping({ params }: { params?: HealthIssue["params"] }) {
  const p = (params ?? {}) as Record<string, unknown>;
  if (typeof p.host !== "string" || typeof p.remotePath !== "string" || typeof p.localPath !== "string") return null;
  const replaces = typeof p.replacesMappingId === "number" && p.replacesMappingId > 0 ? p.replacesMappingId : undefined;
  return <AddMappingButton mapping={{ host: p.host, remotePath: p.remotePath, localPath: p.localPath, replacesMappingId: replaces }}
    testId="health-add-mapping" />;
}
