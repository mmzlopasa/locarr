import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Disc3 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge } from "@/components/ui";

type S = components["schemas"];
export type Artist = S["Artist"];
export type ArtistLookup = S["ArtistLookup"];
export type Album = S["Album"];
export type AlbumStatus = S["AlbumStatus"];
export type AlbumDetail = S["AlbumDetail"];
export type AlbumRelease = S["AlbumRelease"];
export type AlbumTrack = S["AlbumTrack"];
export type MetadataProfile = S["MetadataProfile"];
export type MetadataProfileInput = S["MetadataProfileInput"];
export type MusicSettings = S["MusicSettings"];
export type MusicImportPreview = S["MusicImportPreview"];
export type ExistingArtistFolder = S["ExistingArtistFolder"];
export type AlbumCalendarEntry = S["AlbumCalendarEntry"];
export type MonitorNewAlbums = S["MonitorNewAlbums"];

export const musicKeys = {
  artists: ["music", "artists"] as const,
  artist: (id: number) => ["music", "artists", id] as const,
  albums: (artistId: number) => ["music", "artists", artistId, "albums"] as const,
  album: (id: number) => ["music", "albums", id] as const,
  wanted: (status: string, page: number) => ["music", "wanted", status, page] as const,
  profiles: ["music", "metadataprofiles"] as const,
  settings: ["settings", "music"] as const,
  calendar: (start: string, end: string) => ["music", "calendar", start, end] as const,
};

export const PRIMARY_TYPES = ["album", "ep", "single", "broadcast", "other"] as const;
export const SECONDARY_TYPES = ["studio", "compilation", "soundtrack", "spokenword", "interview", "audiobook", "audio drama", "live", "remix",
  "dj-mix", "mixtape/street", "demo", "field recording"] as const;
export const RELEASE_STATUSES = ["official", "promotion", "bootleg", "pseudo-release", "withdrawn", "cancelled"] as const;
export const MONITOR_NEW: MonitorNewAlbums[] = ["all", "future", "none"];

/** i18n key part for a MusicBrainz type ("audio drama" -> "audio_drama"). */
export const typeKey = (s: string) => s.replace(/[ /-]/g, "_");

/** URL of a cached Cover Art Archive front ("" when there is none). */
export function coverUrl(file: string | undefined, size: "250" | "500" | "1200" = "500"): string {
  return file ? `/api/v1/images/coverart/${size}/${encodeURIComponent(file)}` : "";
}

/** A square cover through the server's image cache, with a quiet placeholder. */
export function Cover({ file, title, size = "500", className = "" }: { file?: string; title: string; size?: "250" | "500" | "1200"; className?: string }) {
  const src = coverUrl(file, size);
  return (
    <div className={`relative aspect-square overflow-hidden rounded-ui border border-line bg-surface-2 ${className}`}>
      <div className="flex size-full flex-col items-center justify-center gap-2 p-2 text-center text-faint">
        <Disc3 className="size-6" aria-hidden />
        <span className="line-clamp-2 text-[11px]">{title}</span>
      </div>
      {src ? (
        <img src={src} alt="" loading="lazy" decoding="async" className="absolute inset-0 size-full object-cover"
          onError={(e) => { e.currentTarget.style.display = "none"; }} />
      ) : null}
    </div>
  );
}

type Tone = "neutral" | "success" | "danger" | "warning" | "info";
const STATUS_TONE: Record<AlbumStatus, Tone> = {
  downloaded: "success", partial: "warning", missing: "danger", downloading: "info", unreleased: "neutral", unmonitored: "neutral",
};

export function AlbumStatusBadge({ status }: { status: AlbumStatus }) {
  const { t } = useTranslation();
  return <Badge tone={STATUS_TONE[status]}>{t(`music.status.${status}`)}</Badge>;
}

export function useMetadataProfiles() {
  return useQuery({ queryKey: musicKeys.profiles, queryFn: async () => unwrap(await api.GET("/music/metadataprofiles")) });
}

/** m:ss for a track length. */
export function formatLength(ms: number): string {
  if (!ms) return "–";
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** The group an album is listed under on the artist page. */
export function albumGroup(a: Pick<Album, "primaryType" | "secondaryTypes">): string {
  const p = (a.primaryType || "other").toLowerCase();
  const sec = a.secondaryTypes.map((s) => s.toLowerCase());
  if (sec.includes("live")) return "live";
  if (sec.includes("compilation")) return "compilation";
  if (sec.length > 0) return "other";
  return p === "album" || p === "ep" || p === "single" ? p : "other";
}

export const GROUP_ORDER = ["album", "ep", "single", "live", "compilation", "other"] as const;
