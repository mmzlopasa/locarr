import { useMemo, useState, type ComponentType } from "react";
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Bell, Captions, Check, ChevronDown, CircleArrowUp, CircleCheck, CircleX, Copy, Download, PencilLine, Trash2,
} from "lucide-react";
import { api, queryKeys, unwrap, type HistoryCategory, type HistoryEntry, type MediaKind } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { Modal } from "@/components/Modal";
import { Badge, Button, ConfirmButton, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useMe } from "@/features/auth/hooks";
import { itemHref } from "@/features/board/board";
import { entryReason, entryTitle, groupHistory, groupSummary, type HistoryRow } from "./history";

export const HISTORY_CATEGORIES: HistoryCategory[] = ["grabbed", "imported", "upgraded", "failed", "deleted", "renamed", "subtitles", "notifications"];
const KINDS: MediaKind[] = ["movies", "series", "music"];
const PAGE = 100;

type Tone = "neutral" | "success" | "danger" | "warning" | "info";

const tone: Record<HistoryCategory, Tone> = {
  grabbed: "info",
  imported: "success",
  upgraded: "success",
  failed: "danger",
  deleted: "warning",
  renamed: "neutral",
  subtitles: "info",
  notifications: "neutral",
};

const icons: Record<HistoryCategory, ComponentType<{ className?: string }>> = {
  grabbed: Download,
  imported: CircleCheck,
  upgraded: CircleArrowUp,
  failed: CircleX,
  deleted: Trash2,
  renamed: PencilLine,
  subtitles: Captions,
  notifications: Bell,
};

// Token-based tints for the event icon (no new colours).
const iconTone: Record<Tone, string> = {
  neutral: "bg-surface-3 text-muted",
  success: "bg-success-bg text-success",
  danger: "bg-danger-bg text-danger",
  warning: "bg-warning-bg text-warning",
  info: "bg-info-bg text-info",
};

export function HistoryCategoryBadge({ category }: { category: HistoryCategory }) {
  const { t } = useTranslation();
  return <Badge tone={tone[category]}>{t(`history.categories.${category}`)}</Badge>;
}

function CategoryIcon({ category }: { category: HistoryCategory }) {
  const { t } = useTranslation();
  const Icon = icons[category];
  return (
    <span className={`mt-0.5 inline-flex size-7 shrink-0 items-center justify-center rounded-ui ${iconTone[tone[category]]}`}
      title={t(`history.categories.${category}`)} data-testid={`history-icon-${category}`}>
      <Icon className="size-4" aria-hidden />
      <span className="sr-only">{t(`history.categories.${category}`)}</span>
    </span>
  );
}

/** The title of a history entry, linked to its page while the item exists. */
export function HistoryTitle({ e, className = "" }: { e: HistoryEntry; className?: string }) {
  const { t } = useTranslation();
  const href = e.itemRef && !e.itemGone ? itemHref({ kind: "movie", itemRef: e.itemRef, parentRef: "" }) : null;
  const title = entryTitle(t, e);
  const gone = !href && (e.itemGone || e.itemRef.includes("#"));
  if (href) return <Link to={href} className={`text-fg hover:underline ${className}`}>{title}</Link>;
  // An older entry of an item that is gone has no stored title: name the release next to it.
  if (gone && !e.itemTitle && e.release) {
    return <span className={`text-muted ${className}`} title={e.release}>{title} <span className="text-faint">· {e.release}</span></span>;
  }
  return <span className={`${gone ? "text-muted" : "text-fg"} ${className}`}>{title}</span>;
}

function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation();
  const [done, setDone] = useState(false);
  return (
    <button type="button" className="inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg"
      onClick={() => {
        void navigator.clipboard?.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        });
      }} data-testid="history-copy">
      {done ? <Check className="size-3.5" aria-hidden /> : <Copy className="size-3.5" aria-hidden />}
      {done ? t("history.copied") : t("history.copy")}
    </button>
  );
}

