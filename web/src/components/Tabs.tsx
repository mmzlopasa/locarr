import { Link, useRouterState } from "@tanstack/react-router";

export type Tab = {
  to: "/settings" | "/settings/media" | "/settings/users" | "/settings/profiles" | "/settings/quality" | "/settings/customformats"
    | "/settings/rules" | "/settings/tester" | "/settings/mediamanagement" | "/settings/metadata" | "/settings/importlists" | "/settings/metadataprofiles" | "/settings/subtitles" | "/system/status" | "/system/tasks" | "/activity/queue" | "/activity/blocklist";
  label: string;
};

/** Section tabs rendered as links (each tab is its own address). */
export function Tabs({ tabs, label }: { tabs: Tab[]; label: string }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname.replace(/\/$/, "") });
  return (
    <nav aria-label={label} className="mb-4 flex gap-1 overflow-x-auto border-b border-line">
      {tabs.map((tab) => {
        const active = pathname === tab.to;
        return (
          <Link
            key={tab.to}
            to={tab.to}
            aria-current={active ? "page" : undefined}
            className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-[13px] transition-colors ${
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
