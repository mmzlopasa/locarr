import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { itemLink } from "@/features/discover/common";
import { requestItem } from "@/features/discover/DiscoverPage";
import { canManageRequests } from "@/features/users/permissions";
import { RequestComments } from "./common";
import { RequestRow } from "./RequestsPage";

export function RequestDetailPage() {
  const { t, i18n } = useTranslation();
  const { id } = useParams({ from: "/app/requests/$id" });
  const rid = Number(id);
  const me = useCurrentUser();
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: queryKeys.request(rid),
    queryFn: async () => unwrap(await api.GET("/requests/{id}", { params: { path: { id: rid } } })),
    refetchInterval: 15_000,
  });
  if (q.isPending) return <Spinner />;
  if (q.error) return <ErrorAlert error={q.error} />;
  const r = q.data;
  const link = itemLink(requestItem(r));
  return (
    <div className="flex flex-col gap-4" data-testid="request-detail">
      <PageHeader title={r.title} description={t("requests.detailSubtitle", { id: r.id, when: formatDateTime(r.createdAt, i18n.language) })}
        actions={<>
          {link ? <Link to={link.to as never} params={link.params as never} className="text-[12px] text-muted hover:text-fg">{t("requests.openTitle")}</Link> : null}
          <Link to="/requests" className="text-[12px] text-muted hover:text-fg">{t("requests.backToList")}</Link>
        </>} />
      <ul>
        <RequestRow r={r} manage={canManageRequests(me)} selected={false} onSelect={() => undefined}
          onChanged={() => void qc.invalidateQueries({ queryKey: queryKeys.requests })} />
      </ul>
      <Panel title={t("comments.title")}>
        <RequestComments id={rid} />
      </Panel>
    </div>
  );
}
