import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowDownToLine, ArrowUpToLine, Pause, Play, RefreshCw } from "lucide-react";
import { api, queryKeys, unwrap, type DownloadState, type QueueClient, type QueueItem } from "@/api/client";
import { formatBytes, formatDuration } from "@/components/format";
import { Alert, Badge, Button, ConfirmButton, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";

const STATES: DownloadState[] = ["queued", "downloading", "paused", "seeding", "completed", "failed", "warning"];

type Tone = "neutral" | "success" | "danger" | "warning" | "info";

function stateTone(item: QueueItem): Tone {
  if (item.status === "failed" || item.state === "failed") return "danger";
  if (item.status === "completed") return "success";
  switch (item.state) {
    case "downloading":
      return "info";
    case "seeding":
    case "completed":
      return "success";
    case "warning":
      return "warning";
  }
  return "neutral";
}

export function StateBadge({ item }: { item: QueueItem }) {
  const { t } = useTranslation();
  const label = item.status === "completed" ? t("queue.readyToImport")
    : item.status === "failed" ? t("downloadStates.failed") : t(`downloadStates.${item.state}`);
  return (
    <span title={item.rawState} data-testid="state-badge">
      <Badge tone={stateTone(item)}>{label}</Badge>
    </span>
  );
}

function ProgressBar({ value, tone }: { value: number; tone: Tone }) {
  const { t } = useTranslation();
  const pct = Math.round(Math.max(0, Math.min(1, value)) * 1000) / 10;
  const bar = tone === "danger" ? "bg-danger" : tone === "success" ? "bg-success" : tone === "warning" ? "bg-warning" : "bg-info";
  return (
    <div className="flex items-center gap-2">
      <div role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label={t("queue.columns.progress")}
        className="h-1.5 w-24 overflow-hidden rounded-full bg-surface-3">
        <div className={`h-full ${bar} transition-[width] duration-500`} style={{ width: `${pct}%` }} />
      </div>
      <span className="w-12 text-right tabular-nums text-[12px] text-muted">{pct.toFixed(pct < 10 ? 1 : 0)}%</span>
    </div>
  );
}

function Actions({ item, client }: { item: QueueItem; client?: QueueClient }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const done = () => qc.invalidateQueries({ queryKey: queryKeys.queue });
  const id = item.id;
  const pause = useMutation({ mutationFn: async () => unwrap(await api.POST("/queue/{id}/pause", { params: { path: { id } } })), onSuccess: done });
  const resume = useMutation({ mutationFn: async () => unwrap(await api.POST("/queue/{id}/resume", { params: { path: { id } } })), onSuccess: done });
  const prio = useMutation({
    mutationFn: async (priority: "top" | "last") =>
      unwrap(await api.POST("/queue/{id}/priority", { params: { path: { id } }, body: { priority } })),
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async (o: { deleteData?: boolean; blocklist?: boolean }) =>
      unwrap(await api.DELETE("/queue/{id}", { params: { path: { id }, query: o } })),
    onSuccess: () => {
      void done();
      if (remove.variables?.blocklist) void qc.invalidateQueries({ queryKey: queryKeys.blocklist });
    },
  });
  if (item.unknown) return <span className="text-[12px] text-faint">{t("queue.unknown")}</span>;
  const paused = item.state === "paused";
  const err = pause.error ?? resume.error ?? prio.error ?? remove.error;
  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex flex-wrap justify-end gap-1">
        {item.status === "active" ? (
          paused ? (
            <Button variant="ghost" onClick={() => resume.mutate()} busy={resume.isPending} aria-label={t("queue.actions.resume")} title={t("queue.actions.resume")}>
              <Play className="size-3.5" />
            </Button>
          ) : (
            <Button variant="ghost" onClick={() => pause.mutate()} busy={pause.isPending} aria-label={t("queue.actions.pause")} title={t("queue.actions.pause")}>
              <Pause className="size-3.5" />
            </Button>
          )
        ) : null}
        {client?.canPrioritise && item.status === "active" ? (
          <>
            <Button variant="ghost" onClick={() => prio.mutate("top")} aria-label={t("queue.actions.top")} title={t("queue.actions.top")}>
              <ArrowUpToLine className="size-3.5" />
            </Button>
            <Button variant="ghost" onClick={() => prio.mutate("last")} aria-label={t("queue.actions.last")} title={t("queue.actions.last")}>
              <ArrowDownToLine className="size-3.5" />
            </Button>
          </>
        ) : null}
      </div>
      <div className="flex flex-wrap justify-end gap-1">
        <ConfirmButton label={t("queue.actions.remove")} confirmLabel={t("queue.actions.removeConfirm")} busy={remove.isPending}
          onConfirm={() => remove.mutate({})} testId={`remove-${id}`} />
        <ConfirmButton label={t("queue.actions.removeData")} confirmLabel={t("queue.actions.removeDataConfirm")} busy={remove.isPending}
          onConfirm={() => remove.mutate({ deleteData: true })} testId={`remove-data-${id}`} />
        <ConfirmButton label={t("queue.actions.blocklist")} confirmLabel={t("queue.actions.blocklistConfirm")} busy={remove.isPending}
          onConfirm={() => remove.mutate({ blocklist: true })} testId={`blocklist-${id}`} />
      </div>
      {err ? <ErrorAlert error={err} /> : null}
    </div>
  );
}

