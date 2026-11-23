import { useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, ExternalLink, RefreshCw } from "lucide-react";
import { api, queryKeys, unwrap, type BoardActionResult, type BoardGroup, type BoardKind } from "@/api/client";
import { formatRelative } from "@/components/format";
import { Alert, Badge, Button, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import {
  BOARD_ACTIONS,
  BOARD_KINDS,
  DOCS_BASE,
  describeBoardItem,
  describeReason,
  fixTarget,
  itemHref,
  suggestedActions,
  type BoardActionName,
} from "./board";

const PAGE = 100;

type Tone = "success" | "info" | "warning" | "danger";

/** "Why didn't it land?": every monitored missing item grouped by its current reason. */
export function BoardPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [kind, setKind] = useState<BoardKind | "">("");
  const [open, setOpen] = useState<string | null>(null);
  const board = useQuery({
    queryKey: queryKeys.board(kind),
    queryFn: async () => unwrap(await api.GET("/board", { params: { query: kind ? { kind } : {} } })),
    refetchInterval: 15_000,
  });
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/board/refresh")),
    onSuccess: () => setTimeout(() => void qc.invalidateQueries({ queryKey: ["board"] }), 1500),
  });

  return (
    <div className="flex flex-col gap-4">
      <Panel
        title={t("board.title")}
        description={t("board.subtitle")}
        actions={
          <>
            <div className="w-40">
              <SelectInput label={t("board.kind")} value={kind} onChange={(e) => setKind(e.target.value as BoardKind | "")}
                data-testid="board-kind">
                <option value="">{t("board.kinds.all")}</option>
                {BOARD_KINDS.map((k) => (
                  <option key={k} value={k}>{t(`board.kinds.${k}`)}</option>
                ))}
              </SelectInput>
            </div>
            <Button className="self-end" busy={refresh.isPending} onClick={() => refresh.mutate()} data-testid="board-refresh">
              <RefreshCw className="size-3.5" />
              {t("board.recompute")}
            </Button>
          </>
        }
      >
        {board.isPending ? <Spinner /> : null}
        <ErrorAlert error={board.error ?? refresh.error} />
        {board.data ? (
          <p className="text-[12px] text-faint" data-testid="board-total">
            {t("board.total", { count: board.data.total })}
            {board.data.updatedAt ? ` · ${t("board.updated", { when: formatRelative(board.data.updatedAt, i18n.language) })}` : ""}
          </p>
        ) : null}
        {board.data && board.data.groups.length === 0 ? <EmptyState>{t("board.empty")}</EmptyState> : null}
      </Panel>
      {board.data?.groups.map((g) => (
        <GroupCard key={`${g.code}|${g.fix}`} group={g} kind={kind} open={open === `${g.code}|${g.fix}`}
          onToggle={() => setOpen(open === `${g.code}|${g.fix}` ? null : `${g.code}|${g.fix}`)} />
      ))}
    </div>
  );
}