/** Everything known about one entry: release, indexer, client and the full message. */
function EntryDetails({ e, reason, onDelete, deleting }: { e: HistoryEntry; reason: string; onDelete: () => void; deleting: boolean }) {
  const { t, i18n } = useTranslation();
  return (
    <div className="flex flex-col gap-1.5 text-[12px]" data-testid="history-detail">
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-muted">
        <dt className="text-faint">{t("history.when")}</dt>
        <dd>{formatDateTime(e.at, i18n.language)}</dd>
        {e.release ? <><dt className="text-faint">{t("history.release")}</dt><dd className="break-words [overflow-wrap:anywhere]">{e.release}</dd></> : null}
        {e.indexer ? <><dt className="text-faint">{t("history.indexerLabel")}</dt><dd className="break-words">{e.indexer}</dd></> : null}
        {e.client ? <><dt className="text-faint">{t("history.clientLabel")}</dt><dd className="break-words">{e.client}</dd></> : null}
      </dl>
      {e.message && e.message !== reason ? (
        <div className="rounded-ui border border-line bg-surface-2 px-2.5 py-2">
          <p className="whitespace-pre-wrap break-words font-mono text-[11px] text-muted [overflow-wrap:anywhere]" data-testid="history-message">{e.message}</p>
          <div className="mt-1.5 flex justify-end"><CopyButton text={e.message} /></div>
        </div>
      ) : null}
      <div className="flex justify-end">
        <ConfirmButton label={t("history.deleteEntry")} confirmLabel={t("history.deleteEntryConfirm")} onConfirm={onDelete} busy={deleting}
          testId={`history-delete-${e.id}`} />
      </div>
    </div>
  );
}

function Row({ row, onDelete, deleting }: { row: HistoryRow; onDelete: (ids: string[]) => void; deleting: boolean }) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const e = row.first;
  const grouped = row.entries.length > 1;
  const reason = grouped ? groupSummary(t, row, i18n.language) : entryReason(t, e, i18n.language);
  const oldest = row.entries.at(-1) ?? e;
  return (
    <li className="py-2.5" data-testid={grouped ? "history-group" : "history-row"} data-id={e.id}>
      <div className="flex min-w-0 items-start gap-3">
        <CategoryIcon category={e.category} />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-baseline gap-2">
            <HistoryTitle e={grouped ? { ...e, release: undefined } : e} className="min-w-0 truncate text-[13px] font-medium" />
            {grouped ? <Badge tone="danger">{t("history.times", { count: row.entries.length })}</Badge> : null}
            <time className="ml-auto shrink-0 text-[12px] tabular-nums text-faint" dateTime={e.at}
              title={grouped ? `${formatDateTime(oldest.at, i18n.language)} – ${formatDateTime(e.at, i18n.language)}` : formatDateTime(e.at, i18n.language)}>
              {formatRelative(e.at, i18n.language)}
            </time>
          </div>
          <button type="button" onClick={() => setOpen(!open)} aria-expanded={open}
            className="mt-0.5 flex w-full min-w-0 items-center gap-1 text-left text-[12px] text-muted hover:text-fg" data-testid="history-toggle">
            <span className="shrink-0 text-faint">{t(`history.categories.${e.category}`)}</span>
            <span className="text-faint" aria-hidden>·</span>
            <span className="min-w-0 flex-1 truncate" data-testid="history-reason">{reason}</span>
            <ChevronDown className={`size-3.5 shrink-0 transition-transform ${open ? "rotate-180" : ""}`} aria-hidden />
          </button>
          {open ? (
            <div className="mt-2">
              {grouped ? (
                <div className="flex flex-col gap-2">
                  <ul className="flex flex-col divide-y divide-line rounded-ui border border-line" data-testid="history-group-list">
                    {row.entries.map((x) => (
                      <li key={x.id} className="min-w-0 px-2.5 py-1.5 text-[12px]">
                        <div className="flex min-w-0 items-baseline gap-2">
                          <span className="min-w-0 flex-1 break-words text-fg [overflow-wrap:anywhere]">{x.release || entryTitle(t, x)}</span>
                          <time className="shrink-0 tabular-nums text-faint" title={formatDateTime(x.at, i18n.language)}>
                            {formatRelative(x.at, i18n.language)}
                          </time>
                        </div>
                        <p className="break-words text-muted [overflow-wrap:anywhere]">{entryReason(t, x, i18n.language)}</p>
                      </li>
                    ))}
                  </ul>
                  <div className="flex justify-end">
                    <ConfirmButton label={t("history.deleteGroup", { count: row.entries.length })}
                      confirmLabel={t("history.deleteEntryConfirm")} busy={deleting}
                      onConfirm={() => onDelete(row.entries.map((x) => x.id))} testId={`history-delete-group-${e.id}`} />
                  </div>
                </div>
              ) : (
                <EntryDetails e={e} reason={reason} deleting={deleting} onDelete={() => onDelete([e.id])} />
              )}
            </div>
          ) : null}
        </div>
      </div>
    </li>
  );
}

