import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type ExistingFolder, type ManualImportCandidate, type TransferMode } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Alert, Badge, Button, EmptyState, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { codeMessage } from "@/components/errors";
import { AddOptionsForm, splitTags, useAddDefaults } from "./AddMoviePage";
import { useRootFolders } from "./common";

export function ImportPage() {
  const { t } = useTranslation();
  return (
    <div>
      <PageHeader title={t("import.title")} description={t("import.description")} />
      <div className="space-y-4">
        <ExistingLibrary />
        <ManualImport />
        <RenameFiles />
      </div>
    </div>
  );
}

function ExistingLibrary() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const roots = useRootFolders();
  const movieRoots = (roots.data ?? []).filter((r) => r.kind === "movies");
  const [root, setRoot] = useState(0);
  const [opts, setOpts] = useAddDefaults();
  const [choice, setChoice] = useState<Record<string, number>>({});
  useEffect(() => {
    if (!root && movieRoots[0]) setRoot(movieRoots[0].id);
  }, [root, movieRoots]);
  const scan = useMutation({
    mutationFn: async () => unwrap(await api.GET("/movies/import/scan", { params: { query: { rootFolderId: root } } })),
    onSuccess: (list: ExistingFolder[]) => {
      const c: Record<string, number> = {};
      for (const f of list) if (f.match) c[f.path] = f.match.tmdbId;
      setChoice(c);
    },
  });
  const run = useMutation({
    mutationFn: async () => unwrap(await api.POST("/movies/import/existing", { body: {
      rootFolderId: root, qualityProfileId: opts.qualityProfileId, minimumAvailability: opts.minimumAvailability, monitored: opts.monitored,
      searchOnAdd: opts.searchOnAdd, tags: splitTags(opts.tags),
      items: Object.entries(choice).filter(([, id]) => id > 0).map(([path, tmdbId]) => ({ path, tmdbId })) } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.movies });
      scan.mutate();
    },
  });
  const chosen = Object.values(choice).filter((x) => x > 0).length;
  return (
    <Panel title={t("import.existing.title")} description={t("import.existing.description")}>
      <div className="mb-3 flex flex-wrap items-end gap-2">
        <SelectInput label={t("addMovie.rootFolder")} value={root} onChange={(e) => setRoot(Number(e.target.value))}>
          {movieRoots.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
        </SelectInput>
        <Button onClick={() => scan.mutate()} busy={scan.isPending} disabled={!root}>{t("import.existing.scan")}</Button>
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
                  <th className="px-2 py-1.5 font-medium">{t("import.existing.file")}</th>
                  <th className="px-2 py-1.5 font-medium">{t("import.existing.match")}</th>
                </tr>
              </thead>
              <tbody>
                {scan.data.map((f) => (
                  <tr key={f.path} className="border-t border-line">
                    <td className="px-2 py-1.5 font-mono text-[12px]">{f.name}</td>
                    <td className="px-2 py-1.5 text-muted">{f.video ? `${f.video.split("/").pop()} · ${formatBytes(f.videoSizeBytes, i18n.language)}` : t("import.existing.noVideo")}</td>
                    <td className="px-2 py-1.5">
                      <select aria-label={t("import.existing.match")} value={choice[f.path] ?? 0}
                        onChange={(e) => setChoice({ ...choice, [f.path]: Number(e.target.value) })}
                        className="h-8 w-full rounded-ui border border-line-strong bg-surface-2 px-2 text-[13px]">
                        <option value={0}>{t("import.existing.skip")}</option>
                        {f.candidates.map((c) => <option key={c.tmdbId} value={c.tmdbId}>{c.title} ({c.year || "?"})</option>)}
                      </select>
                      {f.error ? <span className="text-[12px] text-danger">{f.error}</span> : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <AddOptionsForm value={{ ...opts, rootFolderId: root }} onChange={(v) => setOpts(v)} />
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button variant="primary" onClick={() => run.mutate()} busy={run.isPending} disabled={chosen === 0}>
              {t("import.existing.add", { count: chosen })}
            </Button>
            <ErrorAlert error={run.error} />
            {run.data ? (
              <p className="text-[13px] text-muted" role="status">
                {t("import.existing.result", { ok: run.data.filter((r) => !r.error).length, failed: run.data.filter((r) => r.error).length })}
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
  const [picks, setPicks] = useState<Record<string, number>>({});
  const movies = useQuery({
    queryKey: [...queryKeys.movies, "all-titles"],
    queryFn: async () => unwrap(await api.GET("/movies", { params: { query: { limit: 1000, sort: "title" } } })),
  });
  const preview = useMutation({
    mutationFn: async () => unwrap(await api.GET("/movies/import/manual", { params: { query: { path } } })),
    onSuccess: (list: ManualImportCandidate[]) => {
      const p: Record<string, number> = {};
      for (const c of list) if (!c.skipped && c.movieId && !c.rejection) p[c.path] = c.movieId;
      setPicks(p);
    },
  });
  const run = useMutation({
    mutationFn: async () => {
      const items = Object.entries(picks).filter(([, id]) => id > 0).map(([p, movieId]) => ({
        path: p, movieId, mode, sidecars: preview.data?.find((c) => c.path === p)?.sidecars ?? [] }));
      return unwrap(await api.POST("/movies/import/manual", { body: { items } }));
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.movies }),
  });
  return (
    <Panel title={t("import.manual.title")} description={t("import.manual.description")}>
      <div className="mb-3 flex flex-wrap items-end gap-2">
        <div className="min-w-[280px] flex-1">
          <TextInput label={t("import.manual.path")} placeholder="/data/downloads/…" value={path} onChange={(e) => setPath(e.target.value)} />
        </div>
        <Button onClick={() => preview.mutate()} busy={preview.isPending} disabled={!path.startsWith("/")}>{t("import.manual.preview")}</Button>
      </div>
      <ErrorAlert error={preview.error} />
      {preview.data ? (
        preview.data.length === 0 ? <EmptyState>{t("import.manual.none")}</EmptyState> : (
          <>
            <ul className="mb-3 divide-y divide-line rounded-ui border border-line">
              {preview.data.map((c) => (
                <li key={c.path} className="flex flex-wrap items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-mono text-[12px] text-fg">{c.relativePath}</div>
                    <div className="text-[12px] text-faint">
                      {formatBytes(c.sizeBytes, i18n.language)}
                      {c.quality ? ` · ${c.quality.name}` : ""}
                      {c.sidecars.length ? ` · ${t("import.manual.sidecars", { count: c.sidecars.length })}` : ""}
                    </div>
                    {c.rejection ? <div className="text-[12px] text-warning">{codeMessage(t, c.rejection)}</div> : null}
                  </div>
                  {c.skipped ? <Badge>{t(`import.skip.${c.skipped}`, { defaultValue: c.skipped })}</Badge> : (
                    <select aria-label={t("import.manual.movie")} value={picks[c.path] ?? 0} onChange={(e) => setPicks({ ...picks, [c.path]: Number(e.target.value) })}
                      className="h-8 max-w-[260px] rounded-ui border border-line-strong bg-surface-2 px-2 text-[13px]">
                      <option value={0}>{t("import.existing.skip")}</option>
                      {(movies.data?.items ?? []).map((m) => <option key={m.id} value={m.id}>{m.title} ({m.year || "?"})</option>)}
                    </select>
                  )}
                </li>
              ))}
            </ul>
            <div className="flex flex-wrap items-end gap-2">
              <SelectInput label={t("media.transferMode")} value={mode} onChange={(e) => setMode(e.target.value as TransferMode)}>
                {(["hardlink", "copy", "move"] as const).map((m) => <option key={m} value={m}>{t(`transfer.${m}`)}</option>)}
              </SelectInput>
              <Button variant="primary" onClick={() => run.mutate()} busy={run.isPending} disabled={Object.values(picks).every((x) => !x)}>
                {t("import.manual.import")}
              </Button>
            </div>
            {mode === "move" ? <div className="mt-2"><Alert tone="warning">{t("media.moveWarning")}</Alert></div> : null}
            <ErrorAlert error={run.error} />
            {run.data ? (
              <ul className="mt-2 space-y-1 text-[12px]" role="status">
                {run.data.map((r) => (
                  <li key={r.path} className={r.error ? "text-danger" : "text-success"}>
                    {r.path.split("/").pop()}: {r.error ? codeMessage(t, r.error) : t("import.manual.done", { path: r.importedPath, mode: t(`transfer.${r.mode}`, { defaultValue: r.mode }) })}
                  </li>
                ))}
              </ul>
            ) : null}
          </>
        )
      ) : null}
    </Panel>
  );
}

function RenameFiles() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const preview = useMutation({ mutationFn: async () => unwrap(await api.POST("/movies/rename/preview", { body: { movieIds: [] } })) });
  const apply = useMutation({
    mutationFn: async () => unwrap(await api.POST("/movies/rename", { body: { movieIds: (preview.data ?? []).map((r) => r.movieId) } })),
    onSuccess: () => {
      preview.reset();
      void qc.invalidateQueries({ queryKey: queryKeys.movies });
    },
  });
  return (
    <Panel title={t("import.rename.title")} description={t("import.rename.description")}>
      <Button onClick={() => preview.mutate()} busy={preview.isPending} data-testid="rename-preview">{t("import.rename.preview")}</Button>
      <ErrorAlert error={preview.error ?? apply.error} />
      {preview.data && preview.data.length === 0 ? <p className="mt-2 text-[13px] text-muted">{t("import.rename.none")}</p> : null}
      {preview.data && preview.data.length > 0 ? (
        <>
          <ul className="mt-3 divide-y divide-line rounded-ui border border-line">
            {preview.data.map((r) => (
              <li key={r.movieId} className="px-3 py-2 font-mono text-[12px]">
                <div className="text-muted line-through decoration-faint">{r.from.split("/").pop()}</div>
                <div className="text-fg">{r.to.split("/").pop()}</div>
              </li>
            ))}
          </ul>
          <Button className="mt-3" variant="primary" onClick={() => apply.mutate()} busy={apply.isPending}>
            {t("import.rename.apply", { count: preview.data.length })}
          </Button>
        </>
      ) : null}
      {apply.data ? <p role="status" className="mt-2 text-[13px] text-success">{t("import.rename.done", { count: apply.data.length })}</p> : null}
    </Panel>
  );
}
