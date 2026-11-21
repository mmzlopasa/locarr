import type { MediaKind, Quality, QualityDefinition, QualityProfileItem } from "@/api/client";

/** The cutoff key of an item: the quality id, or "group:<name>". */
export function itemKey(item: QualityProfileItem): string {
  return item.quality ? item.quality : `group:${item.name ?? ""}`;
}

export function itemMembers(item: QualityProfileItem): string[] {
  return item.quality ? [item.quality] : (item.qualities ?? []);
}

/** Moves the item at `from` to position `to` (both in the current list). */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return list;
  const next = list.slice();
  const [it] = next.splice(from, 1) as [T];
  next.splice(to, 0, it);
  return next;
}

/** Merges the item at `index` with the one below it into a group. */
export function groupWithNext(items: QualityProfileItem[], index: number, name: string): QualityProfileItem[] {
  if (index < 0 || index >= items.length - 1) return items;
  const a = items[index];
  const b = items[index + 1];
  if (!a || !b) return items;
  const merged: QualityProfileItem = {
    name: a.quality ? name : (a.name ?? name),
    qualities: [...itemMembers(a), ...itemMembers(b)],
    allowed: a.allowed || b.allowed,
  };
  return [...items.slice(0, index), merged, ...items.slice(index + 2)];
}

/** Splits a group back into single qualities (keeping its allowed state). */
export function ungroup(items: QualityProfileItem[], index: number): QualityProfileItem[] {
  const g = items[index];
  if (!g || g.quality) return items;
  const singles = (g.qualities ?? []).map((q) => ({ quality: q, allowed: g.allowed }));
  return [...items.slice(0, index), ...singles, ...items.slice(index + 1)];
}

/** Keeps the cutoff valid: when it is gone or not allowed, picks the best allowed item. */
export function fixCutoff(items: QualityProfileItem[], cutoff: string): string {
  const it = items.find((i) => itemKey(i) === cutoff);
  if (it?.allowed) return cutoff;
  const best = items.find((i) => i.allowed);
  return best ? itemKey(best) : cutoff;
}

/** Fixes keys that changed when a group was renamed or ungrouped. */
export function cutoffAfterChange(before: QualityProfileItem[], after: QualityProfileItem[], cutoff: string): string {
  if (after.some((i) => itemKey(i) === cutoff)) return fixCutoff(after, cutoff);
  const old = before.find((i) => itemKey(i) === cutoff);
  const member = old ? itemMembers(old)[0] : undefined;
  const now = member ? after.find((i) => itemMembers(i).includes(member)) : undefined;
  return fixCutoff(after, now ? itemKey(now) : cutoff);
}

/** A starting profile for a kind: every quality, none allowed except the first. */
export function blankItems(qualities: Quality[], kind: MediaKind): QualityProfileItem[] {
  return qualities
    .filter((q) => q.music === (kind === "music"))
    .slice()
    .reverse()
    .map((q, i) => ({ quality: q.id, allowed: i === 0 }));
}

/** Bytes for a definition limit (MB per minute) over a runtime. */
export function limitBytes(mbPerMinute: number, minutes: number): number {
  return Math.round(mbPerMinute * minutes * 1024 * 1024);
}

/** "4.4 GB" / "850 MB" (decimal style used in release sizes, base 1024). */
export function formatSize(bytes: number, locale: string): string {
  const gb = bytes / 1024 ** 3;
  const nf = new Intl.NumberFormat(locale, { maximumFractionDigits: gb >= 1 ? 1 : 0 });
  if (gb >= 1) return `${nf.format(gb)} GB`;
  return `${nf.format(bytes / 1024 ** 2)} MB`;
}

/** The range shown for a definition at a runtime ("–" bounds mean no limit). */
export function previewRange(d: Pick<QualityDefinition, "min" | "preferred" | "max">, minutes: number, locale: string) {
  return {
    min: d.min > 0 ? formatSize(limitBytes(d.min, minutes), locale) : "0",
    preferred: d.preferred > 0 ? formatSize(limitBytes(d.preferred, minutes), locale) : null,
    max: d.max > 0 ? formatSize(limitBytes(d.max, minutes), locale) : null,
  };
}

/** Puts min ≤ preferred ≤ max after one slider moved. */
export function clampDefinition<T extends Pick<QualityDefinition, "min" | "preferred" | "max">>(
  d: T,
  field: "min" | "preferred" | "max",
  value: number,
): T {
  const next = { ...d, [field]: Math.max(0, Math.round(value * 10) / 10) };
  if (field === "min") {
    if (next.max > 0 && next.min > next.max) next.max = next.min;
    if (next.preferred > 0 && next.preferred < next.min) next.preferred = next.min;
  } else if (field === "max") {
    if (next.max > 0 && next.min > next.max) next.min = next.max;
    if (next.max > 0 && next.preferred > next.max) next.preferred = next.max;
  } else if (next.preferred > 0) {
    if (next.preferred < next.min) next.min = next.preferred;
    if (next.max > 0 && next.preferred > next.max) next.max = next.preferred;
  }
  return next;
}

/** Parses the terms textarea (one term per line). */
export function parseTerms(text: string): string[] {
  return text
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
}

/** Checks a /regex/ term in the browser so mistakes show before saving. */
export function termError(term: string): string | null {
  const m = /^\/(.*)\/(i?)$/.exec(term);
  if (!m) return null;
  try {
    new RegExp((m[1] ?? "").replace(/\(\?<([=!])/g, "(?<$1"), "i");
    return null;
  } catch (e) {
    return e instanceof Error ? e.message : String(e);
  }
}