function GroupCard({ group, kind, open, onToggle }: { group: BoardGroup; kind: BoardKind | ""; open: boolean; onToggle: () => void }) {
  const { t } = useTranslation();
  const fix = fixTarget(group.fix);
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <section className="rounded-ui border border-line bg-surface" data-testid={`board-group-${group.code}`} data-fix={group.fix}>
      <header className="flex flex-wrap items-start justify-between gap-3 px-4 py-3">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 flex-1 items-start gap-2 text-left">
          <Chevron className="mt-0.5 size-4 shrink-0 text-muted" />
          <span className="min-w-0">
            <span className="flex flex-wrap items-center gap-2">
              <span className="text-[14px] font-semibold text-fg">{t(`board.groups.${group.code}.title`, { defaultValue: group.code })}</span>
              <Badge tone={group.tone as Tone}>{group.count}</Badge>
              {BOARD_KINDS.filter((k) => group.kinds[k] > 0).map((k) => (
                <span key={k} className="text-[12px] text-faint">{t(`board.kindCount.${k}`, { count: group.kinds[k] })}</span>
              ))}
            </span>
            <span className="mt-0.5 block text-[12px] text-muted">
              {t(`board.groups.${group.code}.hint`, { defaultValue: "", searches: group.searches })}
            </span>
          </span>
        </button>
        {fix ? (
          <span className="flex items-center gap-3 text-[12px]">
            {fix.route ? (
              <Link to={fix.route} className="text-info hover:underline" data-testid={`board-fix-${group.code}`}>
                {t(`board.fix.${group.fix}`, { defaultValue: t("board.fixIt") })}
              </Link>
            ) : null}
            {fix.docs ? (
              <a href={`${DOCS_BASE}#${fix.docs}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-muted hover:text-fg">
                {t("board.howToFix")}
                <ExternalLink className="size-3" />
              </a>
            ) : null}
          </span>
        ) : null}
      </header>
      {group.reasons.length > 0 ? (
        <div className="flex flex-wrap gap-1.5 border-t border-line px-4 py-2" data-testid={`board-reasons-${group.code}`}>
          <span className="text-[12px] text-faint">{t("board.topReasons")}</span>
          {group.reasons.map((r) => (
            <span key={r.code} className="rounded border border-line-strong bg-surface-2 px-1.5 text-[12px] text-muted" title={r.example}>
              {describeReason(t, r)} · {r.count}
            </span>
          ))}
        </div>
      ) : null}
      {open ? <GroupItems code={group.code} fix={group.fix} kind={kind} /> : null}
    </section>
  );
}

function GroupItems({ code, fix, kind }: { code: string; fix: string; kind: BoardKind | "" }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [q, setQ] = useState("");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [profileId, setProfileId] = useState<number>(0);
  const [result, setResult] = useState<BoardActionResult | null>(null);
  const items = useQuery({
    queryKey: queryKeys.boardItems(`${code}|${fix}`, kind, q, offset),
    queryFn: async () =>
      unwrap(await api.GET("/board/items", { params: { query: { code, fix, ...(kind ? { kind } : {}), ...(q ? { q } : {}), offset, limit: PAGE } } })),
    placeholderData: keepPreviousData,
  });
  const profiles = useQuery({
    queryKey: queryKeys.qualityProfiles,
    queryFn: async () => unwrap(await api.GET("/qualityprofiles")),
  });
  const act = useMutation({
    mutationFn: async (action: BoardActionName) =>
      unwrap(await api.POST("/board/actions", {
        body: { action, keys: [...selected], ...(action === "change_profile" && profileId ? { qualityProfileId: profileId } : {}) },
      })),
    onSuccess: (res) => {
      setResult(res);
      setSelected(new Set());
      void qc.invalidateQueries({ queryKey: ["board"] });
    },
  });
  const list = items.data?.items ?? [];
  const allOnPage = list.length > 0 && list.every((i) => selected.has(i.key));
  const toggle = (key: string) => {
    const next = new Set(selected);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelected(next);
  };
  const order = [...suggestedActions(code), ...BOARD_ACTIONS.filter((a) => !suggestedActions(code).includes(a))];

  return (
    <div className="border-t border-line px-4 py-3">
      <div className="mb-3 flex flex-wrap items-end gap-2">
        <div className="w-56">
          <TextInput label={t("board.filter")} value={q} onChange={(e) => {
            setQ(e.target.value);
            setOffset(0);
          }} />
        </div>
        <div className="flex flex-wrap items-end gap-1.5" role="group" aria-label={t("board.actionsLabel")}>
          {order.map((a) => (
            <Button key={a} disabled={selected.size === 0 || (a === "change_profile" && !profileId)} busy={act.isPending && act.variables === a}
              onClick={() => act.mutate(a)} data-testid={`board-action-${a}`}
              variant={a === suggestedActions(code)[0] ? "primary" : "secondary"}>
              {t(`board.actions.${a}`)}
            </Button>
          ))}
          <div className="w-44">
            <SelectInput label={t("board.profile")} value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
              <option value={0}>{t("board.pickProfile")}</option>
              {profiles.data?.map((p) => (
                <option key={p.id} value={p.id}>{p.name} ({t(`mediaKinds.${p.kind}`)})</option>
              ))}
            </SelectInput>
          </div>
        </div>
      </div>
      <ErrorAlert error={items.error ?? act.error} />
      {result ? (
        <Alert tone={result.failed.length ? "warning" : "success"}>
          <span data-testid="board-action-result">{t("board.result", { done: result.done, failed: result.failed.length })}</span>
          {result.failed.length ? (
            <ul className="mt-1 list-disc pl-5 text-[12px]">
              {result.failed.slice(0, 10).map((f) => (
                <li key={f.key}>{f.key}: {t(`errors.${f.code}`, { defaultValue: f.message })}</li>
              ))}
            </ul>
          ) : null}
        </Alert>
      ) : null}
      {items.isPending ? <Spinner /> : null}
      {items.data && list.length === 0 ? <EmptyState>{t("board.noItems")}</EmptyState> : null}
      {list.length > 0 ? (
        <>
          <label className="mb-1 flex items-center gap-2 text-[12px] text-muted">
            <input type="checkbox" className="size-4 accent-[var(--lc-accent)]" checked={allOnPage} onChange={() => {
              const next = new Set(selected);
              for (const i of list) {
                if (allOnPage) next.delete(i.key);
                else next.add(i.key);
              }
              setSelected(next);
            }} />
            {t("board.selectPage", { count: selected.size })}
          </label>
          <ul className="divide-y divide-line">
            {list.map((i) => {
              const href = itemHref(i);
              return (
                <li key={i.key} className="flex items-start gap-2 py-2" data-testid={`board-item-${i.key}`}>
                  <input type="checkbox" className="mt-1 size-4 accent-[var(--lc-accent)]" checked={selected.has(i.key)}
                    onChange={() => toggle(i.key)} aria-label={i.title} />
                  <div className="min-w-0 flex-1">
                    <p className="flex flex-wrap items-center gap-2 text-[13px]">
                      {href ? <Link to={href} className="text-fg hover:underline">{i.title}</Link> : <span className="text-fg">{i.title}</span>}
                      <Badge>{t(`board.kinds.${i.kind}`)}</Badge>
                      {i.searches > 0 ? <span className="text-[12px] text-faint">{t("board.searches", { count: i.searches })}</span> : null}
                    </p>
                    <p className="mt-0.5 break-words text-[12px] text-muted">{describeBoardItem(t, i, i18n.language)}</p>
                    {i.reasons.length > 0 ? (
                      <p className="mt-0.5 text-[12px] text-faint">
                        {i.reasons.map((r) => `${describeReason(t, r)} (${r.count})`).join(" · ")}
                      </p>
                    ) : null}
                  </div>
                </li>
              );
            })}
          </ul>
          <div className="mt-2 flex items-center justify-between text-[12px] text-faint">
            <span>{t("board.range", { from: offset + 1, to: offset + list.length, total: items.data?.total ?? 0 })}</span>
            <span className="flex gap-1">
              <Button variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>{t("board.prev")}</Button>
              <Button variant="ghost" disabled={offset + PAGE >= (items.data?.total ?? 0)} onClick={() => setOffset(offset + PAGE)}>
                {t("board.next")}
              </Button>
            </span>
          </div>
        </>
      ) : null}
    </div>
  );
}
