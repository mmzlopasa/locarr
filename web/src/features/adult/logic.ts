import type { components } from "@/api/schema.gen";

export type AdultItemStatus = components["schemas"]["AdultItemStatus"];
export type LookupSort = "newest" | "oldest" | "relevance" | "title";
export type ItemSort = "date" | "added" | "title";
export type ViewMode = "grid" | "list";

export const LOOKUP_SORTS: LookupSort[] = ["newest", "oldest", "relevance", "title"];
export const ITEM_SORTS: ItemSort[] = ["date", "added", "title"];
export const ITEM_STATUSES: AdultItemStatus[] = ["wanted", "downloading", "downloaded", "missing"];

export const STATUS_TONE: Record<AdultItemStatus, "success" | "info" | "warning" | "neutral"> = {
  downloaded: "success",
  downloading: "info",
  wanted: "warning",
  missing: "neutral",
};

/** The first letter or digit of a name, for the image placeholder. */
export function initial(name: string | undefined): string {
  const m = (name ?? "").match(/[\p{L}\p{N}]/u);
  return m ? m[0].toLocaleUpperCase() : "?";
}

/**
 * Per-viewer conveniences (sort, view) kept in this browser only. Storage
 * can be missing or refuse (private windows, blocked site data): the
 * default is used then.
 */
export function loadPref<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const v = localStorage.getItem(key);
    return v && (allowed as readonly string[]).includes(v) ? (v as T) : fallback;
  } catch {
    return fallback;
  }
}

export function savePref(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // only a convenience
  }
}

export const PREF = {
  lookupSort: "locarr.adult.lookupSort",
  itemSort: "locarr.adult.itemSort",
  view: "locarr.adult.view",
} as const;

/** Toggle one id in a selection set (a new set, for React state). */
export function toggled(set: ReadonlySet<number>, id: number): Set<number> {
  const next = new Set(set);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}
