import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp, Plus } from "lucide-react";
import { api, queryKeys, unwrap, type DelayProfile, type DelayProfileInput, type ReleaseProfile, type ReleaseProfileInput } from "@/api/client";
import { Badge, Button, Checkbox, ConfirmButton, EmptyState, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { ApiErrorDetail, Chip } from "./common";
import { useDelayProfiles, useReleaseProfiles } from "./hooks";
import { moveItem, parseTerms, termError } from "./logic";

const tagsText = (tags: string[]) => tags.join(", ");
const parseTags = (s: string) => s.split(",").map((x) => x.trim()).filter(Boolean);

export function ReleaseRulesPage() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("rules.title")} description={t("rules.subtitle")} />
      <ReleaseProfiles />
      <DelayProfiles />
    </div>
  );
}

function ReleaseProfiles() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useReleaseProfiles();
  const [editing, setEditing] = useState<(ReleaseProfileInput & { id?: number }) | null>(null);
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/releaseprofiles/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.releaseProfiles }),
  });
  return (
    <Panel title={t("rules.release.title")} description={t("rules.release.subtitle")}
      actions={
        <Button onClick={() => setEditing({ name: "", enabled: true, required: [], ignored: [], tags: [], indexerId: 0 })}>
          <Plus className="size-4" />
          {t("rules.release.add")}
        </Button>
      }>
      {editing ? <ReleaseProfileForm initial={editing} onDone={() => setEditing(null)} /> : null}
      {list.isPending ? <Spinner /> : null}
      <ApiErrorDetail error={list.error ?? remove.error} />
      {list.data?.length === 0 && !editing ? <EmptyState>{t("rules.release.none")}</EmptyState> : null}
      <ul className="flex flex-col divide-y divide-line">
        {list.data?.map((r: ReleaseProfile) => (
          <li key={r.id} className="flex flex-wrap items-center gap-3 py-2" data-testid={`release-${r.name}`}>
            <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setEditing(r)}>
              <span className="flex items-center gap-2 text-[13px] font-medium text-fg">
                {r.name}
                {!r.enabled ? <Badge>{t("rules.disabled")}</Badge> : null}
              </span>
              <span className="flex flex-wrap gap-1 pt-0.5">
                {r.required.map((x) => <Chip key={`r-${x}`} tone="accent">+ {x}</Chip>)}
                {r.ignored.map((x) => <Chip key={`i-${x}`} tone="danger">− {x}</Chip>)}
                {r.tags.map((x) => <Chip key={`t-${x}`} tone="info">#{x}</Chip>)}
              </span>
            </button>
            <ConfirmButton label={t("common.delete")} confirmLabel={t("rules.deleteConfirm", { name: r.name })} onConfirm={() => remove.mutate(r.id)} />
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function ReleaseProfileForm({ initial, onDone }: { initial: ReleaseProfileInput & { id?: number }; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [name, setName] = useState(initial.name);
  const [enabled, setEnabled] = useState(initial.enabled);
  const [required, setRequired] = useState(initial.required.join("\n"));
  const [ignored, setIgnored] = useState(initial.ignored.join("\n"));
  const [tags, setTags] = useState(tagsText(initial.tags ?? []));
  const bad = [...parseTerms(required), ...parseTerms(ignored)].map((x) => [x, termError(x)] as const).filter(([, e]) => e);
  const save = useMutation({
    mutationFn: async () => {
      const body: ReleaseProfileInput = { name, enabled, required: parseTerms(required), ignored: parseTerms(ignored), tags: parseTags(tags),
        indexerId: initial.indexerId ?? 0 };
      return initial.id
        ? unwrap(await api.PUT("/releaseprofiles/{id}", { params: { path: { id: initial.id } }, body }))
        : unwrap(await api.POST("/releaseprofiles", { body }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.releaseProfiles });
      onDone();
    },
  });
  const area = "w-full rounded-ui border border-line-strong bg-surface-2 p-2 font-mono text-[12px] text-fg focus:border-info focus:outline-none";
  return (
    <form className="mb-4 flex flex-col gap-3 rounded-ui border border-line-strong bg-surface-2/50 p-3" data-testid="release-form"
      onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-56 flex-1"><TextInput label={t("rules.name")} value={name} onChange={(e) => setName(e.target.value)} required /></div>
        <div className="min-w-56 flex-1"><TextInput label={t("rules.tags")} value={tags} onChange={(e) => setTags(e.target.value)} hint={t("rules.tagsHint")} /></div>
        <Checkbox label={t("rules.enabled")} checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <label className="flex flex-col gap-1 text-[12px] text-muted">
          {t("rules.release.required")}
          <textarea rows={4} value={required} onChange={(e) => setRequired(e.target.value)} className={area} aria-label={t("rules.release.required")} />
          <span className="text-faint">{t("rules.release.requiredHint")}</span>
        </label>
        <label className="flex flex-col gap-1 text-[12px] text-muted">
          {t("rules.release.ignored")}
          <textarea rows={4} value={ignored} onChange={(e) => setIgnored(e.target.value)} className={area} aria-label={t("rules.release.ignored")} />
          <span className="text-faint">{t("rules.release.ignoredHint")}</span>
        </label>
      </div>
      {bad.map(([x, e]) => <p key={x} className="text-[12px] text-danger">{t("rules.badRegex", { term: x, error: e })}</p>)}
      <ApiErrorDetail error={save.error} />
      <div className="flex justify-end gap-2">
        <Button onClick={onDone}>{t("common.cancel")}</Button>
        <Button type="submit" variant="primary" busy={save.isPending} disabled={!name.trim() || bad.length > 0}>{t("common.save")}</Button>
      </div>
    </form>
  );
}

function DelayProfiles() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useDelayProfiles();
  const [editing, setEditing] = useState<(DelayProfileInput & { id?: number; default?: boolean }) | null>(null);
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/delayprofiles/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.delayProfiles }),
  });
  const reorder = useMutation({
    mutationFn: async (ids: number[]) => unwrap(await api.PUT("/delayprofiles/order", { body: { ids } })),
    onSuccess: (data) => qc.setQueryData(queryKeys.delayProfiles, data),
  });
  const ordered = (list.data ?? []).filter((d) => !d.default);
  const move = (from: number, to: number) => reorder.mutate(moveItem(ordered, from, to).map((d) => d.id));
  const describe = (d: DelayProfile) => {
    const parts = [t("rules.delay.prefers", { protocol: t(`profiles.protocols.${d.preferredProtocol}`) })];
    if (d.usenetEnabled) parts.push(t("rules.delay.usenetWait", { n: d.usenetDelay }));
    else parts.push(t("rules.delay.usenetOff"));
    if (d.torrentEnabled) parts.push(t("rules.delay.torrentWait", { n: d.torrentDelay }));
    else parts.push(t("rules.delay.torrentOff"));
    return parts.join(" · ");
  };
  return (
    <Panel title={t("rules.delay.title")} description={t("rules.delay.subtitle")}
      actions={
        <Button onClick={() => setEditing({ name: "", enabled: true, preferredProtocol: "usenet", usenetEnabled: true, torrentEnabled: true,
          usenetDelay: 0, torrentDelay: 0, bypassIfHighestQuality: true, bypassIfAboveScore: false, minimumScore: 0, tags: [] })}>
          <Plus className="size-4" />
          {t("rules.delay.add")}
        </Button>
      }>
      {editing ? <DelayProfileForm initial={editing} onDone={() => setEditing(null)} /> : null}
      {list.isPending ? <Spinner /> : null}
      <ApiErrorDetail error={list.error ?? remove.error ?? reorder.error} />
      <ol className="flex flex-col divide-y divide-line">
        {(list.data ?? []).map((d) => {
          const i = ordered.findIndex((x) => x.id === d.id);
          return (
            <li key={d.id} className="flex flex-wrap items-center gap-3 py-2" data-testid={`delay-${d.name}`}>
              <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setEditing(d)}>
                <span className="flex items-center gap-2 text-[13px] font-medium text-fg">
                  {d.name}
                  {d.default ? <Badge tone="info">{t("rules.delay.default")}</Badge> : null}
                  {!d.enabled ? <Badge>{t("rules.disabled")}</Badge> : null}
                </span>
                <span className="block text-[12px] text-muted">{describe(d)}</span>
                <span className="flex flex-wrap gap-1 pt-0.5">{d.tags.map((x) => <Chip key={x} tone="info">#{x}</Chip>)}</span>
              </button>
              {!d.default ? (
                <span className="flex items-center gap-1">
                  <Button variant="ghost" aria-label={t("profiles.up")} disabled={i === 0} onClick={() => move(i, i - 1)}><ArrowUp className="size-4" /></Button>
                  <Button variant="ghost" aria-label={t("profiles.down")} disabled={i === ordered.length - 1} onClick={() => move(i, i + 1)}><ArrowDown className="size-4" /></Button>
                  <ConfirmButton label={t("common.delete")} confirmLabel={t("rules.deleteConfirm", { name: d.name })} onConfirm={() => remove.mutate(d.id)} />
                </span>
              ) : null}
            </li>
          );
        })}
      </ol>
    </Panel>
  );
}

function DelayProfileForm({ initial, onDone }: { initial: DelayProfileInput & { id?: number; default?: boolean }; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [d, setD] = useState(initial);
  const [tags, setTags] = useState(tagsText(initial.tags ?? []));
  const save = useMutation({
    mutationFn: async () => {
      const body: DelayProfileInput = { name: d.name, enabled: d.enabled, preferredProtocol: d.preferredProtocol, usenetEnabled: d.usenetEnabled,
        torrentEnabled: d.torrentEnabled, usenetDelay: d.usenetDelay, torrentDelay: d.torrentDelay, bypassIfHighestQuality: d.bypassIfHighestQuality,
        bypassIfAboveScore: d.bypassIfAboveScore, minimumScore: d.minimumScore, tags: parseTags(tags) };
      return d.id
        ? unwrap(await api.PUT("/delayprofiles/{id}", { params: { path: { id: d.id } }, body }))
        : unwrap(await api.POST("/delayprofiles", { body }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.delayProfiles });
      onDone();
    },
  });
  return (
    <form className="mb-4 flex flex-col gap-3 rounded-ui border border-line-strong bg-surface-2/50 p-3" data-testid="delay-form"
      onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <TextInput label={t("rules.name")} value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} required />
        <SelectInput label={t("rules.delay.preferred")} value={d.preferredProtocol}
          onChange={(e) => setD({ ...d, preferredProtocol: e.target.value as "usenet" | "torrent" })}>
          <option value="usenet">{t("profiles.protocols.usenet")}</option>
          <option value="torrent">{t("profiles.protocols.torrent")}</option>
        </SelectInput>
        {d.default ? <p className="self-end text-[12px] text-faint">{t("rules.delay.defaultHint")}</p> : (
          <TextInput label={t("rules.tags")} value={tags} onChange={(e) => setTags(e.target.value)} hint={t("rules.delay.tagsHint")} />
        )}
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <div className="flex flex-col gap-2 rounded-ui border border-line p-2.5">
          <Checkbox label={t("rules.delay.usenet")} checked={d.usenetEnabled} onChange={(e) => setD({ ...d, usenetEnabled: e.target.checked })} />
          <TextInput label={t("rules.delay.delayMinutes")} type="number" min={0} value={d.usenetDelay} disabled={!d.usenetEnabled}
            onChange={(e) => setD({ ...d, usenetDelay: Number(e.target.value) })} />
        </div>
        <div className="flex flex-col gap-2 rounded-ui border border-line p-2.5">
          <Checkbox label={t("rules.delay.torrent")} checked={d.torrentEnabled} onChange={(e) => setD({ ...d, torrentEnabled: e.target.checked })} />
          <TextInput label={t("rules.delay.delayMinutes")} type="number" min={0} value={d.torrentDelay} disabled={!d.torrentEnabled}
            onChange={(e) => setD({ ...d, torrentDelay: Number(e.target.value) })} />
        </div>
      </div>
      <div className="flex flex-wrap items-end gap-4">
        <Checkbox label={t("rules.delay.bypassQuality")} checked={!!d.bypassIfHighestQuality} onChange={(e) => setD({ ...d, bypassIfHighestQuality: e.target.checked })} />
        <Checkbox label={t("rules.delay.bypassScore")} checked={!!d.bypassIfAboveScore} onChange={(e) => setD({ ...d, bypassIfAboveScore: e.target.checked })} />
        <div className="w-32">
          <TextInput label={t("rules.delay.minimumScore")} type="number" value={d.minimumScore ?? 0} disabled={!d.bypassIfAboveScore}
            onChange={(e) => setD({ ...d, minimumScore: Number(e.target.value) })} />
        </div>
        <Checkbox label={t("rules.enabled")} checked={d.enabled} onChange={(e) => setD({ ...d, enabled: e.target.checked })} />
      </div>
      <ApiErrorDetail error={save.error} />
      <div className="flex justify-end gap-2">
        <Button onClick={onDone}>{t("common.cancel")}</Button>
        <Button type="submit" variant="primary" busy={save.isPending} disabled={!d.name.trim()}>{t("common.save")}</Button>
      </div>
    </form>
  );
}
