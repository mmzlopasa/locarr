import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import {
  api,
  queryKeys,
  unwrap,
  type Comment,
  type Explanation,
  type MediaRequest,
  type QuotaStatus,
  type RequestKind,
  type RequestStatus,
  type RequestSummary,
} from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { Badge, Button, EmptyState, ErrorAlert, Spinner } from "@/components/ui";
import { describeExplanation } from "@/features/movies/timeline";

type Tone = "neutral" | "success" | "danger" | "warning" | "info";

export const REQUEST_TONE: Record<RequestStatus, Tone> = {
  pending: "warning",
  approved: "info",
  processing: "info",
  partial: "warning",
  available: "success",
  declined: "danger",
  cancelled: "neutral",
  failed: "danger",
};

export function RequestStatusBadge({ status }: { status: RequestStatus }) {
  const { t } = useTranslation();
  return (
    <span data-testid="request-status" data-status={status}>
      <Badge tone={REQUEST_TONE[status]}>{t(`requests.status.${status}`)}</Badge>
    </span>
  );
}

/** The quota bucket a request kind counts against. */
export const quotaKind = (k: RequestKind): "movie" | "series" | "music" => (k === "album" || k === "artist" ? "music" : k);

export function useRequestSummary() {
  return useQuery({
    queryKey: queryKeys.requestSummary,
    queryFn: async () => unwrap(await api.GET("/requests/summary")),
    staleTime: 30_000,
  });
}

/** "2 of 5 left this week" style text. */
export function quotaText(t: TFunction, q: QuotaStatus | undefined): string {
  if (!q) return "";
  if (q.unlimited) return t("requests.quota.unlimited");
  return t("requests.quota.remaining", { remaining: q.remaining ?? 0, limit: q.limit, days: q.days });
}

export function QuotaLine({ kind, summary, label }: { kind: RequestKind; summary?: RequestSummary; label?: string }) {
  const { t, i18n } = useTranslation();
  const q = summary?.quotas[quotaKind(kind)];
  if (!q) return null;
  const out = !q.unlimited && (q.remaining ?? 0) <= 0;
  return (
    <p className={`text-[12px] ${out ? "text-danger" : "text-faint"}`} data-testid="quota-line">
      {label ? `${label}: ` : ""}
      {quotaText(t, q)}
      {out && q.nextSlotAt ? ` ${t("requests.quota.nextSlot", { when: formatRelative(q.nextSlotAt, i18n.language) })}` : ""}
    </p>
  );
}

/** The library's "why isn't it here yet" for a request, in the user's language. */
export function requestExplanation(t: TFunction, r: MediaRequest, locale: string): string {
  if (!r.explanation) return "";
  const ex: Explanation = { code: r.explanation.code, tone: r.explanation.tone, message: r.explanation.message,
    params: r.explanation.params ?? {}, reasons: [] };
  return describeExplanation(t, ex, locale);
}

export function ProgressBar({ value, label }: { value: number; label?: string }) {
  const pct = Math.max(0, Math.min(100, Math.round(value * 100)));
  return (
    <div className="flex items-center gap-2" data-testid="request-progress">
      <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-3" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
        <div className="h-full rounded-full bg-info transition-[width] duration-500" style={{ width: `${pct}%` }} />
      </div>
      <span className="w-9 text-right text-[11px] tabular-nums text-muted">{pct}%</span>
    </div>
  );
}

/** What was asked for: "Seasons 1, 2", "4K", "All seasons and future ones". */
export function requestScope(t: TFunction, r: MediaRequest): string {
  const parts: string[] = [t(`requests.kinds.${r.kind}`)];
  if (r.kind === "series") {
    if (r.allSeasons) parts.push(r.futureSeasons ? t("requests.allAndFuture") : t("requests.allSeasons"));
    else if (r.seasons.length) parts.push(t("requests.seasonList", { list: r.seasons.map((s) => s.number).join(", ") }));
    if (r.futureSeasons && !r.allSeasons) parts.push(t("requests.futureToo"));
  }
  if (r.is4k) parts.push("4K");
  return parts.join(" · ");
}

/** A comment thread with a box to add one. */
export function CommentThread({ comments, loading, error, onAdd, adding, addError, testId }: {
  comments?: Comment[]; loading: boolean; error: unknown; onAdd: (body: string) => void; adding: boolean; addError: unknown; testId?: string;
}) {
  const { t, i18n } = useTranslation();
  const [body, setBody] = useState("");
  return (
    <div className="flex flex-col gap-3" data-testid={testId}>
      {loading ? <Spinner /> : null}
      <ErrorAlert error={error} />
      {comments && comments.length === 0 ? <EmptyState>{t("comments.none")}</EmptyState> : null}
      <ul className="flex flex-col gap-2">
        {comments?.map((c) => (
          <li key={c.id} className="rounded-ui border border-line bg-surface-2 px-3 py-2" data-testid="comment">
            <p className="text-[12px] text-faint">
              <span className="font-medium text-muted">{c.user.username}</span> · {formatDateTime(c.createdAt, i18n.language)}
            </p>
            <p className="mt-0.5 whitespace-pre-wrap text-[13px] text-fg">{c.body}</p>
          </li>
        ))}
      </ul>
      <form className="flex flex-col gap-2" onSubmit={(e) => {
        e.preventDefault();
        if (!body.trim()) return;
        onAdd(body.trim());
        setBody("");
      }}>
        <label className="text-[12px] font-medium text-muted" htmlFor={`${testId ?? "c"}-body`}>{t("comments.add")}</label>
        <textarea id={`${testId ?? "c"}-body`} value={body} onChange={(e) => setBody(e.target.value)} maxLength={2000} rows={2}
          className="w-full rounded-ui border border-line-strong bg-surface-2 px-2.5 py-1.5 text-[13px] text-fg placeholder:text-faint focus:border-info focus:outline-none" />
        <ErrorAlert error={addError} />
        <div>
          <Button type="submit" busy={adding} disabled={!body.trim()} data-testid="comment-send">{t("comments.send")}</Button>
        </div>
      </form>
    </div>
  );
}

export function RequestComments({ id }: { id: number }) {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: queryKeys.requestComments(id), queryFn: async () =>
    unwrap(await api.GET("/requests/{id}/comments", { params: { path: { id } } })) });
  const add = useMutation({
    mutationFn: async (body: string) => unwrap(await api.POST("/requests/{id}/comments", { params: { path: { id } }, body: { body } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.requestComments(id) });
      void qc.invalidateQueries({ queryKey: queryKeys.requests });
    },
  });
  return <CommentThread comments={list.data} loading={list.isPending} error={list.error} onAdd={(b) => add.mutate(b)}
    adding={add.isPending} addError={add.error} testId="request-comments" />;
}
