import { useMemo, useState, type ReactNode } from "react";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, Images, Plus, RefreshCw, Search, Settings2, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Tabs } from "@/components/Tabs";
import { formatBytes, formatDateTime, formatDuration } from "@/components/format";
import { Alert, Badge, Button, Checkbox, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { HistoryTab } from "@/features/movies/HistoryTab";
import { InteractiveSearch, type ReleaseSource } from "@/features/movies/InteractiveSearch";
import { useQualityProfiles } from "@/features/movies/common";
import {
  adultKey, useAdultCalendar, useAdultInvalidate, useAdultItem, useAdultLookup, useAdultPerformer, useAdultPerformers, useAdultPreferences,
  useAddGallery, useAdultItems, useAdultRootFolders, useAdultStudio, useAdultStudios, useRemoveItem, useRemovePerformer, useRemoveStudio, useSetBlur,
  type AdultLookupResult, type LookupQuery, type RemoveChoice,
} from "./api";
import { BlurImage } from "./BlurImage";
import { GalleryGrid, NewGalleryForm } from "./Gallery";
import { EmptyAdult, ItemBrowser, RemoveChoices, RemoveDialog, StatusBadge, linkButton, primaryLink, useCanManageAdult } from "./components";
import { LOOKUP_SORTS, PREF, loadPref, savePref, type LookupSort } from "./logic";

/** The adult section: its own pages, never mixed into the rest of the app. */
export function AdultLayout() {
  const { t } = useTranslation();
  const prefs = useAdultPreferences();
  const setBlur = useSetBlur();
  // Local value so the checkbox flips on the click itself, before the save returns.
  const [blur, setBlurLocal] = useState<boolean | null>(null);
  return (
    <div data-testid="adult-section" className="min-w-0">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-[18px] font-semibold tracking-tight text-fg">{t("adult.title")}</h1>
          <p className="mt-0.5 text-[13px] text-muted">{t("adult.description")}</p>
        </div>
        <Checkbox label={t("adult.blur")} checked={blur ?? prefs.data?.blur ?? true} disabled={!prefs.data}
          onChange={(e) => {
            const next = e.target.checked;
            setBlurLocal(next);
            setBlur.mutate(next, { onError: () => setBlurLocal(null) });
          }} />
      </div>
      <Tabs label={t("adult.title")} tabs={[
        { to: "/adult", label: t("adult.tabs.scenes") },
        { to: "/adult/movies", label: t("adult.tabs.movies") },
        { to: "/adult/galleries", label: t("adult.tabs.galleries") },
        { to: "/adult/studios", label: t("adult.tabs.studios") },
        { to: "/adult/performers", label: t("adult.tabs.performers") },
        { to: "/adult/wanted", label: t("adult.tabs.wanted") },
        { to: "/adult/calendar", label: t("adult.tabs.calendar") },
        { to: "/adult/add", label: t("adult.tabs.add") },
      ]} />
      <Outlet />
    </div>
  );
}

/** Empty library: what to do next. */
function StartHere({ title }: { title: string }) {
  const { t } = useTranslation();
  const canManage = useCanManageAdult();
  const roots = useAdultRootFolders();
  const noRoot = roots.data?.length === 0;
  return (
    <EmptyAdult title={title} hint={noRoot ? t("adult.noRoot") : t("adult.emptyHint")}
      actions={canManage ? (
        <>
          {noRoot ? <Link to="/settings/adult" className={primaryLink}><Settings2 className="size-3.5" aria-hidden /> {t("adult.addRoot")}</Link> : null}
          <Link to="/adult/add" className={noRoot ? linkButton : primaryLink} data-testid="adult-empty-add">
            <Plus className="size-3.5" aria-hidden /> {t("adult.addFollow")}
          </Link>
        </>
      ) : undefined} />
  );
}

export const AdultScenesPage = () => {
  const { t } = useTranslation();
  return <ItemBrowser base={{ kind: "scene" }} empty={<StartHere title={t("adult.emptyScenes")} />} />;
};

export const AdultMoviesPage = () => {
  const { t } = useTranslation();
  return <ItemBrowser base={{ kind: "movie" }} empty={<StartHere title={t("adult.emptyMovies")} />} />;
};

export function AdultGalleriesPage() {
  const { t } = useTranslation();
  const canManage = useCanManageAdult();
  const [creating, setCreating] = useState(false);
  return (
    <div>
      {canManage ? (
        <div className="mb-3 flex justify-end">
          <Button onClick={() => setCreating(true)} data-testid="adult-gallery-new-open"><Plus className="size-3.5" aria-hidden /> {t("adult.gallery.new")}</Button>
        </div>
      ) : null}
      <ItemBrowser base={{ kind: "gallery" }} testId="adult-galleries"
        empty={<EmptyAdult title={t("adult.emptyGalleries")} hint={t("adult.emptyGalleriesHint")} />} />
      {creating ? <NewGalleryForm onClose={() => setCreating(false)} /> : null}
    </div>
  );
}

export const AdultWantedPage = () => {
  const { t } = useTranslation();
  return (
    <ItemBrowser base={{ missing: true }} showStatusFilter={false}
      empty={<EmptyAdult title={t("adult.emptyWanted")} hint={t("adult.emptyWantedHint")} />} />
  );
};

function isoDay(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function AdultCalendarPage() {
  const { t, i18n } = useTranslation();
  const [offset, setOffset] = useState(0);
  const now = new Date();
  const first = new Date(now.getFullYear(), now.getMonth() + offset, 1);
  const last = new Date(now.getFullYear(), now.getMonth() + offset + 1, 0);
  const cal = useAdultCalendar(isoDay(first), isoDay(last));
  const days = useMemo(() => {
    const m = new Map<string, NonNullable<typeof cal.data>>();
    for (const it of [...(cal.data ?? [])].sort((a, b) => (a.releaseDate ?? "").localeCompare(b.releaseDate ?? ""))) {
      const d = it.releaseDate ?? "";
      m.set(d, [...(m.get(d) ?? []), it]);
    }
    return [...m.entries()];
  }, [cal.data]);
  const month = new Intl.DateTimeFormat(i18n.language, { month: "long", year: "numeric" }).format(first);
  const dayFmt = new Intl.DateTimeFormat(i18n.language, { weekday: "short", day: "numeric", month: "short" });
  const today = isoDay(now);
  return (
    <div>
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-[14px] font-semibold capitalize text-fg" data-testid="adult-calendar-month">{month}</h2>
        <div className="flex gap-1">
          <Button variant="ghost" onClick={() => setOffset((o) => o - 1)} aria-label={t("adult.calendar.previous")}><ChevronLeft className="size-4" aria-hidden /></Button>
          {offset !== 0 ? <Button variant="ghost" onClick={() => setOffset(0)}>{t("adult.calendar.today")}</Button> : null}
          <Button variant="ghost" onClick={() => setOffset((o) => o + 1)} aria-label={t("adult.calendar.next")}><ChevronRight className="size-4" aria-hidden /></Button>
        </div>
      </div>
      {cal.isPending ? <Spinner /> : null}
      <ErrorAlert error={cal.error} />
      {cal.data && days.length === 0 ? <EmptyAdult title={t("adult.calendar.empty")} hint={t("adult.calendar.emptyHint")} /> : null}
      <ol className="space-y-4">
        {days.map(([day, list]) => (
          <li key={day} className="grid gap-2 sm:grid-cols-[120px_minmax(0,1fr)]">
            <div className={`text-[12px] font-medium sm:pt-2 ${day === today ? "text-accent" : "text-muted"}`}>
              {day ? dayFmt.format(new Date(`${day}T12:00:00`)) : "–"}
            </div>
            <ul className="divide-y divide-line overflow-hidden rounded-ui border border-line bg-surface">
              {list.map((it) => (
                <li key={it.id} className="flex items-center gap-3 px-2 py-2">
                  <BlurImage image={it.image} alt={it.title} fallback={it.studio || it.title} className="aspect-video w-20 shrink-0 sm:w-24"
                    link={{ to: "/adult/items/$id", params: { id: String(it.id) } }} />
                  <div className="min-w-0 flex-1">
                    <Link to="/adult/items/$id" params={{ id: String(it.id) }} className="block truncate text-[13px] font-medium text-fg hover:text-accent">{it.title}</Link>
                    <p className="truncate text-[12px] text-muted">{[it.studio, it.performers.map((p) => p.name).join(", ")].filter(Boolean).join(" · ")}</p>
                  </div>
                  <span className="hidden sm:inline-flex"><StatusBadge item={it} /></span>
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ol>
    </div>
  );
}

function useNameFilter<T extends { name: string }>(list: T[] | undefined, term: string) {
  return useMemo(() => {
    const q = term.trim().toLowerCase();
    return (list ?? []).filter((x) => !q || x.name.toLowerCase().includes(q));
  }, [list, term]);
}

export function AdultStudiosPage() {
  const { t } = useTranslation();
  const studios = useAdultStudios();
  const [term, setTerm] = useState("");
  const list = useNameFilter(studios.data, term);
  return (
    <div>
      <div className="mb-3 max-w-sm"><TextInput label={t("adult.filter")} type="search" value={term} onChange={(e) => setTerm(e.target.value)} /></div>
      {studios.isPending ? <Spinner /> : null}
      <ErrorAlert error={studios.error} />
      {studios.data?.length === 0 ? <StartHere title={t("adult.emptyStudios")} /> : null}
      <ul className="grid grid-cols-2 gap-x-2 gap-y-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5" data-testid="adult-studios">
        {list.map((s) => (
          <li key={s.id} className="min-w-0 p-1">
            <BlurImage image={s.image} alt={s.name} fallback={s.name} fit="contain" className="aspect-video w-full"
              link={{ to: "/adult/studios/$id", params: { id: String(s.id) } }} />
            <Link to="/adult/studios/$id" params={{ id: String(s.id) }} className="mt-1.5 block truncate text-[13px] font-medium text-fg hover:text-accent">{s.name}</Link>
            <div className="flex items-center justify-between gap-1">
              <span className="truncate text-[12px] text-muted">{t("adult.studioCounts", { scenes: s.scenes, files: s.files })}</span>
              {s.monitored ? <Badge tone="info">{t("adult.following")}</Badge> : null}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function AdultPerformersPage() {
  const { t } = useTranslation();
  const perfs = useAdultPerformers();
  const [term, setTerm] = useState("");
  const [tracked, setTracked] = useState(false);
  const list = useNameFilter(perfs.data, term).filter((p) => !tracked || p.monitored);
  return (
    <div>
      <div className="mb-3 flex flex-wrap items-end gap-3">
        <div className="w-full max-w-sm"><TextInput label={t("adult.filter")} type="search" value={term} onChange={(e) => setTerm(e.target.value)} /></div>
        <Checkbox label={t("adult.onlyTracked")} checked={tracked} onChange={(e) => setTracked(e.target.checked)} />
      </div>
      {perfs.isPending ? <Spinner /> : null}
      <ErrorAlert error={perfs.error} />
      {perfs.data?.length === 0 ? <StartHere title={t("adult.emptyPerformers")} /> : null}
      <ul className="grid grid-cols-3 gap-x-2 gap-y-3 sm:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8" data-testid="adult-performers">
        {list.map((p) => (
          <li key={p.id} className="min-w-0 p-1">
            <BlurImage image={p.image} alt={p.name} fallback={p.name} className="aspect-[3/4] w-full"
              link={{ to: "/adult/performers/$id", params: { id: String(p.id) } }} />
            <Link to="/adult/performers/$id" params={{ id: String(p.id) }} className="mt-1.5 block truncate text-[13px] font-medium text-fg hover:text-accent">{p.name}</Link>
            <div className="flex items-center justify-between gap-1">
              <span className="text-[12px] tabular-nums text-muted">{t("adult.sceneCount", { count: p.items })}</span>
              {p.monitored ? <Badge tone="info">{t("adult.tracked")}</Badge> : null}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Header of a studio or performer page. */
function OwnerHeader({ image, name, portrait, facts, actions }:
  { image?: string; name: string; portrait?: boolean; facts: ReactNode; actions: ReactNode }) {
  return (
    <div className="mb-5 flex flex-col gap-4 rounded-ui border border-line bg-surface p-3 sm:flex-row sm:items-center">
      <BlurImage image={image} alt={name} fallback={name} fit={portrait ? "cover" : "contain"}
        className={portrait ? "aspect-[3/4] w-28 shrink-0" : "aspect-video w-full shrink-0 sm:w-56"} />
      <div className="min-w-0 flex-1">
        <h2 className="truncate text-[18px] font-semibold tracking-tight text-fg">{name}</h2>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-muted">{facts}</div>
        <div className="mt-3 flex flex-wrap items-center gap-2">{actions}</div>
      </div>
    </div>
  );
}

/** "Keep its scenes" or "remove them too" for a studio or performer. */
function ScenesChoice({ legend, keep, onChange, count, keepHint }:
  { legend: string; keep: boolean; onChange: (keep: boolean) => void; count: number; keepHint: string }) {
  const { t } = useTranslation();
  return (
    <fieldset className="space-y-2">
      <legend className="mb-1 text-fg">{legend}</legend>
      <label className="flex items-start gap-2">
        <input type="radio" name="adult-scenes" checked={keep} onChange={() => onChange(true)} className="mt-0.5 accent-[var(--lc-accent)]" data-testid="adult-remove-keep" />
        <span><span className="text-fg">{t("adult.remove.keepScenes")}</span><span className="block text-[12px] text-faint">{keepHint}</span></span>
      </label>
      <label className="flex items-start gap-2">
        <input type="radio" name="adult-scenes" checked={!keep} onChange={() => onChange(false)} className="mt-0.5 accent-[var(--lc-accent)]" data-testid="adult-remove-scenes" />
        <span className="text-fg">{t("adult.remove.removeScenes", { count })}</span>
      </label>
    </fieldset>
  );
}

function OwnerActions({ monitor, refresh, onRemove }:
  { monitor: ReactNode; refresh: { busy: boolean; run: () => void; count?: number }; onRemove: () => void }) {
  const { t } = useTranslation();
  return (
    <>
      {monitor}
      <Button busy={refresh.busy} onClick={refresh.run}><RefreshCw className="size-3.5" aria-hidden /> {t("adult.refresh")}</Button>
      <Button variant="ghost" onClick={onRemove} data-testid="adult-remove"><Trash2 className="size-3.5" aria-hidden /> {t("adult.remove.button")}</Button>
      {refresh.count !== undefined ? <span role="status" className="text-[12px] text-muted">{t("adult.refreshed", { count: refresh.count })}</span> : null}
    </>
  );
}

export function AdultStudioPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const canManage = useCanManageAdult();
  const { id } = useParams({ strict: false }) as { id: string };
  const sid = Number(id);
  const studio = useAdultStudio(sid);
  const invalidate = useAdultInvalidate();
  const [removing, setRemoving] = useState(false);
  const [keep, setKeep] = useState(true);
  const [choice, setChoice] = useState<RemoveChoice>({ deleteFiles: false, exclude: false });
  const remove = useRemoveStudio();
  const toggle = useMutation({
    mutationFn: async (monitored: boolean) => unwrap(await api.PATCH("/adult/studios/{id}", { params: { path: { id: sid } }, body: { monitored } })),
    onSuccess: () => invalidate(),
  });
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/studios/{id}/refresh", { params: { path: { id: sid } } })),
    onSuccess: () => invalidate(),
  });
  if (studio.isPending) return <Spinner />;
  if (!studio.data) return <ErrorAlert error={studio.error} />;
  const s = studio.data;
  return (
    <div>
      <OwnerHeader image={s.image} name={s.name}
        facts={
          <>
            <span>{t("adult.studioCounts", { scenes: s.scenes, files: s.files })}</span>
            {s.monitored && s.monitorSince ? <span>{t("adult.followingSince", { date: s.monitorSince })}</span> : null}
            {s.refreshedAt ? <span>{t("adult.refreshedAt", { when: formatDateTime(s.refreshedAt, i18n.language) })}</span> : null}
          </>
        }
        actions={canManage ? (
          <OwnerActions onRemove={() => setRemoving(true)}
            monitor={<Checkbox label={t("adult.monitorNew")} checked={s.monitored} onChange={(e) => toggle.mutate(e.target.checked)} />}
            refresh={{ busy: refresh.isPending, run: () => refresh.mutate(), count: refresh.data?.count }} />
        ) : null} />
      <ErrorAlert error={toggle.error ?? refresh.error} />
      <ItemBrowser base={{ studioId: sid }} empty={<EmptyAdult title={t("adult.emptyStudio")} hint={t("adult.emptyStudioHint")} />} />
      {removing ? (
        <RemoveDialog title={t("adult.remove.studioTitle", { name: s.name })} busy={remove.isPending} error={remove.error}
          onClose={() => setRemoving(false)}
          onConfirm={() => remove.mutate({ id: sid, keepScenes: keep, ...(keep ? { deleteFiles: false, exclude: false } : choice) },
            { onSuccess: () => void navigate({ to: "/adult/studios" }) })}>
          <ScenesChoice legend={t("adult.remove.studioScenes", { count: s.scenes })} keep={keep} onChange={setKeep} count={s.scenes}
            keepHint={t("adult.remove.keepScenesHint")} />
          {!keep ? <RemoveChoices value={choice} onChange={setChoice} files={s.files > 0} excludeHint={t("adult.remove.excludeManyHint")} /> : null}
        </RemoveDialog>
      ) : null}
    </div>
  );
}

export function AdultPerformerPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const canManage = useCanManageAdult();
  const { id } = useParams({ strict: false }) as { id: string };
  const pid = Number(id);
  const perf = useAdultPerformer(pid);
  const invalidate = useAdultInvalidate();
  const [removing, setRemoving] = useState(false);
  const [keep, setKeep] = useState(true);
  const [choice, setChoice] = useState<RemoveChoice>({ deleteFiles: false, exclude: false });
  const remove = useRemovePerformer();
  const toggle = useMutation({
    mutationFn: async (monitored: boolean) => unwrap(await api.PATCH("/adult/performers/{id}", { params: { path: { id: pid } }, body: { monitored } })),
    onSuccess: () => invalidate(),
  });
  const sets = useMutation({
    mutationFn: async (galleries: boolean) => unwrap(await api.PATCH("/adult/performers/{id}", { params: { path: { id: pid } }, body: { galleries } })),
    onSuccess: () => invalidate(),
  });
  const addSet = useAddGallery();
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/performers/{id}/refresh", { params: { path: { id: pid } } })),
    onSuccess: () => invalidate(),
  });
  if (perf.isPending) return <Spinner />;
  if (!perf.data) return <ErrorAlert error={perf.error} />;
  const p = perf.data;
  return (
    <div>
      <OwnerHeader image={p.image} name={p.name} portrait
        facts={
          <>
            {p.disambiguation ? <span>{p.disambiguation}</span> : null}
            <span>{t("adult.sceneCount", { count: p.items })}</span>
            {p.monitored && p.monitorSince ? <span>{t("adult.trackedSince", { date: p.monitorSince })}</span> : null}
            {p.refreshedAt ? <span>{t("adult.refreshedAt", { when: formatDateTime(p.refreshedAt, i18n.language) })}</span> : null}
          </>
        }
        actions={canManage ? (
          <OwnerActions onRemove={() => setRemoving(true)}
            monitor={
              <>
                <Checkbox label={t("adult.trackPerformer")} description={t("adult.trackPerformerHint")} checked={p.monitored}
                  onChange={(e) => toggle.mutate(e.target.checked)} />
                <Checkbox label={t("adult.gallery.performerSets")} description={t("adult.gallery.performerSetsHint")} checked={!!p.galleries}
                  onChange={(e) => sets.mutate(e.target.checked)} data-testid="adult-performer-galleries" />
                <Button busy={addSet.isPending} data-testid="adult-performer-add-set"
                  onClick={() => addSet.mutate({ performerId: pid, monitored: true, search: true }, { onSuccess: (it) => void navigate({ to: "/adult/items/$id", params: { id: String(it.id) } }) })}>
                  <Images className="size-3.5" aria-hidden /> {t("adult.gallery.performerAdd")}
                </Button>
              </>
            }
            refresh={{ busy: refresh.isPending, run: () => refresh.mutate(), count: refresh.data?.count }} />
        ) : null} />
      <ErrorAlert error={toggle.error ?? refresh.error ?? sets.error ?? addSet.error} />
      <ItemBrowser base={{ performerId: pid }} empty={<EmptyAdult title={t("adult.emptyPerformer")} hint={t("adult.emptyPerformerHint")} />} />
      {removing ? (
        <RemoveDialog title={t("adult.remove.performerTitle", { name: p.name })} busy={remove.isPending} error={remove.error}
          onClose={() => setRemoving(false)}
          onConfirm={() => remove.mutate({ id: pid, removeScenes: !keep, ...(keep ? { deleteFiles: false, exclude: false } : choice) },
            { onSuccess: () => void navigate({ to: "/adult/performers" }) })}>
          <ScenesChoice legend={t("adult.remove.performerScenes", { count: p.items })} keep={keep} onChange={setKeep} count={p.items}
            keepHint={t("adult.remove.keepPerformerHint")} />
          {!keep ? <RemoveChoices value={choice} onChange={setChoice} files excludeHint={t("adult.remove.excludeManyHint")} /> : null}
        </RemoveDialog>
      ) : null}
    </div>
  );
}

function adultReleases(id: number): ReleaseSource {
  return {
    key: [...adultKey, "item", id, "releases"],
    load: async () => unwrap(await api.GET("/adult/items/{id}/releases", { params: { path: { id } } })),
    grab: async (body) => unwrap(await api.POST("/adult/items/{id}/releases", { params: { path: { id } }, body })),
    refresh: [[...adultKey, "item", id]],
  };
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-[12px] text-faint">{label}</dt>
      <dd className="text-[13px] text-fg">{children}</dd>
    </div>
  );
}

export function AdultItemPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const canManage = useCanManageAdult();
  const { id } = useParams({ strict: false }) as { id: string };
  const iid = Number(id);
  const item = useAdultItem(iid);
  const invalidate = useAdultInvalidate();
  const profiles = useQualityProfiles();
  const [tab, setTab] = useState<"history" | "search">("history");
  const [removing, setRemoving] = useState(false);
  const [choice, setChoice] = useState<RemoveChoice>({ deleteFiles: false, exclude: true });
  const remove = useRemoveItem();
  const edit = useMutation({
    mutationFn: async (monitored: boolean) => unwrap(await api.PATCH("/adult/items/{id}", { params: { path: { id: iid } }, body: { monitored } })),
    onSuccess: () => invalidate(),
  });
  const search = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/items/{id}/search", { params: { path: { id: iid } } })),
  });
  const rename = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/items/{id}/rename", { params: { path: { id: iid }, query: { apply: true } } })),
    onSuccess: () => invalidate(),
  });
  const isScene = item.data?.kind === "scene";
  const sets = useAdultItems({ kind: "gallery", sceneId: iid }, isScene);
  const addSet = useAddGallery();
  if (item.isPending) return <Spinner />;
  if (!item.data) return <ErrorAlert error={item.error} />;
  const it = item.data;
  const gallery = it.kind === "gallery";
  const profile = profiles.data?.find((p) => p.id === it.qualityProfileId);
  const back = it.kind === "movie" ? "/adult/movies" : gallery ? "/adult/galleries" : "/adult";
  const photoSet = sets.data?.items[0];
  return (
    <div data-testid="adult-item">
      <div className="mb-5 grid gap-4 lg:grid-cols-[minmax(0,480px)_minmax(0,1fr)]">
        <BlurImage image={it.image} alt={it.title} fallback={it.studio || it.title} className={gallery ? "aspect-[3/4] w-full max-w-xs" : "aspect-video w-full"} />
        <div className="min-w-0">
          <div className="mb-1.5"><StatusBadge item={it} /></div>
          <h2 className="break-words text-[20px] font-semibold leading-tight tracking-tight text-fg">{it.title}</h2>
          <p className="mt-1 text-[13px] text-muted">
            {it.studioId ? <Link to="/adult/studios/$id" params={{ id: String(it.studioId) }} className="text-fg hover:text-accent">{it.studio}</Link> : it.studio}
            {it.releaseDate ? <span className="tabular-nums"> · {it.releaseDate}</span> : null}
            {it.duration ? <span> · {formatDuration(it.duration, i18n.language)}</span> : null}
            {gallery && it.file?.imageCount ? <span> · {t("adult.gallery.images", { count: it.file.imageCount })}</span> : null}
          </p>
          {gallery && it.sceneId ? (
            <p className="mt-1 text-[12px] text-muted">
              <Link to="/adult/items/$id" params={{ id: String(it.sceneId) }} className="text-fg hover:text-accent" data-testid="adult-gallery-scene">{t("adult.gallery.ofScene")}</Link>
            </p>
          ) : null}
          {isScene && photoSet ? (
            <p className="mt-1 text-[12px] text-muted">
              <Link to="/adult/items/$id" params={{ id: String(photoSet.id) }} className="inline-flex items-center gap-1 text-fg hover:text-accent" data-testid="adult-scene-gallery">
                <Images className="size-3.5" aria-hidden /> {t("adult.gallery.photoSet")}
              </Link>
            </p>
          ) : null}
          {it.performers.length ? (
            <ul className="mt-2 flex flex-wrap gap-1.5">
              {it.performers.map((p) => (
                <li key={p.id}>
                  <Link to="/adult/performers/$id" params={{ id: String(p.id) }}
                    className="inline-flex h-6 items-center rounded-full border border-line-strong bg-surface-3 px-2.5 text-[12px] text-fg hover:border-accent hover:text-accent">{p.name}</Link>
                </li>
              ))}
            </ul>
          ) : null}
          {it.overview ? <p className="mt-3 line-clamp-5 text-[13px] leading-relaxed text-muted">{it.overview}</p> : null}
          {canManage ? (
            <div className="mt-4 flex flex-wrap items-center gap-2">
              <Button variant="primary" busy={search.isPending} onClick={() => search.mutate()} data-testid="adult-search-now">
                <Search className="size-3.5" aria-hidden /> {t("adult.searchNow")}
              </Button>
              <Button onClick={() => setTab("search")}>{t("adult.interactive")}</Button>
              {it.file && !gallery ? <Button busy={rename.isPending} onClick={() => rename.mutate()}>{t("adult.rename")}</Button> : null}
              {isScene && sets.data && !photoSet ? (
                <Button busy={addSet.isPending} onClick={() => addSet.mutate({ sceneId: iid, monitored: true, search: true })} data-testid="adult-find-photo-set">
                  <Images className="size-3.5" aria-hidden /> {t("adult.gallery.findPhotoSet")}
                </Button>
              ) : null}
              <Button variant="ghost" onClick={() => setRemoving(true)} data-testid="adult-remove"><Trash2 className="size-3.5" aria-hidden /> {t("adult.remove.button")}</Button>
              <span className="w-full sm:w-auto"><Checkbox label={t("adult.monitored")} checked={it.monitored} onChange={(e) => edit.mutate(e.target.checked)} /></span>
            </div>
          ) : null}
          {search.isSuccess ? <p role="status" className="mt-2 text-[12px] text-muted">{t("adult.searchQueued")}</p> : null}
          <div className="mt-2"><ErrorAlert error={edit.error ?? search.error ?? rename.error ?? addSet.error} /></div>
        </div>
      </div>

      <div className="mb-5 grid gap-4 lg:grid-cols-2">
        {it.file ? (
          <Panel title={t("adult.file.title")}>
            <dl className="grid grid-cols-2 gap-3" data-testid="adult-file">
              <div className="col-span-2"><Fact label={t("adult.file.path")}><span className="break-all font-mono text-[12px]">{it.file.path}</span></Fact></div>
              <Fact label={t("adult.file.size")}>{formatBytes(it.file.size, i18n.language)}</Fact>
              {gallery ? <Fact label={t("adult.gallery.count")}>{it.file.imageCount ?? 0}</Fact> : <Fact label={t("adult.file.quality")}>{it.file.quality}</Fact>}
              {it.file.releaseGroup ? <Fact label={t("adult.file.group")}>{it.file.releaseGroup}</Fact> : null}
              {it.file.addedAt ? <Fact label={t("adult.file.added")}>{formatDateTime(it.file.addedAt, i18n.language)}</Fact> : null}
              {it.file.sceneName ? <div className="col-span-2"><Fact label={t("adult.file.release")}><span className="break-all font-mono text-[12px]">{it.file.sceneName}</span></Fact></div> : null}
            </dl>
          </Panel>
        ) : null}
        <Panel title={t("adult.details")}>
          <dl className="grid grid-cols-2 gap-3">
            <Fact label={t("adult.add.profile")}>{profile?.name ?? "–"}</Fact>
            <Fact label={t("adult.addedAt")}>{formatDateTime(it.addedAt, i18n.language)}</Fact>
            <div className="col-span-2"><Fact label={t("adult.folder")}><span className="break-all font-mono text-[12px]">{it.path}</span></Fact></div>
            {it.sourceTags.length ? <div className="col-span-2"><Fact label={t("adult.tags")}>{it.sourceTags.join(", ")}</Fact></div> : null}
          </dl>
        </Panel>
      </div>

      {gallery ? (
        <div className="mb-5">
          <h3 className="mb-2 text-[14px] font-semibold text-fg">{t("adult.gallery.imagesTitle")}</h3>
          <GalleryGrid id={iid} title={it.title} hasFile={!!it.file} />
        </div>
      ) : null}

      <div className="mb-3 flex gap-1 overflow-x-auto overflow-y-hidden border-b border-line [scrollbar-width:none] [&::-webkit-scrollbar]:hidden" role="tablist">
        {(["history", "search"] as const).map((k) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}
            className={`whitespace-nowrap border-b-2 px-3 py-2 text-[13px] ${tab === k ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"}`}>
            {k === "history" ? t("adult.history") : t("adult.interactive")}
          </button>
        ))}
      </div>
      {tab === "history" ? (
        <HistoryTab itemRef={it.ref} load={async () => unwrap(await api.GET("/adult/items/{id}/timeline", { params: { path: { id: iid } } }))} />
      ) : (
        <InteractiveSearch source={adultReleases(iid)} itemRef={it.ref} />
      )}

      {removing ? (
        <RemoveDialog title={t("adult.remove.itemTitle", { title: it.title })} busy={remove.isPending} error={remove.error}
          onClose={() => setRemoving(false)}
          onConfirm={() => remove.mutate({ id: iid, ...choice }, { onSuccess: () => void navigate({ to: back }) })}>
          <p className="text-fg">{t("adult.remove.itemQuestion")}</p>
          <RemoveChoices value={choice} onChange={setChoice} files={!!it.file} />
        </RemoveDialog>
      ) : null}
    </div>
  );
}

type AddType = LookupQuery["type"];
type AddChoice = AddType | "gallery";
const ADD_TYPES: AddChoice[] = ["studio", "performer", "scene", "movie", "gallery"];
const keyOf = (r: AdultLookupResult) => `${r.source}:${r.type}:${r.id}`;

export function AdultAddPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canManage = useCanManageAdult();
  const [source, setSource] = useState<"stashdb" | "tpdb">("stashdb");
  const [choiceType, setType] = useState<AddChoice>("studio");
  const type: AddType = choiceType === "gallery" ? "scene" : choiceType;
  const [alsoSet, setAlsoSet] = useState(false);
  const [creating, setCreating] = useState(false);
  const [term, setTerm] = useState("");
  const [sort, setSortState] = useState<LookupSort>(() => loadPref(PREF.lookupSort, LOOKUP_SORTS, "newest"));
  const [submitted, setSubmitted] = useState<LookupQuery | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [added, setAdded] = useState<Set<string>>(new Set());
  const roots = useAdultRootFolders();
  const profiles = useQualityProfiles();
  const [profileId, setProfileId] = useState<number>(0);
  const [rootId, setRootId] = useState<number>(0);
  const invalidate = useAdultInvalidate();
  const lookup = useAdultLookup(submitted);
  const ordered = type === "scene" || type === "movie";
  const setSort = (v: LookupSort) => {
    setSortState(v);
    savePref(PREF.lookupSort, v);
    if (submitted) setSubmitted({ ...submitted, sort: v });
  };
  const addOne = async (r: AdultLookupResult) => {
    const body = { source: r.source, id: r.id, monitored: true, search: true, kind: r.type === "movie" ? "movie" as const : "scene" as const,
      gallery: alsoSet && (r.type === "scene" || r.type === "performer"),
      ...(profileId ? { qualityProfileId: profileId } : {}), ...(rootId ? { rootFolderId: rootId } : {}) };
    if (r.type === "studio") return { to: "studio" as const, v: unwrap(await api.POST("/adult/studios", { body })) };
    if (r.type === "performer") return { to: "performer" as const, v: unwrap(await api.POST("/adult/performers", { body })) };
    return { to: "item" as const, v: unwrap(await api.POST("/adult/items", { body })) };
  };
  const add = useMutation({
    mutationFn: addOne,
    onSuccess: (res) => {
      void invalidate();
      const id = String(res.v.id);
      if (res.to === "studio") void navigate({ to: "/adult/studios/$id", params: { id } });
      else if (res.to === "performer") void navigate({ to: "/adult/performers/$id", params: { id } });
      else void navigate({ to: "/adult/items/$id", params: { id } });
    },
  });
  const results = useMemo(() => lookup.data?.pages.flatMap((p) => p.results) ?? [], [lookup.data]);
  const addMany = useMutation({
    mutationFn: async (list: AdultLookupResult[]) => {
      const failed: string[] = [];
      for (const r of list) {
        try {
          await addOne(r);
          setAdded((s) => new Set(s).add(keyOf(r)));
        } catch {
          failed.push(r.name);
        }
      }
      return failed;
    },
    onSuccess: () => { setSelected(new Set()); void invalidate(); },
  });
  const movieProfiles = (profiles.data ?? []).filter((p) => p.kind === "movies");
  const selectable = results.filter((r) => !r.inLibrary && !added.has(keyOf(r)));
  const chosen = selectable.filter((r) => selected.has(keyOf(r)));
  return (
    <div className="space-y-4">
      {roots.data?.length === 0 ? <Alert tone="warning">{t("adult.noRoot")}</Alert> : null}
      {choiceType === "gallery" ? (
        <Panel title={t("adult.gallery.addTitle")} description={t("adult.gallery.addHint")}>
          <div className="flex flex-wrap items-end gap-3">
            <SelectInput label={t("adult.add.type")} value={choiceType} onChange={(e) => setType(e.target.value as AddChoice)} data-testid="adult-add-type">
              {ADD_TYPES.map((k) => <option key={k} value={k}>{t(`adult.add.types.${k}`)}</option>)}
            </SelectInput>
            {canManage ? (
              <Button variant="primary" onClick={() => setCreating(true)} data-testid="adult-gallery-new-open">
                <Plus className="size-3.5" aria-hidden /> {t("adult.gallery.new")}
              </Button>
            ) : null}
          </div>
          {creating ? <NewGalleryForm onClose={() => setCreating(false)} /> : null}
        </Panel>
      ) : null}
      {choiceType === "gallery" ? null : (
      <Panel title={t("adult.add.title")} description={t("adult.add.hint")}>
        <form className="grid grid-cols-2 gap-3 md:grid-cols-[auto_auto_minmax(0,1fr)_auto_auto] md:items-end"
          onSubmit={(e) => { e.preventDefault(); if (term.trim()) { setSelected(new Set()); setSubmitted({ source, type, term: term.trim(), sort }); } }}>
          <SelectInput label={t("adult.add.source")} value={source} onChange={(e) => setSource(e.target.value as typeof source)}>
            <option value="stashdb">StashDB</option>
            <option value="tpdb">ThePornDB</option>
          </SelectInput>
          <SelectInput label={t("adult.add.type")} value={choiceType} onChange={(e) => setType(e.target.value as AddChoice)} data-testid="adult-add-type">
            {ADD_TYPES.map((k) => <option key={k} value={k}>{t(`adult.add.types.${k}`)}</option>)}
          </SelectInput>
          <div className="col-span-2 md:col-span-1"><TextInput label={t("adult.add.term")} value={term} maxLength={200} onChange={(e) => setTerm(e.target.value)} /></div>
          {ordered ? (
            <SelectInput label={t("adult.add.sort")} value={sort} onChange={(e) => setSort(e.target.value as LookupSort)} data-testid="adult-lookup-sort">
              {LOOKUP_SORTS.map((s) => <option key={s} value={s}>{t(`adult.add.sorts.${s}`)}</option>)}
            </SelectInput>
          ) : <span className="hidden md:block" />}
          <Button type="submit" variant="primary" busy={lookup.isFetching && !lookup.isFetchingNextPage} disabled={!term.trim()} className={ordered ? "" : "col-span-2 md:col-span-1"}>
            <Search className="size-3.5" aria-hidden /> {t("adult.add.search")}
          </Button>
        </form>
        <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:max-w-2xl">
          <SelectInput label={t("adult.add.profile")} value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
            <option value={0}>{t("adult.add.default")}</option>
            {movieProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </SelectInput>
          <SelectInput label={t("adult.add.root")} value={rootId} onChange={(e) => setRootId(Number(e.target.value))}>
            <option value={0}>{t("adult.add.default")}</option>
            {roots.data?.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
          </SelectInput>
          {type === "scene" || type === "performer" ? (
            <div className="sm:col-span-2">
              <Checkbox label={t("adult.gallery.alsoSet")} checked={alsoSet} onChange={(e) => setAlsoSet(e.target.checked)}
                description={type === "scene" ? t("adult.gallery.alsoSetScene") : t("adult.gallery.alsoSetPerformer")} data-testid="adult-also-set" />
            </div>
          ) : null}
        </div>
      </Panel>
      )}
      <ErrorAlert error={lookup.error ?? add.error ?? addMany.error} />
      {addMany.data?.length ? <Alert tone="warning">{t("adult.add.someFailed", { names: addMany.data.join(", ") })}</Alert> : null}
      {lookup.data && results.length === 0 ? <EmptyAdult title={t("adult.add.none")} hint={t("adult.add.noneHint")} /> : null}
      {results.length > 0 ? (
        <div>
          {canManage && selectable.length > 0 ? (
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <Checkbox label={t("adult.select.all")} checked={chosen.length > 0 && chosen.length === selectable.length}
                onChange={(e) => setSelected(e.target.checked ? new Set(selectable.map(keyOf)) : new Set())} />
              <span className="flex-1" />
              <Button variant="primary" disabled={chosen.length === 0} busy={addMany.isPending} onClick={() => addMany.mutate(chosen)} data-testid="adult-add-selected">
                <Plus className="size-3.5" aria-hidden /> {t("adult.add.addSelected", { count: chosen.length })}
              </Button>
            </div>
          ) : null}
          <ul className="divide-y divide-line overflow-hidden rounded-ui border border-line bg-surface" data-testid="adult-lookup">
            {results.map((r) => {
              const k = keyOf(r);
              const have = r.inLibrary || added.has(k);
              const portrait = r.type === "performer";
              return (
                <li key={k} className="flex items-center gap-3 px-2 py-2" data-testid="adult-lookup-row">
                  {canManage ? (
                    <input type="checkbox" className="size-4 shrink-0 accent-[var(--lc-accent)]" disabled={have} checked={!have && selected.has(k)}
                      onChange={() => setSelected((s) => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; })}
                      aria-label={t("adult.select.one", { name: r.name })} />
                  ) : null}
                  <BlurImage image={r.image} alt={r.name} fallback={r.studio || r.name} fit={r.type === "studio" ? "contain" : "cover"}
                    className={portrait ? "aspect-[3/4] w-12 shrink-0" : "aspect-video w-24 shrink-0 sm:w-32"} />
                  <div className="min-w-0 flex-1 text-[13px]">
                    <p className="line-clamp-2 font-medium text-fg">{r.name}</p>
                    <p className="truncate text-[12px] text-muted">{[r.studio, r.date].filter(Boolean).join(" · ") || t(`adult.add.types.${r.type}`)}</p>
                    {r.performers?.length ? <p className="truncate text-[12px] text-faint">{r.performers.join(", ")}</p> : null}
                  </div>
                  {have ? <Badge tone="success">{t("adult.add.inLibrary")}</Badge> : canManage ? (
                    <Button busy={add.isPending && add.variables?.id === r.id} onClick={() => add.mutate(r)}>{t("adult.add.add")}</Button>
                  ) : null}
                </li>
              );
            })}
          </ul>
          {lookup.hasNextPage ? (
            <div className="mt-3 flex justify-center">
              <Button busy={lookup.isFetchingNextPage} onClick={() => void lookup.fetchNextPage()} data-testid="adult-lookup-more">{t("adult.loadMore")}</Button>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
