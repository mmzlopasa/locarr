import type { TFunction } from "i18next";
import type { Rejection } from "@/api/client";
import { formatSize } from "./logic";

type Params = Record<string, unknown>;

function list(v: unknown): string {
  return Array.isArray(v) ? v.join(", ") : String(v ?? "");
}

function minutes(t: TFunction, v: unknown): string {
  const m = Number(v ?? 0);
  if (m < 60) return t("profiles.time.minutes", { n: m });
  const h = Math.floor(m / 60);
  const rest = m % 60;
  return rest ? t("profiles.time.hoursMinutes", { hours: h, minutes: rest }) : t("profiles.time.hours", { n: h });
}

/**
 * Turns a rejection into a sentence in the user's language. The server's
 * English message stays the fallback for codes or values the client does not know.
 */
export function describeRejection(t: TFunction, r: Rejection, locale: string): string {
  const p = (r.params ?? {}) as Params;
  const v: Record<string, string | number> = {};
  for (const [k, val] of Object.entries(p)) {
    if (typeof val === "string" || typeof val === "number") v[k] = val;
    else if (Array.isArray(val)) v[k] = list(val);
  }
  switch (r.code) {
    case "size_too_small":
    case "size_too_large":
      v.size = formatSize(Number(p.size ?? 0), locale);
      v.limit = formatSize(Number(p.limit ?? 0), locale);
      v.runtime = minutes(t, p.runtime);
      if (p.assumed) v.runtime = t("profiles.time.assumed", { runtime: v.runtime });
      break;
    case "wrong_kind":
      v.got = t(`mediaKinds.${String(p.got)}`);
      v.want = t(`mediaKinds.${String(p.want)}`);
      break;
    case "delayed":
      v.remaining = minutes(t, p.remainingMinutes);
      v.delay = minutes(t, p.delayMinutes);
      break;
    case "too_new":
      v.age = minutes(t, p.ageMinutes);
      v.minimum = minutes(t, p.minimumMinutes);
      break;
    case "indexer_limited":
      v.until = p.until ? new Intl.DateTimeFormat(locale, { dateStyle: "short", timeStyle: "short" }).format(new Date(String(p.until))) : "";
      break;
    case "wrong_episode":
      if (!r.params || Object.keys(p).length === 0) return t("rejections.wrong_episode_none", { defaultValue: r.message });
      break;
  }
  return t(`rejections.${r.code}`, { ...v, defaultValue: r.message });
}
