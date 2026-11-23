import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { MessageSquare } from "lucide-react";
import { api, queryKeys, unwrap, type Issue, type IssueStatus } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { Badge, Button, Checkbox, EmptyState, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { PosterImage } from "@/features/discover/common";
import { CommentThread } from "@/features/requests/common";
import { canManageRequests } from "@/features/users/permissions";

export type IssuesSearch = { status?: IssueStatus | "all" };

export function validateIssuesSearch(s: Record<string, unknown>): IssuesSearch {
  return s.status === "open" || s.status === "resolved" || s.status === "all" ? { status: s.status } : {};
}

function issueScope(t: (k: string, o?: Record<string, unknown>) => string, i: Issue): string {
  const parts = [t(`issues.types.${i.type}`)];
  if (i.seasonNumber !== undefined && i.episodeNumber !== undefined) parts.push(t("issues.seasonEpisode", { s: i.seasonNumber, e: i.episodeNumber }));
  else if (i.seasonNumber !== undefined) parts.push(t("request.season", { n: i.seasonNumber }));
  return parts.join(" · ");
}

const mediaType = (i: Issue) => (i.kind === "album" ? "album" : i.kind);

export function IssuesPage() {
  const { t, i18n } = useTranslation();
  const me = useCurrentUser();
  const search = useSearch({ from: "/app/issues" });
  const navigate = useNavigate({ from: "/issues" });
  const status = search.status ?? "open";
  const list = useQuery({
    queryKey: queryKeys.issueList(status, 0),
    queryFn: async () => unwrap(await api.GET("/issues", { params: { query: status === "all" ? { limit: 100 } : { status, limit: 100 } } })),
  });
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("issues.title")} description={canManageRequests(me) ? t("issues.subtitleAll") : t("issues.subtitleMine")} />
      <nav aria-label={t("issues.filter")} className="flex gap-1 border-b border-line">
        {(["open", "resolved", "all"] as const).map((s) => (
          <button key={s} type="button" onClick={() => void navigate({ search: { status: s } })} aria-current={status === s ? "page" : undefined}
            data-testid={`issues-${s}`}
            className={`-mb-px border-b-2 px-3 py-2 text-[13px] ${status === s ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
            {t(`issues.filters.${s}`)}
          </button>
        ))}
      </nav>
      {list.isPending ? <Spinner /> : null}
      <ErrorAlert error={list.error} />
      {list.data && list.data.items.length === 0 ? <EmptyState>{t("issues.none")}</EmptyState> : null}
      <ul className="flex flex-col gap-2" data-testid="issue-list">
        {list.data?.items.map((i) => (
          <li key={i.id} data-testid={`issue-${i.id}`}>
            <Link to="/issues/$id" params={{ id: String(i.id) }} className="flex gap-3 rounded-ui border border-line bg-surface p-3 hover:border-line-strong">
              <PosterImage item={{ mediaType: mediaType(i), poster: i.poster, title: i.title }} className="w-11 shrink-0 border border-line" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate text-[14px] font-medium text-fg">{i.title}</span>
                  <Badge tone={i.status === "open" ? "warning" : "success"}>{t(`issues.status.${i.status}`)}</Badge>
                </div>
                <p className="text-[12px] text-muted">{issueScope(t, i)}</p>
                <p className="line-clamp-2 text-[12.5px] text-fg/90">{i.message}</p>
                <p className="text-[11.5px] text-faint">
                  {t("requests.byWhen", { user: i.reportedBy.username, when: formatRelative(i.createdAt, i18n.language) })}
                  {i.commentCount > 0 ? <span className="ml-1 inline-flex items-center gap-0.5"> · <MessageSquare className="size-3" /> {i.commentCount}</span> : null}
                </p>
              </div>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function IssueDetailPage() {
  const { t, i18n } = useTranslation();
  const { id } = useParams({ from: "/app/issues/$id" });
  const iid = Number(id);
  const me = useCurrentUser();
  const qc = useQueryClient();
  const manager = canManageRequests(me);
  const [replace, setReplace] = useState(false);
  const [note, setNote] = useState("");
  const q = useQuery({ queryKey: queryKeys.issue(iid), queryFn: async () => unwrap(await api.GET("/issues/{id}", { params: { path: { id: iid } } })) });
  const comments = useQuery({ queryKey: queryKeys.issueComments(iid), queryFn: async () =>
    unwrap(await api.GET("/issues/{id}/comments", { params: { path: { id: iid } } })) });
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: queryKeys.issues });
  };
  const addComment = useMutation({
    mutationFn: async (body: string) => unwrap(await api.POST("/issues/{id}/comments", { params: { path: { id: iid } }, body: { body } })),
    onSuccess: refresh,
  });
  const resolve = useMutation({
    mutationFn: async () => unwrap(await api.POST("/issues/{id}/resolve", { params: { path: { id: iid } },
      body: { searchReplacement: replace, ...(note.trim() ? { comment: note.trim() } : {}) } })),
    onSuccess: () => {
      setNote("");
      refresh();
    },
  });
  const reopen = useMutation({
    mutationFn: async () => unwrap(await api.POST("/issues/{id}/reopen", { params: { path: { id: iid } } })),
    onSuccess: refresh,
  });
  if (q.isPending) return <Spinner />;
  if (q.error) return <ErrorAlert error={q.error} />;
  const i = q.data;
  const reporter = i.reportedBy.id === me.id;
  return (
    <div className="flex flex-col gap-4" data-testid="issue-detail">
      <PageHeader title={i.title} description={issueScope(t, i)}
        actions={<Link to="/issues" className="text-[12px] text-muted hover:text-fg">{t("issues.back")}</Link>} />
      <Panel>
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={i.status === "open" ? "warning" : "success"}>{t(`issues.status.${i.status}`)}</Badge>
            <span className="text-[12px] text-faint">
              {t("requests.byWhen", { user: i.reportedBy.username, when: formatDateTime(i.createdAt, i18n.language) })}
            </span>
          </div>
          <p className="whitespace-pre-wrap text-[13.5px] text-fg" data-testid="issue-message-text">{i.message}</p>
          {i.resolvedBy ? (
            <p className="text-[12px] text-muted">
              {t("issues.resolvedBy", { user: i.resolvedBy.username, when: formatDateTime(i.resolvedAt, i18n.language) })}
              {i.replacementSearched ? ` · ${t("issues.replacementSearched")}` : ""}
            </p>
          ) : null}
          {manager && i.status === "open" ? (
            <div className="mt-2 flex flex-col gap-2 border-t border-line pt-3">
              <Checkbox label={t("issues.searchReplacement")} description={t("issues.searchReplacementHint")} checked={replace}
                onChange={(e) => setReplace(e.target.checked)} data-testid="issue-replace" />
              <label htmlFor="resolve-note" className="text-[12px] font-medium text-muted">{t("issues.resolveNote")}</label>
              <textarea id="resolve-note" rows={2} maxLength={2000} value={note} onChange={(e) => setNote(e.target.value)}
                className="w-full rounded-ui border border-line-strong bg-surface-2 px-2.5 py-1.5 text-[13px] text-fg focus:border-info focus:outline-none" />
              <div>
                <Button variant="primary" busy={resolve.isPending} onClick={() => resolve.mutate()} data-testid="issue-resolve">{t("issues.resolve")}</Button>
              </div>
              <ErrorAlert error={resolve.error} />
            </div>
          ) : null}
          {i.status === "resolved" && (manager || reporter) ? (
            <div className="mt-2">
              <Button busy={reopen.isPending} onClick={() => reopen.mutate()} data-testid="issue-reopen">{t("issues.reopen")}</Button>
              <ErrorAlert error={reopen.error} />
            </div>
          ) : null}
        </div>
      </Panel>
      <Panel title={t("comments.title")}>
        <CommentThread comments={comments.data} loading={comments.isPending} error={comments.error} onAdd={(b) => addComment.mutate(b, {
          onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.issueComments(iid) }),
        })} adding={addComment.isPending} addError={addComment.error} testId="issue-comments" />
      </Panel>
    </div>
  );
}