export function QueuePage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const queue = useQuery({
    queryKey: queryKeys.queue,
    queryFn: async () => unwrap(await api.GET("/queue")),
    refetchInterval: 3_000,
  });
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/queue/refresh")),
    onSuccess: () => setTimeout(() => void qc.invalidateQueries({ queryKey: queryKeys.queue }), 1000),
  });
  const [search, setSearch] = useState("");
  const [clientId, setClientId] = useState("");
  const [state, setState] = useState("");
  const clients = useMemo(() => new Map((queue.data?.clients ?? []).map((c) => [c.id, c])), [queue.data]);
  const items = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (queue.data?.items ?? []).filter((it) =>
      (!q || it.title.toLowerCase().includes(q)) &&
      (!clientId || String(it.clientId) === clientId) &&
      (!state || (state === "ready" ? it.status === "completed" : it.state === state)));
  }, [queue.data, search, clientId, state]);
  const failing = (queue.data?.clients ?? []).filter((c) => c.enabled && c.status.checkedAt && !c.status.ok);
  const nf = new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 2 });
  const speed = (n: number) => (n > 0 ? `${formatBytes(n, i18n.language)}/s` : "–");

  return (
    <div className="flex flex-col gap-3">
      {failing.map((c) => (
        <Alert key={c.id} tone="warning">
          {t("queue.clientError", {
            name: c.name,
            error: t(`downloaders.testErrors.${c.status.errorCode ?? "client_error"}`, { defaultValue: t("downloaders.testErrors.client_error") }),
          })}
        </Alert>
      ))}
      {queue.data && queue.data.clients.length === 0 ? <Alert tone="info">{t("queue.noClients")}</Alert> : null}
      <Panel>
        <div className="mb-3 flex flex-wrap items-end gap-3">
          <div className="min-w-48 flex-1">
            <TextInput label={t("queue.filters.search")} value={search} onChange={(e) => setSearch(e.target.value)} type="search" />
          </div>
          <div className="w-44">
            <SelectInput label={t("queue.filters.client")} value={clientId} onChange={(e) => setClientId(e.target.value)}>
              <option value="">{t("queue.filters.all")}</option>
              {queue.data?.clients.map((c) => <option key={c.id} value={String(c.id)}>{c.name}</option>)}
            </SelectInput>
          </div>
          <div className="w-44">
            <SelectInput label={t("queue.filters.state")} value={state} onChange={(e) => setState(e.target.value)}>
              <option value="">{t("queue.filters.all")}</option>
              <option value="ready">{t("queue.readyToImport")}</option>
              {STATES.map((s) => <option key={s} value={s}>{t(`downloadStates.${s}`)}</option>)}
            </SelectInput>
          </div>
          <Button onClick={() => refresh.mutate()} busy={refresh.isPending}>
            <RefreshCw className="size-4" />
            {t("queue.refresh")}
          </Button>
        </div>
        {queue.isPending ? <Spinner /> : null}
        <ErrorAlert error={queue.error} />
        {queue.data && items.length === 0 ? (
          <EmptyState>{queue.data.items.length === 0 ? t("queue.empty") : t("queue.noMatch")}</EmptyState>
        ) : null}
        {items.length > 0 ? (
          <div className="-mx-4 overflow-x-auto px-4">
            <table className="w-full min-w-[860px] text-left text-[13px]" data-testid="queue-table">
              <thead className="text-[11px] uppercase tracking-wide text-faint">
                <tr className="border-b border-line">
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.title")}</th>
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.client")}</th>
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.state")}</th>
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.progress")}</th>
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.speed")}</th>
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.eta")}</th>
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.size")}</th>
                  <th className="py-2 pr-3 font-medium">{t("queue.columns.ratio")}</th>
                  <th className="py-2 font-medium"><span className="sr-only">{t("queue.columns.actions")}</span></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {items.map((it) => (
                  <tr key={it.unknown ? `u-${it.clientId}-${it.clientItemId}` : it.id} className="align-top"
                    data-testid={it.unknown ? "queue-unknown" : `queue-row-${it.id}`}>
                    <td className="max-w-80 py-2 pr-3">
                      <p className="break-all text-fg">{it.title}</p>
                      <p className="text-[11px] text-faint">
                        {[it.indexer, it.category].filter(Boolean).join(" · ")}
                        {it.unknown ? <span className="ml-1"><Badge>{t("queue.notOurs")}</Badge></span> : null}
                      </p>
                      {it.message ? <p className="mt-0.5 break-words text-[12px] text-warning">{it.message}</p> : null}
                    </td>
                    <td className="py-2 pr-3 text-muted">{it.clientName}</td>
                    <td className="py-2 pr-3"><StateBadge item={it} /></td>
                    <td className="py-2 pr-3"><ProgressBar value={it.progress} tone={stateTone(it)} /></td>
                    <td className="whitespace-nowrap py-2 pr-3 tabular-nums text-muted">
                      {speed(it.downloadSpeed)}
                      {it.uploadSpeed > 0 ? <span className="block text-[11px] text-faint">↑ {speed(it.uploadSpeed)}</span> : null}
                    </td>
                    <td className="whitespace-nowrap py-2 pr-3 tabular-nums text-muted">
                      {it.etaSeconds !== undefined && it.progress < 1 ? formatDuration(it.etaSeconds, i18n.language) : "–"}
                    </td>
                    <td className="whitespace-nowrap py-2 pr-3 tabular-nums text-muted">{it.sizeBytes > 0 ? formatBytes(it.sizeBytes, i18n.language) : "–"}</td>
                    <td className="py-2 pr-3 tabular-nums text-muted">{it.protocol === "torrent" ? nf.format(it.ratio) : "–"}</td>
                    <td className="py-2"><Actions item={it} client={it.clientId ? clients.get(it.clientId) : undefined} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </Panel>
    </div>
  );
}
