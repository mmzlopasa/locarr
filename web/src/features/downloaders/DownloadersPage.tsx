import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import {
  api,
  queryKeys,
  unwrap,
  type DownloadClient,
  type DownloadClientImplementation,
  type DownloadClientStatus,
  type RemotePathMapping,
} from "@/api/client";
import { formatBytes } from "@/components/format";
import { Alert, Badge, Button, ConfirmButton, EmptyState, ErrorAlert, PageHeader, Panel, Spinner, TextInput } from "@/components/ui";
import { ClientForm } from "./ClientForm";

type Editing = { impl: DownloadClientImplementation; client?: DownloadClient } | "pick" | null;

export function ClientStatusBadge({ status, enabled }: { status: DownloadClientStatus; enabled: boolean }) {
  const { t } = useTranslation();
  if (!enabled) return <Badge>{t("downloaders.status.disabled")}</Badge>;
  if (!status.checkedAt) return <Badge>{t("downloaders.status.unchecked")}</Badge>;
  if (status.ok) return <Badge tone="success">{t("downloaders.status.ok")}</Badge>;
  return (
    <span title={status.error}>
      <Badge tone="danger">
        {t(`downloaders.testErrors.${status.errorCode ?? "client_error"}`, { defaultValue: t("downloaders.testErrors.client_error") })}
      </Badge>
    </span>
  );
}

export function DownloadersPage() {
  const { t, i18n } = useTranslation();
  const clients = useQuery({
    queryKey: queryKeys.downloadClients,
    queryFn: async () => unwrap(await api.GET("/downloadclients")),
    refetchInterval: 15_000,
  });
  const impls = useQuery({
    queryKey: queryKeys.downloadImplementations,
    queryFn: async () => unwrap(await api.GET("/downloadclients/implementations")),
    staleTime: Infinity,
  });
  const [editing, setEditing] = useState<Editing>(null);
  const implOf = (name: string) => impls.data?.find((i) => i.name === name);

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("downloaders.title")} description={t("downloaders.subtitle")}
        actions={editing === null ? (
          <Button variant="primary" onClick={() => setEditing("pick")} disabled={!impls.data}>
            <Plus className="size-4" />
            {t("downloaders.add")}
          </Button>
        ) : null} />

      {editing === "pick" && impls.data ? (
        <Panel title={t("downloaders.pick.title")} description={t("downloaders.pick.subtitle")}>
          {(["torrent", "usenet"] as const).map((p) => (
            <div key={p} className="mb-3 last:mb-0">
              <p className="mb-1.5 text-[12px] font-semibold uppercase tracking-wide text-muted">{t(`downloaders.protocols.${p}`)}</p>
              <div className="flex flex-wrap gap-2">
                {impls.data.filter((i) => i.protocol === p).map((i) => (
                  <Button key={i.name} onClick={() => setEditing({ impl: i })} data-testid={`pick-${i.name}`}>{i.displayName}</Button>
                ))}
              </div>
            </div>
          ))}
          <div className="mt-3">
            <Button variant="ghost" onClick={() => setEditing(null)}>{t("common.cancel")}</Button>
          </div>
        </Panel>
      ) : null}
      {editing && editing !== "pick" ? (
        <ClientForm key={editing.client?.id ?? editing.impl.name} impl={editing.impl} client={editing.client} onDone={() => setEditing(null)} />
      ) : null}

      <Panel title={t("downloaders.list")}>
        {clients.isPending ? <Spinner /> : null}
        <ErrorAlert error={clients.error ?? impls.error} />
        {clients.data?.length === 0 ? <EmptyState>{t("downloaders.none")}</EmptyState> : null}
        <ul className="divide-y divide-line">
          {clients.data?.map((c) => {
            const impl = implOf(c.implementation);
            const cats = [c.categories.movies, c.categories.series, c.categories.music].filter(Boolean);
            return (
              <li key={c.id} className="flex flex-wrap items-center justify-between gap-3 py-2.5" data-testid={`client-${c.name}`}>
                <div className="min-w-0">
                  <p className="flex flex-wrap items-center gap-2">
                    <span className="text-[13px] font-medium text-fg">{c.name}</span>
                    <Badge tone="info">{t(`downloaders.protocols.${c.protocol}`)}</Badge>
                    <ClientStatusBadge status={c.status} enabled={c.enabled} />
                  </p>
                  <p className="mt-0.5 text-[12px] text-faint">
                    {impl?.displayName ?? c.implementation} · <span className="font-mono">{c.host}:{c.port}{c.urlBase}</span>
                    {c.status.version ? ` · ${t("downloaders.version", { version: c.status.version })}` : ""}
                    {c.status.freeSpaceBytes !== undefined
                      ? ` · ${t("downloaders.free", { free: formatBytes(c.status.freeSpaceBytes, i18n.language) })}` : ""}
                    {cats.length ? ` · ${t("downloaders.categoriesList", { categories: cats.join(", ") })}` : ""}
                    {` · ${t("downloaders.priorityShort", { priority: c.priority })}`}
                  </p>
                  {c.status.error && !c.status.ok && c.enabled ? (
                    <p className="mt-0.5 break-words text-[12px] text-danger" data-testid="client-error">
                      {t(`downloaders.testErrors.${c.status.errorCode ?? "client_error"}`, { defaultValue: t("downloaders.testErrors.client_error") })}
                      {" — "}
                      <span className="font-mono text-[11px]">{c.status.error}</span>
                    </p>
                  ) : null}
                </div>
                {impl ? (
                  <Button onClick={() => setEditing({ impl, client: c })}>{t("downloaders.edit")}</Button>
                ) : null}
              </li>
            );
          })}
        </ul>
      </Panel>
      <MappingsPanel />
      <TrackingPanel />
    </div>
  );
}

