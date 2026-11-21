import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Button, Checkbox, ErrorAlert, Panel, Spinner, TextInput } from "@/components/ui";
import { musicKeys, type MusicSettings } from "./common";

function useDebounced<T>(v: T, ms: number): T {
  const [d, setD] = useState(v);
  useEffect(() => {
    const id = setTimeout(() => setD(v), ms);
    return () => clearTimeout(id);
  }, [v, ms]);
  return d;
}

/** Settings → Media management: music naming (with a live preview) and import options. */
export function MusicNamingPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const current = useQuery({ queryKey: musicKeys.settings, queryFn: async () => unwrap(await api.GET("/settings/music")) });
  const [s, setS] = useState<MusicSettings | null>(null);
  useEffect(() => {
    if (current.data && !s) setS(current.data);
  }, [current.data, s]);
  const save = useMutation({
    mutationFn: async (v: MusicSettings) => unwrap(await api.PUT("/settings/music", { body: v })),
    onSuccess: (v) => {
      setS(v);
      qc.setQueryData(musicKeys.settings, v);
    },
  });
  const naming = useDebounced(s, 250);
  const preview = useQuery({
    queryKey: ["music-naming-preview", naming],
    queryFn: async () => unwrap(await api.POST("/settings/music/preview", { body: naming! })),
    enabled: !!naming,
    placeholderData: (p) => p,
  });
  if (!s) return current.error ? <ErrorAlert error={current.error} /> : <Spinner />;
  const set = <K extends keyof MusicSettings>(k: K, v: MusicSettings[K]) => {
    setS({ ...s, [k]: v });
    save.reset();
  };
  const errs = (preview.data?.errors ?? {}) as Record<string, string>;
  const field = (k: "artistFolderFormat" | "albumFolderFormat" | "trackFormat" | "multiDiscTrackFormat" | "discFolderFormat") => (
    <TextInput label={t(`musicNaming.${k}`)} value={s[k]} onChange={(e) => set(k, e.target.value)} className="font-mono" data-testid={`music-${k}`}
      hint={errs[k] ? <span className="text-danger">{errs[k]}</span> : undefined} />
  );
  const p = preview.data;
  return (
    <Panel title={t("musicNaming.title")} description={t("musicNaming.description")}>
      <div className="space-y-3">
        <Checkbox label={t("musicNaming.rename")} description={t("musicNaming.renameHint")} checked={s.renameTracks} onChange={(e) => set("renameTracks", e.target.checked)} />
        {field("artistFolderFormat")}
        {field("albumFolderFormat")}
        {field("trackFormat")}
        {field("multiDiscTrackFormat")}
        <Checkbox label={t("musicNaming.discFolders")} description={t("musicNaming.discFoldersHint")} checked={s.discFolders}
          onChange={(e) => set("discFolders", e.target.checked)} />
        {s.discFolders ? field("discFolderFormat") : null}
        <div className="rounded-ui border border-line bg-surface-2 p-3" aria-live="polite" data-testid="music-naming-preview">
          <div className="text-[12px] text-muted">{t("musicNaming.preview")}</div>
          {p ? (
            <div className="mt-1 break-all font-mono text-[12px] text-fg">
              <div>{p.artistFolder}/{p.albumFolder}/{p.track}</div>
              <div className="text-muted">{p.artistFolder}/{p.albumFolder}/{p.multiDiscTrack}</div>
            </div>
          ) : null}
          <details className="mt-2 text-[12px]">
            <summary className="cursor-pointer text-muted">{t("media.naming.tokens")}</summary>
            <ul className="mt-1 grid gap-x-4 sm:grid-cols-2">
              {(p?.tokens ?? []).map((x) => <li key={x.name} className="font-mono text-faint"><span className="text-fg">{`{${x.name}}`}</span> {x.example}</li>)}
            </ul>
          </details>
        </div>
        <h3 className="pt-2 text-[13px] font-semibold text-fg">{t("musicNaming.importTitle")}</h3>
        <Checkbox label={t("musicNaming.partial")} description={t("musicNaming.partialHint")} checked={s.importPartialAlbums}
          onChange={(e) => set("importPartialAlbums", e.target.checked)} />
        <Checkbox label={t("musicNaming.cover")} description={t("musicNaming.coverHint")} checked={s.saveCoverArt} onChange={(e) => set("saveCoverArt", e.target.checked)} />
        <Checkbox label={t("musicNaming.writeTags")} description={t("musicNaming.writeTagsHint")} checked={s.writeTags} onChange={(e) => set("writeTags", e.target.checked)} />
        <TextInput label={t("musicNaming.minScore")} hint={t("musicNaming.minScoreHint")} type="number" min={40} max={100} value={s.minimumMatchScore}
          onChange={(e) => set("minimumMatchScore", Number(e.target.value))} />
        <TextInput label={t("musicNaming.contact")} hint={t("musicNaming.contactHint")} value={s.musicBrainzContact}
          onChange={(e) => set("musicBrainzContact", e.target.value)} />
        <div className="flex items-center gap-2">
          <Button variant="primary" onClick={() => save.mutate(s)} busy={save.isPending} data-testid="save-music-settings">{t("common.save")}</Button>
          {save.isSuccess ? <span role="status" className="text-[13px] text-success">{t("common.saved")}</span> : null}
        </div>
        <ErrorAlert error={save.error} />
      </div>
    </Panel>
  );
}
