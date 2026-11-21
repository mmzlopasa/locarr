import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Film } from "lucide-react";
import { api, imageUrl, queryKeys, unwrap, type Movie, type MovieStatus } from "@/api/client";
import { Badge } from "@/components/ui";

type Tone = "neutral" | "success" | "danger" | "warning" | "info";

export const STATUS_TONE: Record<MovieStatus, Tone> = {
  downloaded: "success",
  downloading: "info",
  cutoff_unmet: "warning",
  missing: "danger",
  unavailable: "neutral",
  unmonitored: "neutral",
};

export function StatusBadge({ status }: { status: MovieStatus }) {
  const { t } = useTranslation();
  return <Badge tone={STATUS_TONE[status]}>{t(`movies.status.${status}`)}</Badge>;
}

/** A poster through the server's image cache, with a quiet placeholder. */
export function Poster({ file, title, size = "w342", className = "" }:
  { file?: string; title: string; size?: "w185" | "w342" | "w500"; className?: string }) {
  const src = imageUrl(file, size);
  return (
    <div className={`relative aspect-[2/3] overflow-hidden rounded-ui border border-line bg-surface-2 ${className}`}>
      {src ? (
        <img src={src} alt="" loading="lazy" decoding="async" className="absolute inset-0 size-full object-cover" />
      ) : (
        <div className="flex size-full flex-col items-center justify-center gap-2 p-2 text-center text-faint">
          <Film className="size-6" aria-hidden />
          <span className="line-clamp-3 text-[11px]">{title}</span>
        </div>
      )}
    </div>
  );
}

export function useQualityProfiles() {
  return useQuery({ queryKey: queryKeys.qualityProfiles, queryFn: async () => unwrap(await api.GET("/qualityprofiles")) });
}

export function useRootFolders() {
  return useQuery({ queryKey: queryKeys.rootFolders, queryFn: async () => unwrap(await api.GET("/rootfolders")) });
}

export function useMovie(id: number) {
  return useQuery({
    queryKey: queryKeys.movie(id),
    queryFn: async () => unwrap(await api.GET("/movies/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data?.libraryStatus === "downloading" ? 4_000 : false),
  });
}

/** Short facts under a title: year · runtime · certification. */
export function movieFacts(m: Pick<Movie, "year" | "runtime" | "certification">, t: TFunction): string {
  return [m.year || null, m.runtime ? t("movies.runtime", { minutes: m.runtime }) : null, m.certification || null]
    .filter(Boolean)
    .join(" · ");
}

export const AVAILABILITIES = ["announced", "in_cinemas", "released"] as const;
