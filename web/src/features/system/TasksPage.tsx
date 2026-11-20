import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Play } from "lucide-react";
import { api, queryKeys, unwrap, type JobState } from "@/api/client";
import { formatDateTime, formatDuration, formatRelative } from "@/components/format";
import { Badge, Button, EmptyState, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";

const stateTone: Record<JobState, "neutral" | "success" | "danger" | "warning" | "info"> = {
  queued: "neutral",
  running: "info",
  succeeded: "success",
  failed: "danger",
  cancelled: "warning",
};

export function JobStateBadge({ state }: { state: JobState }) {
  const { t } = useTranslation();
  return <Badge tone={stateTone[state]}>{t(`jobStates.${state}`)}</Badge>;
}

/** Translated task name, falling back to the server's name for tasks this client does not know. */
function useTaskLabel() {
  const { t } = useTranslation();
  return (kind: string) => t(`tasks.names.${kind}`, { defaultValue: kind });
}

export function TasksPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const label = useTaskLabel();
  const tasks = useQuery({
    queryKey: queryKeys.tasks,
    queryFn: async () => unwrap(await api.GET("/system/tasks", { params: { query: { limit: 100 } } })),
    refetchInterval: 5_000,
  });
  const refresh = () => qc.invalidateQueries({ queryKey: queryKeys.tasks });
  const run = useMutation({
    mutationFn: async (name: string) => unwrap(await api.POST("/system/tasks/{name}/run", { params: { path: { name } } })),
    onSuccess: refresh,
  });
  const cancel = useMutation({
    mutationFn: async (id: number) => unwrap(await api.POST("/system/jobs/{id}/cancel", { params: { path: { id } } })),
    onSuccess: refresh,
  });

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("tasks.title")} description={t("tasks.subtitle")} />
      <ErrorAlert error={run.error ?? cancel.error} />
      {tasks.isPending ? <Spinner /> : null}
      <ErrorAlert error={tasks.error} />
      {tasks.data ? (
        <>
          <Panel title={t("tasks.scheduled")}>
            {tasks.data.scheduled.length === 0 ? <EmptyState>{t("tasks.noScheduled")}</EmptyState> : null}
            <div className="overflow-x-auto">
              <table className="w-full text-left text-[13px]" data-testid="scheduled-tasks">
                <thead className="text-[11px] uppercase tracking-wide text-faint">
                  <tr>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.name")}</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.interval")}</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.lastRun")}</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.nextRun")}</th>
                    <th />
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {tasks.data.scheduled.map((s) => (
                    <tr key={s.name}>
                      <td className="py-1.5 pr-3 text-fg">{label(s.name)}</td>
                      <td className="py-1.5 pr-3 text-muted">{formatDuration(s.intervalSeconds, i18n.language)}</td>
                      <td className="py-1.5 pr-3 text-muted">
                        <span className="inline-flex items-center gap-2">
                          {s.lastRunAt ? formatRelative(s.lastRunAt, i18n.language) : t("common.never")}
                          {s.lastState ? <JobStateBadge state={s.lastState} /> : null}
                        </span>
                      </td>
                      <td className="py-1.5 pr-3 text-muted">{formatRelative(s.nextRunAt, i18n.language)}</td>
                      <td className="py-1.5 text-right">
                        <Button variant="ghost" busy={run.isPending && run.variables === s.name} onClick={() => run.mutate(s.name)}
                          aria-label={t("tasks.runNamed", { name: label(s.name) })}>
                          <Play className="size-3.5" />
                          {t("tasks.run")}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Panel>
          <Panel title={t("tasks.history")} description={t("tasks.historyHint")}>
            {tasks.data.jobs.length === 0 ? <EmptyState>{t("tasks.noJobs")}</EmptyState> : null}
            <div className="overflow-x-auto">
              <table className="w-full text-left text-[13px]" data-testid="job-history">
                <thead className="text-[11px] uppercase tracking-wide text-faint">
                  <tr>
                    <th className="py-1.5 pr-3 font-medium">#</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.name")}</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.state")}</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.attempt")}</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.when")}</th>
                    <th className="py-1.5 pr-3 font-medium">{t("tasks.error")}</th>
                    <th />
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {tasks.data.jobs.map((j) => (
                    <tr key={j.id}>
                      <td className="py-1.5 pr-3 font-mono text-[12px] text-faint">{j.id}</td>
                      <td className="py-1.5 pr-3 text-fg">{label(j.kind)}</td>
                      <td className="py-1.5 pr-3"><JobStateBadge state={j.state} /></td>
                      <td className="py-1.5 pr-3 text-muted">{j.attempt}/{j.maxAttempts}</td>
                      <td className="py-1.5 pr-3 text-muted" title={formatDateTime(j.finishedAt ?? j.startedAt ?? j.runAt, i18n.language)}>
                        {formatRelative(j.finishedAt ?? j.startedAt ?? j.runAt, i18n.language)}
                      </td>
                      <td className="max-w-xs truncate py-1.5 pr-3 text-[12px] text-danger" title={j.error}>{j.error ?? ""}</td>
                      <td className="py-1.5 text-right">
                        {j.state === "queued" || j.state === "running" ? (
                          <Button variant="ghost" busy={cancel.isPending && cancel.variables === j.id} onClick={() => cancel.mutate(j.id)}>
                            {t("tasks.cancel")}
                          </Button>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Panel>
        </>
      ) : null}
    </div>
  );
}
