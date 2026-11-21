import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, Info, XCircle } from "lucide-react";
import { api, queryKeys, unwrap, type Explanation, type Rejection, type TimelineEvent } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { EmptyState, ErrorAlert, Spinner } from "@/components/ui";
import { describeRejection } from "@/features/profiles/rejections";
import { describeEvent, describeExplanation, eventTone } from "./timeline";

const toneClass = {
  success: "border-success/40 bg-success-bg text-success",
  danger: "border-danger/40 bg-danger-bg text-danger",
  warning: "border-warning/40 bg-warning-bg text-warning",
  info: "border-info/40 bg-info-bg text-info",
  neutral: "border-line-strong bg-surface-3 text-muted",
} as const;

/** The one-line answer to "why isn't it here?", with the most common rejection reasons. */
export function WhyBox({ ex }: { ex: Explanation }) {
  const { t, i18n } = useTranslation();
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
                <li key={r.code} className="rounded border border-line-strong bg-surface px-1.5 py-0.5 text-[12px] text-muted">
                  {t(`rejectionCodes.${r.code}`, { defaultValue: r.code })} <span className="tabular-nums text-faint">×{r.count}</span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      </div>
    </section>
  );
}

type TopRejected = { title: string; indexer: string; quality: string; status: string; reasons: Rejection[] };

function DecisionDetail({ e }: { e: TimelineEvent }) {
  const { t, i18n } = useTranslation();
  const d = (e.data ?? {}) as { summary?: { topRejected?: TopRejected[] } };
  const list = d.summary?.topRejected ?? [];
  if (list.length === 0) return null;
  return (
    <ul className="mt-2 space-y-1.5">
      {list.map((r, i) => (
        <li key={i} className="rounded border border-line bg-surface-2 px-2 py-1.5">
          <div className="truncate font-mono text-[12px] text-fg">{r.title}</div>
          <div className="text-[11px] text-faint">{[r.indexer, r.quality].filter(Boolean).join(" · ")}</div>
          <ul className="mt-1 list-inside list-disc text-[12px] text-muted">
            {r.reasons.map((x, j) => <li key={j}>{describeRejection(t, x, i18n.language)}</li>)}
          </ul>
        </li>
      ))}
    </ul>
  );
}

function Event({ e }: { e: TimelineEvent }) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const tone = eventTone(e);
  const expandable = e.type === "decision" && ((e.data as { summary?: { topRejected?: unknown[] } })?.summary?.topRejected?.length ?? 0) > 0;
  return (
    <li className="relative pl-6" data-testid="timeline-event" data-type={e.type}>
      <span className={`absolute -left-[5px] top-1.5 size-2.5 rounded-full border ${toneClass[tone]}`} aria-hidden />
      <div className="flex flex-wrap items-baseline justify-between gap-x-3">
        <p className="text-[13px] text-fg">
          {expandable ? (
            <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} className="mr-1 inline-flex align-middle text-muted hover:text-fg">
              {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
              <span className="sr-only">{t("timeline.details")}</span>
            </button>
          ) : null}
          {describeEvent(t, e)}
        </p>
        <time dateTime={e.at} title={formatDateTime(e.at, i18n.language)} className="text-[12px] tabular-nums text-faint">
          {formatRelative(e.at, i18n.language)}
        </time>
      </div>
      {open ? <DecisionDetail e={e} /> : null}
    </li>
  );
}

export function HistoryTab({ itemRef }: { itemRef: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const tl = useQuery({
    queryKey: queryKeys.timeline(itemRef),
    queryFn: async () => unwrap(await api.GET("/timeline", { params: { query: { item: itemRef } } })),
    refetchInterval: 5_000,
  });
  // New steps on the timeline (a grab, an import) change the movie too.
  const code = tl.data?.explanation.code;
  const count = tl.data?.events.length;
  useEffect(() => {
    if (code === undefined) return;
    void qc.invalidateQueries({ queryKey: ["movies"] });
    void qc.invalidateQueries({ queryKey: ["series"] });
  }, [code, count, qc]);
  if (tl.isLoading) return <Spinner />;
  if (tl.error) return <ErrorAlert error={tl.error} />;
  if (!tl.data) return null;
  return (
    <div>
      <WhyBox ex={tl.data.explanation} />
      {tl.data.events.length === 0 ? (
        <EmptyState>{t("timeline.empty")}</EmptyState>
      ) : (
        <ol className="space-y-3 border-l border-line pl-0" aria-label={t("timeline.title")} data-testid="timeline">
          {tl.data.events.map((e) => <Event key={`${e.source}-${e.id}`} e={e} />)}
        </ol>
      )}
    </div>
  );
}
