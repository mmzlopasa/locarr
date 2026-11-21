import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type Episode, type SeriesStatus } from "@/api/client";
import { Badge } from "@/components/ui";

export const SERIES_TYPES = ["standard", "daily", "anime"] as const;
export const MONITOR_OPTIONS = ["all", "future", "missing", "existing", "first_season", "latest_season", "pilot", "none"] as const;

const STATUS_TONE: Record<SeriesStatus, "success" | "info" | "neutral"> = { continuing: "success", upcoming: "info", ended: "neutral" };

export function SeriesStatusBadge({ status }: { status: SeriesStatus }) {
  const { t } = useTranslation();
  return <Badge tone={STATUS_TONE[status]}>{t(`series.status.${status}`)}</Badge>;
}

/** "S01E02" (or the air date for daily shows). */
export function episodeCode(e: Pick<Episode, "seasonNumber" | "episodeNumber">): string {
  return `S${String(e.seasonNumber).padStart(2, "0")}E${String(e.episodeNumber).padStart(2, "0")}`;
}

/** What an episode row shows: one state, in order of importance. */
export type EpisodeState = "downloading" | "downloaded" | "upgrade" | "missing" | "unaired" | "unmonitored" | "tba";

export function episodeState(e: Episode): EpisodeState {
  if (e.download && e.download.status === "active") return "downloading";
  if (e.file) return e.wanted === "upgrade" ? "upgrade" : "downloaded";
  if (!e.monitored) return "unmonitored";
  if (!e.airDate) return "tba";
  if (!e.aired) return "unaired";
  return "missing";
}

const STATE_TONE: Record<EpisodeState, "success" | "info" | "warning" | "danger" | "neutral"> = {
  downloading: "info", downloaded: "success", upgrade: "warning", missing: "danger", unaired: "neutral", unmonitored: "neutral", tba: "neutral",
};

export function EpisodeStateBadge({ e }: { e: Episode }) {
  const { t } = useTranslation();
  const s = episodeState(e);
  const pct = s === "downloading" && e.download ? ` ${Math.round(e.download.progress * 100)}%` : "";
  return <Badge tone={STATE_TONE[s]}>{t(`episode.state.${s}`)}{pct}</Badge>;
}

/** The air time in the viewer's zone, or the air date when the time is unknown. */
export function formatAir(e: Pick<Episode, "airDate" | "airDateUtc">, locale: string): string {
  const at = e.airDateUtc ? new Date(e.airDateUtc) : null;
  if (at && !Number.isNaN(at.getTime())) return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(at);
  const day = e.airDate ? new Date(e.airDate) : null;
  return day && !Number.isNaN(day.getTime()) ? new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(day) : "";
}

export function useSeriesDetail(id: number) {
  return useQuery({
    queryKey: queryKeys.series(id),
    queryFn: async () => unwrap(await api.GET("/series/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => ((q.state.data?.downloading ?? 0) > 0 ? 4_000 : false),
  });
}

export function useEpisodes(id: number, downloading: boolean) {
  return useQuery({
    queryKey: queryKeys.seriesEpisodes(id),
    queryFn: async () => unwrap(await api.GET("/series/{id}/episodes", { params: { path: { id } } })),
    refetchInterval: downloading ? 4_000 : false,
  });
}

/** Groups episodes by season number, seasons newest first (specials last). */
export function bySeason(list: Episode[]): Map<number, Episode[]> {
  const m = new Map<number, Episode[]>();
  for (const e of list) m.set(e.seasonNumber, [...(m.get(e.seasonNumber) ?? []), e]);
  return m;
}
