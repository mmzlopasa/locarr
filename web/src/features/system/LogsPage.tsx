import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Download } from "lucide-react";
import { api, queryKeys, unwrap, type LogEntry, type LogLevel } from "@/api/client";
import { formatBytes, formatDateTime } from "@/components/format";
import { Badge, Checkbox, EmptyState, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";

const LEVELS: LogLevel[] = ["debug", "info", "warn", "error"];
const MAX_LIVE = 2000;
const levelTone = { debug: "neutral", info: "info", warn: "warning", error: "danger" } as const;

function matches(e: LogEntry, q: string): boolean {
  if (!q) return true;
  const needle = q.toLowerCase();
  return e.message.toLowerCase().includes(needle) || JSON.stringify(e.fields).toLowerCase().includes(needle);
}

/** System → Logs: recent entries, live tail and downloads of the log files. */
export function LogsPage() {
  const { t } = useTranslation();
  const [level, setLevel] = useState<LogLevel>("info");
  const [q, setQ] = useState("");
  const [live, setLive] = useState(false);
  const [tail, setTail] = useState<LogEntry[]>([]);
  const logs = useQuery({
    queryKey: queryKeys.logs(level, q),
    queryFn: async () => unwrap(await api.GET("/system/logs", { params: { query: { level, ...(q.trim() ? { q: q.trim() } : {}), limit: 500 } } })),
    enabled: !live,
  });
  const files = useQuery({ queryKey: queryKeys.logFiles, queryFn: async () => unwrap(await api.GET("/system/logs/files")) });

  useEffect(() => {
    if (!live || typeof EventSource === "undefined") return;
    setTail(logs.data ?? []);
    const es = new EventSource(`/api/v1/system/logs/stream?level=${encodeURIComponent(level)}`);
    es.addEventListener("entry", (ev) => {
      try {
        const e = JSON.parse((ev as MessageEvent<string>).data) as LogEntry;
        setTail((prev) => [...prev, e].slice(-MAX_LIVE));
      } catch {
        // a malformed line is skipped
      }
    });
    return () => es.close();
  }, [live, level]);

  const entries = (live ? tail : logs.data ?? []).filter((e) => !live || matches(e, q));
  const bottom = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (live) bottom.current?.scrollIntoView?.({ block: "nearest" });
  }, [entries.length, live]);

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("logs.title")} description={t("logs.subtitle")} />
      <Panel>
        <div className="mb-3 flex flex-wrap items-end gap-3">
          <div className="w-36">
            <SelectInput label={t("logs.level")} value={level} onChange={(e) => setLevel(e.target.value as LogLevel)} data-testid="logs-level">
              {LEVELS.map((l) => <option key={l} value={l}>{t(`logs.levels.${l}`)}</option>)}
            </SelectInput>
          </div>
          <div className="w-64">
            <TextInput label={t("logs.search")} value={q} onChange={(e) => setQ(e.target.value)} data-testid="logs-search" />
          </div>
          <div className="pb-1.5">
            <Checkbox label={t("logs.live")} checked={live} onChange={(e) => setLive(e.target.checked)} data-testid="logs-live" />
          </div>
        </div>
        {!live && logs.isPending ? <Spinner /> : null}
        <ErrorAlert error={logs.error} />
        {entries.length === 0 && !logs.isPending ? <EmptyState>{t("logs.none")}</EmptyState> : null}
        <div className="max-h-[60vh] overflow-auto rounded-ui border border-line bg-surface-2 font-mono text-[12px]" data-testid="logs-list">
          {entries.map((e) => (
            <div key={e.seq} className="flex gap-2 border-b border-line px-2 py-1 last:border-b-0">
              <span className="shrink-0 text-faint">{formatDateTime(e.time, "en-GB")}</span>
              <span className="shrink-0"><Badge tone={levelTone[e.level]}>{e.level}</Badge></span>
              <span className="min-w-0 break-words text-fg">
                {e.message}
                {Object.keys(e.fields).length > 0 ? (
                  <span className="text-muted"> {Object.entries(e.fields).map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`).join(" ")}</span>
                ) : null}
              </span>
            </div>
          ))}
          <div ref={bottom} />
        </div>
      </Panel>
      <Panel title={t("logs.files")} description={t("logs.filesHint")}>
        <ErrorAlert error={files.error} />
        {files.data?.length === 0 ? <EmptyState>{t("logs.noFiles")}</EmptyState> : null}
        <ul className="divide-y divide-line">
          {files.data?.map((f) => (
            <LogFileRow key={f.name} name={f.name} size={f.size} modified={f.modifiedAt} />
          ))}
        </ul>
      </Panel>
    </div>
  );
}

function LogFileRow({ name, size, modified }: { name: string; size: number; modified: string }) {
  const { i18n } = useTranslation();
  return (
    <li className="flex items-center justify-between gap-3 py-1.5 text-[13px]">
      <a href={`/api/v1/system/logs/files/${encodeURIComponent(name)}`} download className="inline-flex items-center gap-1.5 font-mono text-info hover:underline">
        <Download className="size-3.5" />
        {name}
      </a>
      <span className="text-[12px] text-faint">{formatBytes(size, i18n.language)} · {formatDateTime(modified, i18n.language)}</span>
    </li>
  );
}