type Scope = "all" | "older" | "failed";
type Filters = { category: HistoryCategory | ""; kind: MediaKind | ""; q: string };

/** Asks what to clear, shows how many entries that is and deletes them. */
function ClearDialog({ filters, onClose }: { filters: Filters; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [scope, setScope] = useState<Scope>("all");
  const [days, setDays] = useState(90);
  const body = {
    dryRun: false,
    ...(scope === "failed" ? { category: "failed" as HistoryCategory } : filters.category ? { category: filters.category } : {}),
    ...(filters.kind ? { kind: filters.kind } : {}),
    ...(filters.q.trim() ? { q: filters.q.trim() } : {}),
    ...(scope === "older" ? { olderThanDays: Math.max(1, days) } : {}),
  };
  const count = useQuery({
    queryKey: ["history-clear-count", body],
    queryFn: async () => unwrap(await api.POST("/history/clear", { body: { ...body, dryRun: true } })),
  });
  const clear = useMutation({
    mutationFn: async () => unwrap(await api.POST("/history/clear", { body })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["history"] });
      onClose();
    },
  });
  const filtered = !!(filters.category || filters.kind || filters.q.trim());
  const n = count.data?.count ?? 0;
  const options: { v: Scope; label: string }[] = [
    { v: "all", label: filtered ? t("history.clear.allFiltered") : t("history.clear.all") },
    { v: "older", label: t("history.clear.older") },
  ];
  if (!filters.category || filters.category === "failed") options.push({ v: "failed", label: t("history.clear.failed") });
  return (
    <Modal title={t("history.clear.title")} onClose={onClose} testId="history-clear-dialog"
      footer={<>
        <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
        <Button variant="danger" busy={clear.isPending} disabled={count.isPending || n === 0} onClick={() => clear.mutate()}
          data-testid="history-clear-confirm">
          {t("history.clear.confirm", { count: n })}
        </Button>
      </>}>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-[13px] text-muted">{t("history.clear.what")}</legend>
        {options.map((o) => (
          <label key={o.v} className="flex items-center gap-2 text-[13px] text-fg">
            <input type="radio" name="history-clear-scope" value={o.v} checked={scope === o.v} onChange={() => setScope(o.v)}
              className="accent-accent" data-testid={`history-clear-${o.v}`} />
            {o.label}
          </label>
        ))}
        {scope === "older" ? (
          <div className="w-40 pl-6">
            <TextInput label={t("history.clear.days")} type="number" min={1} max={36500} value={String(days)}
              onChange={(e) => setDays(Math.max(1, Number(e.target.value) || 1))} data-testid="history-clear-days" />
          </div>
        ) : null}
      </fieldset>
      {filtered ? <p className="mt-3 text-[12px] text-faint">{t("history.clear.filterNote")}</p> : null}
      <div className="mt-3 rounded-ui border border-line bg-surface-2 px-3 py-2 text-[13px]" data-testid="history-clear-count">
        {count.isPending ? <Spinner /> : <p className="text-fg">{n === 0 ? t("history.clear.none") : t("history.clear.count", { count: n })}</p>}
        <p className="mt-1 text-[12px] text-muted">{t("history.clear.shared")}</p>
      </div>
      <div className="mt-2"><ErrorAlert error={count.error ?? clear.error} /></div>
    </Modal>
  );
}

