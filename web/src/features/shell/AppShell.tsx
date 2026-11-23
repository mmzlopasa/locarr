import { useState, type ComponentType } from "react";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Activity,
  ArrowRightLeft,
  AlertTriangle,
  CalendarDays,
  Compass,
  Disc3,
  Download,
  Film,
  Inbox,
  LogOut,
  Menu,
  Radar,
  Search,
  SearchX,
  Server,
  Settings,
  Tv,
  UserRound,
  X,
} from "lucide-react";
import { LanguagePicker } from "@/components/LanguagePicker";
import { Logo } from "@/components/Logo";
import type { User } from "@/api/client";
import { isAdmin, useChangeLanguage, useMe, useSignOut, useSystemInfo } from "@/features/auth/hooks";
import { NotificationBell } from "@/features/notifications/NotificationBell";
import { canManageLibrary } from "@/features/users/permissions";

type NavItem = { to: string; key: string; icon: ComponentType<{ className?: string }>; admin?: boolean; library?: boolean };

const NAV: NavItem[][] = [
  [
    { to: "/discover", key: "discover", icon: Compass },
    { to: "/requests", key: "requests", icon: Inbox },
    { to: "/issues", key: "issues", icon: AlertTriangle },
  ],
  [
    { to: "/movies", key: "movies", icon: Film, library: true },
    { to: "/series", key: "series", icon: Tv, library: true },
    { to: "/music", key: "music", icon: Disc3, library: true },
  ],
  [
    { to: "/activity", key: "activity", icon: Activity, library: true },
    { to: "/calendar", key: "calendar", icon: CalendarDays, library: true },
    { to: "/wanted", key: "wanted", icon: SearchX, library: true },
  ],
  [
    { to: "/search", key: "search", icon: Search, library: true },
    { to: "/indexers", key: "indexers", icon: Radar, admin: true },
    { to: "/downloaders", key: "downloaders", icon: Download, admin: true },
    { to: "/settings", key: "settings", icon: Settings, admin: true },
    { to: "/system", key: "system", icon: Server, admin: true },
    { to: "/migrate", key: "migrate", icon: ArrowRightLeft, admin: true },
  ],
];

/**
 * The signed-in layout: sidebar navigation and the page. When the session ends
 * (the cached user becomes null) nothing is rendered while the app moves to sign-in.
 */
export function AppShell() {
  const { data: me } = useMe();
  return me ? <Shell me={me} /> : null;
}

function Shell({ me }: { me: User }) {
  const { t } = useTranslation();
  const { data: info } = useSystemInfo();
  const changeLanguage = useChangeLanguage();
  const signOut = useSignOut();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const doSignOut = () =>
    signOut.mutate(undefined, { onSettled: () => void navigate({ to: "/login", search: {} }) });

  const sidebar = (
    <nav aria-label={t("nav.label")} className="flex h-full flex-col">
      <div className="flex h-12 items-center justify-between border-b border-line px-3">
        <Link to="/" className="truncate" onClick={() => setOpen(false)}>
          <Logo />
        </Link>
        <button type="button" className="text-muted hover:text-fg md:hidden" onClick={() => setOpen(false)} aria-label={t("nav.close")}>
          <X className="size-4" />
        </button>
      </div>
      <div className="flex-1 overflow-y-auto px-2 py-2">
        {NAV.map((group) => group.filter((item) => (!item.admin || isAdmin(me)) && (!item.library || canManageLibrary(me))))
          .filter((group) => group.length > 0).map((group, i) => (
          <ul key={i} className="mb-2 border-b border-line pb-2 last:border-b-0">
            {group
              .map((item) => {
                const active = pathname === item.to || pathname.startsWith(item.to + "/");
                const Icon = item.icon;
                return (
                  <li key={item.to}>
                    <Link
                      to={item.to}
                      onClick={() => setOpen(false)}
                      aria-current={active ? "page" : undefined}
                      className={`flex h-8 items-center gap-2.5 rounded-ui px-2.5 text-[13px] transition-colors ${
                        active ? "bg-surface-3 text-fg" : "text-muted hover:bg-surface-2 hover:text-fg"
                      }`}
                    >
                      <Icon className={`size-4 ${active ? "text-accent" : ""}`} />
                      {t(`nav.${item.key}`)}
                    </Link>
                  </li>
                );
              })}
          </ul>
        ))}
      </div>
      <div className="border-t border-line p-2">
        <Link
          to="/account"
          onClick={() => setOpen(false)}
          data-testid="nav-account"
          className={`flex items-center gap-2.5 rounded-ui px-2.5 py-1.5 hover:bg-surface-2 ${pathname === "/account" ? "bg-surface-3" : ""}`}
        >
          <UserRound className="size-4 text-muted" />
          <span className="flex min-w-0 flex-col">
            <span className="truncate text-[13px] text-fg" data-testid="current-username">{me.username}</span>
            <span className="text-[11px] text-faint">{t(`roles.${me.role}`)}</span>
          </span>
        </Link>
        <div className="mt-1 flex items-center justify-between gap-2 px-1">
          <span className="flex items-center gap-1">
            <span className="hidden md:inline-flex"><NotificationBell align="left" /></span>
            <LanguagePicker onChange={changeLanguage} />
          </span>
          <button
            type="button"
            onClick={doSignOut}
            className="inline-flex h-7 items-center gap-1.5 rounded-ui px-2 text-[12px] text-muted hover:bg-surface-2 hover:text-fg"
          >
            <LogOut className="size-3.5" />
            {t("nav.signOut")}
          </button>
        </div>
      </div>
    </nav>
  );

  return (
    <div className="flex min-h-full">
      <aside className="sticky top-0 hidden h-screen w-56 shrink-0 border-r border-line bg-surface md:block">{sidebar}</aside>
      {open ? (
        <div className="fixed inset-0 z-40 md:hidden">
          <button type="button" aria-label={t("nav.close")} className="absolute inset-0 bg-black/60" onClick={() => setOpen(false)} />
          <aside className="relative h-full w-64 border-r border-line bg-surface">{sidebar}</aside>
        </div>
      ) : null}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-12 items-center gap-3 border-b border-line bg-surface px-4 md:hidden">
          <button type="button" onClick={() => setOpen(true)} aria-label={t("nav.open")} className="text-muted hover:text-fg">
            <Menu className="size-5" />
          </button>
          <span className="flex-1 truncate text-[14px] font-semibold">{info?.instanceName ?? "Locarr"}</span>
          <NotificationBell align="right" testId="notif-bell-mobile" />
        </header>
        <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-5 md:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
