import { useQuery } from "@tanstack/react-query";
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type DiscoverListKey } from "@/api/client";
import { Button, EmptyState, ErrorAlert, PageHeader, Spinner } from "@/components/ui";
import { PosterGrid } from "./common";

export type ListSearch = { id?: number; name?: string; page?: number };

export function validateListSearch(s: Record<string, unknown>): ListSearch {
  const out: ListSearch = {};
  const id = Number(s.id);
  if (Number.isFinite(id) && id > 0) out.id = id;
  if (typeof s.name === "string" && s.name) out.name = s.name.slice(0, 100);
  const page = Number(s.page);
  if (Number.isFinite(page) && page > 1) out.page = Math.min(50, Math.floor(page));
  return out;
}

export function DiscoverListPage() {
  const { t } = useTranslation();
  const { key } = useParams({ from: "/app/discover/list/$key" });
  const search = useSearch({ from: "/app/discover/list/$key" });
  const list = key as DiscoverListKey;
  const page = search.page ?? 1;
  const res = useQuery({
    queryKey: queryKeys.discoverList(list, search.id, page),
    queryFn: async () => unwrap(await api.GET("/discover/lists/{list}", { params: { path: { list }, query: { page, id: search.id } } })),
    staleTime: 60_000,
  });
  const title = search.name ? `${t(`discover.rows.${list}`)}: ${search.name}` : t(`discover.rows.${list}`);
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={title} actions={<Link to="/discover" className="text-[12px] text-muted hover:text-fg">{t("discover.back")}</Link>} />
      {res.isPending ? <Spinner /> : null}
      <ErrorAlert error={res.error} />
      {res.data && res.data.items.length === 0 ? <EmptyState>{t("discover.emptyRow")}</EmptyState> : null}
      {res.data ? <PosterGrid items={res.data.items} testId="discover-list" /> : null}
      {res.data && res.data.totalPages > 1 ? (
        <div className="flex items-center justify-center gap-2">
          <Link to="/discover/list/$key" params={{ key }} search={{ ...search, page: page - 1 }} disabled={page <= 1}>
            <Button disabled={page <= 1}>{t("discover.previous")}</Button>
          </Link>
          <span className="text-[12px] text-muted">{t("discover.pageOf", { page, total: res.data.totalPages })}</span>
          <Link to="/discover/list/$key" params={{ key }} search={{ ...search, page: page + 1 }} disabled={page >= res.data.totalPages}>
            <Button disabled={page >= res.data.totalPages}>{t("discover.next")}</Button>
          </Link>
        </div>
      ) : null}
    </div>
  );
}
