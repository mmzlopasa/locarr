import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCircle2, Download, Eye, Info, ListFilter, Search, Timer, Trash2, Wand2, XCircle } from "lucide-react";
import { api, queryKeys, unwrap, type Subtitle, type SubtitleExplanation, type SubtitleItem, type SubtitleResult } from "@/api/client";
import { Alert, Badge, Button, Checkbox, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { cueTime, describeSubtitleExplanation, languageName, signed, syncReferences, useSubtitleItem, wantLabel } from "./common";

const toneClass = {
  success: "border-success/40 bg-success-bg text-success",
  danger: "border-danger/40 bg-danger-bg text-danger",
  warning: "border-warning/40 bg-warning-bg text-warning",
  info: "border-info/40 bg-info-bg text-info",
} as const;

/** The subtitle part of "why isn't it here?". */
export function SubtitleWhy({ ex, compact }: { ex: SubtitleExplanation; compact?: boolean }) {
  const { t, i18n } = useTranslation();
  const Icon = ex.tone === "success" ? CheckCircle2 : ex.tone === "danger" ? XCircle : ex.tone === "warning" ? AlertTriangle : Info;
  return (
    <section aria-label={t("subtitles.explainTitle")} data-testid="subtitle-why" data-code={ex.code}
      className={`rounded-ui border px-3 py-2 ${toneClass[ex.tone]} ${compact ? "mb-4" : "mb-3"}`}>
      <div className="flex items-start gap-2">
        <Icon className="mt-0.5 size-3.5 shrink-0" aria-hidden />
        <p className="text-[13px] text-fg"><span className="font-semibold">{t("subtitles.explainTitle")}: </span>{describeSubtitleExplanation(t, ex, i18n.language)}</p>
      </div>
    </section>
  );
}

/** The subtitle answer under the item's history ("why isn't it here?"). */
export function SubtitleWhyForItem({ itemRef }: { itemRef: string }) {
  const item = useSubtitleItem(itemRef);
  if (!item.data || !item.data.hasFile) return null;
  return <SubtitleWhy ex={item.data.explanation} compact />;
}

const statusTone = { present: "success", embedded: "info", missing: "warning", skipped: "neutral" } as const;

/** The Subtitles section of a movie or an episode. */
export function SubtitlesSection({ itemRef }: { itemRef: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const item = useSubtitleItem(itemRef);
  const [manual, setManual] = useState(false);
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: queryKeys.subtitleItem(itemRef) });
    void qc.invalidateQueries({ queryKey: queryKeys.timeline(itemRef) });
  };
  const search = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/items/{ref}/search", { params: { path: { ref: itemRef } } })),
    onSuccess: refresh,
  });
  if (item.isLoading) return <Spinner />;
  if (item.error || !item.data) return <ErrorAlert error={item.error} />;
  const it = item.data;
  if (!it.hasFile) return <SubtitleWhy ex={it.explanation} />;
  return (
    <div className="space-y-4" data-testid="subtitles-section">
      <SubtitleWhy ex={it.explanation} />
      <Panel title={t("subtitles.wanted")} description={profileText(t, it)}
        actions={
          <>
            <Button onClick={() => search.mutate()} busy={search.isPending} data-testid="subtitles-search">
              <Search className="size-3.5" /> {t("subtitles.searchNow")}
            </Button>
            <Button variant={manual ? "secondary" : "ghost"} aria-pressed={manual} onClick={() => setManual(!manual)} data-testid="subtitles-manual">
              <ListFilter className="size-3.5" /> {t("subtitles.manualSearch")}
            </Button>
          </>
        }>
        {it.wants.length === 0 ? <p className="text-[13px] text-muted">{t("subtitles.noWants")}</p> : (
          <ul className="flex flex-wrap gap-2" aria-label={t("subtitles.wanted")}>
            {it.wants.map((w) => (
              <li key={w.key} data-testid="subtitle-want" data-key={w.key} data-status={w.status}
                className="flex items-center gap-1.5 rounded border border-line bg-surface-2 px-2 py-1 text-[13px]">
                <span className="text-fg">{wantLabel(t, w.key)}</span>
                <Badge tone={statusTone[w.status]}>{t(`subtitles.status.${w.status}`)}</Badge>
              </li>
            ))}
          </ul>
        )}
        {search.data ? (
          <p className="mt-2 text-[12px] text-muted" role="status">
            {search.data.downloaded.length > 0
              ? t("subtitles.searchDone", { count: search.data.downloaded.length })
              : t("subtitles.searchNothing", { count: search.data.providers.filter((p) => !p.skipped).length })}
          </p>
        ) : null}
        <ErrorAlert error={search.error} />
      </Panel>
      {manual ? <ManualSearch itemRef={itemRef} onSaved={refresh} /> : null}
      <Panel title={t("subtitles.files")}>
        {it.subtitles.length === 0 ? <EmptyState>{t("subtitles.noFiles")}</EmptyState> : (
          <ul className="divide-y divide-line" data-testid="subtitle-files">
            {it.subtitles.map((s) => <SubtitleRow key={s.id} s={s} item={it} onChanged={refresh} />)}
          </ul>
        )}
      </Panel>
      <Panel title={t("subtitles.tracks")} description={it.probeError ? t("subtitles.tracksUnreadable") : undefined}>
        {it.tracks.length === 0 ? <p className="text-[13px] text-muted">{it.probeError ? it.probeError : t("subtitles.noTracks")}</p> : (
          <table className="w-full text-left text-[13px]">
            <thead className="text-[12px] text-faint">
              <tr><th className="py-1 pr-2 font-normal">{t("subtitles.track.kind")}</th><th className="py-1 pr-2 font-normal">{t("subtitles.track.language")}</th>
                <th className="py-1 pr-2 font-normal">{t("subtitles.track.codec")}</th><th className="py-1 font-normal">{t("subtitles.track.flags")}</th></tr>
            </thead>
            <tbody>
              {it.tracks.filter((tr) => tr.kind !== "video").map((tr) => (
                <tr key={`${tr.kind}-${tr.index}`} className="border-t border-line" data-testid="media-track">
                  <td className="py-1 pr-2 text-muted">{t(`subtitles.track.kinds.${tr.kind}`)} {tr.index + 1}</td>
                  <td className="py-1 pr-2 text-fg">{languageName(t, tr.language)}{tr.name ? <span className="text-faint"> · {tr.name}</span> : null}</td>
                  <td className="py-1 pr-2 font-mono text-[12px] text-muted">{tr.codec}</td>
                  <td className="py-1 text-[12px] text-muted">{[tr.default ? t("subtitles.track.default") : "", tr.forced ? t("subtitles.forced") : "",
                    tr.hearingImpaired ? t("subtitles.hi") : "", tr.kind === "subtitle" && !tr.text ? t("subtitles.track.image") : ""].filter(Boolean).join(" · ") || "–"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </div>
  );
}

function profileText(t: ReturnType<typeof useTranslation>["t"], it: SubtitleItem): string {
  if (it.profile) return t(`subtitles.profileFrom.${it.profileSource}`, { name: it.profile.name });
  return it.profileSource === "none" && it.profileOverride === -1 ? t("subtitles.profileOff") : t("subtitles.profileNone");
}

function SubtitleRow({ s, item, onChanged }: { s: Subtitle; item: SubtitleItem; onChanged: () => void }) {
  const { t } = useTranslation();
  const [tool, setTool] = useState<"" | "preview" | "shift" | "fps" | "sync" | "delete">("");
  const toggle = (k: typeof tool) => setTool(tool === k ? "" : k);
  const file = s.path.split("/").pop();
  return (
    <li className="py-2" data-testid="subtitle-row" data-language={s.language}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-[13px] font-medium text-fg">{languageName(t, s.language)}</span>
        {s.forced ? <Badge tone="info">{t("subtitles.forced")}</Badge> : null}
        {s.hearingImpaired ? <Badge tone="info">{t("subtitles.hi")}</Badge> : null}
        <Badge>{s.format}</Badge>
        <span className="text-[12px] text-muted">
          {s.source === "disk" ? t("subtitles.fromDisk") : t("subtitles.fromProvider", { provider: s.provider, score: s.score ?? 0 })}
        </span>
        {s.syncState ? <SyncBadge s={s} /> : null}
        <span className="ml-auto flex flex-wrap gap-1">
          <ToolButton active={tool === "preview"} onClick={() => toggle("preview")} icon={<Eye className="size-3.5" />} label={t("subtitles.tools.preview")} id="preview" />
          <ToolButton active={tool === "shift"} onClick={() => toggle("shift")} icon={<Timer className="size-3.5" />} label={t("subtitles.tools.shift")} id="shift" />
          <ToolButton active={tool === "fps"} onClick={() => toggle("fps")} icon={<Timer className="size-3.5" />} label={t("subtitles.tools.fps")} id="fps" />
          <ToolButton active={tool === "sync"} onClick={() => toggle("sync")} icon={<Wand2 className="size-3.5" />} label={t("subtitles.tools.sync")} id="sync" />
          <ToolButton active={tool === "delete"} onClick={() => toggle("delete")} icon={<Trash2 className="size-3.5" />} label={t("common.delete")} id="delete" />
        </span>
      </div>
      <div className="mt-0.5 break-all font-mono text-[11px] text-faint">{file}{s.release ? ` · ${s.release}` : ""}</div>
      {tool === "preview" ? <Preview id={s.id} /> : null}
      {tool === "shift" ? <ShiftTool s={s} onDone={onChanged} /> : null}
      {tool === "fps" ? <FpsTool s={s} onDone={onChanged} /> : null}
      {tool === "sync" ? <SyncTool s={s} item={item} onDone={onChanged} /> : null}
      {tool === "delete" ? <DeleteTool s={s} onDone={onChanged} /> : null}
    </li>
  );
}

function ToolButton({ active, onClick, icon, label, id }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string; id: string }) {
  return (
    <Button variant={active ? "secondary" : "ghost"} aria-pressed={active} onClick={onClick} data-testid={`subtitle-tool-${id}`}>
      {icon} {label}
    </Button>
  );
}

function SyncBadge({ s }: { s: Subtitle }) {
  const { t } = useTranslation();
  const tone = s.syncState === "done" ? "success" : s.syncState === "running" ? "info" : s.syncState === "not_confident" ? "warning" : "danger";
  return (
    <span title={s.syncMessage} data-testid="sync-state" data-state={s.syncState}>
      <Badge tone={tone}>{s.syncState === "done" && s.syncOffsetMs !== 0
        ? t("subtitles.sync.doneOffset", { offset: signed(s.syncOffsetMs) })
        : t(`subtitles.sync.states.${s.syncState}`, { defaultValue: s.syncState })}</Badge>
    </span>
  );
}

function Preview({ id }: { id: number }) {
  const { t } = useTranslation();
  const pv = useQuery({
    queryKey: ["subtitles", id, "preview"],
    queryFn: async () => unwrap(await api.GET("/subtitles/{id}/preview", { params: { path: { id }, query: { lines: 12 } } })),
  });
  if (pv.isLoading) return <Spinner />;
  if (pv.error || !pv.data) return <ErrorAlert error={pv.error} />;
  return (
    <div className="mt-2 rounded border border-line bg-surface-2 p-2" data-testid="subtitle-preview">
      <p className="mb-1 text-[11px] text-faint">{t("subtitles.previewOf", { shown: pv.data.lines.length, total: pv.data.total })}</p>
      <ol className="space-y-1">
        {pv.data.lines.map((l, i) => (
          <li key={i} className="grid grid-cols-[auto_1fr] gap-3 text-[12px]">
            <span className="font-mono tabular-nums text-faint" data-testid="preview-start">{cueTime(l.startMs)}</span>
            <span className="whitespace-pre-line text-fg">{l.text}</span>
          </li>
        ))}
      </ol>
    </div>
  );
}

function useToolDone(id: number, onDone: () => void) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: ["subtitles", id, "preview"] });
    onDone();
  };
}

