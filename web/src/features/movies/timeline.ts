import type { TFunction } from "i18next";
import type { Explanation, TimelineEvent } from "@/api/client";
import { formatDateTime } from "@/components/format";

type Data = Record<string, unknown>;

const str = (v: unknown) => (v === undefined || v === null ? "" : String(v));

/** The icon tone of an event on the timeline. */
export function eventTone(e: TimelineEvent): "success" | "danger" | "warning" | "info" | "neutral" {
  switch (e.type) {
    case "file_imported":
    case "download_imported":
    case "upgraded":
    case "download_completed":
    case "file_found":
      return "success";
    case "import_failed":
    case "download_import_failed":
    case "download_failed":
    case "grab_failed":
      return "danger";
    case "pending":
    case "retry":
    case "download_blocklisted":
    case "recycled":
    case "deleted":
      return "warning";
    case "search":
    case "decision":
    case "download_grabbed":
    case "download_progress":
      return "info";
  }
  return "neutral";
}

/**
 * One line for an event in the user's language. Unknown types fall back to
 * the server's English message.
 */
export function describeEvent(t: TFunction, e: TimelineEvent): string {
  const d = (e.data ?? {}) as Data;
  const summary = (d.summary ?? {}) as Data;
  switch (e.type) {
    case "added":
      return t("timeline.events.added", { path: str(d.path) });
    case "edited":
      return t("timeline.events.edited");
    case "refreshed":
      return t("timeline.events.refreshed");
    case "search": {
      const inds = (d.indexers as { name: string; count: number; status: string }[] | undefined) ?? [];
      return t("timeline.events.search", {
        source: t(`timeline.sources.${str(d.source) || "automatic"}`, { defaultValue: str(d.source) }),
        count: Number(d.resultCount ?? 0),
        indexers: inds.map((i) => `${i.name}: ${i.status === "ok" ? i.count : t(`timeline.indexerStatus.${i.status}`, { defaultValue: i.status })}`).join(", ") || "–",
      });
    }
    case "decision":
      return t("timeline.events.decision", {
        reason: t(`timeline.reasons.${str(d.reason)}`, { defaultValue: str(d.reason) }),
        candidates: Number(summary.candidates ?? 0),
        accepted: Number(summary.accepted ?? 0),
        pending: Number(summary.pending ?? 0),
        rejected: Number(summary.rejected ?? 0),
      }) + (summary.best ? " " + t("timeline.events.best", { title: str(summary.best) }) : "");
    case "pending":
      return t("timeline.events.pending", { title: str(d.title) });
    case "grab_failed":
      return t("timeline.events.grabFailed", { title: str(d.title), error: str(d.error) });
    case "retry":
      return t("timeline.events.retry", { title: str(d.title), message: str(d.message) });
    case "file_imported":
      return t("timeline.events.imported", { quality: str(d.quality), mode: t(`transfer.${str(d.mode)}`, { defaultValue: str(d.mode) }), path: str(d.path) });
    case "file_found":
      return t("timeline.events.found", { quality: str(d.quality), path: str(d.path) });
    case "import_failed":
      return t("timeline.events.importFailed", { message: e.message });
    case "renamed":
      return t("timeline.events.renamed", { from: str(d.from).split("/").pop(), to: str(d.to).split("/").pop() });
    case "upgraded":
      return t("timeline.events.upgraded", { from: str(d.from), to: str(d.to) });
    case "recycled":
      return d.restored ? t("timeline.events.restored", { path: str(d.path) }) : t("timeline.events.recycled", { path: str(d.path) });
    case "deleted":
      return t("timeline.events.deleted", { message: e.message });
    case "notified":
      return t(d.ok ? "timeline.events.notified" : "timeline.events.notifyFailed", { server: str(d.server) });
    case "download_grabbed":
      return t("timeline.events.grabbed", { title: str(d.title), indexer: str(d.indexer) || "–", client: str(d.client) });
    case "download_progress":
      return t("timeline.events.progress", { message: e.message });
    case "download_state":
      return t("timeline.events.state", { from: t(`downloadStates.${str(d.fromState)}`, { defaultValue: str(d.fromState) }),
        to: t(`downloadStates.${str(d.toState)}`, { defaultValue: str(d.toState) }) });
    case "download_completed":
      return t("timeline.events.completed", { title: str(d.title) });
    case "download_failed":
      return t("timeline.events.failed", { title: str(d.title), message: e.message });
    case "download_removed":
      return t("timeline.events.removed", { message: e.message });
    case "download_imported":
      return t("timeline.events.downloadImported", { message: e.message });
    case "download_import_failed":
      return t("timeline.events.importFailed", { message: e.message });
    case "download_blocklisted":
      return t("timeline.events.blocklisted", { title: str(d.title) });
  }
  return e.message;
}

/** The "why isn't it here?" answer in the user's language. */
export function describeExplanation(t: TFunction, ex: Explanation, locale: string): string {
  const p = (ex.params ?? {}) as Data;
  const when = (v: unknown) => (v ? formatDateTime(str(v), locale) : t("explain.unknownTime"));
  const reason = ex.reasons[0];
  const reasonText = reason ? t(`rejectionCodes.${reason.code}`, { defaultValue: reason.example }) : "";
  const vars = {
    progress: Math.round(Number(p.progress ?? 0) * 100),
    title: str(p.title),
    state: t(`downloadStates.${str(p.state)}`, { defaultValue: str(p.state) }),
    detail: str(p.detail),
    searches: Number(p.searches ?? 0),
    candidates: Number(p.candidates ?? 0),
    next: when(p.nextSearch),
    from: when(p.availableFrom),
    retry: when(p.retryAt),
    reason: reasonText,
  };
  return t(`explain.codes.${ex.code}`, { ...vars, defaultValue: ex.message });
}
