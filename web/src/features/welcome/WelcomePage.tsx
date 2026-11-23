import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useRouter } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CheckCircle2, FolderOpen } from "lucide-react";
import { api, queryKeys, unwrap, type MediaKind, type RootFolder } from "@/api/client";
import { formatBytes } from "@/components/format";
import { Alert, Badge, Button, ErrorAlert, PageHeader, Panel, TextInput } from "@/components/ui";
import { FolderBrowser } from "@/features/settings/RootFoldersPage";

const KINDS: MediaKind[] = ["movies", "series", "music"];
const SUGGESTED: Record<MediaKind, string> = { movies: "/data/media/movies", series: "/data/media/tv", music: "/data/media/music" };

/** Whether the setup import wizard (P10) is part of this build. */
export function useHasMigrate(): boolean {
  const router = useRouter();
  const byPath = (router as unknown as { routesByPath?: Record<string, unknown> }).routesByPath ?? {};
  return "/migrate" in byPath;
}

/** First run, after the administrator exists: media folders, then (optionally) importing an existing setup. */
export function WelcomePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const hasMigrate = useHasMigrate();
  const [step, setStep] = useState<1 | 2>(1);
  const folders = useQuery({ queryKey: queryKeys.rootFolders, queryFn: async () => unwrap(await api.GET("/rootfolders")) });

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4" data-testid="welcome">
      <PageHeader title={t("welcome.title")} description={t("welcome.subtitle")} />
      <ol className="flex gap-4 text-[12px]">
        <li className={step === 1 ? "font-semibold text-fg" : "text-faint"}>1. {t("welcome.steps.folders")}</li>
        <li className={step === 2 ? "font-semibold text-fg" : "text-faint"}>2. {t("welcome.steps.import")}</li>
      </ol>
      {step === 1 ? (
        <>
          <ErrorAlert error={folders.error} />
          {KINDS.map((k) => (
            <KindFolder key={k} kind={k} existing={folders.data?.filter((f) => f.kind === k) ?? []} />
          ))}
          <div className="flex gap-2">
            <Button variant="primary" onClick={() => setStep(2)} data-testid="welcome-next">{t("welcome.next")}</Button>
            <Button variant="ghost" onClick={() => setStep(2)} data-testid="welcome-skip">{t("welcome.skip")}</Button>
          </div>
        </>
      ) : (
        <>
          <Panel title={t("welcome.importTitle")} description={t("welcome.importHint")}>
            {hasMigrate ? (
              <Link to={"/migrate" as never} className="text-[13px] text-info hover:underline" data-testid="welcome-migrate">
                {t("welcome.importLink")}
              </Link>
            ) : (
              <p className="text-[13px] text-muted" data-testid="welcome-no-migrate">{t("welcome.importLater")}</p>
            )}
          </Panel>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={() => setStep(1)}>{t("common.back")}</Button>
            <Button variant="primary" onClick={() => void navigate({ to: "/" })} data-testid="welcome-done">{t("common.done")}</Button>
          </div>
        </>
      )}
    </div>
  );
}

function KindFolder({ kind, existing }: { kind: MediaKind; existing: RootFolder[] }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [path, setPath] = useState(SUGGESTED[kind]);
  const [browsing, setBrowsing] = useState(false);
  const add = useMutation({
    mutationFn: async () => unwrap(await api.POST("/rootfolders", { body: { path: path.trim(), kind, create: true } })),
    onSuccess: () => {
      setBrowsing(false);
      void qc.invalidateQueries({ queryKey: queryKeys.rootFolders });
    },
  });
  return (
    <Panel title={t(`welcome.kinds.${kind}`)} description={t("welcome.folderHint")}>
      <div className="flex flex-col gap-3" data-testid={`welcome-${kind}`}>
        {existing.map((f) => (
          <p key={f.id} className="flex flex-wrap items-center gap-2 text-[13px]">
            <CheckCircle2 className="size-4 text-success" />
            <span className="break-all font-mono text-fg">{f.path}</span>
            {!f.accessible ? <Badge tone="danger">{t("rootFolders.missing")}</Badge>
              : !f.writable ? <Badge tone="warning">{t("rootFolders.readOnly")}</Badge>
              : <Badge tone="success">{t("welcome.writable")}</Badge>}
            {f.accessible ? (
              <span className="text-[12px] text-faint">
                {t("rootFolders.space", { free: formatBytes(f.freeSpaceBytes, i18n.language), total: formatBytes(f.totalSpaceBytes, i18n.language) })}
              </span>
            ) : null}
          </p>
        ))}
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-64 flex-1">
            <TextInput label={t("rootFolders.path")} className="font-mono" value={path} onChange={(e) => setPath(e.target.value)}
              data-testid={`welcome-path-${kind}`} />
          </div>
          <Button onClick={() => setBrowsing(!browsing)} aria-expanded={browsing}>
            <FolderOpen className="size-4" />
            {t("rootFolders.browse")}
          </Button>
          <Button variant={existing.length ? "secondary" : "primary"} busy={add.isPending} disabled={!path.trim()} onClick={() => add.mutate()}
            data-testid={`welcome-add-${kind}`}>
            {existing.length ? t("welcome.addAnother") : t("welcome.use")}
          </Button>
        </div>
        {browsing ? <FolderBrowser start="" onPick={setPath} /> : null}
        <ErrorAlert error={add.error} />
        {add.data && !add.data.writable ? <Alert tone="warning">{t("welcome.notWritable")}</Alert> : null}
      </div>
    </Panel>
  );
}
