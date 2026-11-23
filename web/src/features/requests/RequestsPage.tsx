import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { MessageSquare } from "lucide-react";
import {
  api,
  queryKeys,
  unwrap,
  type MediaRequest,
  type RequestFilter,
  type RequestKind,
  type RequestOptions,
} from "@/api/client";
import { codeMessage } from "@/components/errors";
import { formatRelative } from "@/components/format";
import { Modal } from "@/components/Modal";
import { Alert, Button, EmptyState, ErrorAlert, PageHeader, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { PosterImage } from "@/features/discover/common";
import { requestItem } from "@/features/discover/DiscoverPage";
import { canManageRequests } from "@/features/users/permissions";
import { ProgressBar, QuotaLine, RequestStatusBadge, requestExplanation, requestScope, useRequestSummary } from "./common";

const FILTERS: RequestFilter[] = ["active", "pending", "approved", "processing", "partial", "available", "failed", "declined", "cancelled", "all"];
const KINDS: RequestKind[] = ["movie", "series", "album", "artist"];
const PAGE = 50;

export type RequestsSearch = { view?: "mine" | "all"; status?: RequestFilter; kind?: RequestKind; offset?: number };

export function validateRequestsSearch(s: Record<string, unknown>): RequestsSearch {
  const out: RequestsSearch = {};
  if (s.view === "mine" || s.view === "all") out.view = s.view;
  if (FILTERS.includes(s.status as RequestFilter)) out.status = s.status as RequestFilter;
  if (KINDS.includes(s.kind as RequestKind)) out.kind = s.kind as RequestKind;
  const off = Number(s.offset);
  if (Number.isFinite(off) && off > 0) out.offset = Math.floor(off);
  return out;
}

export function RequestsPage() {
  const { t } = useTranslation();
  const me = useCurrentUser();
  const manager = canManageRequests(me);
  const search = useSearch({ from: "/app/requests" });
  const navigate = useNavigate({ from: "/requests" });
  const view = manager ? (search.view ?? "all") : "mine";
  const status = search.status ?? (view === "all" ? "pending" : "all");
  const offset = search.offset ?? 0;
  const filter = { mine: view === "mine", status, kind: search.kind, offset, limit: PAGE };
  const list = useQuery({
    queryKey: queryKeys.requestList(filter),
    queryFn: async () => unwrap(await api.GET("/requests", { params: { query: filter } })),
    refetchInterval: 15_000,
  });
  const summary = useRequestSummary();
  const [selected, setSelected] = useState<number[]>([]);
  const qc = useQueryClient();
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: queryKeys.requests });
    void qc.invalidateQueries({ queryKey: ["discover"] });
  };
  const bulk = useMutation({
    mutationFn: async () => unwrap(await api.POST("/requests/approve", { body: { ids: selected } })),
    onSuccess: () => {
      setSelected([]);
      refresh();
    },
  });
  const set = (patch: Partial<RequestsSearch>) => void navigate({ search: (s: RequestsSearch) => ({ ...s, offset: undefined, ...patch }) });
  const pending = (list.data?.items ?? []).filter((r) => r.status === "pending");

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("requests.title")} description={view === "all" ? t("requests.subtitleAll") : t("requests.subtitleMine")}
        actions={<Link to="/discover" className="text-[12px] text-muted hover:text-fg">{t("requests.discoverMore")}</Link>} />
      {manager ? (
        <nav aria-label={t("requests.views")} className="flex gap-1 border-b border-line">
          {(["all", "mine"] as const).map((v) => (
            <Link key={v} to="/requests" search={{ view: v }} aria-current={view === v ? "page" : undefined} data-testid={`requests-view-${v}`}
              className={`-mb-px border-b-2 px-3 py-2 text-[13px] ${view === v ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
              {v === "all" ? t("requests.viewAll") : t("requests.viewMine")}
              {v === "all" && summary.data?.pending ? <span className="ml-1.5 rounded-full bg-warning px-1.5 text-[10.5px] font-semibold text-bg">{summary.data.pending}</span> : null}
            </Link>
          ))}
        </nav>
      ) : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-44">
          <SelectInput label={t("requests.filterStatus")} value={status} onChange={(e) => set({ status: e.target.value as RequestFilter })} data-testid="requests-filter-status">
            {FILTERS.map((f) => <option key={f} value={f}>{t(`requests.filters.${f}`)}</option>)}
          </SelectInput>
        </div>
        <div className="w-40">
          <SelectInput label={t("requests.filterKind")} value={search.kind ?? ""} onChange={(e) => set({ kind: (e.target.value || undefined) as RequestKind | undefined })}>
            <option value="">{t("requests.allKinds")}</option>
            {KINDS.map((k) => <option key={k} value={k}>{t(`requests.kinds.${k}`)}</option>)}
          </SelectInput>
        </div>
        {view === "mine" && summary.data ? (
          <div className="flex flex-col gap-0.5 pb-1">
            <QuotaLine kind="movie" summary={summary.data} label={t("requests.kinds.movie")} />
            <QuotaLine kind="series" summary={summary.data} label={t("requests.kinds.series")} />
            <QuotaLine kind="album" summary={summary.data} label={t("requests.music")} />
          </div>
        ) : null}
        {view === "all" && pending.length > 0 ? (
          <div className="ml-auto flex items-center gap-2">
            <Button onClick={() => setSelected(selected.length === pending.length ? [] : pending.map((r) => r.id))} data-testid="select-all">
              {selected.length === pending.length ? t("requests.selectNone") : t("requests.selectPending")}
            </Button>
            <Button variant="primary" disabled={selected.length === 0} busy={bulk.isPending} onClick={() => bulk.mutate()} data-testid="bulk-approve">
              {t("requests.approveSelected", { n: selected.length })}
            </Button>
          </div>
        ) : null}
      </div>
      <ErrorAlert error={bulk.error} />
      {bulk.data && bulk.data.errors.length > 0 ? (
        <Alert tone="warning">
          {bulk.data.errors.map((e) => <span key={e.id} className="block">#{e.id}: {codeMessage(t, e)}</span>)}
        </Alert>
      ) : null}
      {list.isPending ? <Spinner /> : null}
      <ErrorAlert error={list.error} />
      {list.data && list.data.items.length === 0 ? <EmptyState>{t("requests.none")}</EmptyState> : null}
      <ul className="flex flex-col gap-2" data-testid="request-list">
        {list.data?.items.map((r) => (
          <RequestRow key={r.id} r={r} manage={view === "all"} selected={selected.includes(r.id)}
            onSelect={(on) => setSelected((s) => (on ? [...s, r.id] : s.filter((x) => x !== r.id)))} onChanged={refresh} />
        ))}
      </ul>
      {list.data && list.data.total > PAGE ? (
        <div className="flex items-center justify-center gap-2">
          <Button disabled={offset === 0} onClick={() => set({ offset: Math.max(0, offset - PAGE) || undefined })}>{t("discover.previous")}</Button>
          <span className="text-[12px] text-muted">{t("requests.range", { from: offset + 1, to: Math.min(offset + PAGE, list.data.total), total: list.data.total })}</span>
          <Button disabled={offset + PAGE >= list.data.total} onClick={() => set({ offset: offset + PAGE })}>{t("discover.next")}</Button>
        </div>
      ) : null}
    </div>
  );
}

export function RequestRow({ r, manage, selected, onSelect, onChanged }:
  { r: MediaRequest; manage: boolean; selected: boolean; onSelect: (on: boolean) => void; onChanged: () => void }) {
  const { t, i18n } = useTranslation();
  const [declining, setDeclining] = useState(false);
  const [editing, setEditing] = useState(false);
  const [reason, setReason] = useState("");
  const path = { params: { path: { id: r.id } } };
  const act = useMutation({
    mutationFn: async (a: { kind: "approve" | "decline" | "cancel" | "retry"; body?: RequestOptions }) => {
      switch (a.kind) {
        case "approve":
          return unwrap(await api.POST("/requests/{id}/approve", { ...path, body: a.body ?? {} }));
        case "decline":
          return unwrap(await api.POST("/requests/{id}/decline", { ...path, body: reason.trim() ? { reason: reason.trim() } : {} }));
        case "cancel":
          return unwrap(await api.POST("/requests/{id}/cancel", path));
        case "retry":
          return unwrap(await api.POST("/requests/{id}/retry", path));
      }
    },
    onSuccess: () => {
      setDeclining(false);
      setEditing(false);
      onChanged();
    },
  });
  const item = requestItem(r);
  const explain = r.explanation && !["available", "declined", "cancelled", "pending"].includes(r.status) ? requestExplanation(t, r, i18n.language) : "";
  return (
    <li className="rounded-ui border border-line bg-surface" data-testid={`request-${r.id}`} data-status={r.status}>
      <div className="flex gap-3 p-3">
        {manage && r.status === "pending" ? (
          <input type="checkbox" className="mt-1 size-4 shrink-0 accent-[var(--lc-accent)]" checked={selected} onChange={(e) => onSelect(e.target.checked)}
            aria-label={t("requests.select", { title: r.title })} data-testid={`select-${r.id}`} />
        ) : null}
        <Link to="/requests/$id" params={{ id: String(r.id) }} className="w-12 shrink-0 sm:w-14">
          <PosterImage item={item} className="border border-line" />
        </Link>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <Link to="/requests/$id" params={{ id: String(r.id) }} className="truncate text-[14px] font-medium text-fg hover:underline" data-testid="request-title">
              {r.title}
            </Link>
            {r.year ? <span className="text-[12px] text-faint">{r.year}</span> : null}
            <RequestStatusBadge status={r.status} />
          </div>
          <p className="text-[12px] text-muted">{requestScope(t, r)}</p>
          <p className="text-[12px] text-faint">
            {t("requests.byWhen", { user: r.requestedBy.username, when: formatRelative(r.createdAt, i18n.language) })}
            {r.decidedBy ? ` · ${t(r.status === "declined" ? "requests.declinedBy" : "requests.approvedBy", { user: r.decidedBy.username })}` : ""}
            {r.autoApproved ? ` · ${t("requests.auto")}` : ""}
            {r.commentCount > 0 ? <span className="ml-1 inline-flex items-center gap-0.5"> · <MessageSquare className="size-3" /> {r.commentCount}</span> : null}
          </p>
          {r.progress && r.progress.downloads > 0 ? <div className="max-w-sm"><ProgressBar value={r.progress.percent} label={t("requests.progress")} /></div> : null}
          {explain ? <p className="text-[12px] text-muted" data-testid="request-explanation">{explain}</p> : null}
          {r.declineReason ? <p className="text-[12px] text-danger">{t("requests.declinedBecause", { reason: r.declineReason })}</p> : null}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1 sm:flex-row sm:items-start">
          {manage && r.status === "pending" ? (
            <>
              <Button variant="primary" busy={act.isPending} onClick={() => act.mutate({ kind: "approve" })} data-testid={`approve-${r.id}`}>{t("requests.approve")}</Button>
              <Button variant="ghost" onClick={() => setEditing(true)} data-testid={`edit-${r.id}`}>{t("requests.editApprove")}</Button>
              <Button variant="ghost" onClick={() => setDeclining((v) => !v)} data-testid={`decline-${r.id}`}>{t("requests.decline")}</Button>
            </>
          ) : null}
          {manage && r.status === "failed" ? (
            <Button busy={act.isPending} onClick={() => act.mutate({ kind: "retry" })} data-testid={`retry-${r.id}`}>{t("requests.retry")}</Button>
          ) : null}
          {r.canCancel ? (
            <Button variant="ghost" busy={act.isPending} onClick={() => act.mutate({ kind: "cancel" })} data-testid={`cancel-${r.id}`}>{t("requests.cancel")}</Button>
          ) : null}
        </div>
      </div>
      {declining ? (
        <form className="flex flex-wrap items-end gap-2 border-t border-line px-3 py-2.5" onSubmit={(e) => {
          e.preventDefault();
          act.mutate({ kind: "decline" });
        }}>
          <div className="min-w-48 flex-1">
            <TextInput label={t("requests.declineReason")} value={reason} maxLength={1000} onChange={(e) => setReason(e.target.value)} data-testid={`decline-reason-${r.id}`} />
          </div>
          <Button type="submit" variant="danger" busy={act.isPending} data-testid={`decline-confirm-${r.id}`}>{t("requests.declineConfirm")}</Button>
        </form>
      ) : null}
      {act.error ? <div className="px-3 pb-3"><ErrorAlert error={act.error} /></div> : null}
      {editing ? <ApproveDialog r={r} busy={act.isPending} error={act.error} onClose={() => setEditing(false)}
        onApprove={(body) => act.mutate({ kind: "approve", body })} /> : null}
    </li>
  );
}

const libraryKind = (k: RequestKind) => (k === "movie" ? "movies" : k === "series" ? "series" : "music");

/** Change a request's library options, then approve it. */
export function ApproveDialog({ r, busy, error, onClose, onApprove }:
  { r: MediaRequest; busy: boolean; error: unknown; onClose: () => void; onApprove: (o: RequestOptions) => void }) {
  const { t } = useTranslation();
  const profiles = useQuery({ queryKey: queryKeys.qualityProfiles, queryFn: async () => unwrap(await api.GET("/qualityprofiles")) });
  const roots = useQuery({ queryKey: queryKeys.rootFolders, queryFn: async () => unwrap(await api.GET("/rootfolders")) });
  const [profileId, setProfileId] = useState(r.qualityProfileId ?? 0);
  const [rootFolderId, setRootFolderId] = useState(r.rootFolderId ?? 0);
  const [tags, setTags] = useState(r.tags.join(", "));
  const [seasons, setSeasons] = useState(r.seasons.map((s) => s.number).join(", "));
  const lk = libraryKind(r.kind);
  const submit = () => {
    const o: RequestOptions = { tags: tags.split(",").map((s) => s.trim()).filter(Boolean) };
    if (profileId) o.qualityProfileId = profileId;
    if (rootFolderId) o.rootFolderId = rootFolderId;
    if (r.kind === "series" && !r.allSeasons) {
      const list = seasons.split(/[,\s]+/).map(Number).filter((n) => Number.isInteger(n) && n >= 0);
      if (list.length) o.seasons = list;
    }
    onApprove(o);
  };
  return (
    <Modal title={t("requests.editTitle", { title: r.title })} onClose={onClose} testId="approve-dialog"
      footer={<>
        <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
        <Button variant="primary" busy={busy} onClick={submit} data-testid="approve-confirm">{t("requests.approve")}</Button>
      </>}>
      <div className="flex flex-col gap-3">
        <SelectInput label={t("request.profile")} value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
          <option value={0}>{t("request.useDefault")}</option>
          {profiles.data?.filter((p) => p.kind === lk).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("request.rootFolder")} value={rootFolderId} onChange={(e) => setRootFolderId(Number(e.target.value))}>
          <option value={0}>{t("request.useDefault")}</option>
          {roots.data?.filter((f) => f.kind === lk).map((f) => <option key={f.id} value={f.id}>{f.path}</option>)}
        </SelectInput>
        <TextInput label={t("request.tags")} hint={t("request.tagsHint")} value={tags} onChange={(e) => setTags(e.target.value)} />
        {r.kind === "series" && !r.allSeasons ? (
          <TextInput label={t("requests.seasonsField")} hint={t("requests.seasonsHint")} value={seasons} onChange={(e) => setSeasons(e.target.value)} />
        ) : null}
        <ErrorAlert error={error} />
      </div>
    </Modal>
  );
}
