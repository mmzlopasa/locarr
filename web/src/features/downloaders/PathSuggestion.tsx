import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type ClientPathCheck, type PathMappingSuggestion } from "@/api/client";
import { Alert, Button, ErrorAlert } from "@/components/ui";
import { isAdmin, useMe } from "@/features/auth/hooks";

/** The fields needed to add a suggested mapping. */
export type MappingToAdd = Pick<PathMappingSuggestion, "host" | "remotePath" | "localPath" | "replacesMappingId">;

/**
 * Adds a suggested remote path mapping in one click (administrators only;
 * nothing is ever added without this click). The tracker is asked to look again
 * at once so the waiting download is imported.
 */
export function AddMappingButton({ mapping, testId = "add-suggested-mapping" }: { mapping: MappingToAdd; testId?: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const me = useMe();
  const add = useMutation({
    mutationFn: async () => {
      const body = { host: mapping.host, remotePath: mapping.remotePath, localPath: mapping.localPath };
      const saved = mapping.replacesMappingId
        ? unwrap(await api.PUT("/remotepathmappings/{id}", { params: { path: { id: mapping.replacesMappingId } }, body }))
        : unwrap(await api.POST("/remotepathmappings", { body }));
      await api.POST("/queue/refresh").catch(() => undefined);
      return saved;
    },
    onSuccess: () => {
      for (const key of [queryKeys.remotePathMappings, queryKeys.queue, queryKeys.downloadClients, queryKeys.health]) {
        void qc.invalidateQueries({ queryKey: key });
      }
      // The tracker polls right after the refresh; look again shortly after.
      setTimeout(() => void qc.invalidateQueries({ queryKey: queryKeys.queue }), 1500);
    },
  });
  if (!isAdmin(me.data)) return null;
  if (add.isSuccess) return <span className="text-[12px] text-success" data-testid={`${testId}-done`}>{t("mappings.suggest.added")}</span>;
  return (
    <span className="inline-flex flex-col gap-1">
      <Button variant="primary" busy={add.isPending} onClick={() => add.mutate()} data-testid={testId}>
        {mapping.replacesMappingId ? t("mappings.suggest.replace") : t("mappings.suggest.add")}
      </Button>
      <ErrorAlert error={add.error} />
    </span>
  );
}

/** One suggestion as a sentence: where Locarr found the content and which mapping would fix it. */
export function suggestionText(t: (k: string, o?: Record<string, unknown>) => string, s: PathMappingSuggestion): string {
  const vars = { foundAt: s.foundAt, clientPath: s.clientPath, remotePath: s.remotePath, localPath: s.localPath, host: s.host,
    client: s.clientName ?? s.host };
  return s.source === "folder" ? t("mappings.suggest.folder", vars) : t("mappings.suggest.download", vars);
}

/** A suggestion with its button, for the queue row and the mappings panel. */
export function SuggestionBox({ suggestion, testId }: { suggestion: PathMappingSuggestion; testId?: string }) {
  const { t } = useTranslation();
  const me = useMe();
  return (
    <div className="mt-1.5" data-testid={testId ?? "path-suggestion"}>
      <Alert tone="info">
        <p className="break-words">{suggestionText(t, suggestion)}</p>
        {suggestion.replacesMappingId ? <p className="mt-0.5 text-[12px] opacity-80">{t("mappings.suggest.replacesHint")}</p> : null}
        <div className="mt-2">
          {isAdmin(me.data) ? <AddMappingButton mapping={suggestion} /> : <span className="text-[12px] opacity-80">{t("mappings.suggest.askAdmin")}</span>}
        </div>
      </Alert>
    </div>
  );
}

/** The client folders Locarr cannot see, with the likely mapping for each. */
export function PathChecks({ checks }: { checks: ClientPathCheck[] | undefined }) {
  const { t } = useTranslation();
  const hidden = (checks ?? []).filter((c) => !c.visible);
  if (hidden.length === 0) return null;
  return (
    <div className="flex flex-col gap-1.5" data-testid="path-checks">
      {hidden.map((c) => (
        <Alert key={`${c.category ?? ""}:${c.clientPath}`} tone="warning">
          <p className="break-words">
            {c.category
              ? t("mappings.check.category", { category: c.category, path: c.clientPath })
              : t("mappings.check.default", { path: c.clientPath })}{" "}
            {c.suggestion ? suggestionText(t, c.suggestion) : t("mappings.check.noSuggestion")}
          </p>
          {c.suggestion ? <div className="mt-2"><AddMappingButton mapping={c.suggestion} testId={`add-mapping-${c.category ?? "default"}`} /></div> : null}
        </Alert>
      ))}
    </div>
  );
}
