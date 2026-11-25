import { useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, Info, RotateCw, Trash2, XCircle } from "lucide-react";
import { api, queryKeys, unwrap, type Explanation, type Rejection, type Timeline, type TimelineEvent } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { Button, EmptyState, ErrorAlert, Spinner } from "@/components/ui";
import { describeRejection } from "@/features/profiles/rejections";
import { Modal } from "@/components/Modal";
import { useMe } from "@/features/auth/hooks";
import { describeEvent, describeExplanation, eventTone, reasonChip } from "./timeline";
import { cycleSummary, groupTimeline, rowEvent, rowIds, type TimelineRow } from "./timelineGroups";
import { SubtitleWhyForItem } from "@/features/subtitles/SubtitlesSection";

const toneClass = {
  success: "border-success/40 bg-success-bg text-success",
  danger: "border-danger/40 bg-danger-bg text-danger",
  warning: "border-warning/40 bg-warning-bg text-warning",
  info: "border-info/40 bg-info-bg text-info",
  neutral: "border-line-strong bg-surface-3 text-muted",
} as const;

/** Explanations after which searching again right away can help. */
const retryable = new Set(["grab_failed", "nothing_found", "all_rejected", "download_failed", "not_searched_yet", "upgrade_wanted"]);

/** "Retry now": clear the item's search backoff and search and grab at once. */
function RetryNow({ itemRef }: { itemRef: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const retry = useMutation({
    mutationFn: async () => unwrap(await api.POST("/timeline/retry", { params: { query: { item: itemRef } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.timeline(itemRef) }),
  });
  return (
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <Button onClick={() => retry.mutate()} busy={retry.isPending} title={t("explain.retryHint")} data-testid="retry-now">
        <RotateCw className="size-3.5" aria-hidden /> {retry.isPending ? t("explain.retrying") : t("explain.retryNow")}
      </Button>
      {retry.isSuccess ? <span role="status" className="text-[12px] text-muted">{t("explain.retryQueued")}</span> : null}
      {retry.error ? <ErrorAlert error={retry.error} /> : null}
    </div>
  );
}

/** The one-line answer to "why isn't it here?", with the most common rejection reasons. */
export function WhyBox({ ex, itemRef }: { ex: Explanation; itemRef?: string }) {
  const { t, i18n } = useTranslation();
  const { data: me } = useMe();
  const adult = itemRef !== undefined && /^(scene|adult_movie):/.test(itemRef);
  const admin = me?.role === "admin";
  const canRetry = itemRef !== undefined && /^(movie|episode|season|album):/.test(itemRef) && retryable.has(ex.code);
  const Icon = ex.tone === "success" ? CheckCircle2 : ex.tone === "danger" ? XCircle : ex.tone === "warning" ? AlertTriangle : Info;
  return (
    <section aria-label={t("explain.title")} data-testid="why-box" data-code={ex.code}
      className={`mb-4 rounded-ui border px-4 py-3 ${toneClass[ex.tone]}`}>
      <div className="flex items-start gap-2.5">
        <Icon className="mt-0.5 size-4 shrink-0" aria-hidden />
        <div className="min-w-0">
          <h2 className="text-[12px] font-semibold uppercase tracking-wide opacity-80">{ex.code === "available" ? t("explain.titleOk") : t("explain.title")}</h2>
          <p className="mt-0.5 text-[14px] text-fg">{describeExplanation(t, ex, i18n.language)}</p>
          {ex.reasons.length > 0 && ex.code !== "available" ? (
            <ul className="mt-2 flex flex-wrap gap-1.5">
              {ex.reasons.map((r) => (
                <li key={r.code} className="rounded border border-line-strong bg-surface px-1.5 py-0.5 text-[12px] text-muted" data-testid={`why-reason-${r.code}`}>
                  {r.code === "blocked_tag" && adult && admin ? (
                    <Link to="/settings/adult" hash="tag-blocklist" title={t("explain.chips.blocklistLink")} className="underline-offset-2 hover:text-fg hover:underline">
                      {reasonChip(t, r)}
                    </Link>
                  ) : reasonChip(t, r)} <span className="tabular-nums text-faint">×{r.count}</span>
                </li>
              ))}
            </ul>
          ) : null}
          {adult && ex.code !== "available" && ex.reasons[0]?.code === "quality_not_allowed" ? (
            <p className="mt-2 text-[12px] text-muted" data-testid="why-adult-profile">
              {t("explain.adultProfileHint")}
              {admin ? <> <Link to="/settings/adult" className="underline underline-offset-2 hover:text-fg">{t("explain.adultProfileLink")}</Link></> : null}
            </p>
          ) : null}
          {canRetry ? <RetryNow itemRef={itemRef} /> : null}
        </div>
      </div>
    </section>
  );
}

type TopRejected = { title: string; indexer: string; quality: string; status: string; reasons: Rejection[] };

function topRejected(e?: TimelineEvent): TopRejected[] {
  return (e?.data as { summary?: { topRejected?: TopRejected[] } } | undefined)?.summary?.topRejected ?? [];
}

function DecisionDetail({ e }: { e: TimelineEvent }) {
  const { t, i18n } = useTranslation();
  const list = topRejected(e);
  if (list.length === 0) return null;
  return (
    <ul className="mt-2 space-y-1.5">
      {list.map((r, i) => (
        <li key={i} className="rounded border border-line bg-surface-2 px-2 py-1.5">
          <div className="break-all font-mono text-[12px] text-fg sm:truncate">{r.title}</div>
          <div className="text-[11px] text-faint">{[r.indexer, r.quality].filter(Boolean).join(" · ")}</div>
          <ul className="mt-1 list-inside list-disc text-[12px] text-muted">
            {r.reasons.map((x, j) => <li key={j}>{describeRejection(t, x, i18n.language)}</li>)}
          </ul>
        </li>
      ))}
    </ul>
  );
}

/** Deletes one row or a group after an inline confirmation (managers only). */
function RowDelete({ count, busy, onDelete, testId }: { count: number; busy: boolean; onDelete: () => void; testId: string }) {
  const { t } = useTranslation();
  const [asking, setAsking] = useState(false);
  if (!asking) {
    return (
      <button type="button" onClick={() => setAsking(true)} title={t("timeline.delete.open")} data-testid={testId}
        className="inline-flex size-6 shrink-0 items-center justify-center rounded text-faint hover:bg-surface-3 hover:text-danger">
        <Trash2 className="size-3.5" aria-hidden /><span className="sr-only">{t("timeline.delete.open")}</span>
      </button>
    );
  }
  return (
    <div className="mt-1.5 flex basis-full flex-wrap items-center gap-2 rounded border border-danger/40 bg-danger-bg px-2 py-1.5" role="group">
      <span className="text-[12px] text-muted">{t("timeline.delete.note")}</span>
      <span className="ml-auto inline-flex gap-1">
        <Button variant="danger" busy={busy} data-testid={`${testId}-confirm`} onClick={() => { onDelete(); setAsking(false); }}>
          {count > 1 ? t("timeline.delete.group", { count }) : t("timeline.delete.row")}
        </Button>
        <Button variant="ghost" onClick={() => setAsking(false)}>{t("common.cancel")}</Button>
      </span>
    </div>
  );
}

type Del = { canDelete: boolean; busy: boolean; onDelete: (row: TimelineRow) => void };

function Dot({ tone }: { tone: keyof typeof toneClass }) {
  return <span className={`absolute -left-[5px] top-1.5 size-2.5 rounded-full border ${toneClass[tone]}`} aria-hidden />;
}

function When({ at }: { at: string }) {
  const { i18n } = useTranslation();
  return (
    <time dateTime={at} title={formatDateTime(at, i18n.language)} className="shrink-0 text-[12px] tabular-nums text-faint">
      {formatRelative(at, i18n.language)}
    </time>
  );
}

function Chevron({ open, onClick, label }: { open: boolean; onClick: () => void; label: string }) {
  return (
    <button type="button" onClick={onClick} aria-expanded={open} className="mr-1 inline-flex align-middle text-muted hover:text-fg">
      {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
      <span className="sr-only">{label}</span>
    </button>
  );
}

function EventRow({ row, del }: { row: Extract<TimelineRow, { kind: "event" }>; del: Del }) {
  const { t, i18n } = useTranslation();
  const e = row.event;
  const [open, setOpen] = useState(false);
  const expandable = e.type === "decision" && topRejected(e).length > 0;
  return (
    <li className="relative pl-6" data-testid="timeline-event" data-type={e.type}>
      <Dot tone={eventTone(e)} />
      <div className="flex flex-wrap items-start gap-x-3">
        <p className="min-w-0 flex-1 break-words text-[13px] text-fg">
          {expandable ? <Chevron open={open} onClick={() => setOpen(!open)} label={t("timeline.details")} /> : null}
          {describeEvent(t, e, i18n.language)}
        </p>
        <span className="flex items-center gap-1">
          <When at={e.at} />
          {del.canDelete ? <RowDelete count={1} busy={del.busy} onDelete={() => del.onDelete(row)} testId={`timeline-delete-${row.key}`} /> : null}
        </span>
      </div>
      {open ? <DecisionDetail e={e} /> : null}
    </li>
  );
}

/** One search cycle: every query form's search and the decision on their results, as one line. */
function CycleRow({ row, del }: { row: Extract<TimelineRow, { kind: "cycle" }>; del: Del }) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const sum = cycleSummary(row);
  const head = rowEvent(row);
  const indexers = sum.indexers.map((i) => `${i.name}: ${i.status === "ok" ? i.count : t(`timeline.indexerStatus.${i.status}`, { defaultValue: i.status })}`)
    .join(", ");
  const tone = row.decision ? eventTone(row.decision) : "info";
  return (
    <li className="relative pl-6" data-testid="timeline-cycle" data-type="search" data-searches={row.searches.length}>
      <Dot tone={tone} />
      <div className="flex flex-wrap items-start gap-x-3">
        <div className="min-w-0 flex-1 text-[13px] text-fg">
          <p className="break-words">
            <Chevron open={open} onClick={() => setOpen(!open)} label={t("timeline.cycle.expand")} />
            {t("timeline.cycle.title", { source: t(`timeline.sources.${sum.source}`, { defaultValue: sum.source }), count: sum.total })}
            {sum.queries.length > 1 ? <span className="text-faint"> · {t("timeline.cycle.queriesCount", { count: sum.queries.length })}</span> : null}
          </p>
          {indexers ? <p className="break-words text-[12px] text-muted" data-testid="timeline-cycle-indexers">{indexers}</p> : null}
          {row.decision ? <p className="mt-0.5 break-words text-[12px] text-muted" data-testid="timeline-cycle-decision" data-type="decision">{describeEvent(t, row.decision, i18n.language)}</p> : null}
        </div>
        <span className="flex items-center gap-1">
          <When at={head.at} />
          {del.canDelete ? (
            <RowDelete count={rowIds(row).length} busy={del.busy} onDelete={() => del.onDelete(row)} testId={`timeline-delete-${row.key}`} />
          ) : null}
        </span>
      </div>
      {open ? (
        <div className="mt-2 space-y-2" data-testid="timeline-cycle-detail">
          <div>
            <p className="text-[11px] font-medium uppercase tracking-wide text-faint">{t("timeline.cycle.queries")}</p>
            <ol className="mt-1 list-inside list-decimal space-y-0.5 text-[12px] text-muted">
              {[...row.searches].reverse().map((s) => <li key={s.id} className="break-all font-mono">{String((s.data as { query?: string })?.query ?? s.message)}</li>)}
            </ol>
          </div>
          {row.decision && topRejected(row.decision).length > 0 ? (
            <div>
              <p className="text-[11px] font-medium uppercase tracking-wide text-faint">{t("timeline.cycle.rejected")}</p>
              <DecisionDetail e={row.decision} />
            </div>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}

/** "Clear this item's history": shows how many rows go and what stays. */
function ClearItemDialog({ itemRef, onClose, onDone }: { itemRef: string; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const count = useQuery({
    queryKey: ["timeline-clear-count", itemRef],
    queryFn: async () => unwrap(await api.POST("/history/clear", { body: { item: itemRef, dryRun: true } })),
  });
  const clear = useMutation({
    mutationFn: async () => unwrap(await api.POST("/history/clear", { body: { item: itemRef, dryRun: false } })),
    onSuccess: () => { onDone(); onClose(); },
  });
  const n = count.data?.count ?? 0;
  return (
    <Modal title={t("timeline.clear.title")} onClose={onClose} testId="timeline-clear-dialog"
      footer={<>
        <Button onClick={onClose}>{t("common.cancel")}</Button>
        <Button variant="danger" busy={clear.isPending} disabled={count.isPending || n === 0} onClick={() => clear.mutate()} data-testid="timeline-clear-confirm">
          {t("timeline.clear.confirm", { count: n })}
        </Button>
      </>}>
      <div className="space-y-2 text-[13px]">
        <div className="rounded-ui border border-line bg-surface-2 px-3 py-2" data-testid="timeline-clear-count">
          {count.isPending ? <Spinner /> : <p className="text-fg">{n === 0 ? t("timeline.clear.none") : t("timeline.clear.count", { count: n })}</p>}
        </div>
        <p className="text-muted">{t("timeline.clear.what")}</p>
        <p className="text-muted">{t("timeline.clear.kept")}</p>
        <ErrorAlert error={count.error ?? clear.error} />
      </div>
    </Modal>
  );
}

export function HistoryTab({ itemRef, load }: { itemRef: string; load?: () => Promise<Timeline> }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const [clearing, setClearing] = useState(false);
  const tl = useQuery({
    queryKey: queryKeys.timeline(itemRef),
    queryFn: load ?? (async () => unwrap(await api.GET("/timeline", { params: { query: { item: itemRef } } }))),
    refetchInterval: 5_000,
  });
  // New steps on the timeline (a grab, an import) change the movie too.
  const code = tl.data?.explanation.code;
  const count = tl.data?.events.length;
  useEffect(() => {
    if (code === undefined) return;
    const kind = itemRef.split(":")[0];
    const key = kind === "movie" ? "movies" : kind === "album" || kind === "artist" ? "music" : kind === "scene" || kind === "adult_movie" ? "adult" : "series";
    void qc.invalidateQueries({ queryKey: [key] });
  }, [code, count, qc, itemRef]);
  const rows = useMemo(() => groupTimeline(tl.data?.events ?? []), [tl.data]);
  const refresh = () => void qc.invalidateQueries({ queryKey: queryKeys.timeline(itemRef) });
  const remove = useMutation({
    mutationFn: async (row: TimelineRow) => {
      const ids = rowIds(row);
      const ref = rowEvent(row).itemRef ?? itemRef;
      const [only] = ids;
      if (ids.length === 1 && only) {
        const res = await api.DELETE("/history/{id}", { params: { path: { id: only }, query: { item: ref } } });
        if (res.error && res.response.status !== 404) unwrap(res);
        return;
      }
      unwrap(await api.POST("/history/clear", { body: { item: ref, ids, dryRun: false } }));
    },
    onSettled: refresh,
  });
  if (tl.isLoading) return <Spinner />;
  if (tl.error) return <ErrorAlert error={tl.error} />;
  if (!tl.data) return null;
  const canDelete = !!me?.permissions.manageLibrary;
  const del: Del = { canDelete, busy: remove.isPending, onDelete: (row) => remove.mutate(row) };
  return (
    <div>
      <WhyBox ex={tl.data.explanation} itemRef={itemRef} />
      {/^(movie|episode):/.test(itemRef) ? <SubtitleWhyForItem itemRef={itemRef} /> : null}
      {tl.data.events.length === 0 ? (
        <EmptyState>{t("timeline.empty")}</EmptyState>
      ) : (
        <>
          {canDelete ? (
            <div className="mb-3 flex justify-end">
              <Button variant="ghost" onClick={() => setClearing(true)} data-testid="timeline-clear">
                <Trash2 className="size-3.5" aria-hidden />{t("timeline.clear.open")}
              </Button>
            </div>
          ) : null}
          <ErrorAlert error={remove.error} />
          <ol className="space-y-3 border-l border-line pl-0" aria-label={t("timeline.title")} data-testid="timeline">
            {rows.map((row) => row.kind === "cycle"
              ? <CycleRow key={row.key} row={row} del={del} />
              : <EventRow key={row.key} row={row} del={del} />)}
          </ol>
        </>
      )}
      {clearing ? <ClearItemDialog itemRef={itemRef} onClose={() => setClearing(false)} onDone={refresh} /> : null}
    </div>
  );
}
