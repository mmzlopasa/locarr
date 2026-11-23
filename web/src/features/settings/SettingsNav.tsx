import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowUpRight, ChevronDown, ChevronRight, Search, Settings, X } from "lucide-react";
import type { User } from "@/api/client";
import {
  activeItem,
  filterNav,
  loadCollapsed,
  saveCollapsed,
  visibleNav,
  type SettingsNavGroup,
} from "./settingsNav";

function usePathname() {
  return useRouterState({ select: (s) => s.location.pathname });
}

/**
 * The settings sections: a filter box and the grouped pages. Arrow keys move
 * between the filter and the links; Enter in the filter opens the first match.
 */
export function SettingsNav({ me, onNavigate, focusActive, idPrefix }:
  { me: User; onNavigate?: () => void; focusActive?: boolean; idPrefix: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const pathname = usePathname();
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<string[]>(loadCollapsed);
  const root = useRef<HTMLElement>(null);

  const all = useMemo(() => visibleNav(me), [me]);
  const groups = useMemo(() => filterNav(all, query, t), [all, query, t]);
  const active = activeItem(all, pathname);
  const filtering = query.trim() !== "";

  useEffect(() => {
    if (!focusActive) return;
    const el = root.current?.querySelector<HTMLElement>('a[aria-current="page"]') ?? root.current?.querySelector<HTMLElement>("a[data-nav-item]");
    el?.focus();
  }, [focusActive]);

  const toggle = (key: string) => {
    const next = collapsed.includes(key) ? collapsed.filter((k) => k !== key) : [...collapsed, key];
    setCollapsed(next);
    saveCollapsed(next);
  };

  const focusables = () => Array.from(root.current?.querySelectorAll<HTMLElement>("input[type=search], a[data-nav-item]") ?? []);
  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) return;
    const list = focusables();
    if (list.length === 0) return;
    const at = list.indexOf(document.activeElement as HTMLElement);
    const isInput = document.activeElement instanceof HTMLInputElement;
    if ((e.key === "Home" || e.key === "End") && isInput) return; // caret movement in the filter
    e.preventDefault();
    const next = e.key === "Home" ? 0 : e.key === "End" ? list.length - 1
      : e.key === "ArrowDown" ? Math.min(at + 1, list.length - 1) : Math.max(at - 1, 0);
    list[next]?.focus();
  };

  const onFilterKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape" && query) {
      e.stopPropagation();
      setQuery("");
    } else if (e.key === "Enter") {
      const first = groups[0]?.items[0];
      if (first) {
        e.preventDefault();
        setQuery("");
        void navigate({ to: first.to });
        onNavigate?.();
      }
    }
  };

  const total = groups.reduce((n, g) => n + g.items.length, 0);

  return (
    <nav ref={root} aria-label={t("settings.nav.label")} onKeyDown={onKeyDown} data-testid={`${idPrefix}-settings-nav`}>
      <div className="relative mb-3">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-faint" aria-hidden />
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onFilterKey}
          aria-label={t("settings.nav.filter")}
          aria-controls={`${idPrefix}-settings-groups`}
          placeholder={t("settings.nav.filterPlaceholder")}
          data-testid="settings-nav-filter"
          className="h-8 w-full rounded-ui border border-line bg-surface pr-2 pl-8 text-[13px] text-fg placeholder:text-faint hover:border-line-strong focus:border-line-strong focus:outline-none focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-[var(--lc-focus)]"
        />
      </div>
      <div id={`${idPrefix}-settings-groups`}>
        {groups.map((g) => (
          <Group key={g.key} group={g} idPrefix={idPrefix} open={filtering || !collapsed.includes(g.key)}
            onToggle={filtering ? undefined : () => toggle(g.key)} activeTo={active?.to} onNavigate={onNavigate} />
        ))}
        {total === 0 ? (
          <p className="px-2.5 py-3 text-[12px] text-muted" role="status">{t("settings.nav.noMatches", { query: query.trim() })}</p>
        ) : null}
      </div>
    </nav>
  );
}

