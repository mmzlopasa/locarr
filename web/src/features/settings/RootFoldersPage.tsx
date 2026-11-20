import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Folder, FolderOpen } from "lucide-react";
import { api, queryKeys, unwrap, type MediaKind } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Alert, Badge, Button, ConfirmButton, EmptyState, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";

const KINDS: MediaKind[] = ["movies", "series", "music"];

export function RootFoldersPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const folders = useQuery({ queryKey: queryKeys.rootFolders, queryFn: async () => unwrap(await api.GET("/rootfolders")) });
  const [path, setPath] = useState("");
  const [kind, setKind] = useState<MediaKind>("movies");
  const [browsing, setBrowsing] = useState(false);

  const add = useMutation({
    mutationFn: async () => unwrap(await api.POST("/rootfolders", { body: { path: path.trim(), kind } })),
    onSuccess: () => {
      setPath("");
      setBrowsing(false);
      void qc.invalidateQueries({ queryKey: queryKeys.rootFolders });
    },
  });
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/rootfolders/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.rootFolders }),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    add.mutate();
  };

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("rootFolders.title")} description={t("rootFolders.subtitle")} />
      <Alert tone="info">{t("rootFolders.layoutHint")}</Alert>
      <Panel title={t("rootFolders.add")}>
        <form onSubmit={submit} className="flex flex-col gap-3">
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-64 flex-1">
              <TextInput label={t("rootFolders.path")} className="font-mono" placeholder="/data/media/movies" value={path}
                onChange={(e) => setPath(e.target.value)} />
            </div>
            <div className="w-40">
              <SelectInput label={t("rootFolders.kind")} value={kind} onChange={(e) => setKind(e.target.value as MediaKind)}>
                {KINDS.map((k) => (
                  <option key={k} value={k}>
                    {t(`mediaKinds.${k}`)}
                  </option>
                ))}
              </SelectInput>
            </div>
            <Button onClick={() => setBrowsing(!browsing)} aria-expanded={browsing}>
              <FolderOpen className="size-4" />
              {t("rootFolders.browse")}
            </Button>
            <Button type="submit" variant="primary" busy={add.isPending} disabled={!path.trim()}>
              {t("rootFolders.addButton")}
            </Button>
          </div>
          {browsing ? <FolderBrowser start={path.trim()} onPick={setPath} /> : null}
          <ErrorAlert error={add.error} />
        </form>
      </Panel>
      <Panel title={t("rootFolders.list")}>
        {folders.isPending ? <Spinner /> : null}
        <ErrorAlert error={folders.error ?? remove.error} />
        {folders.data?.length === 0 ? <EmptyState>{t("rootFolders.none")}</EmptyState> : null}
        <ul className="divide-y divide-line">
          {folders.data?.map((f) => (
            <li key={f.id} className="flex flex-wrap items-center justify-between gap-3 py-2">
              <div className="min-w-0">
                <p className="flex flex-wrap items-center gap-2">
                  <span className="break-all font-mono text-[13px] text-fg">{f.path}</span>
                  <Badge>{t(`mediaKinds.${f.kind}`)}</Badge>
                  {!f.accessible ? <Badge tone="danger">{t("rootFolders.missing")}</Badge> : !f.writable ? <Badge tone="warning">{t("rootFolders.readOnly")}</Badge> : null}
                </p>
                {f.accessible ? (
                  <p className="text-[12px] text-faint">
                    {t("rootFolders.space", {
                      free: formatBytes(f.freeSpaceBytes, i18n.language),
                      total: formatBytes(f.totalSpaceBytes, i18n.language),
                    })}
                  </p>
                ) : null}
              </div>
              <ConfirmButton label={t("rootFolders.remove")} confirmLabel={t("rootFolders.removeConfirm")} busy={remove.isPending}
                onConfirm={() => remove.mutate(f.id)} />
            </li>
          ))}
        </ul>
        <p className="mt-2 text-[12px] text-faint">{t("rootFolders.removeHint")}</p>
      </Panel>
    </div>
  );
}

/** Lets an administrator click through the folders inside the allowed data roots. */
function FolderBrowser({ start, onPick }: { start: string; onPick: (p: string) => void }) {
  const { t } = useTranslation();
  const [current, setCurrent] = useState(start);
  const listing = useQuery({
    queryKey: queryKeys.folders(current),
    queryFn: async () => unwrap(await api.GET("/filesystem", { params: { query: current ? { path: current } : {} } })),
    retry: false,
  });
  const go = (p: string) => {
    setCurrent(p);
    onPick(p);
  };
  return (
    <div className="rounded-ui border border-line bg-surface-2 p-2" data-testid="folder-browser">
      <div className="mb-1 flex items-center justify-between gap-2 px-1">
        <span className="truncate font-mono text-[12px] text-muted">{listing.data?.path ?? t("rootFolders.dataRoots")}</span>
        {listing.data?.path ? (
          <Button variant="ghost" onClick={() => go(listing.data?.parent ?? "")}>
            {t("rootFolders.up")}
          </Button>
        ) : null}
      </div>
      {listing.isPending ? <Spinner /> : null}
      {listing.error ? (
        <div className="flex flex-col gap-2">
          <ErrorAlert error={listing.error} />
          <div>
            <Button variant="ghost" onClick={() => setCurrent("")}>
              {t("rootFolders.dataRoots")}
            </Button>
          </div>
        </div>
      ) : null}
      {listing.data?.entries.length === 0 ? <EmptyState>{t("rootFolders.noSubfolders")}</EmptyState> : null}
      <ul className="max-h-64 overflow-y-auto">
        {listing.data?.entries.map((e) => (
          <li key={e.path}>
            <button type="button" onClick={() => go(e.path)}
              className="flex w-full items-center gap-2 rounded px-2 py-1 text-left text-[13px] text-fg hover:bg-surface-3">
              <Folder className="size-4 text-muted" />
              <span className="truncate font-mono">{e.name}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
