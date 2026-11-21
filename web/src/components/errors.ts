import type { TFunction } from "i18next";
import { ApiError } from "@/api/client";

/** Turns an API or network error into a translated sentence. */
export function errorMessage(t: TFunction, err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "rate_limited" && err.retryAfter) {
      return t("errors.rate_limited_wait", { seconds: err.retryAfter });
    }
    return t(`errors.${err.code}`, { defaultValue: t("errors.generic", { status: err.status }) });
  }
  if (err instanceof TypeError) return t("errors.network");
  return t("errors.unknown");
}

/** Translates an error body that arrived as data (per-item results), falling back to its English message. */
export function codeMessage(t: TFunction, e: { code: string; message: string }): string {
  return t(`errors.${e.code}`, { defaultValue: e.message });
}