function ShiftTool({ s, onDone }: { s: Subtitle; onDone: () => void }) {
  const { t } = useTranslation();
  const [ms, setMs] = useState("500");
  const done = useToolDone(s.id, onDone);
  const shift = useMutation({
    mutationFn: async (offsetMs: number) => unwrap(await api.POST("/subtitles/{id}/shift", { params: { path: { id: s.id } }, body: { offsetMs } })),
    onSuccess: done,
  });
  const n = Math.round(Number(ms));
  const valid = Number.isFinite(n) && n > 0 && n <= 3_600_000;
  return (
    <div className="mt-2 flex flex-wrap items-end gap-2 rounded border border-line bg-surface-2 p-2">
      <div className="w-36"><TextInput label={t("subtitles.shiftBy")} inputMode="numeric" value={ms} onChange={(e) => setMs(e.target.value)} data-testid="shift-ms" /></div>
      <Button onClick={() => shift.mutate(-n)} disabled={!valid} busy={shift.isPending} data-testid="shift-earlier">{t("subtitles.earlier")}</Button>
      <Button onClick={() => shift.mutate(n)} disabled={!valid} busy={shift.isPending} data-testid="shift-later">{t("subtitles.later")}</Button>
      <p className="w-full text-[11px] text-faint">{t("subtitles.editHint")}</p>
      {shift.isSuccess ? <p className="text-[12px] text-success" role="status">{t("subtitles.shifted")}</p> : null}
      <ErrorAlert error={shift.error} />
    </div>
  );
}

