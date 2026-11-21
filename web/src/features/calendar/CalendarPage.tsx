import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, Link2 } from "lucide-react";
import { api, queryKeys, unwrap, type CalendarEntry } from "@/api/client";
import { Button, ConfirmButton, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";
import { musicKeys, type AlbumCalendarEntry } from "@/features/music/common";
import { monthGrid, rangeFor, step, weekDays, ymd, type CalendarView } from "./dates";

type Item = { kind: "movie"; e: CalendarEntry } | { kind: "album"; e: AlbumCalendarEntry };

const itemKey = (i: Item) => (i.kind === "movie" ? entryKey(i.e) : `a${i.e.albumId}`);

function AlbumEntry({ e, compact }: { e: AlbumCalendarEntry; compact?: boolean }) {
  const { t } = useTranslation();
  return (
    <Link to="/music/albums/$id" params={{ id: String(e.albumId) }} data-testid="calendar-entry"
      className={`block truncate rounded border border-line border-l-2 border-l-success bg-surface-2 px-1.5 py-0.5 text-[12px] hover:bg-surface-3 ${e.hasFiles ? "text-muted line-through decoration-faint" : "text-fg"}`}
      title={`${e.artistName} – ${e.title} · ${t("calendar.kinds.album")}`}>
      {e.artistName} – {e.title}{compact ? "" : <span className="ml-1 text-faint">· {t("calendar.kinds.album")}</span>}
    </Link>
  );
}

function ItemEntry({ i, compact }: { i: Item; compact?: boolean }) {
  return i.kind === "movie" ? <Entry e={i.e} compact={compact} /> : <AlbumEntry e={i.e} compact={compact} />;
}

const kindTone: Record<CalendarEntry["kind"], string> = {
  in_cinemas: "border-l-warning",
  digital: "border-l-info",
  physical: "border-l-accent",
  episode: "border-l-muted",
};

const code = (e: CalendarEntry) => `S${String(e.seasonNumber ?? 0).padStart(2, "0")}E${String(e.episodeNumber ?? 0).padStart(2, "0")}`;

const entryKey = (e: CalendarEntry) => (e.kind === "episode" ? `e${e.episodeId}` : `${e.movieId}-${e.kind}`);

function Entry({ e, compact }: { e: CalendarEntry; compact?: boolean }) {
  const { t, i18n } = useTranslation();
  if (e.kind === "episode") {
    const time = e.airDateUtc ? new Intl.DateTimeFormat(i18n.language, { timeStyle: "short" }).format(new Date(e.airDateUtc)) : "";
    return (
      <Link to="/series/$id" params={{ id: String(e.seriesId) }} data-testid="calendar-entry" data-kind="episode"
        className={`block truncate rounded border border-line border-l-2 ${kindTone.episode} bg-surface-2 px-1.5 py-0.5 text-[12px] hover:bg-surface-3 ${e.hasFile ? "text-muted line-through decoration-faint" : "text-fg"}`}
        title={`${e.title} ${code(e)} ${e.episodeTitle ?? ""}${time ? ` · ${time}` : ""}${e.network ? ` · ${e.network}` : ""}`}>
        {e.title} <span className="tabular-nums text-muted">{code(e)}</span>
        {compact ? "" : <span className="ml-1 text-faint">{e.episodeTitle ? `· ${e.episodeTitle}` : ""}{time ? ` · ${time}` : ""}</span>}
      </Link>
    );
  }
  return (
    <Link to="/movies/$id" params={{ id: String(e.movieId) }} data-testid="calendar-entry"
      className={`block truncate rounded border border-line border-l-2 ${kindTone[e.kind]} bg-surface-2 px-1.5 py-0.5 text-[12px] hover:bg-surface-3 ${e.hasFile ? "text-muted line-through decoration-faint" : "text-fg"}`}
      title={`${e.title} (${e.year}) · ${t(`calendar.kinds.${e.kind}`)}`}>
      {e.title}{compact ? "" : <span className="ml-1 text-faint">· {t(`calendar.kinds.${e.kind}`)}</span>}
    </Link>
  );
}

export function CalendarPage() {
  const { t, i18n } = useTranslation();
  const [view, setView] = useState<CalendarView>("month");
  const [anchor, setAnchor] = useState(() => new Date());
  const { start, end } = rangeFor(view, anchor);
  const cal = useQuery({
    queryKey: queryKeys.calendar(start, end),
    queryFn: async () => unwrap(await api.GET("/calendar", { params: { query: { start, end } } })),
  });
  const albums = useQuery({
    queryKey: musicKeys.calendar(start, end),
    queryFn: async () => unwrap(await api.GET("/music/calendar", { params: { query: { start, end } } })),
  });
  const byDay = useMemo(() => {
    const m = new Map<string, Item[]>();
    const add = (date: string, i: Item) => m.set(date, [...(m.get(date) ?? []), i]);
    for (const e of cal.data ?? []) add(e.date, { kind: "movie", e });
    for (const e of albums.data ?? []) add(e.date, { kind: "album", e });
    return new Map([...m.entries()].sort(([a], [b]) => a.localeCompare(b)));
  }, [cal.data, albums.data]);
  const today = ymd(new Date());
  const title = view === "month"
    ? new Intl.DateTimeFormat(i18n.language, { month: "long", year: "numeric" }).format(anchor)
    : `${new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium" }).format(new Date(start))} – ${new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium" }).format(new Date(end))}`;
  const dayName = (d: Date) => new Intl.DateTimeFormat(i18n.language, { weekday: "short" }).format(d);

  return (
    <div>
      <PageHeader title={t("nav.calendar")} description={t("calendar.description")} />
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Button variant="ghost" onClick={() => setAnchor(step(view, anchor, -1))} aria-label={t("calendar.previous")}><ChevronLeft className="size-4" /></Button>
        <Button variant="ghost" onClick={() => setAnchor(new Date())}>{t("calendar.today")}</Button>
        <Button variant="ghost" onClick={() => setAnchor(step(view, anchor, 1))} aria-label={t("calendar.next")}><ChevronRight className="size-4" /></Button>
        <h2 className="text-[15px] font-semibold capitalize text-fg" aria-live="polite">{title}</h2>
        <div className="ml-auto flex gap-1" role="group" aria-label={t("calendar.view")}>
          {(["month", "week", "agenda"] as const).map((v) => (
            <Button key={v} variant={view === v ? "secondary" : "ghost"} aria-pressed={view === v} onClick={() => setView(v)}>{t(`calendar.views.${v}`)}</Button>
          ))}
        </div>
      </div>
      <ErrorAlert error={cal.error ?? albums.error} />
      {cal.isLoading ? <Spinner /> : null}
      {view === "month" ? (
        <div className="grid grid-cols-7 gap-px overflow-hidden rounded-ui border border-line bg-line text-[12px]">
          {monthGrid(anchor).slice(0, 7).map((d) => <div key={`h${ymd(d)}`} className="bg-surface-2 px-2 py-1 text-muted">{dayName(d)}</div>)}
          {monthGrid(anchor).map((d) => {
            const key = ymd(d);
            const out = d.getMonth() !== anchor.getMonth();
            return (
              <div key={key} className={`min-h-[96px] bg-surface p-1 ${out ? "opacity-50" : ""}`}>
                <div className={`mb-1 text-right text-[11px] tabular-nums ${key === today ? "font-semibold text-accent" : "text-faint"}`}>{d.getDate()}</div>
                <div className="space-y-0.5">{(byDay.get(key) ?? []).map((i) => <ItemEntry key={itemKey(i)} i={i} compact />)}</div>
              </div>
            );
          })}
        </div>
      ) : null}
      {view === "week" ? (
        <div className="grid gap-2 sm:grid-cols-7">
          {weekDays(anchor).map((d) => {
            const key = ymd(d);
            return (
              <div key={key} className="min-h-[160px] rounded-ui border border-line bg-surface p-2">
                <div className={`mb-2 text-[12px] ${key === today ? "font-semibold text-accent" : "text-muted"}`}>{dayName(d)} {d.getDate()}.</div>
                <div className="space-y-1">{(byDay.get(key) ?? []).map((i) => <ItemEntry key={itemKey(i)} i={i} />)}</div>
              </div>
            );
          })}
        </div>
      ) : null}
      {view === "agenda" ? (
        <ol className="space-y-3">
          {[...byDay.entries()].map(([day, list]) => (
            <li key={day}>
              <h3 className="mb-1 text-[12px] font-medium text-muted">
                {new Intl.DateTimeFormat(i18n.language, { weekday: "long", day: "numeric", month: "long", year: "numeric" }).format(new Date(day))}
              </h3>
              <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">{list.map((i) => <ItemEntry key={itemKey(i)} i={i} />)}</div>
            </li>
          ))}
          {cal.data && byDay.size === 0 ? <li className="text-[13px] text-faint">{t("calendar.empty")}</li> : null}
        </ol>
      ) : null}
      <div className="mt-3 flex flex-wrap gap-3 text-[12px] text-muted">
        {(["in_cinemas", "digital", "physical", "episode"] as const).map((k) => (
          <span key={k} className={`border-l-2 pl-1.5 ${kindTone[k]}`}>{t(`calendar.kinds.${k}`)}</span>
        ))}
        <span className="border-l-2 border-l-success pl-1.5">{t("calendar.kinds.album")}</span>
      </div>
      <FeedPanel />
    </div>
  );
}

function FeedPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const state = useQuery({ queryKey: queryKeys.calendarToken, queryFn: async () => unwrap(await api.GET("/calendar/token")) });
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/calendar/token")),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.calendarToken }),
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/calendar/token")),
    onSuccess: () => {
      create.reset();
      void qc.invalidateQueries({ queryKey: queryKeys.calendarToken });
    },
  });
  const url = create.data ? `${window.location.origin}${create.data.path}` : "";
  return (
    <Panel title={t("calendar.feed.title")} description={t("calendar.feed.description")} className="mt-6">
      {url ? (
        <div className="space-y-2">
          <p className="text-[12px] text-warning">{t("calendar.feed.once")}</p>
          <div className="flex gap-2">
            <input readOnly value={url} aria-label={t("calendar.feed.url")} onFocus={(e) => e.currentTarget.select()}
              className="h-8 flex-1 rounded-ui border border-line-strong bg-surface-2 px-2 font-mono text-[12px]" />
            <Button onClick={() => void navigator.clipboard?.writeText(url)}>{t("common.copy")}</Button>
          </div>
        </div>
      ) : (
        <p className="mb-2 text-[13px] text-muted">{state.data?.configured ? t("calendar.feed.active") : t("calendar.feed.none")}</p>
      )}
      <div className="mt-2 flex gap-2">
        <Button onClick={() => create.mutate()} busy={create.isPending}><Link2 className="size-3.5" />
          {state.data?.configured ? t("calendar.feed.replace") : t("calendar.feed.create")}</Button>
        {state.data?.configured ? (
          <ConfirmButton label={t("calendar.feed.stop")} confirmLabel={t("calendar.feed.stopConfirm")} onConfirm={() => remove.mutate()} busy={remove.isPending} />
        ) : null}
      </div>
      <ErrorAlert error={create.error ?? remove.error} />
    </Panel>
  );
}
