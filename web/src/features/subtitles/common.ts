import type { TFunction } from "i18next";
import { useQuery } from "@tanstack/react-query";
import { api, queryKeys, unwrap, type SubtitleExplanation, type TimelineEvent } from "@/api/client";
import { formatDateTime } from "@/components/format";

type Data = Record<string, unknown>;
const str = (v: unknown) => (v === undefined || v === null ? "" : String(v));

/** "hr", "hr:forced", "en:hi" → "Croatian", "Croatian (forced)", "English (hearing impaired)". */
export function wantLabel(t: TFunction, key: string): string {
  const [lang = "", flag] = key.split(":");
  const name = t(`languageNames.${lang}`, { defaultValue: lang.toUpperCase() });
  if (flag === "forced") return t("subtitles.variant.forced", { language: name });
  if (flag === "hi") return t("subtitles.variant.hi", { language: name });
  return name;
}

export function languageName(t: TFunction, code: string): string {
  return t(`languageNames.${code}`, { defaultValue: code ? code.toUpperCase() : t("subtitles.unknownLanguage") });
}

/** The subtitle answer to "why isn't it here?" in the user's language. */
export function describeSubtitleExplanation(t: TFunction, ex: SubtitleExplanation, locale: string): string {
  const p = (ex.params ?? {}) as Data;
  const missing = Array.isArray(p.missing) ? (p.missing as string[]).map((k) => wantLabel(t, k)).join(", ") : "";
  return t(`subtitles.explain.${ex.code}`, {
    defaultValue: ex.message,
    missing,
    searches: Number(p.searches ?? 0),
    providers: Number(p.providers ?? 0),
    next: p.nextSearch ? formatDateTime(str(p.nextSearch), locale) : t("subtitles.soon"),
    detail: str(p.detail),
  });
}

/** Timeline lines for subtitle events (other types return null). */
export function describeSubtitleEvent(t: TFunction, e: TimelineEvent): string | null {
  const d = (e.data ?? {}) as Data;
  const file = str(d.path).split("/").pop() ?? "";
  switch (e.type) {
    case "subtitle_downloaded":
      if (d.reason === "manual") {
        return t("subtitles.events.chosen", { language: wantLabel(t, str(d.key) || str(d.language)), provider: str(d.provider), score: Number(d.score ?? 0), file });
      }
      return t("subtitles.events.downloaded", { language: wantLabel(t, str(d.key) || str(d.language)), provider: str(d.provider), score: Number(d.score ?? 0), file });
    case "subtitle_upgraded":
      return t("subtitles.events.upgraded", { language: wantLabel(t, str(d.key) || str(d.language)), provider: str(d.provider), score: Number(d.score ?? 0), file });
    case "subtitle_not_found":
      return d.bestScore !== undefined
        ? t("subtitles.events.notFoundBelow", { language: wantLabel(t, str(d.key) || str(d.language)), providers: Number(d.providers ?? 0),
          best: Number(d.bestScore), min: Number(d.minScore ?? 0) })
        : t("subtitles.events.notFound", { language: wantLabel(t, str(d.key) || str(d.language)), providers: Number(d.providers ?? 0) });
    case "subtitle_failed":
      return t("subtitles.events.failed", { language: languageName(t, str(d.language)), provider: str(d.provider),
        error: t(`subtitles.errorCodes.${str(d.code)}`, { defaultValue: str(d.error) }) });
    case "subtitle_deleted":
      return d.blacklisted ? t("subtitles.events.deletedBlacklisted", { file }) : t("subtitles.events.deleted", { file });
    case "subtitle_synced":
      if (d.state === "done") {
        return Number(d.offsetMs ?? 0) === 0 && Number(d.scale ?? 1) === 1
          ? t("subtitles.events.inSync", { file })
          : t("subtitles.events.synced", { file, offset: signed(Number(d.offsetMs ?? 0)), reference: t(`subtitles.sync.references.${str(d.reference).split(":")[0] || "audio"}`) });
      }
      return t("subtitles.events.syncFailed", { file, message: str(d.message) });
    case "subtitle_edited":
      return d.tool === "fps"
        ? t("subtitles.events.fps", { file, from: str(d.from), to: str(d.to) })
        : t("subtitles.events.shifted", { file, offset: signed(Number(d.offsetMs ?? 0)) });
    case "subtitle_found_on_disk":
      return t("subtitles.events.found", { file, language: languageName(t, str(d.language)) });
  }
  return null;
}

export function signed(ms: number): string {
  return `${ms > 0 ? "+" : ""}${ms}`;
}

export function subtitleEventTone(type: string): "success" | "danger" | "warning" | "info" | null {
  switch (type) {
    case "subtitle_downloaded":
    case "subtitle_upgraded":
    case "subtitle_found_on_disk":
    case "subtitle_synced":
      return "success";
    case "subtitle_failed":
      return "danger";
    case "subtitle_not_found":
    case "subtitle_deleted":
      return "warning";
    case "subtitle_edited":
      return "info";
  }
  return null;
}

/** mm:ss.mmm for a preview line. */
export function cueTime(ms: number): string {
  const h = Math.floor(ms / 3_600_000);
  const m = Math.floor(ms / 60_000) % 60;
  const s = Math.floor(ms / 1000) % 60;
  const r = ms % 1000;
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${h > 0 ? `${h}:` : ""}${pad(m)}:${pad(s)}.${pad(r, 3)}`;
}

export function useSubtitleProfiles() {
  return useQuery({ queryKey: queryKeys.subtitleProfiles, queryFn: async () => unwrap(await api.GET("/subtitles/profiles")) });
}

export function useSubtitleItem(ref: string, poll = false) {
  return useQuery({
    queryKey: queryKeys.subtitleItem(ref),
    queryFn: async () => unwrap(await api.GET("/subtitles/items/{ref}", { params: { path: { ref } } })),
    refetchInterval: (q) => (poll || q.state.data?.subtitles.some((s) => s.syncState === "running") ? 2_000 : false),
  });
}

/** Sync references the user can pick for a subtitle. */
export function syncReferences(t: TFunction, item: { tracks: { kind: string; index: number; text: boolean; language: string; name: string }[];
  subtitles: { id: number; language: string; path: string }[] }, subtitleId: number): { value: string; label: string }[] {
  const out = [{ value: "audio", label: t("subtitles.sync.references.audio") }];
  for (const tr of item.tracks) {
    if (tr.kind === "subtitle" && tr.text) {
      out.push({ value: `track:${tr.index}`, label: t("subtitles.sync.trackOption", { index: tr.index + 1, language: languageName(t, tr.language), name: tr.name }) });
    }
  }
  for (const s of item.subtitles) {
    if (s.id !== subtitleId) {
      out.push({ value: `subtitle:${s.id}`, label: t("subtitles.sync.subtitleOption", { language: languageName(t, s.language), file: s.path.split("/").pop() ?? "" }) });
    }
  }
  return out;
}