const FPS = ["23.976", "24", "25", "29.97", "30"];

function FpsTool({ s, onDone }: { s: Subtitle; onDone: () => void }) {
  const { t } = useTranslation();
  const [from, setFrom] = useState("25");
  const [to, setTo] = useState("23.976");
  const done = useToolDone(s.id, onDone);
  const conv = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/{id}/fps", { params: { path: { id: s.id } }, body: { from, to } })),
    onSuccess: done,
  });
  return (
    <div className="mt-2 flex flex-wrap items-end gap-2 rounded border border-line bg-surface-2 p-2">
      <div className="w-36">
        <SelectInput label={t("subtitles.fpsFrom")} value={from} onChange={(e) => setFrom(e.target.value)}>
          {FPS.map((f) => <option key={f} value={f}>{f}</option>)}
        </SelectInput>
      </div>
      <div className="w-36">
        <SelectInput label={t("subtitles.fpsTo")} value={to} onChange={(e) => setTo(e.target.value)}>
          {FPS.map((f) => <option key={f} value={f}>{f}</option>)}
        </SelectInput>
      </div>
      <Button onClick={() => conv.mutate()} disabled={from === to} busy={conv.isPending} data-testid="fps-convert">{t("subtitles.convert")}</Button>
      <p className="w-full text-[11px] text-faint">{t("subtitles.editHint")}</p>
      <ErrorAlert error={conv.error} />
    </div>
  );
}

