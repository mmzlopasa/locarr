import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { FolderSearch } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, ErrorAlert, PageHeader, Panel, SelectInput, Spinner } from "@/components/ui";
import { useRootFolders } from "@/features/movies/common";
import { ArtistOptionsForm, defaultArtistOptions, type ArtistOptions } from "./AddArtistPage";
import { musicKeys } from "./common";

/** Import an existing music library: scan a root folder, check the matches, accept. Files stay where they are. */
export function MusicImportPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const roots = useRootFolders();
  const musicRoots = (roots.data ?? []).filter((r) => r.kind === "music");
  const [rootId, setRootId] = useState(0);
  const [scanned, setScanned] = useState(0);
  const [opts, setOpts] = useState<ArtistOptions>({ ...defaultArtistOptions, searchOnAdd: false });
  const [choice, setChoice] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!rootId && musicRoots[0]) setRootId(musicRoots[0].id);
  }, [rootId, musicRoots]);
  const scan = useQuery({
    queryKey: ["music", "scan", scanned],
    queryFn: async () => unwrap(await api.GET("/music/import/scan", { params: { query: { rootFolderId: scanned } } })),
    enabled: scanned > 0,
    retry: false,
  });
  useEffect(() => {
    if (!scan.data) return;
    const next: Record<string, string> = {};
    for (const f of scan.data) if (!f.artistId && f.match) next[f.path] = f.match.mbid;
    setChoice(next);
  }, [scan.data]);
  const accept = useMutation({
    mutationFn: async () => unwrap(await api.POST("/music/import/existing", { body: { rootFolderId: rootId, qualityProfileId: opts.qualityProfileId,
      metadataProfileId: opts.metadataProfileId, monitored: opts.monitored, monitor: opts.monitor, monitorNewAlbums: opts.monitorNewAlbums,
      searchOnAdd: opts.searchOnAdd, items: Object.entries(choice).filter(([, m]) => m).map(([path, mbid]) => ({ path, mbid })) } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: musicKeys.artists }),
  });
  const selected = Object.values(choice).filter(Boolean).length;
  return (
    <div>
      <PageHeader title={t("musicImport.title")} description={t("musicImport.description")} />
      {roots.data && musicRoots.length === 0 ? <Alert tone="warning">{t("addArtist.noRoot")}</Alert> : null}
      <div className="mb-4 flex items-end gap-2">
        <div className="w-80">
          <SelectInput label={t("addMovie.rootFolder")} value={rootId} onChange={(e) => setRootId(Number(e.target.value))}>
            {musicRoots.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
          </SelectInput>
        </div>
        <Button onClick={() => setScanned(rootId)} disabled={!rootId} busy={scan.isFetching} data-testid="scan-music">
          <FolderSearch className="size-3.5" /> {t("musicImport.scan")}
        </Button>
      </div>
      <p className="mb-3 text-[12px] text-faint">{t("musicImport.rate")}</p>
      {scan.isFetching ? <Spinner label={t("musicImport.scanning")} /> : null}
      <ErrorAlert error={scan.error} />
      {scan.data && scan.data.length === 0 ? <p className="text-[13px] text-muted">{t("musicImport.nothing")}</p> : null}
      {scan.data && scan.data.length > 0 ? (
        <>
          <div className="mb-4 overflow-x-auto rounded-ui border border-line">
            <table className="w-full min-w-[760px] text-[13px]">
              <thead className="bg-surface-2 text-left text-[12px] text-muted">
                <tr>
                  <th className="px-2 py-1.5 font-medium">{t("musicImport.folder")}</th>
                  <th className="px-2 py-1.5 font-medium">{t("musicImport.albums")}</th>
                  <th className="px-2 py-1.5 font-medium">{t("musicImport.match")}</th>
                </tr>
              </thead>
              <tbody>
                {scan.data.map((f) => (
                  <tr key={f.path} className="border-t border-line align-top" data-testid="existing-artist">
                    <td className="px-2 py-1.5">
                      <div className="text-fg">{f.name}</div>
                      {f.tagArtist && f.tagArtist !== f.name ? <div className="text-[11px] text-faint">{t("musicImport.tagged", { name: f.tagArtist })}</div> : null}
                    </td>
                    <td className="px-2 py-1.5 text-[12px] text-muted">
                      {f.albums.slice(0, 4).map((a) => <div key={a.path} className="truncate">{a.album || a.name} <span className="text-faint">· {a.files}</span></div>)}
                      {f.albums.length > 4 ? <div className="text-faint">+{f.albums.length - 4}</div> : null}
                    </td>
                    <td className="px-2 py-1.5">
                      {f.artistId ? (
                        <Link to="/music/artists/$id" params={{ id: String(f.artistId) }} className="text-[13px] text-accent hover:underline">{t("addMovie.inLibrary")}</Link>
                      ) : (
                        <select aria-label={t("musicImport.matchFor", { name: f.name })} value={choice[f.path] ?? ""}
                          onChange={(e) => setChoice((c) => ({ ...c, [f.path]: e.target.value }))}
                          className="h-8 w-full rounded-ui border border-line-strong bg-surface-2 px-2 text-[13px]">
                          <option value="">{t("musicImport.skip")}</option>
                          {f.candidates.map((c) => (
                            <option key={c.mbid} value={c.mbid}>{c.name}{c.disambiguation ? ` (${c.disambiguation})` : ""}{c.country ? ` · ${c.country}` : ""}</option>
                          ))}
                        </select>
                      )}
                      {f.error ? <p className="mt-1 text-[12px] text-danger">{f.error}</p> : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Panel title={t("musicImport.options")} description={t("musicImport.optionsHint")}>
            <ArtistOptionsForm value={opts} onChange={setOpts} />
            <div className="mt-3 flex items-center gap-2">
              <Button variant="primary" onClick={() => accept.mutate()} busy={accept.isPending} disabled={selected === 0} data-testid="accept-existing">
                {t("musicImport.accept", { count: selected })}
              </Button>
            </div>
            <ErrorAlert error={accept.error} />
            {accept.data ? (
              <ul className="mt-2 space-y-0.5 text-[13px]" role="status">
                {accept.data.map((r) => (
                  <li key={r.path} className={r.error ? "text-danger" : "text-success"}>
                    {r.artist ? t("musicImport.added", { name: r.artist.name }) : `${r.path}: ${r.error?.message ?? ""}`}
                  </li>
                ))}
              </ul>
            ) : null}
          </Panel>
        </>
      ) : null}
    </div>
  );
}
