import { Link, useRouterState } from "@tanstack/react-router";

export type Tab = {
  to: "/system/status" | "/system/health" | "/system/tasks" | "/system/logs" | "/system/backups"
    | "/activity/queue" | "/activity/board" | "/activity/history" | "/activity/blocklist" | "/activity/stats"
    | "/adult" | "/adult/movies" | "/adult/galleries" | "/adult/studios" | "/adult/performers" | "/adult/wanted" | "/adult/calendar" | "/adult/add";
  label: string;
};

/** Section tabs rendered as links (each tab is its own address). */
export function Tabs({ tabs, label }: { tabs: Tab[]; label: string }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname.replace(/\/$/, "") });
  return (
    // Scrolls sideways on narrow screens. overflow-x makes overflow-y scroll
    // too, so nothing may stick out below (the line is an inset shadow, not
    // a border the tabs overlap) and the scrollbar itself stays hidden.
    <nav aria-label={label}
      className="mb-4 flex gap-1 overflow-x-auto overflow-y-hidden overscroll-x-contain shadow-[inset_0_-1px_0_var(--lc-border)] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
      {tabs.map((tab) => {
        const active = pathname === tab.to;
        return (
          <Link
            key={tab.to}
            to={tab.to}
            aria-current={active ? "page" : undefined}
            className={`shrink-0 whitespace-nowrap border-b-2 px-3 py-2 text-[13px] transition-colors ${
              active ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"
            }`}
          >
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
