import { useRef, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, Disc3, Film, MicVocal, Tv, UserRound } from "lucide-react";
import { imageUrl, type DiscoverItem, type DiscoverStatus } from "@/api/client";
import { coverUrl } from "@/features/music/common";

type Tone = "neutral" | "success" | "danger" | "warning" | "info";

export const STATUS_TONE: Record<DiscoverStatus, Tone> = {
  none: "neutral",
  pending: "warning",
  processing: "info",
  partial: "warning",
  available: "success",
  in_library: "neutral",
};

const toneClass: Record<Tone, string> = {
  neutral: "bg-black/70 text-fg border-white/15",
  success: "bg-success text-accent-ink border-transparent",
  danger: "bg-danger text-bg border-transparent",
  warning: "bg-warning text-bg border-transparent",
  info: "bg-info text-bg border-transparent",
};

/** A small pill on a poster or a title page saying where a title stands. */
export function AvailabilityBadge({ status, is4k, testId }: { status: DiscoverStatus; is4k?: boolean; testId?: string }) {
  const { t } = useTranslation();
  if (status === "none") return null;
  return (
    <span data-testid={testId} data-status={status}
      className={`inline-flex h-5 items-center gap-1 rounded-full border px-2 text-[10.5px] font-semibold uppercase tracking-wide ${toneClass[STATUS_TONE[status]]}`}>
      {is4k ? <span className="opacity-80">4K</span> : null}
      {t(`discover.status.${status}`)}
    </span>
  );
}

/** Where a discover item opens. */
export function itemLink(item: DiscoverItem): { to: string; params: Record<string, string> } | null {
  switch (item.mediaType) {
    case "movie":
      return item.tmdbId ? { to: "/discover/movie/$tmdbId", params: { tmdbId: String(item.tmdbId) } } : null;
    case "series":
      return item.tmdbId ? { to: "/discover/series/$tmdbId", params: { tmdbId: String(item.tmdbId) } } : null;
    case "person":
      return item.tmdbId ? { to: "/discover/person/$tmdbId", params: { tmdbId: String(item.tmdbId) } } : null;
    case "artist":
      return item.mbid ? { to: "/discover/artist/$mbid", params: { mbid: item.mbid } } : null;
    case "album":
      return item.artistMbid ? { to: "/discover/artist/$mbid", params: { mbid: item.artistMbid } } : null;
  }
  return null;
}

/** The image of an item: TMDb posters and profiles, Cover Art Archive for albums. */
export function itemImage(item: Pick<DiscoverItem, "mediaType" | "poster">, size: "small" | "large" = "small"): string {
  if (!item.poster) return "";
  if (item.mediaType === "album" || item.mediaType === "artist") return coverUrl(item.poster, size === "small" ? "250" : "500");
  return imageUrl(item.poster, size === "small" ? "w342" : "w500");
}

export const itemKey = (item: DiscoverItem) => `${item.mediaType}-${item.tmdbId ?? item.mbid ?? item.title}`;

const ICONS = { movie: Film, series: Tv, person: UserRound, artist: MicVocal, album: Disc3 } as const;

/** A poster with a fixed aspect ratio; square for music. */
export function PosterImage({ item, size = "small", className = "" }:
  { item: Pick<DiscoverItem, "mediaType" | "poster" | "title">; size?: "small" | "large"; className?: string }) {
  const src = itemImage(item, size);
  const square = item.mediaType === "album" || item.mediaType === "artist";
  const Icon = ICONS[item.mediaType];
  return (
    <div className={`relative overflow-hidden rounded-ui bg-surface-2 ${square ? "aspect-square" : "aspect-[2/3]"} ${className}`}>
      <div className="flex size-full flex-col items-center justify-center gap-2 p-2 text-center text-faint">
        <Icon className="size-6" aria-hidden />
        <span className="line-clamp-3 text-[11px]">{item.title}</span>
      </div>
      {src ? (
        <img src={src} alt="" loading="lazy" decoding="async" className="absolute inset-0 size-full object-cover"
          onError={(e) => { e.currentTarget.style.display = "none"; }} />
      ) : null}
    </div>
  );
}

