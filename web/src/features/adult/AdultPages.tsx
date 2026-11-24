import { useState } from "react";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Tabs } from "@/components/Tabs";
import { formatBytes } from "@/components/format";
import { Alert, Badge, Button, Checkbox, EmptyState, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { HistoryTab } from "@/features/movies/HistoryTab";
import { InteractiveSearch, type ReleaseSource } from "@/features/movies/InteractiveSearch";
import { useQualityProfiles } from "@/features/movies/common";
import {
  adultKey, useAdultInvalidate, useAdultItem, useAdultItems, useAdultPerformer, useAdultPerformers, useAdultPreferences,
  useAdultRootFolders, useAdultStudio, useAdultStudios, useSetBlur, type AdultItem, type AdultLookupResult,
} from "./api";
import { BlurImage } from "./BlurImage";

/** The adult section: its own pages, never mixed into the rest of the app. */
export function AdultLayout() {
  const { t } = useTranslation();
  const prefs = useAdultPreferences();
  const setBlur = useSetBlur();
  // Local value so the checkbox flips on the click itself, before the save returns.
  const [blur, setBlurLocal] = useState<boolean | null>(null);
  return (
    <div data-testid="adult-section">
      <PageHeader title={t("adult.title")} description={t("adult.description")}
        actions={
          <Checkbox label={t("adult.blur")} checked={blur ?? prefs.data?.blur ?? true} disabled={!prefs.data}
            onChange={(e) => {
              const next = e.target.checked;
              setBlurLocal(next);
              setBlur.mutate(next, { onError: () => setBlurLocal(null) });
            }} />
        } />
      <Tabs label={t("adult.title")} tabs={[
        { to: "/adult", label: t("adult.tabs.scenes") },
        { to: "/adult/movies", label: t("adult.tabs.movies") },
        { to: "/adult/studios", label: t("adult.tabs.studios") },
        { to: "/adult/performers", label: t("adult.tabs.performers") },
        { to: "/adult/wanted", label: t("adult.tabs.wanted") },
        { to: "/adult/calendar", label: t("adult.tabs.calendar") },
        { to: "/adult/add", label: t("adult.tabs.add") },
      ]} />
      <div className="mt-4"><Outlet /></div>
    </div>
  );
}

function ItemGrid({ items }: { items: AdultItem[] }) {
  const { t } = useTranslation();
  if (items.length === 0) return <EmptyState>{t("adult.empty")}</EmptyState>;
  return (
    <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5" data-testid="adult-items">
      {items.map((it) => (
        <li key={it.id} className="min-w-0">
          <BlurImage image={it.image} alt={it.title} className="aspect-video w-full" />
          <Link to="/adult/items/$id" params={{ id: String(it.id) }} className="mt-1.5 block truncate text-[13px] text-fg hover:underline">
            {it.title}
          </Link>
          <div className="flex items-center gap-1.5 truncate text-[12px] text-muted">
            <span className="truncate">{it.studio}{it.releaseDate ? ` · ${it.releaseDate}` : ""}</span>
            {it.file ? <Badge tone="success">{t("adult.have")}</Badge> : it.monitored ? <Badge tone="warning">{t("adult.missing")}</Badge> : null}
          </div>
        </li>
      ))}
    </ul>
  );
}

function ItemsPage({ kind, missing }: { kind?: "scene" | "movie"; missing?: boolean }) {
  const { t } = useTranslation();
  const [term, setTerm] = useState("");
  const items = useAdultItems({ kind, missing, term: term || undefined });
  return (
    <div>
      <div className="mb-3 max-w-sm">
        <TextInput label={t("adult.filter")} value={term} onChange={(e) => setTerm(e.target.value)} />
      </div>
      {items.isPending ? <Spinner /> : null}
      <ErrorAlert error={items.error} />
      {items.data ? <ItemGrid items={items.data.items} /> : null}
    </div>
  );
}

export const AdultScenesPage = () => <ItemsPage kind="scene" />;
export const AdultMoviesPage = () => <ItemsPage kind="movie" />;
export const AdultWantedPage = () => <ItemsPage missing />;

export function AdultCalendarPage() {
  const { t } = useTranslation();
  const today = new Date();
  const start = new Date(today.getFullYear(), today.getMonth() - 1, 1).toISOString().slice(0, 10);
  const end = new Date(today.getFullYear(), today.getMonth() + 2, 0).toISOString().slice(0, 10);
  const [range] = useState({ start, end });
  const q = useAdultItems({});
  const list = (q.data?.items ?? []).filter((i) => i.releaseDate && i.releaseDate >= range.start && i.releaseDate <= range.end)
    .sort((a, b) => (a.releaseDate ?? "").localeCompare(b.releaseDate ?? ""));
  return (
    <Panel title={t("adult.tabs.calendar")} description={t("adult.calendarHint", { start: range.start, end: range.end })}>
      {list.length === 0 ? <EmptyState>{t("adult.empty")}</EmptyState> : (
        <ul className="divide-y divide-line">
          {list.map((i) => (
            <li key={i.id} className="flex items-center gap-3 py-2 text-[13px]">
              <span className="w-24 shrink-0 tabular-nums text-muted">{i.releaseDate}</span>
              <Link to="/adult/items/$id" params={{ id: String(i.id) }} className="truncate hover:underline">{i.studio} – {i.title}</Link>
              {i.file ? <Badge tone="success">{t("adult.have")}</Badge> : null}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

export function AdultStudiosPage() {
  const { t } = useTranslation();
  const studios = useAdultStudios();
  return (
    <div>
      {studios.isPending ? <Spinner /> : null}
      <ErrorAlert error={studios.error} />
      {studios.data?.length === 0 ? <EmptyState>{t("adult.empty")}</EmptyState> : null}
      <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {studios.data?.map((s) => (
          <li key={s.id} className="flex items-center gap-3 rounded-ui border border-line bg-surface p-3">
            <BlurImage image={s.image} alt={s.name} className="size-14 shrink-0" />
            <div className="min-w-0">
              <Link to="/adult/studios/$id" params={{ id: String(s.id) }} className="block truncate text-[14px] hover:underline">{s.name}</Link>
              <p className="text-[12px] text-muted">{t("adult.studioCounts", { scenes: s.scenes, files: s.files })}</p>
              {s.monitored ? <Badge tone="info">{t("adult.monitored")}</Badge> : null}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}


export function AdultStudioPage() {
  const { t } = useTranslation();
  const { id } = useParams({ strict: false }) as { id: string };
  const sid = Number(id);
  const studio = useAdultStudio(sid);
  const items = useAdultItems({ studioId: sid });
  const invalidate = useAdultInvalidate();
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
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <BlurImage image={s.image} alt={s.name} className="size-16" />
        <h2 className="text-lg font-semibold">{s.name}</h2>
        <Checkbox label={t("adult.monitorNew")} checked={s.monitored} onChange={(e) => toggle.mutate(e.target.checked)} />
        <Button busy={refresh.isPending} onClick={() => refresh.mutate()}>{t("adult.refresh")}</Button>
      </div>
      <ErrorAlert error={toggle.error ?? refresh.error} />
      {items.data ? <ItemGrid items={items.data.items} /> : <Spinner />}
    </div>
  );
}

export function AdultPerformersPage() {
  const { t } = useTranslation();
  const perfs = useAdultPerformers();
  return (
    <div>
      {perfs.isPending ? <Spinner /> : null}
      <ErrorAlert error={perfs.error} />
      {perfs.data?.length === 0 ? <EmptyState>{t("adult.empty")}</EmptyState> : null}
      <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {perfs.data?.map((p) => (
          <li key={p.id} className="flex items-center justify-between gap-2 rounded-ui border border-line bg-surface px-3 py-2">
            <Link to="/adult/performers/$id" params={{ id: String(p.id) }} className="truncate text-[13px] hover:underline">{p.name}</Link>
            <span className="flex items-center gap-1.5 text-[12px] text-muted">
              {p.items}
              {p.monitored ? <Badge tone="info">{t("adult.monitored")}</Badge> : null}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function AdultPerformerPage() {
  const { t } = useTranslation();
  const { id } = useParams({ strict: false }) as { id: string };
  const pid = Number(id);
  const perf = useAdultPerformer(pid);
  const items = useAdultItems({ performerId: pid });
  const invalidate = useAdultInvalidate();
  const toggle = useMutation({
    mutationFn: async (monitored: boolean) => unwrap(await api.PATCH("/adult/performers/{id}", { params: { path: { id: pid } }, body: { monitored } })),
    onSuccess: () => invalidate(),
  });
  if (perf.isPending) return <Spinner />;
  if (!perf.data) return <ErrorAlert error={perf.error} />;
  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <BlurImage image={perf.data.image} alt={perf.data.name} className="size-16" />
        <h2 className="text-lg font-semibold">{perf.data.name}</h2>
        <Checkbox label={t("adult.trackPerformer")} description={t("adult.trackPerformerHint")} checked={perf.data.monitored}
          onChange={(e) => toggle.mutate(e.target.checked)} />
      </div>
      <ErrorAlert error={toggle.error} />
      {items.data ? <ItemGrid items={items.data.items} /> : <Spinner />}
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

export function AdultItemPage() {
  const { t, i18n } = useTranslation();
  const { id } = useParams({ strict: false }) as { id: string };
  const iid = Number(id);
  const item = useAdultItem(iid);
  const invalidate = useAdultInvalidate();
  const [tab, setTab] = useState<"history" | "search">("history");
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
  if (item.isPending) return <Spinner />;
  if (!item.data) return <ErrorAlert error={item.error} />;
  const it = item.data;
  return (
    <div data-testid="adult-item">
      <div className="mb-4 grid gap-4 md:grid-cols-[320px_1fr]">
        <BlurImage image={it.image} alt={it.title} className="aspect-video w-full" />
        <div className="min-w-0">
          <h2 className="text-xl font-semibold">{it.title}</h2>
          <p className="text-[13px] text-muted">
            {it.studioId ? <Link to="/adult/studios/$id" params={{ id: String(it.studioId) }} className="hover:underline">{it.studio}</Link> : it.studio}
            {it.releaseDate ? ` · ${it.releaseDate}` : ""}
          </p>
          <ul className="mt-2 flex flex-wrap gap-1.5">
            {it.performers.map((p) => (
              <li key={p.id}><Link to="/adult/performers/$id" params={{ id: String(p.id) }} className="text-[12px] text-accent hover:underline">{p.name}</Link></li>
            ))}
          </ul>
          {it.overview ? <p className="mt-2 text-[13px] text-muted">{it.overview}</p> : null}
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Checkbox label={t("adult.monitored")} checked={it.monitored} onChange={(e) => edit.mutate(e.target.checked)} />
            <Button busy={search.isPending} onClick={() => search.mutate()}>{t("adult.searchNow")}</Button>
            {it.file ? <Button busy={rename.isPending} onClick={() => rename.mutate()}>{t("adult.rename")}</Button> : null}
          </div>
          {it.file ? (
            <p className="mt-2 break-all text-[12px] text-muted" data-testid="adult-file">{it.file.path} · {it.file.quality} · {formatBytes(it.file.size, i18n.language)}</p>
          ) : null}
          <ErrorAlert error={edit.error ?? search.error ?? rename.error} />
        </div>
      </div>
      <div className="mb-3 flex gap-2">
        <Button variant={tab === "history" ? "primary" : "secondary"} onClick={() => setTab("history")}>{t("adult.history")}</Button>
        <Button variant={tab === "search" ? "primary" : "secondary"} onClick={() => setTab("search")}>{t("adult.interactive")}</Button>
      </div>
      {tab === "history" ? (
        <HistoryTab itemRef={it.ref} load={async () => unwrap(await api.GET("/adult/items/{id}/timeline", { params: { path: { id: iid } } }))} />
      ) : (
        <InteractiveSearch source={adultReleases(iid)} itemRef={it.ref} />
      )}
    </div>
  );
}

export function AdultAddPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [source, setSource] = useState<"stashdb" | "tpdb">("stashdb");
  const [type, setType] = useState<"scene" | "movie" | "performer" | "studio">("studio");
  const [term, setTerm] = useState("");
  const roots = useAdultRootFolders();
  const profiles = useQualityProfiles();
  const [profileId, setProfileId] = useState<number>(0);
  const [rootId, setRootId] = useState<number>(0);
  const invalidate = useAdultInvalidate();
  const lookup = useMutation({
    mutationFn: async () => unwrap(await api.GET("/adult/lookup", { params: { query: { source, type, term } } })),
  });
  const add = useMutation({
    mutationFn: async (r: AdultLookupResult) => {
      const body = { source: r.source, id: r.id, monitored: true, search: true, kind: r.type === "movie" ? "movie" as const : "scene" as const,
        ...(profileId ? { qualityProfileId: profileId } : {}), ...(rootId ? { rootFolderId: rootId } : {}) };
      if (r.type === "studio") return { to: "studio", v: unwrap(await api.POST("/adult/studios", { body })) };
      if (r.type === "performer") return { to: "performer", v: unwrap(await api.POST("/adult/performers", { body })) };
      return { to: "item", v: unwrap(await api.POST("/adult/items", { body })) };
    },
    onSuccess: (res) => {
      void invalidate();
      const id = String(res.v.id);
      if (res.to === "studio") void navigate({ to: "/adult/studios/$id", params: { id } });
      else if (res.to === "performer") void navigate({ to: "/adult/performers/$id", params: { id } });
      else void navigate({ to: "/adult/items/$id", params: { id } });
    },
  });
  const movieProfiles = (profiles.data ?? []).filter((p) => p.kind === "movies");
  return (
    <div className="space-y-4">
      {roots.data?.length === 0 ? <Alert tone="warning">{t("adult.noRoot")}</Alert> : null}
      <Panel title={t("adult.add.title")} description={t("adult.add.hint")}>
        <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); if (term.trim()) lookup.mutate(); }}>
          <SelectInput label={t("adult.add.source")} value={source} onChange={(e) => setSource(e.target.value as typeof source)}>
            <option value="stashdb">StashDB</option>
            <option value="tpdb">ThePornDB</option>
          </SelectInput>
          <SelectInput label={t("adult.add.type")} value={type} onChange={(e) => setType(e.target.value as typeof type)}>
            {(["studio", "scene", "movie", "performer"] as const).map((k) => <option key={k} value={k}>{t(`adult.add.types.${k}`)}</option>)}
          </SelectInput>
          <div className="min-w-48 flex-1"><TextInput label={t("adult.add.term")} value={term} maxLength={200} onChange={(e) => setTerm(e.target.value)} /></div>
          <Button type="submit" variant="primary" busy={lookup.isPending} disabled={!term.trim()}>{t("adult.add.search")}</Button>
        </form>
        <div className="mt-3 flex flex-wrap gap-3">
          <SelectInput label={t("adult.add.profile")} value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
            <option value={0}>{t("adult.add.default")}</option>
            {movieProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </SelectInput>
          <SelectInput label={t("adult.add.root")} value={rootId} onChange={(e) => setRootId(Number(e.target.value))}>
            <option value={0}>{t("adult.add.default")}</option>
            {roots.data?.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
          </SelectInput>
        </div>
      </Panel>
      <ErrorAlert error={lookup.error ?? add.error} />
      {lookup.data ? (
        lookup.data.length === 0 ? <EmptyState>{t("adult.add.none")}</EmptyState> : (
          <ul className="divide-y divide-line rounded-ui border border-line bg-surface" data-testid="adult-lookup">
            {lookup.data.map((r) => (
              <li key={`${r.source}-${r.id}`} className="flex items-center justify-between gap-3 px-3 py-2">
                <div className="min-w-0 text-[13px]">
                  <p className="truncate text-fg">{r.name}</p>
                  <p className="truncate text-[12px] text-muted">{[r.studio, r.date, r.performers?.join(", ")].filter(Boolean).join(" · ")}</p>
                </div>
                {r.inLibrary ? <Badge tone="success">{t("adult.add.inLibrary")}</Badge> : (
                  <Button busy={add.isPending && add.variables?.id === r.id} onClick={() => add.mutate(r)}>{t("adult.add.add")}</Button>
                )}
              </li>
            ))}
          </ul>
        )
      ) : null}
    </div>
  );
}
