import type { TFunction } from "i18next";
import type { HistoryEntry, TimelineEvent } from "@/api/client";
import { describeEvent, grabFailureReason } from "@/features/movies/timeline";

type Data = Record<string, unknown>;

/** One line of the history page: a single entry or a run of similar failures. */
export type HistoryRow = { key: string; first: HistoryEntry; entries: HistoryEntry[] };

/** Failures further apart than this belong to different search cycles. */
export const GROUP_WINDOW_MS = 15 * 60_000;

const data = (e: HistoryEntry): Data => (e.data && typeof e.data === "object" ? (e.data as Data) : {});
const str = (v: unknown) => (v === undefined || v === null ? "" : String(v));

/** What makes two failures "the same kind": the event, the item, the reason code and the indexer. */
export function groupKey(e: HistoryEntry): string | null {
  if (e.category !== "failed") return null;
  const d = data(e);
  return [e.type, e.itemRef, str(d.code) || (e.type === "grab_failed" ? "other" : ""), e.indexer ?? str(d.indexer)].join("|");
}

/**
 * Folds consecutive failures of the same kind for the same item, close in
 * time (one search cycle), into one row. The list stays newest first and no
 * entry is dropped.
 */
export function groupHistory(entries: HistoryEntry[], windowMs = GROUP_WINDOW_MS): HistoryRow[] {
  const rows: HistoryRow[] = [];
  let lastKey: string | null = null;
  for (const e of entries) {
    const k = groupKey(e);
    const prev = rows.at(-1);
    if (k !== null && prev && k === lastKey) {
      const last = prev.entries.at(-1)!;
      if (Math.abs(new Date(last.at).getTime() - new Date(e.at).getTime()) <= windowMs) {
        prev.entries.push(e);
        continue;
      }
    }
    rows.push({ key: e.id, first: e, entries: [e] });
    lastKey = k;
  }
  return rows;
}

const URL_RE = /https?:\/\/[^\s"'<>`]+/g;

/** Replaces every URL in a text by its host and path ("…" for a query), for one-line display. */
export function shortenUrls(text: string): string {
  return text.replace(URL_RE, (raw) => {
    let url = raw;
    let trail = "";
    while (url && ".,;:)]}".includes(url.at(-1)!)) {
      trail = url.at(-1)! + trail;
      url = url.slice(0, -1);
    }
    try {
      const u = new URL(url);
      const path = u.pathname === "/" ? "" : u.pathname;
      return `${u.host}${path}${u.search ? "?…" : ""}${trail}`;
    } catch {
      return raw;
    }
  });
}

/** The history entry as a timeline event, so the timeline's wording is reused. */
function asEvent(e: HistoryEntry): TimelineEvent {
  return { id: 0, at: e.at, type: e.type, source: e.id.startsWith("d:") ? "download" : "timeline", message: e.message, data: data(e) };
}

/** The short reason of one entry in the user's language (URLs shortened). */
export function entryReason(t: TFunction, e: HistoryEntry, locale: string): string {
  const d = data(e);
  if (e.type === "grab_failed") {
    if (str(d.code) === "attempts_exhausted") return t("timeline.grabErrors.attempts_exhausted", { attempts: Number(d.attempts ?? 0) });
    const r = grabFailureReason(t, d, locale);
    if (r) return shortenUrls(r);
    if (e.message) return shortenUrls(e.message);
  }
  const text = e.type.startsWith("download_") ? e.message || describeEvent(t, asEvent(e), locale) : describeEvent(t, asEvent(e), locale);
  return shortenUrls(text || e.message);
}

/** The one-line summary of a group of failures. */
export function groupSummary(t: TFunction, row: HistoryRow, locale: string): string {
  return t("history.group.failed", { count: row.entries.length, reason: entryReason(t, row.first, locale) });
}

/** The title shown for an entry: the item as it was, or "removed item" with the release. */
export function entryTitle(t: TFunction, e: HistoryEntry): string {
  const title = e.itemTitle ?? e.title;
  if (title) return title;
  if (e.itemGone || e.itemRef.includes("#")) return t("history.removedItem");
  return e.release || e.title || e.itemRef;
}