function SyncTool({ s, item, onDone }: { s: Subtitle; item: SubtitleItem; onDone: () => void }) {
  const { t } = useTranslation();
  const refs = syncReferences(t, item, s.id);
  const available = refs.filter((r) => item.syncAvailable || r.value.startsWith("subtitle:"));
  const [ref, setRef] = useState(available[0]?.value ?? "");
  const sync = useMutation({
    mutationFn: async () => unwrap(await api.POST("/subtitles/{id}/sync", { params: { path: { id: s.id } }, body: { reference: ref } })),
    onSuccess: onDone,
  });
  return (
    <div className="mt-2 space-y-2 rounded border border-line bg-surface-2 p-2" data-testid="sync-tool">
      {!item.syncAvailable ? <Alert tone="warning">{t("subtitles.sync.unavailable")}</Alert> : null}
      {available.length === 0 ? <p className="text-[12px] text-muted">{t("subtitles.sync.noReference")}</p> : (
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-64">
            <SelectInput label={t("subtitles.sync.reference")} value={ref} onChange={(e) => setRef(e.target.value)} data-testid="sync-reference">
              {available.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
            </SelectInput>
          </div>
          <Button variant="primary" onClick={() => sync.mutate()} busy={sync.isPending || s.syncState === "running"} data-testid="sync-start">
            <Wand2 className="size-3.5" /> {t("subtitles.sync.start")}
          </Button>
        </div>
      )}
      <p className="text-[11px] text-faint">{t("subtitles.sync.hint")}</p>
      {s.syncMessage ? <p className="text-[12px] text-muted" data-testid="sync-message">{s.syncMessage}</p> : null}
      <ErrorAlert error={sync.error} />
    </div>
  );
}

function DeleteTool({ s, onDone }: { s: Subtitle; onDone: () => void }) {
  const { t } = useTranslation();
  const [blacklist, setBlacklist] = useState(s.source === "downloaded");
  const del = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/subtitles/{id}", { params: { path: { id: s.id }, query: { blacklist } } })),
    onSuccess: onDone,
  });
  return (
    <div className="mt-2 space-y-2 rounded border border-line bg-danger-bg/40 p-2">
      <p className="text-[13px] text-fg">{t("subtitles.deleteQuestion")}</p>
      {s.source === "downloaded" ? (
        <Checkbox label={t("subtitles.blacklist")} description={t("subtitles.blacklistHint")} checked={blacklist} onChange={(e) => setBlacklist(e.target.checked)} />
      ) : null}
      <Button variant="danger" onClick={() => del.mutate()} busy={del.isPending} data-testid="subtitle-delete-confirm">{t("subtitles.deleteConfirm")}</Button>
      <ErrorAlert error={del.error} />
    </div>
  );
}

