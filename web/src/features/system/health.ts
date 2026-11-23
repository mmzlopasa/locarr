import type { TFunction } from "i18next";
import type { HealthIssue } from "@/api/client";
import { formatBytes, formatDateTime } from "@/components/format";

/** Parameters that carry byte counts and are shown with units. */
const BYTE_PARAMS = new Set(["free", "total", "min", "size", "limit", "freeBytes", "minBytes", "sizeBytes", "limitBytes", "walBytes"]);
const TIME_PARAMS = new Set(["since", "resetAt", "completedAt", "retryAt"]);

/** One health issue in the user's language (the server's English text is the fallback). */
export function describeHealth(t: TFunction, issue: HealthIssue, locale: string): string {
  const vars: Record<string, string | number> = {};
  for (const [k, v] of Object.entries(issue.params ?? {})) {
    if (typeof v === "number" && BYTE_PARAMS.has(k)) vars[k] = formatBytes(v, locale);
    else if (typeof v === "string" && TIME_PARAMS.has(k)) vars[k] = formatDateTime(v, locale);
    else if (k === "kind" && typeof v === "string") vars[k] = t(`mediaKinds.${v}`, { defaultValue: v });
    else if (typeof v === "string" || typeof v === "number") vars[k] = v;
    else if (Array.isArray(v)) vars[k] = v.join(", ");
  }
  return t(`health.codes.${issue.code}`, { ...vars, defaultValue: issue.message });
}

export const severityTone = { error: "danger", warning: "warning", info: "info" } as const;
