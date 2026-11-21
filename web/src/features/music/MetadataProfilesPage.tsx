import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Button, ErrorAlert, Panel, Spinner, TextInput } from "@/components/ui";
import { PRIMARY_TYPES, RELEASE_STATUSES, SECONDARY_TYPES, musicKeys, typeKey, useMetadataProfiles, type MetadataProfile,
  type MetadataProfileInput } from "./common";

type Draft = { id?: number; name: string; primaryTypes: string[]; secondaryTypes: string[]; releaseStatuses: string[] };

const empty: Draft = { name: "", primaryTypes: ["album"], secondaryTypes: ["studio"], releaseStatuses: ["official"] };

function Choices({ legend, options, value, onChange, labelKey }:
  { legend: string; options: readonly string[]; value: string[]; onChange: (v: string[]) => void; labelKey: string }) {
  const { t } = useTranslation();
  return (
    <fieldset className="mb-3">
      <legend className="mb-1 text-[12px] font-medium text-muted">{legend}</legend>
      <div className="flex flex-wrap gap-1.5">
        {options.map((o) => {
          const on = value.includes(o);
          return (
            <label key={o} className={`inline-flex cursor-pointer items-center gap-1.5 rounded-ui border px-2 py-1 text-[12px] ${on ? "border-accent/60 bg-surface-3 text-fg" : "border-line text-muted"}`}>
              <input type="checkbox" className="size-3.5" checked={on} onChange={(e) => onChange(e.target.checked ? [...value, o] : value.filter((x) => x !== o))} />
              {t(`${labelKey}.${typeKey(o)}`, { defaultValue: o })}
            </label>
          );
        })}
      </div>
    </fieldset>
  );
}

/** Settings → Metadata profiles: which albums of an artist the library holds. */
export function MetadataProfilesPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useMetadataProfiles();
  const [draft, setDraft] = useState<Draft | null>(null);
  const save = useMutation({
    mutationFn: async (d: Draft) => {
      const body = { name: d.name, primaryTypes: d.primaryTypes, secondaryTypes: d.secondaryTypes, releaseStatuses: d.releaseStatuses } as MetadataProfileInput;
      return unwrap(d.id ? await api.PUT("/music/metadataprofiles/{id}", { params: { path: { id: d.id } }, body })
        : await api.POST("/music/metadataprofiles", { body }));
    },
    onSuccess: () => {
      setDraft(null);
      void qc.invalidateQueries({ queryKey: musicKeys.profiles });
    },
  });
  const del = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/music/metadataprofiles/{id}", { params: { path: { id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: musicKeys.profiles }),
  });
  const edit = (p: MetadataProfile) => setDraft({ id: p.id, name: p.name, primaryTypes: p.primaryTypes, secondaryTypes: p.secondaryTypes, releaseStatuses: p.releaseStatuses });
  return (
    <div className="space-y-4">
      <Panel title={t("metadataProfiles.title")} description={t("metadataProfiles.description")}
        actions={<Button onClick={() => setDraft({ ...empty })} data-testid="new-metadata-profile"><Plus className="size-3.5" /> {t("metadataProfiles.new")}</Button>}>
        {list.isLoading ? <Spinner /> : null}
        <ErrorAlert error={list.error ?? del.error} />
        <ul className="divide-y divide-line">
          {(list.data ?? []).map((p) => (
            <li key={p.id} className="flex flex-wrap items-center justify-between gap-2 py-2" data-testid="metadata-profile">
              <div className="min-w-0">
                <div className="text-[14px] font-medium text-fg">{p.name}</div>
                <div className="text-[12px] text-muted">
                  {p.primaryTypes.map((x) => t(`music.types.${typeKey(x)}`, { defaultValue: x })).join(", ")} ·{" "}
                  {p.secondaryTypes.map((x) => t(`music.types.${typeKey(x)}`, { defaultValue: x })).join(", ")} ·{" "}
                  {p.releaseStatuses.map((x) => t(`music.statuses.${typeKey(x)}`, { defaultValue: x })).join(", ")}
                </div>
                <div className="text-[11px] text-faint">{t("metadataProfiles.used", { count: p.artists })}</div>
              </div>
              <div className="flex gap-1">
                <Button variant="ghost" onClick={() => edit(p)}>{t("movie.edit")}</Button>
                <Button variant="ghost" onClick={() => del.mutate(p.id)} disabled={p.artists > 0} title={p.artists > 0 ? t("metadataProfiles.inUse") : undefined}
                  aria-label={t("metadataProfiles.delete", { name: p.name })}><Trash2 className="size-3.5" /></Button>
              </div>
            </li>
          ))}
        </ul>
      </Panel>
      {draft ? (
        <Panel title={draft.id ? t("metadataProfiles.edit") : t("metadataProfiles.new")} description={t("metadataProfiles.studioHint")}>
          <div className="mb-3 max-w-sm">
            <TextInput label={t("metadataProfiles.name")} value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} data-testid="profile-name" />
          </div>
          <Choices legend={t("metadataProfiles.primary")} options={PRIMARY_TYPES} value={draft.primaryTypes} labelKey="music.types"
            onChange={(v) => setDraft({ ...draft, primaryTypes: v })} />
          <Choices legend={t("metadataProfiles.secondary")} options={SECONDARY_TYPES} value={draft.secondaryTypes} labelKey="music.types"
            onChange={(v) => setDraft({ ...draft, secondaryTypes: v })} />
          <Choices legend={t("metadataProfiles.statuses")} options={RELEASE_STATUSES} value={draft.releaseStatuses} labelKey="music.statuses"
            onChange={(v) => setDraft({ ...draft, releaseStatuses: v })} />
          <div className="flex gap-2">
            <Button variant="primary" onClick={() => save.mutate(draft)} busy={save.isPending} disabled={!draft.name.trim()} data-testid="save-metadata-profile">
              {t("common.save")}
            </Button>
            <Button variant="ghost" onClick={() => setDraft(null)}>{t("common.cancel")}</Button>
          </div>
          <ErrorAlert error={save.error} />
        </Panel>
      ) : null}
    </div>
  );
}
