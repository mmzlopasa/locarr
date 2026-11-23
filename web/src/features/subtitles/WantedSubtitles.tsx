import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { api, queryKeys, unwrap } from "@/api/client";
import { formatRelative } from "@/components/format";
import { Button, EmptyState, ErrorAlert, PageHeader, Spinner } from "@/components/ui";
import { ProfileOptions } from "./SubtitleSettingsPage";
import { useSubtitleProfiles, wantLabel } from "./common";

const PAGE = 100;

/** Wanted → Missing subtitles: movies and episodes whose profile languages are missing, with bulk search. */
export function WantedSubtitles() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [kind, setKind] = useState<"" | "movie" | "episode">("");
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [profile, setProfile] = useState<number>(Number.NaN);
  const profiles = useSubtitleProfiles();
  const list = useQuery({
    queryKey: queryKeys.wantedSubtitles(kind, page),
    queryFn: async () => unwrap(await api.GET("/subtitles/wanted", { params: { query: { ...(kind ? { kind } : {}), offset: page * PAGE, limit: PAGE } } })),
  });
  const search = useMutation({
    mutationFn: async (body: { refs?: string[]; all?: boolean }) => unwrap(await api.POST("/subtitles/wanted/search", { body: { ...body, ...(kind ? { kind } : {}) } })),
    onSuccess: () => setSelected(new Set()),
  });
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;
  const assign = useMutation({
    mutationFn: async () => {
      const ids = [...new Set(items.filter((i) => selected.has(i.ref)).map((i) => i.itemId))];
      return unwrap(await api.POST("/subtitles/profiles/assign", { body: { itemIds: ids, profileId: profile } }));
    },
    onSuccess: () => { setSelected(new Set()); void qc.invalidateQueries({ queryKey: ["subtitles", "wanted"] }); },
  });
  const toggle = (ref: string) => setSelected((s) => {
    const n = new Set(s);
    if (n.has(ref)) n.delete(ref);
    else n.add(ref);
    return n;
  });
  const pages = Math.max(1, Math.ceil(total / PAGE));
  return (
    <div>
      <PageHeader title={t("nav.wanted")} description={t("subtitles.wantedPage.description")}
        actions={
          <>
            <Button onClick={() => search.mutate({ refs: [...selected] })} disabled={selected.size === 0} busy={search.isPending} data-testid="subtitles-search-selected">
              <Search className="size-3.5" /> {t("wanted.searchSelected", { count: selected.size })}
            </Button>
            <Button variant="primary" onClick={() => search.mutate({ all: true })} disabled={total === 0} busy={search.isPending} data-testid="subtitles-search-all">
              <Search className="size-3.5" /> {t("subtitles.wantedPage.searchAll", { count: total })}
            </Button>
          </>
        } />
      <div role="tablist" aria-label={t("subtitles.wantedPage.kinds")} className="mb-3 flex gap-1 border-b border-line">
        {(["", "movie", "episode"] as const).map((k) => (
          <button key={k || "all"} type="button" role="tab" aria-selected={kind === k} onClick={() => { setKind(k); setPage(0); setSelected(new Set()); }}
            className={`-mb-px border-b-2 px-3 py-2 text-[13px] ${kind === k ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
            {t(`subtitles.wantedPage.kind.${k || "all"}`)}
          </button>
        ))}
      </div>
      {search.data ? <p className="mb-2 text-[12px] text-success" role="status">{t("wanted.queued", { count: search.data.count })}</p> : null}
      <ErrorAlert error={list.error ?? search.error ?? assign.error} />
      {selected.size > 0 ? (
        <div className="mb-3 flex flex-wrap items-end gap-2 rounded border border-line bg-surface-2 p-2">
          <div className="w-56">
            <ProfileOptions label={t("subtitles.wantedPage.setProfile")} value={Number.isNaN(profile) ? "" : profile} onChange={setProfile} profiles={profiles.data ?? []} keep />
          </div>
          <Button onClick={() => assign.mutate()} disabled={Number.isNaN(profile)} busy={assign.isPending}>{t("subtitles.wantedPage.apply", { count: selected.size })}</Button>
        </div>
      ) : null}
      {list.isLoading ? <Spinner /> : null}
      {list.data && items.length === 0 ? <EmptyState>{t("subtitles.wantedPage.empty")}</EmptyState> : null}
      {items.length > 0 ? (
        <div className="overflow-x-auto rounded-ui border border-line">
          <table className="w-full text-left text-[13px]" data-testid="wanted-subtitles-table">
            <thead className="bg-surface text-[12px] text-faint">
              <tr>
                <th className="w-8 px-2 py-1.5">
                  <input type="checkbox" aria-label={t("wanted.selectAll")} className="accent-[var(--lc-accent)]"
                    checked={items.every((i) => selected.has(i.ref))} onChange={(e) => setSelected(e.target.checked ? new Set(items.map((i) => i.ref)) : new Set())} />
                </th>
                <th className="px-2 py-1.5 font-normal">{t("subtitles.wantedPage.title")}</th>
                <th className="px-2 py-1.5 font-normal">{t("subtitles.wantedPage.missing")}</th>
                <th className="hidden px-2 py-1.5 font-normal md:table-cell">{t("subtitles.wantedPage.searched")}</th>
                <th className="hidden px-2 py-1.5 font-normal lg:table-cell">{t("subtitles.wantedPage.next")}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((w) => (
                <tr key={w.ref} className="border-t border-line" data-testid="wanted-subtitle" data-ref={w.ref}>
                  <td className="px-2 py-1.5">
                    <input type="checkbox" aria-label={w.title} checked={selected.has(w.ref)} onChange={() => toggle(w.ref)} className="accent-[var(--lc-accent)]" />
                  </td>
                  <td className="px-2 py-1.5">
                    {w.kind === "movie" ? (
                      <Link to="/movies/$id" params={{ id: String(w.itemId) }} className="text-fg hover:underline">{w.title}</Link>
                    ) : (
                      <Link to="/series/$id" params={{ id: String(w.itemId) }} className="text-fg hover:underline">
                        {w.title} <span className="font-mono text-[12px] text-muted">S{String(w.season ?? 0).padStart(2, "0")}E{String(w.episode ?? 0).padStart(2, "0")}</span>
                        {w.episodeTitle ? <span className="text-muted"> · {w.episodeTitle}</span> : null}
                      </Link>
                    )}
                  </td>
                  <td className="px-2 py-1.5 text-muted">{w.missing.map((k) => wantLabel(t, k)).join(", ")}</td>
                  <td className="hidden px-2 py-1.5 text-muted md:table-cell" title={w.lastResult}>
                    {w.searches > 0 ? t("subtitles.wantedPage.searches", { count: w.searches, when: formatRelative(w.lastSearchAt, i18n.language) }) : t("subtitles.wantedPage.never")}
                  </td>
                  <td className="hidden px-2 py-1.5 tabular-nums text-muted lg:table-cell">{w.nextSearchAt ? formatRelative(w.nextSearchAt, i18n.language) : "–"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {pages > 1 ? (
        <div className="mt-3 flex items-center justify-end gap-2 text-[12px] text-muted">
          <Button variant="ghost" disabled={page === 0} onClick={() => setPage(page - 1)}>{t("wanted.previous")}</Button>
          <span>{t("wanted.page", { page: page + 1, pages })}</span>
          <Button variant="ghost" disabled={page + 1 >= pages} onClick={() => setPage(page + 1)}>{t("wanted.next")}</Button>
        </div>
      ) : null}
    </div>
  );
}
