import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type SourceStats } from "@/api/client";
import { formatDuration } from "@/components/format";
import { EmptyState, ErrorAlert, Panel, SelectInput, Spinner } from "@/components/ui";

const DAYS = [7, 30, 90, 365];

/** Grabs, success rate and time to import per indexer and per download client. */
export function StatsPage() {
  const { t } = useTranslation();
  const [days, setDays] = useState(30);
  const stats = useQuery({
    queryKey: queryKeys.activityStats(days),
    queryFn: async () => unwrap(await api.GET("/activity/stats", { params: { query: { days } } })),
  });
  return (
    <div className="flex flex-col gap-4">
      <div className="w-40">
        <SelectInput label={t("activityStats.period")} value={days} onChange={(e) => setDays(Number(e.target.value))} data-testid="stats-days">
          {DAYS.map((d) => <option key={d} value={d}>{t("activityStats.days", { count: d })}</option>)}
        </SelectInput>
      </div>
      {stats.isPending ? <Spinner /> : null}
      <ErrorAlert error={stats.error} />
      {stats.data ? (
        <>
          <StatsTable title={t("activityStats.indexers")} rows={stats.data.indexers} testId="stats-indexers" />
          <StatsTable title={t("activityStats.clients")} rows={stats.data.clients} testId="stats-clients" />
        </>
      ) : null}
    </div>
  );
}

function StatsTable({ title, rows, testId }: { title: string; rows: SourceStats[]; testId: string }) {
  const { t, i18n } = useTranslation();
  const pct = new Intl.NumberFormat(i18n.language, { style: "percent", maximumFractionDigits: 0 });
  return (
    <Panel title={title}>
      {rows.length === 0 ? <EmptyState>{t("activityStats.none")}</EmptyState> : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-[13px]" data-testid={testId}>
            <thead className="text-[11px] uppercase tracking-wide text-faint">
              <tr>
                <th className="py-1.5 pr-3 font-medium">{t("activityStats.name")}</th>
                <th className="py-1.5 pr-3 text-right font-medium">{t("activityStats.grabs")}</th>
                <th className="py-1.5 pr-3 text-right font-medium">{t("activityStats.imported")}</th>
                <th className="py-1.5 pr-3 text-right font-medium">{t("activityStats.failed")}</th>
                <th className="py-1.5 pr-3 text-right font-medium">{t("activityStats.inProgress")}</th>
                <th className="py-1.5 pr-3 text-right font-medium">{t("activityStats.successRate")}</th>
                <th className="py-1.5 text-right font-medium">{t("activityStats.avgImport")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {rows.map((r) => (
                <tr key={`${r.id ?? 0}-${r.name}`}>
                  <td className="py-1.5 pr-3 text-fg">{r.name || t("activityStats.unknown")}</td>
                  <td className="py-1.5 pr-3 text-right tabular-nums">{r.grabs}</td>
                  <td className="py-1.5 pr-3 text-right tabular-nums text-success">{r.imported}</td>
                  <td className="py-1.5 pr-3 text-right tabular-nums text-danger">{r.failed}</td>
                  <td className="py-1.5 pr-3 text-right tabular-nums text-muted">{r.inProgress}</td>
                  <td className="py-1.5 pr-3 text-right tabular-nums">{r.successRate === undefined ? "–" : pct.format(r.successRate)}</td>
                  <td className="py-1.5 text-right tabular-nums text-muted">
                    {r.avgImportSeconds === undefined ? "–" : formatDuration(r.avgImportSeconds, i18n.language)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}
