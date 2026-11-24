import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap, type Indexer } from "@/api/client";
import { Alert, Badge, Button, Checkbox, ConfirmButton, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { inputFromIndexer } from "@/features/indexers/BuiltinIndexers";
import { useQualityProfiles } from "@/features/movies/common";
import { useCurrentUser } from "@/features/auth/hooks";
import { adultKey, useAdultRootFolders, useAdultSettings, useSaveAdultSettings, type AdultSettings } from "@/features/adult/api";

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
          <DefaultsPanel s={settings.data} />
          <RootsPanel />
          <IndexersPanel />
          <BlocklistPanel />
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
        <Checkbox label={t("adultSettings.defaults.stats")} description={t("adultSettings.defaults.statsHint")} checked={v.statsAggregate}
          onChange={(e) => setV({ ...v, statsAggregate: e.target.checked })} />
        <Checkbox label={t("adultSettings.defaults.streamero")} description={t("adultSettings.defaults.streameroHint")} checked={v.notifyStreamero}
          onChange={(e) => setV({ ...v, notifyStreamero: e.target.checked })} />
      </div>
      <div className="mt-3">
        <Button variant="primary" busy={save.isPending} onClick={() => save.mutate({ defaultSource: v.defaultSource, defaultProfileId: v.defaultProfileId,
          defaultRootFolderId: v.defaultRootFolderId, sceneFolder: v.sceneFolder, sceneFile: v.sceneFile, movieFolder: v.movieFolder,
          movieFile: v.movieFile, statsAggregate: v.statsAggregate, notifyStreamero: v.notifyStreamero })}>{t("common.save")}</Button>
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
  return (
    <Panel title={t("adultSettings.indexers.title")} description={t("adultSettings.indexers.hint")}>
      <ul className="space-y-1.5">
        {list.data?.map((i) => (
          <li key={i.id}>
            <Checkbox label={i.name} checked={(i.categories.adult ?? []).length > 0} onChange={(e) => toggle.mutate({ i, on: e.target.checked })} />
          </li>
        ))}
      </ul>
      <ErrorAlert error={toggle.error} />
    </Panel>
  );
}

function BlocklistPanel() {
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
      <form className="mb-3 flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (term.trim().length >= 2) add.mutate(); }}>
        <div className="min-w-48 flex-1"><TextInput label={t("adultSettings.blocklist.term")} value={term} maxLength={100} onChange={(e) => setTerm(e.target.value)} /></div>
        <Button type="submit" busy={add.isPending} disabled={term.trim().length < 2}>{t("adultSettings.blocklist.add")}</Button>
      </form>
      <ul className="flex flex-wrap gap-1.5" data-testid="adult-blocklist">
        {list.data?.map((b) => (
          <li key={b.id} className="flex items-center gap-1 rounded border border-line-strong bg-surface px-1.5 py-0.5 text-[12px]">
            {b.term}
            <button type="button" className="text-faint hover:text-danger" aria-label={t("common.remove")} onClick={() => remove.mutate(b.id)}>×</button>
          </li>
        ))}
      </ul>
      <ErrorAlert error={add.error ?? remove.error} />
    </Panel>
  );
}
