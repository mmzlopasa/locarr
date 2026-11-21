import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type ExistingSeriesFolder, type TransferMode } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Badge, Button, EmptyState, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { codeMessage } from "@/components/errors";
import { useRootFolders } from "@/features/movies/common";
import { seriesBody, SeriesOptionsForm, useSeriesAddDefaults } from "./AddSeriesPage";
import { episodeCode } from "./common";

export function SeriesImportPage() {
  const { t } = useTranslation();
  return (
    <div>
      <PageHeader title={t("seriesImport.title")} description={t("seriesImport.description")} />
      <div className="space-y-4">
        <ExistingLibrary />
        <ManualImport />
        <RenameFiles />
      </div>
    </div>
  );
}

const key = (c: { tmdbId: number; tvdbId: number }) => (c.tmdbId ? `tmdb:${c.tmdbId}` : `tvdb:${c.tvdbId}`);

function ExistingLibrary() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const roots = useRootFolders();
  const tvRoots = (roots.data ?? []).filter((r) => r.kind === "series");
  const [root, setRoot] = useState(0);
  const [opts, setOpts] = useSeriesAddDefaults();
  const [choice, setChoice] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!root && tvRoots[0]) setRoot(tvRoots[0].id);
  }, [root, tvRoots]);
  const scan = useMutation({
    mutationFn: async () => unwrap(await api.GET("/series/import/scan", { params: { query: { rootFolderId: root } } })),
    onSuccess: (list: ExistingSeriesFolder[]) => {
      const c: Record<string, string> = {};
      for (const f of list) if (f.match) c[f.path] = key(f.match);
      setChoice(c);
    },
  });
  const run = useMutation({
    mutationFn: async () => unwrap(await api.POST("/series/import/existing", { body: {
      items: Object.entries(choice).filter(([, id]) => id).map(([path, id]) => {
        const [src, n] = id.split(":");
        return { ...seriesBody({ ...opts, rootFolderId: root }), path, ...(src === "tmdb" ? { tmdbId: Number(n) } : { tvdbId: Number(n) }) };
      }) } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.seriesList });
      scan.mutate();
    },
  });
  const chosen = Object.values(choice).filter(Boolean).length;
  return (
    <Panel title={t("seriesImport.existing.title")} description={t("seriesImport.existing.description")}>
      <div className="mb-3 flex flex-wrap items-end gap-2">
        <SelectInput label={t("addMovie.rootFolder")} value={root} onChange={(e) => setRoot(Number(e.target.value))}>
          {tvRoots.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
        </SelectInput>
        <Button onClick={() => scan.mutate()} busy={scan.isPending} disabled={!root} data-testid="series-scan">{t("import.existing.scan")}</Button>
      </div>
      <ErrorAlert error={scan.error} />
      {scan.isPending ? <Spinner label={t("import.existing.scanning")} /> : null}
      {scan.data && scan.data.length === 0 ? <EmptyState>{t("import.existing.none")}</EmptyState> : null}
      {scan.data && scan.data.length > 0 ? (
        <>
          <div className="mb-3 overflow-x-auto rounded-ui border border-line">
            <table className="w-full min-w-[640px] text-[13px]">
              <thead className="bg-surface-2 text-left text-[12px] text-muted">
                <tr>
                  <th className="px-2 py-1.5 font-medium">{t("import.existing.folder")}</th>
                  <th className="px-2 py-1.5 font-medium">{t("seriesImport.existing.videos")}</th>
                  <th className="px-2 py-1.5 font-medium">{t("import.existing.match")}</th>
                </tr>
              </thead>
              <tbody>
                {scan.data.map((f) => (
                  <tr key={f.path} className="border-t border-line">
                    <td className="px-2 py-1.5 font-mono text-[12px]">{f.name}</td>
                    <td className="px-2 py-1.5 tabular-nums text-muted">{f.videos}</td>
                    <td className="px-2 py-1.5">
                      <select aria-label={t("import.existing.match")} value={choice[f.path] ?? ""}
                        onChange={(e) => setChoice({ ...choice, [f.path]: e.target.value })}
                        className="h-8 w-full rounded-ui border border-line-strong bg-surface-2 px-2 text-[13px]">
                        <option value="">{t("import.existing.skip")}</option>
                        {f.candidates.map((c) => <option key={key(c)} value={key(c)}>{c.title} ({c.year || "?"}){c.network ? ` · ${c.network}` : ""}</option>)}
                      </select>
                      {f.error ? <span className="text-[12px] text-danger">{f.error}</span> : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <SeriesOptionsForm value={{ ...opts, rootFolderId: root }} onChange={setOpts} />
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button variant="primary" onClick={() => run.mutate()} busy={run.isPending} disabled={chosen === 0}>
              {t("import.existing.add", { count: chosen })}
            </Button>
            <ErrorAlert error={run.error} />
            {run.data ? (
              <p className="text-[13px] text-muted" role="status">
                {t("seriesImport.existing.result", { ok: run.data.filter((r) => !r.error).length, failed: run.data.filter((r) => r.error).length,
                  files: run.data.reduce((n, r) => n + r.episodeFiles, 0) })}
              </p>
            ) : null}
          </div>
        </>
      ) : null}
    </Panel>
  );
}

function ManualImport() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [path, setPath] = useState("");
  const [mode, setMode] = useState<TransferMode>("hardlink");
  const [skip, setSkip] = useState<Set<string>>(new Set());
  const preview = useMutation({
    mutationFn: async () => unwrap(await api.GET("/series/import/manual", { params: { query: { path } } })),
    onSuccess: (list) => setSkip(new Set(list.filter((c) => c.rejection || !c.seriesId).map((c) => c.path))),
  });
  const run = useMutation({
    mutationFn: async () => unwrap(await api.POST("/series/import/manual", { body: {
      items: (preview.data ?? []).filter((c) => !skip.has(c.path) && c.seriesId).map((c) => ({ path: c.path, seriesId: c.seriesId ?? 0,
        episodeIds: c.episodes.map((e) => e.id), mode, sidecars: c.sidecars })) } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.seriesList }),
  });
  const chosen = (preview.data ?? []).filter((c) => !skip.has(c.path) && c.seriesId).length;
  return (
    <Panel title={t("import.manual.title")} description={t("seriesImport.manual.description")}>
      <div className="mb-3 flex flex-wrap items-end gap-2">
        <div className="min-w-[260px] flex-1">
          <TextInput label={t("import.manual.path")} value={path} onChange={(e) => setPath(e.target.value)} placeholder="/data/downloads/…" />
        </div>
        <SelectInput label={t("seriesImport.manual.mode")} value={mode} onChange={(e) => setMode(e.target.value as TransferMode)}>
          {(["hardlink", "copy", "move"] as const).map((m) => <option key={m} value={m}>{t(`transfer.${m}`)}</option>)}
        </SelectInput>
        <Button onClick={() => preview.mutate()} busy={preview.isPending} disabled={!path.trim()}>{t("import.manual.preview")}</Button>
      </div>
      <ErrorAlert error={preview.error} />
      {preview.data && preview.data.length === 0 ? <EmptyState>{t("import.manual.none")}</EmptyState> : null}
      {preview.data && preview.data.length > 0 ? (
        <>
          <ul className="mb-3 divide-y divide-line rounded-ui border border-line text-[13px]">
            {preview.data.map((c) => (
              <li key={c.path} className="flex flex-wrap items-center gap-2 px-2 py-1.5">
                <input type="checkbox" aria-label={c.relativePath} checked={!skip.has(c.path)} disabled={!c.seriesId}
                  onChange={(e) => setSkip((s) => { const n = new Set(s); if (e.target.checked) n.delete(c.path); else n.add(c.path); return n; })}
                  className="accent-[var(--lc-accent)]" />
                <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg">{c.relativePath}</span>
                <span className="text-muted">{c.seriesTitle ?? "–"} {c.episodes.map((e) => episodeCode(e)).join(", ")}</span>
                {c.quality ? <Badge>{c.quality.name}</Badge> : null}
                <span className="tabular-nums text-faint">{formatBytes(c.sizeBytes, i18n.language)}</span>
                {c.rejection ? <span className="w-full text-[12px] text-warning">{codeMessage(t, c.rejection)}</span> : null}
              </li>
            ))}
          </ul>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="primary" onClick={() => run.mutate()} busy={run.isPending} disabled={chosen === 0}>{t("import.manual.import", { count: chosen })}</Button>
            <ErrorAlert error={run.error} />
            {run.data ? (
              <p className="text-[13px] text-muted" role="status">
                {t("seriesImport.manual.result", { ok: run.data.filter((r) => !r.error).length, failed: run.data.filter((r) => r.error).length })}
              </p>
            ) : null}
          </div>
        </>
      ) : null}
    </Panel>
  );
}

function RenameFiles() {
  const { t } = useTranslation();
  const preview = useMutation({ mutationFn: async () => unwrap(await api.POST("/series/rename/preview", { body: { seriesIds: [] } })) });
  const run = useMutation({ mutationFn: async () => unwrap(await api.POST("/series/rename", { body: { seriesIds: [] } })), onSuccess: () => preview.reset() });
  return (
    <Panel title={t("import.rename.title")} description={t("seriesImport.rename.description")}>
      <div className="flex gap-2">
        <Button onClick={() => preview.mutate()} busy={preview.isPending}>{t("import.rename.preview")}</Button>
        <Button variant="primary" onClick={() => run.mutate()} busy={run.isPending} disabled={!preview.data || preview.data.length === 0}>
          {t("import.rename.apply", { count: preview.data?.length ?? 0 })}
        </Button>
      </div>
      <ErrorAlert error={preview.error ?? run.error} />
      {preview.data ? (
        preview.data.length === 0 ? <p className="mt-2 text-[13px] text-muted">{t("import.rename.none")}</p> : (
          <ul className="mt-3 space-y-1 text-[12px]">
            {preview.data.map((r) => (
              <li key={r.from} className="rounded border border-line bg-surface-2 px-2 py-1 font-mono">
                <div className="truncate text-muted">{r.from}</div>
                <div className="truncate text-fg">→ {r.to}</div>
              </li>
            ))}
          </ul>
        )
      ) : null}
      {run.data ? <p className="mt-2 text-[13px] text-success" role="status">{t("import.rename.done", { count: run.data.length })}</p> : null}
    </Panel>
  );
}