function ManualSearch({ itemRef, onSaved }: { itemRef: string; onSaved: () => void }) {
  const { t } = useTranslation();
  const [language, setLanguage] = useState("");
  const [started, setStarted] = useState(0);
  const results = useQuery({
    queryKey: ["subtitles", "results", itemRef, language, started],
    queryFn: async () => unwrap(await api.GET("/subtitles/items/{ref}/results", { params: { path: { ref: itemRef }, query: language ? { language } : {} } })),
    staleTime: Infinity,
  });
  const dl = useMutation({
    mutationFn: async (key: string) => unwrap(await api.POST("/subtitles/items/{ref}/download", { params: { path: { ref: itemRef } }, body: { key } })),
    onSuccess: () => { onSaved(); setStarted(started + 1); },
  });
  return (
    <Panel title={t("subtitles.manualSearch")} description={t("subtitles.manualHint")}>
      <div className="mb-3 flex flex-wrap items-end gap-2">
        <div className="w-40">
          <TextInput label={t("subtitles.languageCode")} placeholder={t("subtitles.languageProfile")} value={language}
            onChange={(e) => setLanguage(e.target.value.toLowerCase().trim())} maxLength={10} />
        </div>
        <Button onClick={() => setStarted(started + 1)} busy={results.isFetching}><Search className="size-3.5" /> {t("subtitles.searchAgain")}</Button>
      </div>
      {results.data ? (
        <p className="mb-2 text-[12px] text-muted" data-testid="provider-reports">
          {results.data.providers.map((p) => p.skipped ? t("subtitles.reportSkipped", { provider: p.provider })
            : p.error ? t("subtitles.reportError", { provider: p.provider, error: t(`subtitles.errorCodes.${p.code ?? ""}`, { defaultValue: p.error }) })
              : t("subtitles.reportOk", { provider: p.provider, count: p.results })).join(" · ")}
        </p>
      ) : null}
      {results.isLoading ? <Spinner /> : null}
      <ErrorAlert error={results.error ?? dl.error} />
      {results.data && results.data.results.length === 0 ? <EmptyState>{t("subtitles.noResults")}</EmptyState> : null}
      {results.data && results.data.results.length > 0 ? (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-[13px]">
            <thead className="text-[12px] text-faint">
              <tr>
                <th className="py-1 pr-2 font-normal">{t("subtitles.results.score")}</th>
                <th className="py-1 pr-2 font-normal">{t("subtitles.results.release")}</th>
                <th className="py-1 pr-2 font-normal">{t("subtitles.results.language")}</th>
                <th className="py-1 pr-2 font-normal">{t("subtitles.results.provider")}</th>
                <th className="py-1 font-normal" />
              </tr>
            </thead>
            <tbody>
              {results.data.results.map((r) => <ResultRow key={r.key} r={r} busy={dl.isPending && dl.variables === r.key} onPick={() => dl.mutate(r.key)} />)}
            </tbody>
          </table>
        </div>
      ) : null}
    </Panel>
  );
}

