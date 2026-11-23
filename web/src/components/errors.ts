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

/** Codes whose server message adds nothing (or must not be shown). */
const PLAIN_CODES = new Set(["unauthorized", "forbidden", "invalid_credentials", "invalid_totp", "totp_required", "account_locked",
  "rate_limited", "internal", "not_found", "method_not_allowed", "setup_done", "bad_setup_code", "session_required",
  "path_not_allowed", "path_not_found", "request_too_large"]);

/**
 * The server's own explanation of an error (for example what a download client answered), shown under the
 * translated sentence; empty when it would only repeat it.
 */
export function errorDetail(err: unknown): string {
  if (!(err instanceof ApiError) || PLAIN_CODES.has(err.code)) return "";
  const msg = err.message.trim();
  return msg && msg !== err.code ? msg : "";
}

/** Translates an error body that arrived as data (per-item results), falling back to its English message. */
export function codeMessage(t: TFunction, e: { code: string; message: string }): string {
  return t(`errors.${e.code}`, { defaultValue: e.message });
}
