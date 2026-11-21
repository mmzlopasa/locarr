import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type SeriesNamingSettings } from "@/api/client";
import { Button, Checkbox, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";

const STYLES = ["prefixed_range", "range", "extend", "duplicate", "repeat", "scene"] as const;

type TemplateField = "seriesFolderFormat" | "seasonFolderFormat" | "specialsFolderFormat" | "standardEpisodeFormat" | "dailyEpisodeFormat" | "animeEpisodeFormat";
const FIELDS: TemplateField[] = ["seriesFolderFormat", "seasonFolderFormat", "specialsFolderFormat", "standardEpisodeFormat", "dailyEpisodeFormat", "animeEpisodeFormat"];

function useDebounced<T>(v: T, ms: number): T {
  const [d, setD] = useState(v);
  useEffect(() => {
    const id = setTimeout(() => setD(v), ms);
    return () => clearTimeout(id);
  }, [v, ms]);
  return d;
}

/** Series folder, season folder and episode file templates with a live preview (standard, multi-episode, daily, anime). */
export function SeriesNamingPanel({ colon }: { colon?: SeriesNamingSettings["colonReplacement"] }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const current = useQuery({ queryKey: queryKeys.seriesNaming, queryFn: async () => unwrap(await api.GET("/settings/mediamanagement/series")) });
  const [s, setS] = useState<SeriesNamingSettings | null>(null);
  useEffect(() => {
    if (current.data && !s) setS(current.data);
  }, [current.data, s]);
  const save = useMutation({
    mutationFn: async (v: SeriesNamingSettings) => unwrap(await api.PUT("/settings/mediamanagement/series", { body: v })),
    onSuccess: (v) => {
      setS(v);
      qc.setQueryData(queryKeys.seriesNaming, v);
    },
  });
  const debounced = useDebounced(s ? { ...s, ...(colon ? { colonReplacement: colon } : {}) } : null, 250);
  const preview = useQuery({
    queryKey: ["series-naming-preview", debounced],
    queryFn: async () => unwrap(await api.POST("/settings/mediamanagement/series/preview", { body: debounced! })),
    enabled: !!debounced,
    placeholderData: (p) => p,
  });
  if (!s) return current.error ? <ErrorAlert error={current.error} /> : <Spinner />;
  const set = <K extends keyof SeriesNamingSettings>(k: K, v: SeriesNamingSettings[K]) => {
    setS({ ...s, [k]: v });
    save.reset();
  };
  const p = preview.data;
  const errors = p?.errors ?? {};
  const rows: [string, string | undefined][] = p ? [
    [t("seriesNaming.examples.folder"), `${p.seriesFolder}/${p.seasonFolder}/`],
    [t("seriesNaming.examples.standard"), p.standard],
    [t("seriesNaming.examples.multi"), p.multiEpisode],
    [t("seriesNaming.examples.daily"), p.daily],
    [t("seriesNaming.examples.anime"), p.anime],
    [t("seriesNaming.examples.specials"), `${p.seriesFolder}/${p.specialsFolder}/`],
  ] : [];
  return (
    <Panel title={t("seriesNaming.title")} description={t("seriesNaming.description")}>
      <div className="space-y-3">
        <Checkbox label={t("seriesNaming.rename")} description={t("media.naming.renameHint")} checked={s.renameEpisodes}
          onChange={(e) => set("renameEpisodes", e.target.checked)} />
        <div className="grid gap-3 lg:grid-cols-2">
          {FIELDS.map((f) => (
            <TextInput key={f} label={t(`seriesNaming.fields.${f}`)} value={s[f]} onChange={(e) => set(f, e.target.value)} className="font-mono"
              data-testid={`series-${f}`} hint={errors[f] ? <span className="text-danger">{errors[f]}</span> : undefined} />
          ))}
        </div>
        <div className="grid gap-3 sm:grid-cols-3">
          <SelectInput label={t("seriesNaming.multiStyle")} value={s.multiEpisodeStyle} onChange={(e) => set("multiEpisodeStyle", e.target.value as SeriesNamingSettings["multiEpisodeStyle"])}>
            {STYLES.map((m) => <option key={m} value={m}>{t(`seriesNaming.styles.${m}`)}</option>)}
          </SelectInput>
          <TextInput label={t("seriesNaming.packThreshold")} hint={t("seriesNaming.packThresholdHint")} type="number" min={2} max={100}
            value={s.seasonPackThreshold} onChange={(e) => set("seasonPackThreshold", Math.max(2, Number(e.target.value) || 2))} />
          <Checkbox label={t("seriesNaming.searchOnAdd")} checked={s.searchOnAddDefault} onChange={(e) => set("searchOnAddDefault", e.target.checked)} />
        </div>
        <div className="rounded-ui border border-line bg-surface-2 p-3" aria-live="polite" data-testid="series-naming-preview">
          <div className="mb-1 text-[12px] font-medium text-muted">{t("media.naming.preview")}</div>
          <dl className="space-y-1 text-[12px]">
            {rows.map(([label, value]) => (
              <div key={label} className="grid gap-1 sm:grid-cols-[140px_1fr]">
                <dt className="text-faint">{label}</dt>
                <dd className="break-all font-mono text-fg">{value || "–"}</dd>
              </div>
            ))}
          </dl>
        </div>
        <details className="text-[12px] text-muted">
          <summary className="cursor-pointer">{t("media.naming.tokens")}</summary>
          <p className="mt-1">{t("media.naming.tokensHint")}</p>
          <ul className="mt-2 grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
            {(p?.tokens ?? []).map((tk) => (
              <li key={tk.name}><code className="text-fg">{`{${tk.name}}`}</code> <span className="text-faint">{tk.example}</span></li>
            ))}
          </ul>
        </details>
        <div className="flex items-center gap-2">
          <Button variant="primary" onClick={() => save.mutate(s)} busy={save.isPending} disabled={Object.keys(errors).length > 0} data-testid="series-naming-save">
            {t("common.save")}
          </Button>
          {save.isSuccess ? <span className="text-[12px] text-success" role="status">{t("common.saved")}</span> : null}
          <ErrorAlert error={save.error} />
        </div>
      </div>
    </Panel>
  );
}
