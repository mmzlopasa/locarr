import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Download, Search } from "lucide-react";
import { api, queryKeys, unwrap, type InteractiveDecision } from "@/api/client";
import { formatBytes, formatRelative } from "@/components/format";
import { Badge, Button, EmptyState, ErrorAlert, Spinner } from "@/components/ui";
import { describeRejection } from "@/features/profiles/rejections";

const tone = { accepted: "success", pending: "warning", rejected: "danger" } as const;

/** Every release the indexers have for a movie, with the decision on each (nothing is grabbed until you choose). */
export function InteractiveSearch({ movieId, itemRef }: { movieId: number; itemRef: string }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [run, setRun] = useState(0);
  const [show, setShow] = useState<"all" | "accepted" | "rejected">("all");
  const search = useQuery({
    queryKey: ["movies", movieId, "releases", run],
    queryFn: async () => unwrap(await api.GET("/movies/{id}/releases", { params: { path: { id: movieId } } })),
    enabled: run > 0,
    staleTime: Infinity,
    retry: false,
  });
  const [grabbed, setGrabbed] = useState<Set<string>>(new Set());
  const grab = useMutation({
    mutationFn: async (d: InteractiveDecision) => {
      const c = d.decision.candidate;
      return unwrap(await api.POST("/movies/{id}/releases", { params: { path: { id: movieId } }, body: {
        indexerId: c.indexerId ?? 0, title: c.title, protocol: c.protocol ?? "torrent", guid: c.guid,
        ...(d.downloadToken ? { downloadToken: d.downloadToken } : {}), ...(d.magnetUrl ? { magnetUrl: d.magnetUrl } : {}),
        ...(c.publishDate ? { publishDate: c.publishDate } : {}) } }));
    },
    onSuccess: (_, d) => {
      setGrabbed((s) => new Set(s).add(`${d.decision.candidate.indexerId}:${d.decision.candidate.guid}`));
      void qc.invalidateQueries({ queryKey: queryKeys.timeline(itemRef) });
      void qc.invalidateQueries({ queryKey: queryKeys.movie(movieId) });
    },
  });
  const rows = useMemo(() => (search.data?.decisions ?? []).filter((d) =>
    show === "all" || (show === "accepted" ? d.decision.status !== "rejected" : d.decision.status === "rejected")), [search.data, show]);

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={() => setRun((n) => n + 1)} busy={search.isFetching} data-testid="interactive-search">
          <Search className="size-3.5" /> {run ? t("interactive.again") : t("interactive.start")}
        </Button>
        {search.data ? (
          <p className="text-[13px] text-muted">
            {t("interactive.summary", { accepted: search.data.accepted, pending: search.data.pending, rejected: search.data.rejected,
              profile: search.data.profileName })}
          </p>
        ) : <p className="text-[13px] text-muted">{t("interactive.hint")}</p>}
        {search.data ? (
          <div className="ml-auto flex gap-1" role="group" aria-label={t("interactive.filter")}>
            {(["all", "accepted", "rejected"] as const).map((k) => (
              <Button key={k} variant={show === k ? "secondary" : "ghost"} aria-pressed={show === k} onClick={() => setShow(k)}>
                {t(`interactive.show.${k}`)}
              </Button>
            ))}
          </div>
        ) : null}
      </div>
      {search.isFetching ? <Spinner label={t("interactive.searching")} /> : null}
      <ErrorAlert error={search.error ?? grab.error} />
      {search.data ? (
        <div className="mb-3 flex flex-wrap gap-1.5 text-[12px]">
          {search.data.indexers.map((ix) => (
            <span key={ix.indexerId} className="rounded border border-line bg-surface-2 px-1.5 py-0.5 text-muted">
              {ix.indexerName}: {ix.status === "ok" ? ix.count : t(`timeline.indexerStatus.${ix.status}`, { defaultValue: ix.status })}
            </span>
          ))}
        </div>
      ) : null}
      {search.data && rows.length === 0 ? <EmptyState>{t("interactive.none")}</EmptyState> : null}
      {rows.length > 0 ? (
        <div className="overflow-x-auto rounded-ui border border-line">
          <table className="w-full min-w-[860px] text-[13px]" data-testid="decisions">
            <thead className="bg-surface-2 text-left text-[12px] text-muted">
              <tr>
                <th className="px-2 py-1.5 font-medium">{t("interactive.columns.decision")}</th>
                <th className="px-2 py-1.5 font-medium">{t("interactive.columns.release")}</th>
                <th className="px-2 py-1.5 font-medium">{t("interactive.columns.quality")}</th>
                <th className="px-2 py-1.5 text-right font-medium">{t("interactive.columns.size")}</th>
                <th className="px-2 py-1.5 text-right font-medium">{t("interactive.columns.peers")}</th>
                <th className="px-2 py-1.5 font-medium">{t("interactive.columns.age")}</th>
                <th className="px-2 py-1.5 text-right font-medium">{t("interactive.columns.score")}</th>
                <th className="px-2 py-1.5" />
              </tr>
            </thead>
            <tbody>
              {rows.map((d) => {
                const c = d.decision.candidate;
                const key = `${c.indexerId}:${c.guid}`;
                return (
                  <tr key={key} className="border-t border-line align-top" data-testid="decision-row" data-status={d.decision.status}>
                    <td className="px-2 py-1.5">
                      <Badge tone={tone[d.decision.status]}>{t(`interactive.status.${d.decision.status}`)}</Badge>
                      {d.decision.rank ? <span className="ml-1 text-[11px] tabular-nums text-faint">#{d.decision.rank}</span> : null}
                    </td>
                    <td className="max-w-[420px] px-2 py-1.5">
                      <div className="break-words font-mono text-[12px] text-fg">{c.title}</div>
                      <div className="text-[11px] text-faint">{c.indexerName}</div>
                      {d.decision.rejections.length > 0 ? (
                        <ul className="mt-1 space-y-0.5 text-[12px]">
                          {d.decision.rejections.map((r, i) => (
                            <li key={i} className={r.temporary ? "text-warning" : "text-danger"}>{describeRejection(t, r, i18n.language)}</li>
                          ))}
                        </ul>
                      ) : null}
                    </td>
                    <td className="whitespace-nowrap px-2 py-1.5 text-muted">
                      {d.decision.quality.name}
                      {d.decision.customFormats.length ? <div className="text-[11px] text-faint">{d.decision.customFormats.map((f) => f.name).join(", ")}</div> : null}
                    </td>
                    <td className="whitespace-nowrap px-2 py-1.5 text-right tabular-nums text-muted">{c.size ? formatBytes(c.size, i18n.language) : "–"}</td>
                    <td className="whitespace-nowrap px-2 py-1.5 text-right tabular-nums text-muted">{c.seeders ?? "–"}</td>
                    <td className="whitespace-nowrap px-2 py-1.5 text-muted">{formatRelative(c.publishDate, i18n.language)}</td>
                    <td className="px-2 py-1.5 text-right tabular-nums text-muted">{d.decision.score}</td>
                    <td className="px-2 py-1.5 text-right">
                      {grabbed.has(key) ? (
                        <span className="text-[12px] text-success">{t("interactive.sent")}</span>
                      ) : (
                        <Button variant={d.decision.status === "accepted" ? "primary" : "ghost"} onClick={() => grab.mutate(d)}
                          busy={grab.isPending && grab.variables === d} disabled={!d.downloadToken && !d.magnetUrl}
                          title={d.decision.status === "rejected" ? t("interactive.grabAnyway") : t("interactive.grab")} data-testid="grab">
                          <Download className="size-3.5" /> {d.decision.status === "rejected" ? t("interactive.grabAnyway") : t("interactive.grab")}
                        </Button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}
