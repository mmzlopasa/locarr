import { useQuery } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { EmptyState, ErrorAlert, Spinner } from "@/components/ui";
import { PosterGrid } from "./common";
import { DiscoverSearchBox } from "./DiscoverPage";

export const SEARCH_TYPES = ["all", "movie", "series", "person", "music"] as const;
export type SearchKind = (typeof SEARCH_TYPES)[number];
export type DiscoverSearch = { q: string; type: SearchKind };

export function validateDiscoverSearch(s: Record<string, unknown>): DiscoverSearch {
  const q = typeof s.q === "string" ? s.q.slice(0, 200) : "";
  const type = SEARCH_TYPES.includes(s.type as SearchKind) ? (s.type as SearchKind) : "all";
  return { q, type };
}

export function DiscoverSearchPage() {
  const { t } = useTranslation();
  const { q, type } = useSearch({ from: "/app/discover/search" });
  const res = useQuery({
    queryKey: queryKeys.discoverSearch(q, type),
    queryFn: async () => unwrap(await api.GET("/discover/search", { params: { query: { q, type } } })),
    enabled: q.trim().length > 0,
    staleTime: 60_000,
  });
  return (
    <div className="flex flex-col gap-4">
      <div className="max-w-2xl">
        <DiscoverSearchBox key={q} initial={q} autoFocus={!q} />
      </div>
      <nav aria-label={t("discover.searchTypes")} className="flex gap-1 overflow-x-auto border-b border-line">
        {SEARCH_TYPES.map((k) => (
          <Link key={k} to="/discover/search" search={{ q, type: k }} aria-current={type === k ? "page" : undefined}
            data-testid={`search-tab-${k}`}
            className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-[13px] ${type === k ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
            {t(`discover.searchTab.${k}`)}
          </Link>
        ))}
      </nav>
      {res.isFetching && !res.data ? <Spinner /> : null}
      <ErrorAlert error={res.error} />
      {!q ? <EmptyState>{t("discover.searchHint")}</EmptyState> : null}
      {res.data && res.data.items.length === 0 ? <EmptyState>{t("discover.noResults", { q })}</EmptyState> : null}
      {res.data ? <PosterGrid items={res.data.items} testId="discover-results" /> : null}
    </div>
  );
}
