import { useState, type DragEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp, GripVertical, Group, Plus, Ungroup } from "lucide-react";
import { api, queryKeys, unwrap, type MediaKind, type QualityProfile, type QualityProfileInput, type QualityProfileItem } from "@/api/client";
import { Badge, Button, Checkbox, ConfirmButton, EmptyState, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { ApiErrorDetail, Chip } from "./common";
import { useCustomFormats, useLanguages, useQualities, useQualityNames, useQualityProfiles } from "./hooks";
import { blankItems, cutoffAfterChange, fixCutoff, groupWithNext, itemKey, itemMembers, moveItem, ungroup } from "./logic";

const KINDS: MediaKind[] = ["movies", "series", "music"];

export function QualityProfilesPage() {
  const { t } = useTranslation();
  const profiles = useQualityProfiles();
  const qualities = useQualities();
  const names = useQualityNames();
  const [editing, setEditing] = useState<QualityProfileInput & { id?: number } | null>(null);
  const [newKind, setNewKind] = useState<MediaKind>("movies");

  const startNew = () => {
    const items = blankItems(qualities.data ?? [], newKind);
    setEditing({ name: "", kind: newKind, items, cutoff: items[0] ? itemKey(items[0]) : "", upgradesAllowed: true,
      minFormatScore: 0, cutoffFormatScore: 0, formatScores: [], languages: [] });
  };

  if (editing) return <ProfileEditor initial={editing} onDone={() => setEditing(null)} />;
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("profiles.title")} description={t("profiles.subtitle")}
        actions={
          <div className="flex items-end gap-2">
            <div className="w-36">
              <SelectInput label={t("profiles.kind")} value={newKind} onChange={(e) => setNewKind(e.target.value as MediaKind)}>
                {KINDS.map((k) => <option key={k} value={k}>{t(`mediaKinds.${k}`)}</option>)}
              </SelectInput>
            </div>
            <Button variant="primary" onClick={startNew} disabled={!qualities.data}>
              <Plus className="size-4" />
              {t("profiles.add")}
            </Button>
          </div>
        } />
      {profiles.isPending ? <Spinner /> : null}
      <ApiErrorDetail error={profiles.error} />
      {KINDS.map((k) => {
        const list = (profiles.data ?? []).filter((p) => p.kind === k);
        return (
          <Panel key={k} title={t(`mediaKinds.${k}`)}>
            {list.length === 0 ? <EmptyState>{t("profiles.none")}</EmptyState> : null}
            <ul className="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-3">
              {list.map((p) => (
                <li key={p.id}>
                  <button type="button" onClick={() => setEditing(p)} data-testid={`profile-${p.kind}-${p.name}`}
                    className="flex w-full flex-col gap-1.5 rounded-ui border border-line bg-surface-2 p-3 text-left transition-colors hover:border-line-strong">
                    <span className="text-[14px] font-medium text-fg">{p.name}</span>
                    <span className="text-[12px] text-muted">{t("profiles.cutoffIs", { name: cutoffLabel(p, names) })}</span>
                    <span className="flex flex-wrap gap-1">
                      {p.items.filter((i) => i.allowed).slice(0, 6).map((i) => <Chip key={itemKey(i)}>{i.quality ? names.get(i.quality) : i.name}</Chip>)}
                      {p.items.filter((i) => i.allowed).length > 6 ? <Chip>+{p.items.filter((i) => i.allowed).length - 6}</Chip> : null}
                    </span>
                    <span className="flex gap-2 text-[11px] text-faint">
                      {p.upgradesAllowed ? <span>{t("profiles.upgrades")}</span> : null}
                      {p.formatScores.length ? <span>{t("profiles.scoredFormats", { n: p.formatScores.length })}</span> : null}
                      {p.languages.length ? <span>{p.languages.join(", ")}</span> : null}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </Panel>
        );
      })}
    </div>
  );
}

function cutoffLabel(p: Pick<QualityProfile, "items" | "cutoff">, names: Map<string, string>): string {
  const it = p.items.find((i) => itemKey(i) === p.cutoff);
  if (!it) return p.cutoff;
  return it.quality ? (names.get(it.quality) ?? it.quality) : (it.name ?? "");
}

function ProfileEditor({ initial, onDone }: { initial: QualityProfileInput & { id?: number }; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const names = useQualityNames();
  const formats = useCustomFormats();
  const langs = useLanguages();
  const [p, setP] = useState(initial);
  const [dragFrom, setDragFrom] = useState<number | null>(null);
  const [addLang, setAddLang] = useState("");

  const done = () => {
    void qc.invalidateQueries({ queryKey: queryKeys.qualityProfiles });
    onDone();
  };
  const save = useMutation({
    mutationFn: async () => {
      const body: QualityProfileInput = { name: p.name, kind: p.kind, items: p.items, cutoff: p.cutoff, upgradesAllowed: p.upgradesAllowed,
        minFormatScore: p.minFormatScore ?? 0, cutoffFormatScore: p.cutoffFormatScore ?? 0,
        formatScores: (p.formatScores ?? []).filter((f) => f.score !== 0), languages: p.languages ?? [] };
      return p.id
        ? unwrap(await api.PUT("/qualityprofiles/{id}", { params: { path: { id: p.id } }, body }))
        : unwrap(await api.POST("/qualityprofiles", { body }));
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/qualityprofiles/{id}", { params: { path: { id: p.id ?? 0 } } })),
    onSuccess: done,
  });

  const setItems = (items: QualityProfileItem[]) => setP({ ...p, items, cutoff: cutoffAfterChange(p.items, items, p.cutoff) });
  const onDrop = (e: DragEvent, to: number) => {
    e.preventDefault();
    if (dragFrom !== null) setItems(moveItem(p.items, dragFrom, to));
    setDragFrom(null);
  };
  const score = (id: number) => p.formatScores?.find((f) => f.formatId === id)?.score ?? 0;
  const setScore = (id: number, v: number) =>
    setP({ ...p, formatScores: [...(p.formatScores ?? []).filter((f) => f.formatId !== id), { formatId: id, score: v }] });
  const languages = p.languages ?? [];

  return (
    <form className="flex flex-col gap-4" data-testid="profile-editor" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
      <PageHeader title={p.id ? t("profiles.edit", { name: initial.name }) : t("profiles.new")}
        description={t(`mediaKinds.${p.kind}`)}
        actions={
          <>
            {p.id ? <ConfirmButton label={t("common.delete")} confirmLabel={t("profiles.deleteConfirm")} onConfirm={() => remove.mutate()} busy={remove.isPending} /> : null}
            <Button onClick={onDone}>{t("common.cancel")}</Button>
            <Button type="submit" variant="primary" busy={save.isPending} disabled={!p.name.trim()}>{t("common.save")}</Button>
          </>
        } />
      <ApiErrorDetail error={save.error ?? remove.error} />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[3fr_2fr]">
        <Panel title={t("profiles.qualities")} description={t("profiles.qualitiesHint")}>
          <ol className="flex flex-col gap-1" data-testid="profile-items">
            {p.items.map((it, i) => {
              const key = itemKey(it);
              const isGroup = !it.quality;
              const label = it.quality ? (names.get(it.quality) ?? it.quality) : (it.name ?? "");
              return (
                <li key={key} draggable onDragStart={() => setDragFrom(i)} onDragOver={(e) => e.preventDefault()} onDrop={(e) => onDrop(e, i)}
                  onDragEnd={() => setDragFrom(null)} data-testid={`item-${label}`}
                  className={`flex flex-wrap items-center gap-2 rounded-ui border px-2 py-1.5 transition-colors ${
                    dragFrom === i ? "border-info bg-info-bg" : it.allowed ? "border-line-strong bg-surface-2" : "border-line bg-surface"}`}>
                  <GripVertical className="size-4 cursor-grab text-faint" aria-hidden />
                  <input type="checkbox" checked={it.allowed} aria-label={t("profiles.allow", { name: label })}
                    onChange={(e) => {
                      const items = p.items.map((x, j) => (j === i ? { ...x, allowed: e.target.checked } : x));
                      setP({ ...p, items, cutoff: fixCutoff(items, p.cutoff) });
                    }} className="size-4 accent-[var(--lc-accent)]" />
                  {isGroup ? (
                    <input value={it.name ?? ""} aria-label={t("profiles.groupName")}
                      onChange={(e) => setItems(p.items.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
                      className="h-7 w-32 rounded border border-line-strong bg-surface-3 px-1.5 text-[13px] text-fg focus:border-info focus:outline-none" />
                  ) : (
                    <span className={`text-[13px] ${it.allowed ? "text-fg" : "text-muted"}`}>{label}</span>
                  )}
                  {isGroup ? (
                    <span className="flex flex-wrap gap-1">{itemMembers(it).map((m) => <Chip key={m}>{names.get(m) ?? m}</Chip>)}</span>
                  ) : null}
                  <span className="ml-auto flex items-center gap-0.5">
                    {it.allowed ? (
                      <label className={`mr-1 inline-flex cursor-pointer items-center gap-1 rounded px-1.5 py-0.5 text-[11px] ${
                        p.cutoff === key ? "bg-accent text-accent-ink" : "text-faint hover:text-fg"}`}>
                        <input type="radio" name="cutoff" className="sr-only" checked={p.cutoff === key} onChange={() => setP({ ...p, cutoff: key })} />
                        {t("profiles.cutoff")}
                      </label>
                    ) : null}
                    <IconButton label={t("profiles.up")} disabled={i === 0} onClick={() => setItems(moveItem(p.items, i, i - 1))}><ArrowUp className="size-3.5" /></IconButton>
                    <IconButton label={t("profiles.down")} disabled={i === p.items.length - 1} onClick={() => setItems(moveItem(p.items, i, i + 1))}><ArrowDown className="size-3.5" /></IconButton>
                    {isGroup ? (
                      <IconButton label={t("profiles.ungroup")} onClick={() => setItems(ungroup(p.items, i))}><Ungroup className="size-3.5" /></IconButton>
                    ) : (
                      <IconButton label={t("profiles.groupWithNext")} disabled={i === p.items.length - 1}
                        onClick={() => setItems(groupWithNext(p.items, i, t("profiles.newGroup", { name: label })))}><Group className="size-3.5" /></IconButton>
                    )}
                  </span>
                </li>
              );
            })}
          </ol>
        </Panel>
        <div className="flex flex-col gap-4">
          <Panel title={t("profiles.general")}>
            <div className="flex flex-col gap-3">
              <TextInput label={t("profiles.name")} value={p.name} onChange={(e) => setP({ ...p, name: e.target.value })} required maxLength={100} />
              <Checkbox label={t("profiles.upgradesAllowed")} description={t("profiles.upgradesHint")} checked={p.upgradesAllowed}
                onChange={(e) => setP({ ...p, upgradesAllowed: e.target.checked })} />
              <div className="grid grid-cols-2 gap-3">
                <TextInput label={t("profiles.minScore")} type="number" value={p.minFormatScore ?? 0} hint={t("profiles.minScoreHint")}
                  onChange={(e) => setP({ ...p, minFormatScore: Number(e.target.value) })} />
                <TextInput label={t("profiles.cutoffScore")} type="number" value={p.cutoffFormatScore ?? 0} hint={t("profiles.cutoffScoreHint")}
                  onChange={(e) => setP({ ...p, cutoffFormatScore: Number(e.target.value) })} />
              </div>
            </div>
          </Panel>
          <Panel title={t("profiles.languages")} description={t("profiles.languagesHint")}>
            <div className="mb-2 flex flex-wrap gap-1.5" data-testid="profile-languages">
              {languages.length === 0 ? <Badge tone="info">{t("profiles.anyLanguage")}</Badge> : null}
              {languages.map((l) => (
                <button key={l} type="button" onClick={() => setP({ ...p, languages: languages.filter((x) => x !== l) })}
                  className="inline-flex h-6 items-center gap-1 rounded border border-line-strong bg-surface-3 px-2 text-[12px] text-fg hover:border-danger/60"
                  aria-label={t("profiles.removeLanguage", { name: langName(t, l) })}>
                  {langName(t, l)} <span aria-hidden className="text-faint">×</span>
                </button>
              ))}
            </div>
            <div className="flex items-end gap-2">
              <div className="flex-1">
                <SelectInput label={t("profiles.addLanguage")} value={addLang} onChange={(e) => setAddLang(e.target.value)}>
                  <option value="">—</option>
                  <option value="original">{t("profiles.original")}</option>
                  {(langs.data ?? []).map((l) => <option key={l.code} value={l.code}>{t(`languageNames.${l.code}`, { defaultValue: l.name })}</option>)}
                </SelectInput>
              </div>
              <Button disabled={!addLang || languages.includes(addLang)} onClick={() => { setP({ ...p, languages: [...languages, addLang] }); setAddLang(""); }}>
                {t("common.add")}
              </Button>
            </div>
          </Panel>
          <Panel title={t("profiles.formatScores")} description={t("profiles.formatScoresHint")}>
            {(formats.data ?? []).length === 0 ? <EmptyState>{t("profiles.noFormats")}</EmptyState> : null}
            <ul className="flex flex-col divide-y divide-line">
              {(formats.data ?? []).map((f) => (
                <li key={f.id} className="flex items-center justify-between gap-2 py-1.5">
                  <span className="truncate text-[13px] text-fg">{f.name}</span>
                  <ScoreInput value={score(f.id)} label={t("profiles.scoreFor", { name: f.name })} onChange={(v) => setScore(f.id, v)} />
                </li>
              ))}
            </ul>
          </Panel>
        </div>
      </div>
    </form>
  );
}

function langName(t: TFunction, code: string) {
  if (code === "original") return t("profiles.original");
  if (code === "any") return t("profiles.anyLanguage");
  return t(`languageNames.${code}`, { defaultValue: code });
}

function IconButton({ label, children, disabled, onClick }: { label: string; children: React.ReactNode; disabled?: boolean; onClick: () => void }) {
  return (
    <button type="button" aria-label={label} title={label} disabled={disabled} onClick={onClick}
      className="inline-flex size-6 items-center justify-center rounded text-muted transition-colors hover:bg-surface-3 hover:text-fg disabled:opacity-30">
      {children}
    </button>
  );
}

/** A number field that lets "-" be typed before the digits. */
function ScoreInput({ value, label, onChange }: { value: number; label: string; onChange: (v: number) => void }) {
  const [text, setText] = useState(String(value));
  return (
    <input inputMode="numeric" value={text} aria-label={label}
      onChange={(e) => {
        const v = e.target.value.replace(/[^\d-]/g, "");
        setText(v);
        if (/^-?\d+$/.test(v)) onChange(Number(v));
        else if (v === "") onChange(0);
      }}
      className="h-7 w-24 rounded border border-line-strong bg-surface-2 px-1.5 text-right font-mono text-[12px] text-fg focus:border-info focus:outline-none" />
  );
}