function ResultRow({ r, onPick, busy }: { r: SubtitleResult; onPick: () => void; busy: boolean }) {
  const { t } = useTranslation();
  const below = r.score < r.minScore;
  return (
    <tr className={`border-t border-line align-top ${r.rejected ? "opacity-60" : ""}`} data-testid="subtitle-result" data-provider={r.provider} data-id={r.id}>
      <td className="py-1.5 pr-2">
        <span className={`font-mono tabular-nums ${below ? "text-warning" : "text-success"}`} title={r.parts.map((p) => `${t(`subtitles.parts.${p.code}`, { defaultValue: p.code })} ${signed(p.points)}`).join("\n")}
          data-testid="result-score">{r.score}</span>
      </td>
      <td className="max-w-[420px] py-1.5 pr-2">
        <div className="truncate font-mono text-[12px] text-fg" title={r.releases.join("\n")}>{r.releases[0] || r.fileName}</div>
        <div className="mt-0.5 flex flex-wrap gap-1">
          {r.hashMatch ? <Badge tone="success">{t("subtitles.parts.hash")}</Badge> : null}
          {r.hearingImpaired ? <Badge tone="info">{t("subtitles.hi")}</Badge> : null}
          {r.forced ? <Badge tone="info">{t("subtitles.forced")}</Badge> : null}
          {r.trusted ? <Badge>{t("subtitles.parts.trusted")}</Badge> : null}
          {r.machineTranslated ? <Badge tone="warning">{t("subtitles.parts.machine_translated")}</Badge> : null}
          {r.downloaded ? <Badge>{t("subtitles.results.downloaded")}</Badge> : null}
          {r.blacklisted ? <Badge tone="danger">{t("subtitles.results.blacklisted")}</Badge> : null}
          {r.rejected ? <Badge tone="warning">{t(`subtitles.rejected.${r.rejected}`, { defaultValue: r.rejected })}</Badge> : null}
        </div>
        <div className="mt-0.5 text-[11px] text-faint">{r.parts.map((p) => `${t(`subtitles.parts.${p.code}`, { defaultValue: p.code })} ${signed(p.points)}`).join(" · ")}</div>
      </td>
      <td className="py-1.5 pr-2 text-muted">{languageName(t, r.language)}</td>
      <td className="py-1.5 pr-2 text-muted">{r.provider}{r.downloads ? <span className="block text-[11px] text-faint">{t("subtitles.results.downloads", { count: r.downloads })}</span> : null}</td>
      <td className="py-1.5 text-right">
        <Button onClick={onPick} busy={busy} disabled={r.rejected === "language" || r.rejected === "other_movie" || r.rejected === "other_episode"} data-testid="result-download">
          <Download className="size-3.5" /> {t("subtitles.results.download")}
        </Button>
      </td>
    </tr>
  );
}
