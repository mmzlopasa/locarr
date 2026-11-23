import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { formatBytes, formatRelative } from "@/components/format";
import { Alert, Badge, EmptyState, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";
import { HistoryCategoryBadge, HistoryTitle } from "@/features/activity/HistoryPage";
import { IssueRow } from "@/features/system/HealthPage";

function day(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

const linkClass = "text-[12px] text-info hover:underline";

/** The administrator's home: what is downloading, what is coming, what is wrong, what happened. */
export function OverviewPage() {
  const { t } = useTranslation();
  const folders = useQuery({ queryKey: queryKeys.rootFolders, queryFn: async () => unwrap(await api.GET("/rootfolders")) });
  return (
    <div className="flex flex-col gap-4" data-testid="overview">
      <PageHeader title={t("overview.title")} description={t("overview.subtitle")} />
      {folders.data && folders.data.length === 0 ? (
        <Alert tone="info">
          <span data-testid="overview-welcome-hint">
            {t("overview.noFolders")} <Link to="/welcome" className="underline">{t("overview.openWelcome")}</Link>
          </span>
        </Alert>
      ) : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <QueuePanel />
        <BoardPanel />
        <HealthPanel />
        <UpcomingPanel />
        <ActivityPanel />
        <Panel title={t("overview.disk")} actions={<Link to="/settings/media" className={linkClass}>{t("overview.manage")}</Link>}>
          <ErrorAlert error={folders.error} />
          {folders.isPending ? <Spinner /> : null}
          {folders.data?.length === 0 ? <EmptyState>{t("rootFolders.none")}</EmptyState> : null}
          <ul className="flex flex-col gap-3" data-testid="overview-disk">
            {folders.data?.map((f) => <DiskBar key={f.id} path={f.path} kind={f.kind} free={f.freeSpaceBytes} total={f.totalSpaceBytes} ok={f.accessible} />)}
          </ul>
        </Panel>
      </div>
    </div>
  );
}

function DiskBar({ path, kind, free, total, ok }: { path: string; kind: string; free?: number; total?: number; ok: boolean }) {
  const { t, i18n } = useTranslation();
  const used = total && free !== undefined ? Math.min(1, Math.max(0, (total - free) / total)) : 0;
  return (
    <li>
      <p className="flex flex-wrap items-center justify-between gap-2 text-[12px]">
        <span className="flex items-center gap-2">
          <span className="break-all font-mono text-fg">{path}</span>
          <Badge>{t(`mediaKinds.${kind}`)}</Badge>
        </span>
        {ok ? (
          <span className="text-faint">{t("rootFolders.space", { free: formatBytes(free, i18n.language), total: formatBytes(total, i18n.language) })}</span>
        ) : <Badge tone="danger">{t("rootFolders.missing")}</Badge>}
      </p>
      <div className="mt-1 h-1.5 overflow-hidden rounded bg-surface-3" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(used * 100)}
        aria-label={path}>
        <div className={`h-full ${used > 0.95 ? "bg-danger" : used > 0.85 ? "bg-warning" : "bg-accent"}`} style={{ width: `${used * 100}%` }} />
      </div>
    </li>
  );
}

function QueuePanel() {
  const { t, i18n } = useTranslation();
  const queue = useQuery({
    queryKey: [...queryKeys.queue, "overview"],
    queryFn: async () => unwrap(await api.GET("/queue", { params: { query: { includeUnknown: false } } })),
    refetchInterval: 10_000,
  });
  const items = queue.data?.items ?? [];
  const active = items.filter((i) => i.status === "active");
  const speed = active.reduce((s, i) => s + i.downloadSpeed, 0);
  return (
    <Panel title={t("overview.queue")} actions={<Link to="/activity/queue" className={linkClass}>{t("overview.openQueue")}</Link>}>
      <ErrorAlert error={queue.error} />
      {queue.isPending ? <Spinner /> : null}
      {queue.data ? (
        <div data-testid="overview-queue">
          <p className="text-[13px] text-muted">
            {t("overview.queueSummary", { count: items.length, active: active.length, speed: `${formatBytes(speed, i18n.language)}/s` })}
          </p>
          <ul className="mt-2 flex flex-col gap-1.5">
            {items.slice(0, 5).map((i) => (
              <li key={`${i.clientId ?? 0}-${i.clientItemId}`} className="text-[12px]">
                <div className="flex justify-between gap-2">
                  <span className="truncate text-fg">{i.item?.title ?? i.title}</span>
                  <span className="shrink-0 tabular-nums text-faint">{Math.round(i.progress * 100)}%</span>
                </div>
                <div className="mt-0.5 h-1 overflow-hidden rounded bg-surface-3">
                  <div className="h-full bg-info" style={{ width: `${Math.round(i.progress * 100)}%` }} />
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </Panel>
  );
}

function BoardPanel() {
  const { t } = useTranslation();
  const board = useQuery({ queryKey: queryKeys.board(""), queryFn: async () => unwrap(await api.GET("/board")), refetchInterval: 30_000 });
  return (
    <Panel title={t("board.title")} actions={<Link to="/activity/board" className={linkClass}>{t("overview.openBoard")}</Link>}>
      <ErrorAlert error={board.error} />
      {board.isPending ? <Spinner /> : null}
      {board.data && board.data.total === 0 ? <EmptyState>{t("board.empty")}</EmptyState> : null}
      <ul className="flex flex-col gap-1" data-testid="overview-board">
        {board.data?.groups.slice(0, 6).map((g) => (
          <li key={g.code} className="flex items-center justify-between gap-2 text-[13px]">
            <span className="text-fg">{t(`board.groups.${g.code}.title`, { defaultValue: g.code })}</span>
            <Badge tone={g.tone as "info"}>{g.count}</Badge>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function HealthPanel() {
  const { t } = useTranslation();
  const health = useQuery({ queryKey: queryKeys.health, queryFn: async () => unwrap(await api.GET("/system/health")), refetchInterval: 60_000 });
  const issues = health.data?.issues ?? [];
  return (
    <Panel title={t("health.title")} actions={<Link to="/system/health" className={linkClass}>{t("overview.openHealth")}</Link>}>
      <ErrorAlert error={health.error} />
      {health.isPending ? <Spinner /> : null}
      {health.data && issues.length === 0 ? <p className="text-[13px] text-success">{t("health.allGood")}</p> : null}
      <ul className="divide-y divide-line" data-testid="overview-health">
        {issues.slice(0, 5).map((i) => <IssueRow key={i.key} issue={i} compact />)}
      </ul>
      {issues.length > 5 ? <p className="mt-1 text-[12px] text-faint">{t("overview.more", { count: issues.length - 5 })}</p> : null}
    </Panel>
  );
}

function UpcomingPanel() {
  const { t, i18n } = useTranslation();
  const start = new Date();
  const end = new Date(start.getTime() + 7 * 86400_000);
  const cal = useQuery({
    queryKey: queryKeys.calendar(day(start), day(end)),
    queryFn: async () => unwrap(await api.GET("/calendar", { params: { query: { start: day(start), end: day(end) } } })),
  });
  const fmt = new Intl.DateTimeFormat(i18n.language, { weekday: "short", day: "numeric", month: "short" });
  return (
    <Panel title={t("overview.upcoming")} actions={<Link to="/calendar" className={linkClass}>{t("overview.openCalendar")}</Link>}>
      <ErrorAlert error={cal.error} />
      {cal.isPending ? <Spinner /> : null}
      {cal.data?.length === 0 ? <EmptyState>{t("overview.nothingUpcoming")}</EmptyState> : null}
      <ul className="flex flex-col gap-1" data-testid="overview-upcoming">
        {cal.data?.slice(0, 8).map((e, i) => (
          <li key={`${e.kind}-${e.movieId ?? e.episodeId ?? i}`} className="flex justify-between gap-2 text-[13px]">
            <span className="truncate text-fg">
              {e.title}
              {e.kind === "episode" ? ` S${String(e.seasonNumber ?? 0).padStart(2, "0")}E${String(e.episodeNumber ?? 0).padStart(2, "0")}` : ""}
            </span>
            <span className="shrink-0 text-[12px] text-faint">{fmt.format(new Date(`${e.date}T00:00:00`))}</span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function ActivityPanel() {
  const { t, i18n } = useTranslation();
  const hist = useQuery({
    queryKey: queryKeys.history({ overview: true }),
    queryFn: async () => unwrap(await api.GET("/history", { params: { query: { limit: 10 } } })),
    refetchInterval: 30_000,
  });
  return (
    <Panel title={t("overview.activity")} actions={<Link to="/activity/history" className={linkClass}>{t("overview.openHistory")}</Link>}>
      <ErrorAlert error={hist.error} />
      {hist.isPending ? <Spinner /> : null}
      {hist.data?.items.length === 0 ? <EmptyState>{t("history.none")}</EmptyState> : null}
      <ul className="flex flex-col gap-1.5" data-testid="overview-activity">
        {hist.data?.items.map((e) => (
          <li key={e.id} className="flex items-center justify-between gap-2 text-[13px]">
            <span className="flex min-w-0 items-center gap-2">
              <HistoryCategoryBadge category={e.category} />
              <span className="truncate"><HistoryTitle e={e} /></span>
            </span>
            <span className="shrink-0 text-[12px] text-faint">{formatRelative(e.at, i18n.language)}</span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}