function MappingRow({ m }: { m: RemotePathMapping }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/remotepathmappings/{id}", { params: { path: { id: m.id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.remotePathMappings }),
  });
  return (
    <li className="flex flex-wrap items-center justify-between gap-2 py-2">
      <span className="break-all font-mono text-[12px] text-fg">
        {m.host}:{m.remotePath} → {m.localPath}
      </span>
      <ConfirmButton label={t("mappings.remove")} confirmLabel={t("mappings.removeConfirm")} busy={remove.isPending}
        onConfirm={() => remove.mutate()} />
    </li>
  );
}

function MappingsPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: queryKeys.remotePathMappings, queryFn: async () => unwrap(await api.GET("/remotepathmappings")) });
  const [host, setHost] = useState("");
  const [remote, setRemote] = useState("");
  const [local, setLocal] = useState("");
  const add = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/remotepathmappings", { body: { host: host.trim(), remotePath: remote.trim(), localPath: local.trim() } })),
    onSuccess: () => {
      setHost("");
      setRemote("");
      setLocal("");
      void qc.invalidateQueries({ queryKey: queryKeys.remotePathMappings });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    add.mutate();
  };
  return (
    <Panel title={t("mappings.title")} description={t("mappings.subtitle")}>
      <form onSubmit={submit} className="mb-3 grid gap-3 sm:grid-cols-[1fr_1.5fr_1.5fr_auto] sm:items-end">
        <TextInput label={t("mappings.host")} value={host} onChange={(e) => setHost(e.target.value)} placeholder="seedbox" />
        <TextInput label={t("mappings.remotePath")} value={remote} onChange={(e) => setRemote(e.target.value)} className="font-mono"
          placeholder="/downloads" />
        <TextInput label={t("mappings.localPath")} value={local} onChange={(e) => setLocal(e.target.value)} className="font-mono"
          placeholder="/data/downloads/torrents" />
        <Button type="submit" variant="primary" busy={add.isPending} disabled={!host.trim() || !remote.trim() || !local.trim()}>
          {t("mappings.add")}
        </Button>
      </form>
      <ErrorAlert error={add.error ?? list.error} />
      {list.data?.length === 0 ? <EmptyState>{t("mappings.none")}</EmptyState> : null}
      <ul className="divide-y divide-line">
        {list.data?.map((m) => <MappingRow key={m.id} m={m} />)}
      </ul>
    </Panel>
  );
}

function TrackingPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: queryKeys.downloadSettings, queryFn: async () => unwrap(await api.GET("/downloadclients/settings")) });
  const [poll, setPoll] = useState<string | null>(null);
  const [stalled, setStalled] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: async () =>
      unwrap(await api.PATCH("/downloadclients/settings", {
        body: { pollSeconds: Number(poll ?? settings.data?.pollSeconds), stalledHours: Number(stalled ?? settings.data?.stalledHours) },
      })),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.downloadSettings, data);
      setPoll(null);
      setStalled(null);
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  if (!settings.data) return null;
  return (
    <Panel title={t("tracking.title")} description={t("tracking.subtitle")}>
      <form onSubmit={submit} className="flex flex-wrap items-end gap-3">
        <div className="w-48">
          <TextInput label={t("tracking.pollSeconds")} type="number" min={10} max={3600} value={poll ?? String(settings.data.pollSeconds)}
            onChange={(e) => setPoll(e.target.value)} />
        </div>
        <div className="w-48">
          <TextInput label={t("tracking.stalledHours")} type="number" min={0} max={720}
            value={stalled ?? String(settings.data.stalledHours)} onChange={(e) => setStalled(e.target.value)} />
        </div>
        <Button type="submit" variant="primary" busy={save.isPending} disabled={poll === null && stalled === null}>{t("common.save")}</Button>
      </form>
      <p className="mt-2 text-[12px] text-faint">{t("tracking.hint")}</p>
      {save.isSuccess ? <div className="mt-2"><Alert tone="success">{t("common.saved")}</Alert></div> : null}
      <ErrorAlert error={save.error} />
    </Panel>
  );
}
