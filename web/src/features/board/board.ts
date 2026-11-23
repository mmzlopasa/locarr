import type { TFunction } from "i18next";
import type { BoardItem, ReasonCount } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { describeExplanation } from "@/features/movies/timeline";

export const BOARD_KINDS = ["movie", "episode", "album", "subtitle"] as const;

export const BOARD_ACTIONS = ["search", "change_profile", "blocklist_retry", "retry_import", "unmonitor"] as const;
export type BoardActionName = (typeof BOARD_ACTIONS)[number];

/** Where each kind of fix is made: an in-app page or the troubleshooting guide. */
export const DOCS_BASE = "https://github.com/mmzlopasa/locarr/blob/main/docs/user/troubleshooting.md";

export function fixTarget(fix: string): { route?: string; docs?: string } | null {
  switch (fix) {
    case "path_mapping":
    case "client_category":
    case "download_clients":
      return { route: "/downloaders", docs: fix === "path_mapping" ? "remote-path-mapping" : "download-clients" };
    case "indexers":
      return { route: "/indexers", docs: "indexers" };
    case "disk_space":
      return { route: "/settings/media", docs: "disk-space" };
    case "subtitle_providers":
      return { route: "/settings/subtitles", docs: "subtitle-providers" };
    case "manual_import":
      return { route: "/movies/import", docs: "import-failed" };
    case "profile":
      return { route: "/settings/profiles", docs: "all-rejected" };
    case "permissions":
      return { docs: "permissions" };
    default:
      return null;
  }
}

/** Which actions make sense for a group (all are allowed; these are offered first). */
export function suggestedActions(code: string): BoardActionName[] {
  switch (code) {
    case "import_failed":
    case "path_not_visible":
    case "importing":
      return ["retry_import", "blocklist_retry", "unmonitor"];
    case "stalled":
    case "download_failed":
      return ["blocklist_retry", "search", "unmonitor"];
    case "all_rejected":
      return ["change_profile", "search", "unmonitor"];
    case "subtitle_not_found":
    case "subtitle_not_searched":
    case "subtitle_no_providers":
    case "subtitle_quota":
      return ["search"];
    default:
      return ["search", "change_profile", "unmonitor"];
  }
}

/** The numeric id at the end of a ref like "movie:12" (0 when there is none). */
export function refId(ref: string): number {
  const n = Number(ref.split(":").pop());
  return Number.isFinite(n) ? n : 0;
}

/** The page of a board item in the web client, or null when it has none. */
export function itemHref(item: Pick<BoardItem, "kind" | "itemRef" | "parentRef">): string | null {
  const [kind] = item.itemRef.split(":");
  const id = refId(item.itemRef);
  switch (kind) {
    case "movie":
      return `/movies/${id}`;
    case "album":
      return `/music/albums/${id}`;
    case "episode":
    case "season":
    case "series": {
      const series = item.parentRef.startsWith("series:") ? refId(item.parentRef) : kind === "series" ? id : 0;
      return series ? `/series/${series}` : null;
    }
  }
  return null;
}

const str = (v: unknown) => (v === undefined || v === null ? "" : String(v));

/** One board item's answer in the user's language. */
export function describeBoardItem(t: TFunction, item: BoardItem, locale: string): string {
  const p = (item.params ?? {}) as Record<string, unknown>;
  const vars = {
    title: str(p.title),
    detail: str(p.detail),
    progress: Math.round(Number(p.progress ?? 0) * 100),
    searches: Number(p.searches ?? item.searches ?? 0),
    languages: Array.isArray(p.languages) ? p.languages.join(", ") : str(p.languages),
    next: p.nextSearch ? formatDateTime(str(p.nextSearch), locale) : t("explain.unknownTime"),
    since: p.stalledSince ? formatDateTime(str(p.stalledSince), locale) : t("explain.unknownTime"),
    provider: str(p.provider),
  };
  if (t(`board.itemCodes.${item.code}`, { defaultValue: "" })) {
    return t(`board.itemCodes.${item.code}`, { ...vars, defaultValue: item.message });
  }
  return describeExplanation(t, { code: item.code, tone: item.tone, message: item.message, params: item.params, reasons: item.reasons }, locale);
}

/** A rejection reason count in the user's language. */
export function describeReason(t: TFunction, r: ReasonCount): string {
  return t(`rejectionCodes.${r.code}`, { defaultValue: r.example || r.code });
}
