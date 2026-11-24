import type { ComponentType } from "react";
import {
  Activity,
  ArrowRightLeft,
  BellRing,
  Captions,
  Clapperboard,
  Download,
  EyeOff,
  FileCog,
  FlaskConical,
  FolderOpen,
  FolderTree,
  Gauge,
  Inbox,
  ListFilter,
  ListPlus,
  Radar,
  Ruler,
  Server,
  SlidersHorizontal,
  Sparkles,
  Tags,
  UsersRound,
} from "lucide-react";
import type { User } from "@/api/client";

/** Every address the settings navigation links to (routes are unchanged, so deep links keep working). */
export type SettingsPath =
  | "/settings" | "/settings/media" | "/settings/mediamanagement" | "/settings/metadata" | "/settings/importlists"
  | "/settings/metadataprofiles" | "/settings/subtitles" | "/settings/profiles" | "/settings/quality"
  | "/settings/customformats" | "/settings/rules" | "/settings/tester" | "/settings/requests" | "/settings/users"
  | "/settings/mediaserver" | "/settings/notifications" | "/settings/system" | "/settings/adult"
  | "/indexers" | "/downloaders" | "/system/status" | "/migrate";

export type SettingsNavItem = {
  /** i18n key under settings.nav.items (label) and settings.nav.keywords (extra search words). */
  key: string;
  to: SettingsPath;
  icon: ComponentType<{ className?: string }>;
  /** Who may see it. Everything under /settings is for administrators today. */
  visible?: (me: User) => boolean;
  /** Links out of the settings area (shown with an arrow). */
  external?: boolean;
};

export type SettingsNavGroup = { key: string; items: SettingsNavItem[] };

const admin = (me: User) => me.role === "admin";

export const SETTINGS_NAV: SettingsNavGroup[] = [
  {
    key: "server",
    items: [
      { key: "general", to: "/settings", icon: SlidersHorizontal, visible: admin },
      { key: "monitoring", to: "/settings/system", icon: Activity, visible: admin },
    ],
  },
  {
    key: "library",
    items: [
      { key: "media", to: "/settings/media", icon: FolderOpen, visible: admin },
      { key: "mediaManagement", to: "/settings/mediamanagement", icon: FolderTree, visible: admin },
      { key: "metadata", to: "/settings/metadata", icon: FileCog, visible: admin },
      { key: "metadataProfiles", to: "/settings/metadataprofiles", icon: Tags, visible: admin },
      { key: "importLists", to: "/settings/importlists", icon: ListPlus, visible: admin },
      { key: "subtitles", to: "/settings/subtitles", icon: Captions, visible: admin },
      { key: "adult", to: "/settings/adult", icon: EyeOff, visible: admin },
    ],
  },
  {
    key: "quality",
    items: [
      { key: "profiles", to: "/settings/profiles", icon: Gauge, visible: admin },
      { key: "quality", to: "/settings/quality", icon: Ruler, visible: admin },
      { key: "customFormats", to: "/settings/customformats", icon: Sparkles, visible: admin },
      { key: "rules", to: "/settings/rules", icon: ListFilter, visible: admin },
      { key: "tester", to: "/settings/tester", icon: FlaskConical, visible: admin },
    ],
  },
  {
    key: "people",
    items: [
      { key: "requests", to: "/settings/requests", icon: Inbox, visible: admin },
      { key: "users", to: "/settings/users", icon: UsersRound, visible: admin },
    ],
  },
  {
    key: "integrations",
    items: [
      { key: "mediaServer", to: "/settings/mediaserver", icon: Clapperboard, visible: admin },
      { key: "notifications", to: "/settings/notifications", icon: BellRing, visible: admin },
    ],
  },
  {
    key: "elsewhere",
    items: [
      { key: "indexers", to: "/indexers", icon: Radar, visible: admin, external: true },
      { key: "downloaders", to: "/downloaders", icon: Download, visible: admin, external: true },
      { key: "system", to: "/system/status", icon: Server, visible: admin, external: true },
      { key: "migrate", to: "/migrate", icon: ArrowRightLeft, visible: admin, external: true },
    ],
  },
];

/** Lower-case and without diacritics, so "cesto" finds "često" and "prilagodeni" finds "prilagođeni". */
export function normalize(s: string): string {
  // "đ" is a letter of its own, not "d" with a mark, so NFD leaves it alone.
  return s.normalize("NFD").replace(/\p{Diacritic}/gu, "").toLowerCase().replace(/đ/g, "d").trim();
}

type Translate = (key: string) => string;

/** The groups and items this user may see. */
export function visibleNav(me: User, groups: SettingsNavGroup[] = SETTINGS_NAV): SettingsNavGroup[] {
  return groups
    .map((g) => ({ ...g, items: g.items.filter((i) => !i.visible || i.visible(me)) }))
    .filter((g) => g.items.length > 0);
}

/**
 * Items whose label, group name or keywords contain every word of the query.
 * An empty query keeps everything.
 */
export function filterNav(groups: SettingsNavGroup[], query: string, t: Translate): SettingsNavGroup[] {
  const words = normalize(query).split(/\s+/).filter(Boolean);
  if (words.length === 0) return groups;
  return groups
    .map((g) => {
      const groupLabel = t(`settings.nav.groups.${g.key}`);
      return {
        ...g,
        items: g.items.filter((i) => {
          const hay = normalize([t(`settings.nav.items.${i.key}`), t(`settings.nav.keywords.${i.key}`), groupLabel].join(" "));
          return words.every((w) => hay.includes(w));
        }),
      };
    })
    .filter((g) => g.items.length > 0);
}

/** The item for the current address: the longest matching path wins, so /settings is only "General" itself. */
export function activeItem(groups: SettingsNavGroup[], pathname: string): SettingsNavItem | undefined {
  const path = pathname.replace(/\/+$/, "") || "/";
  let best: SettingsNavItem | undefined;
  for (const g of groups) {
    for (const i of g.items) {
      const exact = path === i.to;
      const nested = i.to !== "/settings" && path.startsWith(i.to + "/");
      if ((exact || nested) && (!best || i.to.length > best.to.length)) best = i;
    }
  }
  return best;
}

const COLLAPSED_KEY = "locarr.settingsNav.collapsed";

/** Groups the user folded away; storage may be unavailable (private mode), then nothing is folded. */
export function loadCollapsed(): string[] {
  try {
    const raw = localStorage.getItem(COLLAPSED_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

export function saveCollapsed(keys: string[]) {
  try {
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(keys));
  } catch {
    // Not remembered; the groups still fold for this visit.
  }
}