/** One title as a poster card that opens its page (a link, so Enter and middle-click work). */
export function MediaCard({ item, width = "w-[132px] sm:w-[150px]" }: { item: DiscoverItem; width?: string }) {
  const { t } = useTranslation();
  const link = itemLink(item);
  const status = item.status !== "none" ? item.status : item.status4k;
  const body = (
    <>
      <div className="relative">
        <PosterImage item={item} className="border border-line shadow-[0_6px_24px_-12px_rgba(0,0,0,0.8)] transition-transform duration-200 group-hover:-translate-y-0.5 group-focus-visible:-translate-y-0.5" />
        <div className="absolute left-1.5 top-1.5 flex flex-col items-start gap-1">
          <AvailabilityBadge status={status} is4k={item.status === "none" && item.status4k !== "none"} testId="card-status" />
        </div>
        {item.mediaType !== "movie" && item.mediaType !== "series" ? (
          <span className="absolute bottom-1.5 right-1.5 rounded bg-black/70 px-1.5 text-[10px] uppercase tracking-wide text-muted">
            {t(`discover.types.${item.mediaType}`)}
          </span>
        ) : null}
      </div>
      <div className="mt-1.5 min-w-0 px-0.5">
        <p className="truncate text-[12.5px] font-medium text-fg">{item.title}</p>
        <p className="truncate text-[11.5px] text-faint">{item.subtitle || (item.year ? String(item.year) : " ")}</p>
      </div>
    </>
  );
  const cls = `group block shrink-0 snap-start rounded-ui outline-offset-4 ${width}`;
  const testId = `poster-${item.mediaType}-${item.tmdbId ?? item.mbid}`;
  return link ? (
    <Link to={link.to} params={link.params} className={cls} data-testid={testId} title={item.title}>
      {body}
    </Link>
  ) : (
    <div className={cls} data-testid={testId}>{body}</div>
  );
}

/** A horizontal row of posters: swipe on touch, arrow buttons with a mouse. */
export function PosterRow({ title, items, action, testId, empty }:
  { title: string; items: DiscoverItem[]; action?: ReactNode; testId?: string; empty?: ReactNode }) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDivElement>(null);
  const scroll = (dir: number) => ref.current?.scrollBy({ left: dir * ref.current.clientWidth * 0.85, behavior: "smooth" });
  return (
    <section className="flex flex-col gap-2" data-testid={testId} aria-label={title}>
      <div className="flex items-end justify-between gap-3">
        <h2 className="text-[15px] font-semibold tracking-tight text-fg">{title}</h2>
        <div className="flex items-center gap-1">
          {action}
          {items.length > 3 ? (
            <span className="hidden gap-1 sm:flex">
              <button type="button" onClick={() => scroll(-1)} aria-label={t("discover.scrollBack")}
                className="inline-flex size-7 items-center justify-center rounded-full border border-line text-muted hover:bg-surface-3 hover:text-fg">
                <ChevronLeft className="size-4" />
              </button>
              <button type="button" onClick={() => scroll(1)} aria-label={t("discover.scrollForward")}
                className="inline-flex size-7 items-center justify-center rounded-full border border-line text-muted hover:bg-surface-3 hover:text-fg">
                <ChevronRight className="size-4" />
              </button>
            </span>
          ) : null}
        </div>
      </div>
      {items.length === 0 ? (
        <p className="py-4 text-[13px] text-faint">{empty ?? t("discover.emptyRow")}</p>
      ) : (
        <div ref={ref} className="-mx-1 flex snap-x snap-mandatory gap-3 overflow-x-auto scroll-px-1 px-1 pb-2 [scrollbar-width:thin]">
          {items.map((it) => <MediaCard key={itemKey(it)} item={it} />)}
        </div>
      )}
    </section>
  );
}

/** A wrapping grid of posters (search results, lists). */
export function PosterGrid({ items, testId }: { items: DiscoverItem[]; testId?: string }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(120px,1fr))] gap-x-3 gap-y-4 sm:grid-cols-[repeat(auto-fill,minmax(145px,1fr))]" data-testid={testId}>
      {items.map((it) => <MediaCard key={itemKey(it)} item={it} width="w-full" />)}
    </div>
  );
}