/** Everything that happened across the library: grabs, imports, failures, renames, subtitles, notifications. */
export function HistoryPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const [category, setCategory] = useState<HistoryCategory | "">("");
  const [kind, setKind] = useState<MediaKind | "">("");
  const [q, setQ] = useState("");
  const [clearing, setClearing] = useState(false);
  const filter = { category, kind, q };
  const list = useInfiniteQuery({
    queryKey: queryKeys.history(filter),
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/history", {
        params: { query: { ...(category ? { category } : {}), ...(kind ? { kind } : {}), ...(q.trim() ? { q: q.trim() } : {}), offset: pageParam, limit: PAGE } },
      })),
    getNextPageParam: (last, pages) => (last.hasMore ? pages.length * PAGE : undefined),
    placeholderData: keepPreviousData,
  });
  const entries = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const rows = useMemo(() => groupHistory(entries), [entries]);
  const retention = list.data?.pages[0]?.retentionDays;
  const remove = useMutation({
    mutationFn: async (ids: string[]) => {
      for (const id of ids) {
        const res = await api.DELETE("/history/{id}", { params: { path: { id } } });
        if (res.error && res.response.status !== 404) unwrap(res);
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["history"] }),
  });
  const filtered = !!(category || kind || q.trim());
  return (
    <Panel title={t("history.title")} description={t("history.subtitle")}
      actions={entries.length > 0 ? (
        <Button variant="ghost" onClick={() => setClearing(true)} data-testid="history-clear">
          <Trash2 className="size-3.5" aria-hidden />{t("history.clear.open")}
        </Button>
      ) : undefined}>
      <div className="mb-3 grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-end">
        <div className="sm:w-44">
          <SelectInput label={t("history.category")} value={category} data-testid="history-category"
            onChange={(e) => setCategory(e.target.value as HistoryCategory | "")}>
            <option value="">{t("history.all")}</option>
            {HISTORY_CATEGORIES.map((c) => <option key={c} value={c}>{t(`history.categories.${c}`)}</option>)}
          </SelectInput>
        </div>
        <div className="sm:w-36">
          <SelectInput label={t("history.kind")} value={kind} data-testid="history-kind"
            onChange={(e) => setKind(e.target.value as MediaKind | "")}>
            <option value="">{t("history.all")}</option>
            {KINDS.map((k) => <option key={k} value={k}>{t(`mediaKinds.${k}`)}</option>)}
          </SelectInput>
        </div>
        <div className="col-span-2 sm:w-56">
          <TextInput label={t("history.search")} value={q} data-testid="history-search" onChange={(e) => setQ(e.target.value)} />
        </div>
      </div>
      {list.isPending ? <Spinner /> : null}
      <ErrorAlert error={list.error ?? remove.error} />
      {list.isSuccess && entries.length === 0 ? (
        <div className="flex flex-col items-center gap-1 py-10 text-center" data-testid="history-empty">
          <p className="text-[13px] text-fg">{filtered ? t("history.noMatch") : t("history.none")}</p>
          <p className="max-w-sm text-[12px] text-faint">{filtered ? t("history.noMatchHint") : t("history.noneHint")}</p>
          {filtered ? (
            <div className="mt-2">
              <Button variant="ghost" onClick={() => { setCategory(""); setKind(""); setQ(""); }} data-testid="history-reset">
                {t("history.resetFilters")}
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
      <ul className="divide-y divide-line" data-testid="history-list">
        {rows.map((row) => <Row key={row.key} row={row} deleting={remove.isPending} onDelete={(ids) => remove.mutate(ids)} />)}
      </ul>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <p className="text-[12px] text-faint" data-testid="history-retention">
          {retention === undefined ? null : retention === 0 ? t("history.retentionForever") : t("history.retention", { count: retention })}
          {retention !== undefined && me?.role === "admin" ? (
            <> <Link to="/settings/system" className="text-muted underline-offset-2 hover:text-fg hover:underline">{t("history.retentionChange")}</Link></>
          ) : null}
        </p>
        {list.hasNextPage ? (
          <Button variant="ghost" busy={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()} data-testid="history-more">
            {t("history.more")}
          </Button>
        ) : null}
      </div>
      {clearing ? <ClearDialog filters={filter} onClose={() => setClearing(false)} /> : null}
    </Panel>
  );
}
