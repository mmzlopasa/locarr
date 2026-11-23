import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { Badge, ConfirmButton, EmptyState, ErrorAlert, Panel, Spinner } from "@/components/ui";

export function BlocklistPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: queryKeys.blocklist, queryFn: async () => unwrap(await api.GET("/queue/blocklist")) });
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/queue/blocklist/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.blocklist }),
  });
  const clear = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/queue/blocklist")),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.blocklist }),
  });
  return (
    <Panel title={t("blocklist.title")} description={t("blocklist.subtitle")}
      actions={list.data?.length ? (
        <ConfirmButton label={t("blocklist.clear")} confirmLabel={t("blocklist.clearConfirm", { count: list.data.length })}
          busy={clear.isPending} onConfirm={() => clear.mutate()} testId="blocklist-clear" />
      ) : undefined}>
      {list.isPending ? <Spinner /> : null}
      <ErrorAlert error={list.error ?? remove.error ?? clear.error} />
      {list.data?.length === 0 ? <EmptyState>{t("blocklist.none")}</EmptyState> : null}
      <ul className="divide-y divide-line" data-testid="blocklist">
        {list.data?.map((b) => (
          <li key={b.id} className="flex flex-wrap items-center justify-between gap-3 py-2">
            <div className="min-w-0">
              <p className="break-all text-[13px] text-fg">{b.title}</p>
              <p className="mt-0.5 flex flex-wrap items-center gap-2 text-[12px] text-faint">
                <Badge>{t(`blocklist.reasons.${b.reason}`, { defaultValue: b.reason })}</Badge>
                {b.indexer ? <span>{b.indexer}</span> : null}
                <span>{formatDateTime(b.createdAt, i18n.language)}</span>
                {b.message ? <span>{b.message}</span> : null}
              </p>
            </div>
            <ConfirmButton label={t("blocklist.unblock")} confirmLabel={t("blocklist.unblockConfirm")} busy={remove.isPending}
              onConfirm={() => remove.mutate(b.id)} />
          </li>
        ))}
      </ul>
    </Panel>
  );
}
