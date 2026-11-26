import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap, type Indexer } from "@/api/client";
import { Alert, Badge, Button, Checkbox, ConfirmButton, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { inputFromIndexer } from "@/features/indexers/BuiltinIndexers";
import { useQualityProfiles } from "@/features/movies/common";
import { useCurrentUser } from "@/features/auth/hooks";
import {
  adultKey, useAdultExclusions, useAdultRootFolders, useAdultSettings, useDeleteExclusion, useSaveAdultSettings, type AdultSettings,
} from "@/features/adult/api";
import { formatDateTime } from "@/components/format";

/** Settings → Adult: off by default; enabling needs an explicit confirmation. */
export function AdultSettingsPage() {
  const { t } = useTranslation();
  const settings = useAdultSettings();
  if (settings.isPending) return <Spinner />;
  if (!settings.data) return <ErrorAlert error={settings.error} />;
  return (
    <div className="space-y-4" data-testid="adult-settings">
      <PageHeader title={t("adultSettings.title")} description={t("adultSettings.description")} />
      <EnablePanel s={settings.data} />
      {settings.data.enabled ? (
        <>
          <KeysPanel s={settings.data} />
          <ProfilePanel s={settings.data} />
          <DefaultsPanel s={settings.data} />
          <RootsPanel />
          <IndexersPanel />
          <div id="tag-blocklist" className="scroll-mt-4"><BlocklistPanel s={settings.data} /></div>
          <ExclusionsPanel />
        </>
      ) : null}
    </div>
  );
}

function EnablePanel({ s }: { s: AdultSettings }) {
  const { t } = useTranslation();
  const save = useSaveAdultSettings();
  const [asking, setAsking] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  return (
    <Panel title={t("adultSettings.enable.title")} description={t("adultSettings.enable.hint")}>
      <div className="flex flex-wrap items-center gap-3">
        <Badge tone={s.enabled ? "success" : "neutral"}>{s.enabled ? t("adultSettings.enable.on") : t("adultSettings.enable.off")}</Badge>
        {s.enabled ? (
          <Button busy={save.isPending} onClick={() => save.mutate({ enabled: false })}>{t("adultSettings.enable.disable")}</Button>
        ) : (
          <Button variant="primary" onClick={() => setAsking(true)} data-testid="adult-enable">{t("adultSettings.enable.enable")}</Button>
        )}
      </div>
      {asking && !s.enabled ? (
        <div className="mt-3 space-y-3 rounded-ui border border-line-strong p-3" role="dialog" aria-label={t("adultSettings.enable.confirmTitle")}>
          <Alert tone="warning">{t("adultSettings.enable.warning")}</Alert>
          <Checkbox label={t("adultSettings.enable.confirm")} checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)}
            data-testid="adult-confirm" />
          <div className="flex gap-2">
            <Button variant="primary" disabled={!confirmed} busy={save.isPending} data-testid="adult-enable-confirm"
              onClick={() => save.mutate({ enabled: true, confirmAdults: true }, { onSuccess: () => setAsking(false) })}>
              {t("adultSettings.enable.enable")}
            </Button>
            <Button onClick={() => setAsking(false)}>{t("common.cancel")}</Button>
          </div>
        </div>
      ) : null}
      {s.confirmedAt ? <p className="mt-2 text-[12px] text-faint">{t("adultSettings.enable.confirmedAt", { at: s.confirmedAt })}</p> : null}
      <p className="mt-2 text-[12px] text-muted">{t("adultSettings.enable.permission")}</p>
      <ErrorAlert error={save.error} />
    </Panel>
  );
}

