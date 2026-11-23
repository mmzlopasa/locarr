import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type HistoryCategory, type HistoryEntry, type MediaKind } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { Badge, Button, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { itemHref } from "@/features/board/board";

export const HISTORY_CATEGORIES: HistoryCategory[] = ["grabbed", "imported", "upgraded", "failed", "deleted", "renamed", "subtitles", "notifications"];
const KINDS: MediaKind[] = ["movies", "series", "music"];
const PAGE = 50;

const tone: Record<HistoryCategory, "neutral" | "success" | "danger" | "warning" | "info"> = {
  grabbed: "info",
  imported: "success",
  upgraded: "success",
  failed: "danger",
  deleted: "warning",
  renamed: "neutral",
  subtitles: "info",
  notifications: "neutral",
};

export function HistoryCategoryBadge({ category }: { category: HistoryCategory }) {
  const { t } = useTranslation();
  return <Badge tone={tone[category]}>{t(`history.categories.${category}`)}</Badge>;
}

/** The title of a history entry linked to its page when it has one. */
export function HistoryTitle({ e }: { e: HistoryEntry }) {
  const href = e.itemRef ? itemHref({ kind: "movie", itemRef: e.itemRef, parentRef: "" }) : null;
  const title = e.title || e.release || e.itemRef;
  return href ? <Link to={href} className="text-fg hover:underline">{title}</Link> : <span className="text-fg">{title}</span>;
}

/** Everything that happened across the library: grabs, imports, failures, renames, subtitles, notifications. */
export function HistoryPage() {
  const { t, i18n } = useTranslation();
  const [category, setCategory] = useState<HistoryCategory | "">("");
  const [kind, setKind] = useState<MediaKind | "">("");
  const [q, setQ] = useState("");
  const [offset, setOffset] = useState(0);
  const filter = { category, kind, q, offset };
  const list = useQuery({
    queryKey: queryKeys.history(filter),
    queryFn: async () =>
      unwrap(await api.GET("/history", {
        params: { query: { ...(category ? { category } : {}), ...(kind ? { kind } : {}), ...(q.trim() ? { q: q.trim() } : {}), offset, limit: PAGE } },
      })),
    placeholderData: keepPreviousData,
  });
  const reset = () => setOffset(0);
  return (
    <Panel title={t("history.title")} description={t("history.subtitle")}>
      <div className="mb-3 flex flex-wrap items-end gap-2">
        <div className="w-44">
          <SelectInput label={t("history.category")} value={category} data-testid="history-category"
            onChange={(e) => { setCategory(e.target.value as HistoryCategory | ""); reset(); }}>
            <option value="">{t("history.all")}</option>
            {HISTORY_CATEGORIES.map((c) => <option key={c} value={c}>{t(`history.categories.${c}`)}</option>)}
          </SelectInput>
        </div>
        <div className="w-36">
          <SelectInput label={t("history.kind")} value={kind} data-testid="history-kind"
            onChange={(e) => { setKind(e.target.value as MediaKind | ""); reset(); }}>
            <option value="">{t("history.all")}</option>
            {KINDS.map((k) => <option key={k} value={k}>{t(`mediaKinds.${k}`)}</option>)}
          </SelectInput>
        </div>
        <div className="w-56">
          <TextInput label={t("history.search")} value={q} data-testid="history-search" onChange={(e) => { setQ(e.target.value); reset(); }} />
        </div>
      </div>
      {list.isPending ? <Spinner /> : null}
      <ErrorAlert error={list.error} />
      {list.data?.items.length === 0 ? <EmptyState>{t("history.none")}</EmptyState> : null}
      <ul className="divide-y divide-line" data-testid="history-list">
        {list.data?.items.map((e) => (
          <li key={e.id} className="flex flex-wrap items-start justify-between gap-2 py-2">
            <div className="min-w-0 flex-1">
              <p className="flex flex-wrap items-center gap-2 text-[13px]">
                <HistoryCategoryBadge category={e.category} />
                <HistoryTitle e={e} />
              </p>
              <p className="mt-0.5 flex flex-wrap gap-x-3 text-[12px] text-faint">
                {e.release && e.release !== e.title ? <span className="break-all">{e.release}</span> : null}
                {e.indexer ? <span>{t("history.indexer", { name: e.indexer })}</span> : null}
                {e.client ? <span>{t("history.client", { name: e.client })}</span> : null}
                {e.message ? <span className="break-words text-muted">{e.message}</span> : null}
              </p>
            </div>
            <span className="text-[12px] text-faint" title={formatDateTime(e.at, i18n.language)}>{formatRelative(e.at, i18n.language)}</span>
          </li>
        ))}
      </ul>
      <div className="mt-2 flex justify-end gap-1">
        <Button variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>{t("board.prev")}</Button>
        <Button variant="ghost" disabled={!list.data?.hasMore} onClick={() => setOffset(offset + PAGE)} data-testid="history-next">
          {t("board.next")}
        </Button>
      </div>
    </Panel>
  );
}
