import type { TimelineEvent } from "@/api/client";

/**
 * One line of an item's timeline: a single event, or one search cycle (the
 * searches of all query forms tried in a row, plus the decision made on
 * their results). The stored rows stay as they are; this is presentation.
 */
export type TimelineRow =
  | { kind: "event"; key: string; event: TimelineEvent }
  | { kind: "cycle"; key: string; searches: TimelineEvent[]; decision?: TimelineEvent };

/** Searches further apart than this belong to different cycles. */
export const CYCLE_GAP_MS = 2 * 60_000;

type Data = Record<string, unknown>;
type IndexerLine = { name: string; status: string; count: number };

const data = (e: TimelineEvent): Data => (e.data && typeof e.data === "object" ? (e.data as Data) : {});
const time = (iso: unknown) => new Date(String(iso)).getTime();

/** When a search finished (its start when the server did not say). */
function finished(e: TimelineEvent): number {
  const d = data(e);
  const f = d.finishedAt ? time(d.finishedAt) : NaN;
  return Number.isNaN(f) ? time(e.at) : f;
}

const source = (e: TimelineEvent) => String(data(e).source ?? "automatic");

/** Decisions that follow a search (RSS and held-back releases are decided without one). */
function fromSearch(e: TimelineEvent): boolean {
  const reason = String(data(e).reason ?? "");
  return e.type === "decision" && reason !== "rss" && reason !== "pending";
}

/**
 * Folds the timeline (newest first) into rows: each run of searches of one
 * item and source that follow each other within `gapMs`, with the decision
 * made right after them, becomes one cycle row. Nothing is dropped and the
 * order stays newest first.
 */
export function groupTimeline(events: TimelineEvent[], gapMs = CYCLE_GAP_MS): TimelineRow[] {
  const rows: TimelineRow[] = [];
  let i = 0;
  while (i < events.length) {
    const e = events[i]!;
    const next = events[i + 1];
    let decision: TimelineEvent | undefined;
    let start = i;
    if (fromSearch(e) && next?.type === "search" && next.itemRef === e.itemRef && time(e.at) - finished(next) <= gapMs) {
      decision = e;
      start = i + 1;
    }
    const first = events[start]!;
    if (first.type !== "search") {
      rows.push({ kind: "event", key: rowKey(e), event: e });
      i++;
      continue;
    }
    const searches = [first];
    let j = start + 1;
    while (j < events.length) {
      const older = events[j]!;
      const newer = searches[searches.length - 1]!;
      if (older.type !== "search" || older.itemRef !== newer.itemRef || source(older) !== source(newer) || time(newer.at) - finished(older) > gapMs) break;
      searches.push(older);
      j++;
    }
    rows.push({ kind: "cycle", key: rowKey(decision ?? first), searches, decision });
    i = j;
  }
  return rows;
}

/** The id the history API knows a timeline row by ("t:12", "s:7", "d:3"). */
export function rowKey(e: TimelineEvent): string {
  const prefix = e.source === "search" ? "s" : e.source === "download" ? "d" : "t";
  return `${prefix}:${e.id}`;
}

/** Every row id of a timeline line. */
export function rowIds(row: TimelineRow): string[] {
  if (row.kind === "event") return [rowKey(row.event)];
  return [...(row.decision ? [rowKey(row.decision)] : []), ...row.searches.map(rowKey)];
}

/** The newest event of a line (its time and item). */
export function rowEvent(row: TimelineRow): TimelineEvent {
  return row.kind === "event" ? row.event : row.decision ?? row.searches[0]!;
}

/**
 * One cycle in numbers: results per indexer added up over the query forms
 * (an indexer that answered once counts as ok), the total, and the queries
 * in the order they were tried.
 */
export function cycleSummary(row: Extract<TimelineRow, { kind: "cycle" }>): { total: number; indexers: IndexerLine[]; queries: string[]; source: string } {
  const by = new Map<string, IndexerLine>();
  let total = 0;
  const oldestFirst = [...row.searches].reverse();
  for (const s of oldestFirst) {
    const d = data(s);
    total += Number(d.resultCount ?? 0);
    for (const x of (d.indexers as IndexerLine[] | undefined) ?? []) {
      const cur = by.get(x.name);
      if (!cur) by.set(x.name, { name: x.name, status: x.status, count: x.status === "ok" ? x.count : 0 });
      else if (x.status === "ok") {
        cur.status = "ok";
        cur.count += x.count;
      }
    }
  }
  const queries = oldestFirst.map((s) => String(data(s).query ?? s.message)).filter(Boolean);
  return { total, indexers: [...by.values()].sort((a, b) => a.name.localeCompare(b.name)), queries, source: source(row.searches[0]!) };
}
