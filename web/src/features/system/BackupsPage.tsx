import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Archive, Download, Upload } from "lucide-react";
import { ApiError, api, queryKeys, unwrap, type Backup, type RestoreStarted } from "@/api/client";
import { formatBytes, formatDateTime } from "@/components/format";
import { Alert, Badge, Button, ConfirmButton, EmptyState, ErrorAlert, PageHeader, Panel, Spinner, TextInput } from "@/components/ui";

/** The word an administrator types to confirm a restore (the same in every language). */
export const RESTORE_WORD = "RESTORE";

/** Waits until the server answers again after an in-process restart, then reloads the page. */
async function waitForRestart(signal: { cancelled: boolean }) {
  await new Promise((r) => setTimeout(r, 2000));
  for (let i = 0; i < 180 && !signal.cancelled; i++) {
    try {
      const ready = await fetch("/readyz", { cache: "no-store" });
      if (ready.ok) {
        const info = await fetch("/api/v1/system/info", { cache: "no-store" });
        if (info.ok) {
          window.location.reload();
          return;
        }
      }
    } catch {
      // still restarting
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
}

/** Sends an uploaded backup zip (not JSON, so not through the typed client). */
async function uploadRestore(file: File): Promise<RestoreStarted> {
  const res = await fetch(`/api/v1/system/backups/restore?confirm=${RESTORE_WORD}`, {
    method: "POST",
    body: file,
    headers: { "Content-Type": "application/zip" },
    credentials: "same-origin",
  });
  const body = (await res.json().catch(() => ({}))) as Partial<RestoreStarted> & { code?: string; message?: string };
  if (!res.ok) throw new ApiError(res.status, body.code ?? `http_${res.status}`, body.message ?? res.statusText);
  return body as RestoreStarted;
}

/** System → Backups: scheduled and manual backups, download, delete and restore. */
export function BackupsPage({ onRestarting = waitForRestart }: { onRestarting?: (signal: { cancelled: boolean }) => Promise<void> }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: queryKeys.backups, queryFn: async () => unwrap(await api.GET("/system/backups")) });
  const [target, setTarget] = useState<Backup | File | null>(null);
  const [typed, setTyped] = useState("");
  const [restarting, setRestarting] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const signal = useRef({ cancelled: false });
  useEffect(() => {
    const s = signal.current;
    return () => {
      s.cancelled = true;
    };
  }, []);

  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/system/backups")),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.backups }),
  });
  const remove = useMutation({
    mutationFn: async (name: string) => unwrap(await api.DELETE("/system/backups/{name}", { params: { path: { name } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.backups }),
  });
  const restore = useMutation({
    mutationFn: async () => {
      if (!target) throw new Error("nothing to restore");
      if (target instanceof File) return uploadRestore(target);
      return unwrap(await api.POST("/system/backups/{name}/restore", { params: { path: { name: target.name }, query: { confirm: RESTORE_WORD } } }));
    },
    onSuccess: () => {
      setRestarting(true);
      void onRestarting(signal.current);
    },
  });

  const targetName = target ? target.name : "";
  const confirmed = typed.trim() === RESTORE_WORD;

  if (restarting) {
    return (
      <Panel>
        <div className="flex flex-col gap-2" data-testid="restore-restarting">
          <Spinner label={t("backups.restarting")} />
          <p className="text-[12px] text-muted">{t("backups.restartingHint")}</p>
        </div>
      </Panel>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("backups.title")}
        description={t("backups.subtitle")}
        actions={
          <Button variant="primary" busy={create.isPending} onClick={() => create.mutate()} data-testid="backup-now">
            <Archive className="size-3.5" />
            {t("backups.now")}
          </Button>
        }
      />
      <Alert tone="warning">{t("backups.secretWarning")}</Alert>
      <p className="text-[12px] text-muted">
        {t("backups.scheduleHint")} <Link to="/settings/system" className="text-info hover:underline">{t("backups.scheduleLink")}</Link>
      </p>
      <ErrorAlert error={create.error ?? remove.error} />
      <Panel title={t("backups.list")}>
        {list.isPending ? <Spinner /> : null}
        <ErrorAlert error={list.error} />
        {list.data?.length === 0 ? <EmptyState>{t("backups.none")}</EmptyState> : null}
        <ul className="divide-y divide-line" data-testid="backup-list">
          {list.data?.map((b) => (
            <li key={b.name} className="flex flex-wrap items-center justify-between gap-2 py-2" data-testid={`backup-${b.name}`}>
              <div className="min-w-0">
                <p className="flex flex-wrap items-center gap-2 text-[13px]">
                  <span className="break-all font-mono text-fg">{b.name}</span>
                  <Badge>{t(`backups.kinds.${b.kind}`)}</Badge>
                </p>
                <p className="text-[12px] text-faint">
                  {formatDateTime(b.createdAt, i18n.language)} · {formatBytes(b.size, i18n.language)}
                  {b.version ? ` · ${t("backups.version", { version: b.version })}` : ""}
                </p>
              </div>
              <span className="flex items-center gap-1">
                <a href={`/api/v1/system/backups/${encodeURIComponent(b.name)}`} download
                  className="inline-flex h-8 items-center gap-1.5 rounded-ui px-3 text-[13px] text-muted hover:bg-surface-3 hover:text-fg">
                  <Download className="size-3.5" />
                  {t("backups.download")}
                </a>
                <Button variant="ghost" onClick={() => { setTarget(b); setTyped(""); }} data-testid={`backup-restore-${b.name}`}>
                  {t("backups.restore")}
                </Button>
                <ConfirmButton label={t("common.delete")} confirmLabel={t("backups.deleteConfirm")} busy={remove.isPending}
                  onConfirm={() => remove.mutate(b.name)} />
              </span>
            </li>
          ))}
        </ul>
      </Panel>
      <Panel title={t("backups.upload")} description={t("backups.uploadHint")}>
        <input ref={fileInput} type="file" accept=".zip,application/zip" className="hidden" data-testid="backup-upload-input"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) {
              setTarget(f);
              setTyped("");
            }
          }} />
        <Button onClick={() => fileInput.current?.click()}>
          <Upload className="size-3.5" />
          {t("backups.chooseFile")}
        </Button>
      </Panel>
      {target ? (
        <Panel title={t("backups.confirmTitle", { name: targetName })}>
          <div className="flex flex-col gap-3" data-testid="restore-confirm">
            <Alert tone="danger">{t("backups.confirmText")}</Alert>
            <TextInput label={t("backups.typeWord", { word: RESTORE_WORD })} value={typed} onChange={(e) => setTyped(e.target.value)}
              autoComplete="off" spellCheck={false} className="font-mono" data-testid="restore-typed" />
            <ErrorAlert error={restore.error} />
            <div className="flex gap-2">
              <Button variant="danger" disabled={!confirmed} busy={restore.isPending} onClick={() => restore.mutate()} data-testid="restore-submit">
                {t("backups.restoreNow")}
              </Button>
              <Button variant="ghost" onClick={() => setTarget(null)}>{t("common.cancel")}</Button>
            </div>
          </div>
        </Panel>
      ) : null}
    </div>
  );
}
