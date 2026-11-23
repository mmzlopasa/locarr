import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { Bell } from "lucide-react";
import { api, queryKeys, unwrap, type AppNotification } from "@/api/client";
import { formatRelative } from "@/components/format";
import { Button, EmptyState, ErrorAlert } from "@/components/ui";

/** Polling interval: notifications come without push (the apps poll the same endpoint). */
export const NOTIFICATION_POLL_MS = 30_000;

export function useNotifications() {
  return useQuery({
    queryKey: queryKeys.notifications,
    queryFn: async () => unwrap(await api.GET("/notifications", { params: { query: { limit: 30 } } })),
    refetchInterval: NOTIFICATION_POLL_MS,
    refetchIntervalInBackground: false,
  });
}

/** One notification as a sentence in the user's language. */
export function describeNotification(t: TFunction, n: AppNotification): string {
  const vars = { title: n.title, actor: n.actor?.username ?? "", message: n.message ?? "" };
  const base = t(`notifications.types.${n.type.replace(".", "_")}`, { ...vars, defaultValue: n.title });
  return n.message && n.type !== "request.comment" && n.type !== "issue.comment" ? `${base} ${t("notifications.detail", vars)}` : base;
}

/** The bell with the unread count and a list of the latest notifications. */
export function NotificationBell({ align = "left", testId = "notif-bell" }: { align?: "left" | "right"; testId?: string }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const list = useNotifications();
  const read = useMutation({
    mutationFn: async (body: { ids?: number[]; all?: boolean }) => unwrap(await api.POST("/notifications/read", { body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.notifications }),
  });
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);
  const unread = list.data?.unread ?? 0;
  const openItem = (n: AppNotification) => {
    if (!n.read) read.mutate({ ids: [n.id] });
    setOpen(false);
    if (n.requestId) void navigate({ to: "/requests/$id", params: { id: String(n.requestId) } });
    else if (n.issueId) void navigate({ to: "/issues/$id", params: { id: String(n.issueId) } });
  };
  return (
    <div className="relative" ref={box}>
      <button type="button" onClick={() => setOpen((v) => !v)} aria-expanded={open} data-testid={testId}
        aria-label={unread ? t("notifications.labelUnread", { n: unread }) : t("notifications.label")}
        className="relative inline-flex size-8 items-center justify-center rounded-ui text-muted hover:bg-surface-2 hover:text-fg">
        <Bell className="size-4" />
        {unread > 0 ? (
          <span data-testid="notif-count"
            className="absolute -right-0.5 -top-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-bold text-accent-ink">
            {unread > 99 ? "99+" : unread}
          </span>
        ) : null}
      </button>
      {open ? (
        <div role="dialog" aria-label={t("notifications.title")} data-testid="notif-panel"
          className={`absolute z-50 mt-2 w-[min(22rem,calc(100vw-2rem))] rounded-xl border border-line bg-surface shadow-2xl ${
            align === "right" ? "right-0" : "left-0 bottom-full mb-2 mt-0"}`}>
          <div className="flex items-center justify-between border-b border-line px-3 py-2">
            <h2 className="text-[13px] font-semibold text-fg">{t("notifications.title")}</h2>
            <Button variant="ghost" className="h-7 px-2 text-[12px]" disabled={unread === 0} busy={read.isPending}
              onClick={() => read.mutate({ all: true })} data-testid="notif-read-all">
              {t("notifications.markAll")}
            </Button>
          </div>
          <ErrorAlert error={list.error} />
          {list.data && list.data.items.length === 0 ? <EmptyState>{t("notifications.none")}</EmptyState> : null}
          <ul className="max-h-[60vh] overflow-y-auto py-1">
            {list.data?.items.map((n) => (
              <li key={n.id}>
                <button type="button" onClick={() => openItem(n)} data-testid="notif-item" data-type={n.type} data-read={n.read}
                  className="flex w-full gap-2 px-3 py-2 text-left hover:bg-surface-2">
                  <span className={`mt-1.5 size-2 shrink-0 rounded-full ${n.read ? "bg-transparent" : "bg-accent"}`} aria-hidden />
                  <span className="min-w-0">
                    <span className={`block text-[12.5px] ${n.read ? "text-muted" : "text-fg"}`}>{describeNotification(t, n)}</span>
                    <span className="block text-[11px] text-faint">{formatRelative(n.createdAt, i18n.language)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