function Group({ group, open, onToggle, activeTo, onNavigate, idPrefix }:
  { group: SettingsNavGroup; open: boolean; onToggle?: () => void; activeTo?: string; onNavigate?: () => void; idPrefix: string }) {
  const { t } = useTranslation();
  const listId = `${idPrefix}-settings-group-${group.key}`;
  // A folded group still shows the page you are on, so the current place never disappears.
  const items = open ? group.items : group.items.filter((i) => i.to === activeTo);
  const label = t(`settings.nav.groups.${group.key}`);
  return (
    <div className="mb-3 last:mb-0">
      <h2 className="m-0">
        <button
          type="button"
          onClick={onToggle}
          disabled={!onToggle}
          aria-expanded={open}
          aria-controls={listId}
          className="group flex h-7 w-full items-center gap-1.5 rounded-ui px-2.5 text-left text-[11px] font-medium tracking-[0.06em] text-faint uppercase hover:text-muted disabled:cursor-default disabled:hover:text-faint"
        >
          <span className="min-w-0 flex-1 truncate">{label}</span>
          {!open ? <span className="tabular-nums tracking-normal normal-case">{group.items.length}</span> : null}
          {onToggle ? (open ? <ChevronDown className="size-3.5 shrink-0" aria-hidden /> : <ChevronRight className="size-3.5 shrink-0" aria-hidden />) : null}
        </button>
      </h2>
      <ul id={listId} className="m-0 list-none p-0">
        {items.map((item) => {
          const active = item.to === activeTo;
          const Icon = item.icon;
          return (
            <li key={item.to}>
              <Link
                to={item.to}
                activeOptions={{ exact: true, includeSearch: false }}
                data-nav-item
                data-testid={`settings-nav-${item.key}`}
                onClick={onNavigate}
                aria-current={active ? "page" : undefined}
                className={`relative flex min-h-8 items-center gap-2.5 rounded-ui px-2.5 py-1.5 text-[13px] leading-tight transition-colors ${
                  active ? "bg-surface-3 text-fg before:absolute before:inset-y-1.5 before:left-0 before:w-0.5 before:rounded-full before:bg-accent"
                    : "text-muted hover:bg-surface-2 hover:text-fg"
                }`}
              >
                <Icon className={`size-4 shrink-0 ${active ? "text-accent" : ""}`} />
                <span className="min-w-0 flex-1">{t(`settings.nav.items.${item.key}`)}</span>
                {item.external ? (
                  <ArrowUpRight className="size-3.5 shrink-0 text-faint" aria-label={t("settings.nav.leaves")} />
                ) : null}
              </Link>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

/**
 * Below the wide layout: one bar that says where you are ("Settings › Custom formats")
 * and opens the sections as a sheet.
 */
export function SettingsNavBar({ me }: { me: User }) {
  const { t } = useTranslation();
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const active = activeItem(visibleNav(me), pathname);

  const close = () => {
    setOpen(false);
    button.current?.focus();
  };

  useEffect(() => {
    if (!open) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = overflow;
    };
  }, [open]);

  return (
    <>
      <button
        ref={button}
        type="button"
        onClick={() => setOpen(true)}
        aria-haspopup="dialog"
        aria-expanded={open}
        data-testid="settings-nav-toggle"
        className="flex h-10 w-full items-center gap-2 rounded-ui border border-line bg-surface px-3 text-left text-[13px] hover:border-line-strong"
      >
        <Settings className="size-4 shrink-0 text-muted" aria-hidden />
        <span className="shrink-0 text-muted">{t("settings.nav.title")}</span>
        <ChevronRight className="size-3.5 shrink-0 text-faint" aria-hidden />
        <span className="min-w-0 flex-1 truncate font-medium text-fg">{active ? t(`settings.nav.items.${active.key}`) : ""}</span>
        <span className="sr-only">{t("settings.nav.open")}</span>
        <ChevronDown className="size-4 shrink-0 text-muted" aria-hidden />
      </button>
      {/* In a portal: the sticky bar's backdrop blur would otherwise become the sheet's containing block. */}
      {open ? createPortal(
        <div className="fixed inset-0 z-50" role="dialog" aria-modal="true" aria-label={t("settings.nav.title")} data-testid="settings-nav-sheet">
          <button type="button" tabIndex={-1} aria-label={t("settings.nav.close")} className="absolute inset-0 bg-black/60 backdrop-blur-[2px]" onClick={close} />
          <div className="absolute inset-x-0 bottom-0 flex max-h-[85vh] flex-col rounded-t-xl border border-line bg-surface shadow-2xl sm:inset-y-0 sm:right-auto sm:left-0 sm:max-h-none sm:w-80 sm:rounded-none sm:border-y-0 sm:border-l-0">
            <div className="flex h-12 shrink-0 items-center justify-between border-b border-line px-4">
              <h2 className="text-[14px] font-semibold text-fg">{t("settings.nav.title")}</h2>
              <button type="button" onClick={close} className="text-muted hover:text-fg" aria-label={t("settings.nav.close")}>
                <X className="size-4" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
              <SettingsNav me={me} idPrefix="sheet" focusActive onNavigate={() => setOpen(false)} />
            </div>
          </div>
        </div>,
        document.body,
      ) : null}
    </>
  );
}