function KeysPanel({ s }: { s: AdultSettings }) {
  const { t } = useTranslation();
  const save = useSaveAdultSettings();
  const [stash, setStash] = useState("");
  const [tpdb, setTpdb] = useState("");
  return (
    <Panel title={t("adultSettings.keys.title")} description={t("adultSettings.keys.hint")}>
      <form className="grid gap-3 sm:grid-cols-2" onSubmit={(e) => {
        e.preventDefault();
        save.mutate({ ...(stash ? { stashdbKey: stash } : {}), ...(tpdb ? { tpdbKey: tpdb } : {}) }, { onSuccess: () => { setStash(""); setTpdb(""); } });
      }}>
        <TextInput label="StashDB" type="password" autoComplete="off" value={stash} onChange={(e) => setStash(e.target.value)}
          hint={s.stashdbKeySet ? t("adultSettings.keys.set") : t("adultSettings.keys.notSet")} />
        <TextInput label="ThePornDB" type="password" autoComplete="off" value={tpdb} onChange={(e) => setTpdb(e.target.value)}
          hint={s.tpdbKeySet ? t("adultSettings.keys.set") : t("adultSettings.keys.notSet")} />
        <div className="sm:col-span-2"><Button type="submit" variant="primary" busy={save.isPending} disabled={!stash && !tpdb}>{t("common.save")}</Button></div>
      </form>
      <ErrorAlert error={save.error} />
    </Panel>
  );
}

/**
 * The profile that allows every quality (D-109): new adult items use it;
 * items still on another profile are counted and can be moved in one step.
 */
function ProfilePanel({ s }: { s: AdultSettings }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const profiles = useQualityProfiles();
  const open = s.openProfileId;
  const name = (profiles.data ?? []).find((p) => p.id === open)?.name ?? "";
  const apply = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/items/quality-profile", { body: { profileId: open ?? 0 } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: adultKey }),
  });
  const u = s.profileUsage;
  const others = u ? u.items + u.studios + u.performers : 0;
  return (
    <Panel title={t("adultSettings.profile.title")} description={t("adultSettings.profile.hint", { profile: name || "Any (Unknown allowed)" })}>
      <div data-testid="adult-profile">
        {open === undefined ? <Alert tone="warning">{t("adultSettings.profile.missing")}</Alert> : null}
        {open !== undefined && u && others > 0 ? (
          <div className="space-y-2">
            <p className="text-[13px] text-fg" data-testid="adult-profile-usage">{t("adultSettings.profile.usage", { items: u.items, studios: u.studios, performers: u.performers })}</p>
            <div className="flex flex-wrap items-center gap-2">
              <ConfirmButton label={t("adultSettings.profile.apply", { profile: name })} confirmLabel={t("adultSettings.profile.applyConfirm")}
                busy={apply.isPending} onConfirm={() => apply.mutate()} testId="adult-profile-apply" />
              <span className="text-[12px] text-faint">{t("adultSettings.profile.applyNote")}</span>
            </div>
          </div>
        ) : null}
        {open !== undefined && u && others === 0 ? <p className="text-[13px] text-muted">{t("adultSettings.profile.allOpen", { profile: name })}</p> : null}
        {apply.data ? <p role="status" className="mt-2 text-[12px] text-muted">{t("adultSettings.profile.applied", { count: apply.data.updated })}</p> : null}
        <ErrorAlert error={apply.error} />
      </div>
    </Panel>
  );
}

function DefaultsPanel({ s }: { s: AdultSettings }) {
  const { t } = useTranslation();
  const save = useSaveAdultSettings();
  const profiles = useQualityProfiles();
  const roots = useAdultRootFolders();
  const [v, setV] = useState(s);
  useEffect(() => setV(s), [s]);
  return (
    <Panel title={t("adultSettings.defaults.title")} description={t("adultSettings.defaults.hint", { tokens: s.tokens.map((x) => `{${x}}`).join(" ") })}>
      <div className="grid gap-3 sm:grid-cols-2">
        <SelectInput label={t("adultSettings.defaults.source")} value={v.defaultSource} onChange={(e) => setV({ ...v, defaultSource: e.target.value as "stashdb" | "tpdb" })}>
          <option value="stashdb">StashDB</option>
          <option value="tpdb">ThePornDB</option>
        </SelectInput>
        <SelectInput label={t("adultSettings.defaults.profile")} value={v.defaultProfileId} onChange={(e) => setV({ ...v, defaultProfileId: Number(e.target.value) })}>
          <option value={0}>-</option>
          {(profiles.data ?? []).filter((p) => p.kind === "movies").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("adultSettings.defaults.root")} value={v.defaultRootFolderId} onChange={(e) => setV({ ...v, defaultRootFolderId: Number(e.target.value) })}>
          <option value={0}>-</option>
          {roots.data?.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
        </SelectInput>
        <span />
        <TextInput label={t("adultSettings.defaults.sceneFolder")} value={v.sceneFolder} onChange={(e) => setV({ ...v, sceneFolder: e.target.value })} />
        <TextInput label={t("adultSettings.defaults.sceneFile")} value={v.sceneFile} onChange={(e) => setV({ ...v, sceneFile: e.target.value })} />
        <TextInput label={t("adultSettings.defaults.movieFolder")} value={v.movieFolder} onChange={(e) => setV({ ...v, movieFolder: e.target.value })} />
        <TextInput label={t("adultSettings.defaults.movieFile")} value={v.movieFile} onChange={(e) => setV({ ...v, movieFile: e.target.value })} />
        <TextInput label={t("adultSettings.defaults.galleryFolder")} hint={t("adultSettings.defaults.galleryFolderHint")} value={v.galleryFolder}
          onChange={(e) => setV({ ...v, galleryFolder: e.target.value })} data-testid="adult-gallery-folder" />
        <div className="grid grid-cols-2 gap-3">
          <TextInput label={t("adultSettings.defaults.galleryMin")} type="number" min={0} value={v.galleryMinMB}
            onChange={(e) => setV({ ...v, galleryMinMB: Math.max(0, Number(e.target.value) || 0) })} />
          <TextInput label={t("adultSettings.defaults.galleryMax")} type="number" min={0} value={v.galleryMaxMB}
            onChange={(e) => setV({ ...v, galleryMaxMB: Math.max(0, Number(e.target.value) || 0) })} />
        </div>
        <Checkbox label={t("adultSettings.defaults.galleryRename")} description={t("adultSettings.defaults.galleryRenameHint")} checked={v.galleryRename}
          onChange={(e) => setV({ ...v, galleryRename: e.target.checked })} />
        <Checkbox label={t("adultSettings.defaults.galleryCover")} description={t("adultSettings.defaults.galleryCoverHint")} checked={v.galleryCover}
          onChange={(e) => setV({ ...v, galleryCover: e.target.checked })} />
        <Checkbox label={t("adultSettings.defaults.stats")} description={t("adultSettings.defaults.statsHint")} checked={v.statsAggregate}
          onChange={(e) => setV({ ...v, statsAggregate: e.target.checked })} />
        <Checkbox label={t("adultSettings.defaults.streamero")} description={t("adultSettings.defaults.streameroHint")} checked={v.notifyStreamero}
          onChange={(e) => setV({ ...v, notifyStreamero: e.target.checked })} />
      </div>
      <div className="mt-3">
        <Button variant="primary" busy={save.isPending} onClick={() => save.mutate({ defaultSource: v.defaultSource, defaultProfileId: v.defaultProfileId,
          defaultRootFolderId: v.defaultRootFolderId, sceneFolder: v.sceneFolder, sceneFile: v.sceneFile, movieFolder: v.movieFolder,
          movieFile: v.movieFile, galleryFolder: v.galleryFolder, galleryRename: v.galleryRename, galleryCover: v.galleryCover,
          galleryMinMB: v.galleryMinMB, galleryMaxMB: v.galleryMaxMB, statsAggregate: v.statsAggregate, notifyStreamero: v.notifyStreamero })}>{t("common.save")}</Button>
      </div>
      <ErrorAlert error={save.error} />
    </Panel>
  );
}

function RootsPanel() {
  const me = useCurrentUser();
  // Folders belong to the adult library itself, which even admins only reach
  // with the permission; say so instead of failing with a 404.
  return me.permissions.viewAdult ? <RootsEditor /> : <RootsNeedPermission />;
}

function RootsNeedPermission() {
  const { t } = useTranslation();
  return (
    <Panel title={t("adultSettings.roots.title")} description={t("adultSettings.roots.hint")}>
      <Alert tone="warning">{t("adultSettings.roots.needPermission")}</Alert>
    </Panel>
  );
}

function RootsEditor() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const roots = useAdultRootFolders();
  const [path, setPath] = useState("/data/media/adult");
  const add = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/rootfolders", { body: { path } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: adultKey }),
  });
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/adult/rootfolders/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: adultKey }),
  });
  return (
    <Panel title={t("adultSettings.roots.title")} description={t("adultSettings.roots.hint")}>
      <ul className="mb-3 space-y-1 text-[13px]">
        {roots.data?.map((r) => (
          <li key={r.id} className="flex items-center justify-between gap-2">
            <span className="break-all">{r.path}</span>
            <ConfirmButton label={t("common.remove")} confirmLabel={t("common.yes")} onConfirm={() => remove.mutate(r.id)} />
          </li>
        ))}
      </ul>
      <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); add.mutate(); }}>
        <div className="min-w-64 flex-1"><TextInput label={t("adultSettings.roots.path")} value={path} onChange={(e) => setPath(e.target.value)} /></div>
        <Button type="submit" busy={add.isPending}>{t("adultSettings.roots.add")}</Button>
      </form>
      <ErrorAlert error={add.error ?? remove.error} />
    </Panel>
  );
}

function IndexersPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["indexers"], queryFn: async () => unwrap(await api.GET("/indexers")) });
  const toggle = useMutation({
    mutationFn: async ({ i, on }: { i: Indexer; on: boolean }) => unwrap(await api.PUT("/indexers/{id}", { params: { path: { id: i.id } },
      body: inputFromIndexer(i, { categories: { ...i.categories, adult: on ? [6000] : [] } }) })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["indexers"] }),
  });
  const on = (i: Indexer) => (i.categories.adult ?? []).length > 0;
  // Indexers without adult categories are never asked; one still ticked from
  // before can only be unticked.
  const capable = list.data?.filter((i) => i.adultCapable !== false) ?? [];
  const other = list.data?.filter((i) => i.adultCapable === false) ?? [];
  return (
    <Panel title={t("adultSettings.indexers.title")} description={t("adultSettings.indexers.hint")}>
      <ul className="space-y-1.5" data-testid="adult-indexers">
        {capable.map((i) => (
          <li key={i.id}>
            <Checkbox label={i.name} checked={on(i)} onChange={(e) => toggle.mutate({ i, on: e.target.checked })} />
          </li>
        ))}
      </ul>
      {other.length ? (
        <div className="mt-3" data-testid="adult-indexers-unsupported">
          <p className="text-[12px] font-medium text-muted">{t("adultSettings.indexers.noAdult")}</p>
          <p className="mb-1.5 text-[12px] text-faint">{t("adultSettings.indexers.noAdultHint")}</p>
          <ul className="space-y-1.5">
            {other.map((i) => (
              <li key={i.id}>
                {on(i)
                  ? <Checkbox label={i.name} checked onChange={() => toggle.mutate({ i, on: false })} />
                  : <span className="text-[13px] text-faint">{i.name}</span>}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      <ErrorAlert error={toggle.error} />
    </Panel>
  );
}

function BlocklistPanel({ s }: { s: AdultSettings }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const key = [...adultKey, "blocklist"];
  const list = useQuery({ queryKey: key, queryFn: async () => unwrap(await api.GET("/adult/blocklist")) });
  const [term, setTerm] = useState("");
  const add = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/blocklist", { body: { term } })),
    onSuccess: () => { setTerm(""); void qc.invalidateQueries({ queryKey: key }); },
  });
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/adult/blocklist/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
  return (
    <Panel title={t("adultSettings.blocklist.title")} description={t("adultSettings.blocklist.hint")}>
      <div className="mb-4" data-testid="adult-blocklist-protected">
        <p className="text-[12px] font-medium text-fg">{t("adultSettings.blocklist.protectedTitle")}</p>
        <p className="mb-1.5 text-[12px] text-faint">{t("adultSettings.blocklist.protectedHint")}</p>
        <ul className="flex flex-wrap gap-1.5">
          {(s.protectedTerms ?? []).map((x) => (
            <li key={x} className="rounded border border-line bg-surface-3 px-1.5 py-0.5 text-[12px] text-muted">{x}</li>
          ))}
        </ul>
      </div>
      <p className="mb-1.5 text-[12px] font-medium text-fg">{t("adultSettings.blocklist.own")}</p>
      {list.data && list.data.length === 0 ? <p className="text-[12px] text-faint" data-testid="adult-blocklist-empty">{t("adultSettings.blocklist.empty")}</p> : null}
      <ul className="flex flex-wrap gap-1.5" data-testid="adult-blocklist">
        {list.data?.map((b) => (
          <li key={b.id} className="flex items-center gap-1 rounded border border-line-strong bg-surface px-1.5 py-0.5 text-[12px]">
            {b.term}
            <button type="button" className="px-0.5 text-faint hover:text-danger" aria-label={`${t("common.remove")} ${b.term}`} onClick={() => remove.mutate(b.id)}>×</button>
          </li>
        ))}
      </ul>
      <form className="mt-3 flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (term.trim().length >= 2) add.mutate(); }}>
        <div className="min-w-48 flex-1"><TextInput label={t("adultSettings.blocklist.term")} value={term} maxLength={100} onChange={(e) => setTerm(e.target.value)} /></div>
        <Button type="submit" busy={add.isPending} disabled={term.trim().length < 2}>{t("adultSettings.blocklist.add")}</Button>
      </form>
      <ErrorAlert error={add.error ?? remove.error} />
    </Panel>
  );
}

/** "Not interested" entries (D-110): only for accounts that see adult content. */
function ExclusionsPanel() {
  const { t, i18n } = useTranslation();
  const me = useCurrentUser();
  const visible = !!me.adultVisible;
  const list = useAdultExclusions(visible);
  const undo = useDeleteExclusion();
  const canManage = me.role === "admin" || !!me.permissions.manageLibrary;
  if (!visible) return null;
  return (
    <Panel title={t("adultSettings.exclusions.title")} description={t("adultSettings.exclusions.hint")}>
      {list.isPending ? <Spinner /> : null}
      <ErrorAlert error={list.error ?? undo.error} />
      {list.data?.length === 0 ? <p className="text-[12px] text-faint" data-testid="adult-exclusions-empty">{t("adultSettings.exclusions.empty")}</p> : null}
      {list.data?.length ? (
        <ul className="divide-y divide-line" data-testid="adult-exclusions">
          {list.data.map((e) => (
            <li key={e.id} className="flex items-center justify-between gap-3 py-2">
              <div className="min-w-0 text-[13px]">
                <p className="truncate text-fg">{e.title}</p>
                <p className="truncate text-[12px] text-muted">
                  {[e.studio, e.releaseDate, t("adultSettings.exclusions.added", { when: formatDateTime(e.addedAt, i18n.language) })].filter(Boolean).join(" · ")}
                </p>
              </div>
              {canManage ? (
                <Button variant="ghost" busy={undo.isPending && undo.variables === e.id} onClick={() => undo.mutate(e.id)} data-testid="adult-exclusion-undo">
                  {t("adultSettings.exclusions.undo")}
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </Panel>
  );
}
