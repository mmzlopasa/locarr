import { useMemo, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, CheckSquare, Eye, EyeOff, LayoutGrid, Rows3, Trash2, X } from "lucide-react";
import { formatBytes } from "@/components/format";
import { Modal } from "@/components/Modal";
import { Badge, Button, Checkbox, ErrorAlert, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { useAdultItemPages, useBulkItems, type AdultItem, type ItemQuery, type RemoveChoice } from "./api";
import { BlurImage } from "./BlurImage";
import { ITEM_SORTS, ITEM_STATUSES, PREF, STATUS_TONE, loadPref, savePref, toggled, type ItemSort, type ViewMode } from "./logic";

/** Managers change the library; everyone else with view_adult only looks. */
export function useCanManageAdult() {
  const me = useCurrentUser();
  return me.role === "admin" || !!me.permissions.manageLibrary;
}

export function StatusBadge({ item }: { item: AdultItem }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex items-center gap-1" data-testid="adult-status" data-status={item.status}>
      <Badge tone={STATUS_TONE[item.status]}>{t(`adult.status.${item.status}`)}</Badge>
      {item.file ? <Badge>{item.kind === "gallery" ? t("adult.gallery.images", { count: item.file.imageCount ?? 0 }) : item.file.quality}</Badge> : null}
    </span>
  );
}

function Meta({ item }: { item: AdultItem }) {
  return (
    <p className="truncate text-[12px] text-muted">
      {item.studio || "–"}
      {item.releaseDate ? <span className="tabular-nums"> · {item.releaseDate}</span> : null}
    </p>
  );
}

function Performers({ item }: { item: AdultItem }) {
  if (item.performers.length === 0) return null;
  return <p className="truncate text-[12px] text-faint">{item.performers.map((p) => p.name).join(", ")}</p>;
}

function SelectMark({ on }: { on: boolean }) {
  return (
    <span aria-hidden className={`absolute left-1.5 top-1.5 flex size-5 items-center justify-center rounded border ${
      on ? "border-accent bg-accent text-accent-ink" : "border-line-strong bg-bg/80 text-transparent"}`}>
      <Check className="size-3.5" />
    </span>
  );
}

/**
 * A scene, movie or gallery card: image (16:9, or 3:4 for galleries), status,
 * two-line title, studio, date, performers.
 */
export function SceneCard({ item, portrait, selecting, selected, onSelect }:
  { item: AdultItem; portrait?: boolean; selecting?: boolean; selected?: boolean; onSelect?: () => void }) {
  const link = { to: "/adult/items/$id" as const, params: { id: String(item.id) } };
  const aspect = portrait ? "aspect-[3/4]" : "aspect-video";
  const body = (
    <>
      <div className="relative">
        {selecting ? (
          <BlurImage image={item.image} alt={item.title} fallback={item.studio || item.title} className={`pointer-events-none ${aspect} w-full`} />
        ) : (
          <BlurImage image={item.image} alt={item.title} fallback={item.studio || item.title} className={`${aspect} w-full`} link={link} />
        )}
        <span className="pointer-events-none absolute bottom-1.5 left-1.5"><StatusBadge item={item} /></span>
        {!item.monitored && !selecting ? (
          <span className="pointer-events-none absolute right-1.5 top-1.5 size-2 rounded-full bg-faint ring-2 ring-bg" aria-hidden />
        ) : null}
        {selecting ? <SelectMark on={!!selected} /> : null}
      </div>
      <div className="mt-1.5 min-w-0">
        {selecting ? (
          <p className="line-clamp-2 min-h-[2lh] text-[13px] font-medium leading-snug text-fg">{item.title}</p>
        ) : (
          <Link {...link} className="line-clamp-2 min-h-[2lh] text-[13px] font-medium leading-snug text-fg hover:text-accent">{item.title}</Link>
        )}
        <Meta item={item} />
        <Performers item={item} />
      </div>
    </>
  );
  if (selecting) {
    return (
      <button type="button" role="checkbox" aria-checked={!!selected} onClick={onSelect} data-testid="adult-card"
        className={`block w-full rounded-ui p-1 text-left outline-offset-2 ${selected ? "bg-surface-3" : ""}`}>
        {body}
      </button>
    );
  }
  return <div className="p-1" data-testid="adult-card">{body}</div>;
}

/** The same item as a dense list row. */
export function SceneRow({ item, selecting, selected, onSelect }:
  { item: AdultItem; selecting?: boolean; selected?: boolean; onSelect?: () => void }) {
  const { i18n } = useTranslation();
  const link = { to: "/adult/items/$id" as const, params: { id: String(item.id) } };
  const inner = (
    <>
      {selecting ? (
        <span className={`flex size-5 shrink-0 items-center justify-center rounded border ${selected ? "border-accent bg-accent text-accent-ink" : "border-line-strong text-transparent"}`} aria-hidden>
          <Check className="size-3.5" />
        </span>
      ) : null}
      <BlurImage image={item.image} alt={item.title} fallback={item.studio || item.title}
        className={`aspect-video w-24 shrink-0 sm:w-32 ${selecting ? "pointer-events-none" : ""}`} link={selecting ? undefined : link} />
      <div className="min-w-0 flex-1">
        {selecting ? <p className="truncate text-[13px] font-medium text-fg">{item.title}</p> : (
          <Link {...link} className="block truncate text-[13px] font-medium text-fg hover:text-accent">{item.title}</Link>
        )}
        <Meta item={item} />
        <Performers item={item} />
      </div>
      <div className="hidden shrink-0 flex-col items-end gap-1 sm:flex">
        <StatusBadge item={item} />
        {item.file ? <span className="text-[12px] tabular-nums text-faint">{formatBytes(item.file.size, i18n.language)}</span> : null}
      </div>
    </>
  );
  if (selecting) {
    return (
      <button type="button" role="checkbox" aria-checked={!!selected} onClick={onSelect} data-testid="adult-card"
        className={`flex w-full items-center gap-3 px-2 py-2 text-left ${selected ? "bg-surface-3" : "hover:bg-surface-2"}`}>{inner}</button>
    );
  }
  return <div className="flex items-center gap-3 px-2 py-2 hover:bg-surface-2" data-testid="adult-card">{inner}</div>;
}

/** "Also delete the file" and "Not interested" choices shared by every remove dialog. */
export function RemoveChoices({ value, onChange, files, excludeHint }:
  { value: RemoveChoice; onChange: (v: RemoveChoice) => void; files: boolean; excludeHint?: string }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-3">
      <Checkbox label={t("adult.remove.exclude")} description={excludeHint ?? t("adult.remove.excludeHint")} checked={value.exclude}
        onChange={(e) => onChange({ ...value, exclude: e.target.checked })} data-testid="adult-remove-exclude" />
      {files ? (
        <Checkbox label={t("adult.remove.files")} description={t("adult.remove.filesHint")} checked={value.deleteFiles}
          onChange={(e) => onChange({ ...value, deleteFiles: e.target.checked })} data-testid="adult-remove-files" />
      ) : null}
    </div>
  );
}

export function RemoveDialog({ title, children, busy, error, onConfirm, onClose, confirm }:
  { title: string; children: ReactNode; busy: boolean; error: unknown; onConfirm: () => void; onClose: () => void; confirm?: string }) {
  const { t } = useTranslation();
  return (
    <Modal title={title} onClose={onClose} testId="adult-remove-dialog"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="danger" busy={busy} onClick={onConfirm} data-testid="adult-remove-confirm">
            <Trash2 className="size-3.5" aria-hidden /> {confirm ?? t("adult.remove.confirm")}
          </Button>
        </>
      }>
      <div className="space-y-4 text-[13px]">
        {children}
        <ErrorAlert error={error} />
      </div>
    </Modal>
  );
}

export function EmptyAdult({ title, hint, actions }: { title: string; hint?: string; actions?: ReactNode }) {
  return (
    <div className="flex flex-col items-center rounded-ui border border-dashed border-line-strong px-4 py-12 text-center" data-testid="adult-empty">
      <p className="text-[14px] font-medium text-fg">{title}</p>
      {hint ? <p className="mt-1 max-w-md text-[13px] text-muted">{hint}</p> : null}
      {actions ? <div className="mt-4 flex flex-wrap justify-center gap-2">{actions}</div> : null}
    </div>
  );
}

export const linkButton = "inline-flex h-8 items-center gap-1.5 rounded-ui border border-line-strong bg-surface-3 px-3 text-[13px] text-fg hover:bg-line";
export const primaryLink = "inline-flex h-8 items-center gap-1.5 rounded-ui bg-accent px-3 text-[13px] font-semibold text-accent-ink hover:bg-accent-hover";

/**
 * A filterable, sortable list of scenes or movies with grid/list views and
 * a selection mode for bulk monitor, unmonitor and remove (managers).
 */
export function ItemBrowser({ base, empty, showStatusFilter = true, testId = "adult-items" }:
  { base: ItemQuery; empty: ReactNode; showStatusFilter?: boolean; testId?: string }) {
  const { t } = useTranslation();
  const canManage = useCanManageAdult();
  const [term, setTerm] = useState("");
  const [status, setStatus] = useState<ItemQuery["status"] | "">("");
  const [monitored, setMonitored] = useState<"" | "true" | "false">("");
  const defaultSort: ItemSort = base.kind === "movie" ? "title" : "date";
  const portrait = base.kind === "gallery";
  const [sort, setSortState] = useState<ItemSort>(() => loadPref(`${PREF.itemSort}.${base.kind ?? "all"}`, ITEM_SORTS, defaultSort));
  const [view, setViewState] = useState<ViewMode>(() => loadPref(PREF.view, ["grid", "list"] as const, "grid"));
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [removing, setRemoving] = useState(false);
  const [choice, setChoice] = useState<RemoveChoice>({ deleteFiles: false, exclude: true });
  const bulk = useBulkItems();

  const query: ItemQuery = {
    ...base, term: term.trim() || undefined, status: status || undefined, sort,
    monitored: monitored === "" ? undefined : monitored === "true",
  };
  const pages = useAdultItemPages(query);
  const items = useMemo(() => pages.data?.pages.flatMap((p) => p.items) ?? [], [pages.data]);
  const total = pages.data?.pages[0]?.total ?? 0;
  const filtered = !!(query.term || query.status || query.monitored !== undefined);
  const selectedItems = items.filter((i) => selected.has(i.id));
  const anyFile = selectedItems.some((i) => i.file);

  const setSort = (v: ItemSort) => { setSortState(v); savePref(`${PREF.itemSort}.${base.kind ?? "all"}`, v); };
  const setView = (v: ViewMode) => { setViewState(v); savePref(PREF.view, v); };
  const stop = () => { setSelecting(false); setSelected(new Set()); };
  const run = (action: "monitor" | "unmonitor" | "remove") => {
    const ids = [...selected];
    if (ids.length === 0) return;
    bulk.mutate({ ids, action, ...(action === "remove" ? choice : { deleteFiles: false, exclude: false }) }, {
      onSuccess: () => { setRemoving(false); if (action === "remove") stop(); },
    });
  };

  return (
    <div data-testid={testId}>
      <div className="mb-3 grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-[minmax(0,2fr)_repeat(3,minmax(0,1fr))_auto]">
        <div className="col-span-2 sm:col-span-4 lg:col-span-1">
          <TextInput label={t("adult.filter")} value={term} onChange={(e) => setTerm(e.target.value)} placeholder={t("adult.filterHint")} type="search" />
        </div>
        <SelectInput label={t("adult.sort.label")} value={sort} onChange={(e) => setSort(e.target.value as ItemSort)} data-testid="adult-item-sort">
          {ITEM_SORTS.map((s) => <option key={s} value={s}>{t(`adult.sort.${s}`)}</option>)}
        </SelectInput>
        {showStatusFilter ? (
          <SelectInput label={t("adult.statusFilter")} value={status} onChange={(e) => setStatus(e.target.value as typeof status)} data-testid="adult-status-filter">
            <option value="">{t("adult.all")}</option>
            {ITEM_STATUSES.map((s) => <option key={s} value={s}>{t(`adult.status.${s}`)}</option>)}
          </SelectInput>
        ) : <span className="hidden sm:block" />}
        <SelectInput label={t("adult.monitoredFilter")} value={monitored} onChange={(e) => setMonitored(e.target.value as typeof monitored)}>
          <option value="">{t("adult.all")}</option>
          <option value="true">{t("adult.monitored")}</option>
          <option value="false">{t("adult.unmonitored")}</option>
        </SelectInput>
        <div className="col-span-2 flex items-end justify-between gap-1 sm:col-span-1 sm:justify-end">
          <div className="flex gap-1" role="group" aria-label={t("adult.view.label")}>
            <Button variant={view === "grid" ? "secondary" : "ghost"} onClick={() => setView("grid")} aria-pressed={view === "grid"} title={t("adult.view.grid")}>
              <LayoutGrid className="size-3.5" aria-hidden /><span className="sr-only">{t("adult.view.grid")}</span>
            </Button>
            <Button variant={view === "list" ? "secondary" : "ghost"} onClick={() => setView("list")} aria-pressed={view === "list"} title={t("adult.view.list")}>
              <Rows3 className="size-3.5" aria-hidden /><span className="sr-only">{t("adult.view.list")}</span>
            </Button>
          </div>
          {canManage ? (
            <Button variant={selecting ? "secondary" : "ghost"} aria-pressed={selecting} onClick={() => (selecting ? stop() : setSelecting(true))}
              data-testid="adult-select">
              <CheckSquare className="size-3.5" aria-hidden /> {selecting ? t("adult.select.done") : t("adult.select.start")}
            </Button>
          ) : null}
        </div>
      </div>

      <div className="mb-2 flex items-center justify-between text-[12px] text-muted">
        <span data-testid="adult-count">{pages.data ? t("adult.count", { count: total }) : " "}</span>
      </div>

      {pages.isPending ? <Spinner /> : null}
      <ErrorAlert error={pages.error} />
      {pages.data && items.length === 0 ? (filtered ? <EmptyAdult title={t("adult.emptyFiltered")} /> : empty) : null}

      {items.length > 0 && view === "grid" ? (
        <ul className="grid grid-cols-2 gap-x-2 gap-y-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
          {items.map((it) => (
            <li key={it.id} className="min-w-0">
              <SceneCard item={it} portrait={portrait} selecting={selecting} selected={selected.has(it.id)} onSelect={() => setSelected((s) => toggled(s, it.id))} />
            </li>
          ))}
        </ul>
      ) : null}
      {items.length > 0 && view === "list" ? (
        <ul className="divide-y divide-line overflow-hidden rounded-ui border border-line bg-surface">
          {items.map((it) => (
            <li key={it.id}>
              <SceneRow item={it} selecting={selecting} selected={selected.has(it.id)} onSelect={() => setSelected((s) => toggled(s, it.id))} />
            </li>
          ))}
        </ul>
      ) : null}
      {pages.hasNextPage ? (
        <div className="mt-4 flex justify-center">
          <Button busy={pages.isFetchingNextPage} onClick={() => void pages.fetchNextPage()}>{t("adult.loadMore")}</Button>
        </div>
      ) : null}

      {selecting ? (
        <div className="sticky bottom-2 z-20 mt-4 flex flex-wrap items-center gap-2 rounded-ui border border-line-strong bg-surface-2 p-2 shadow-2xl"
          data-testid="adult-selection-bar">
          <span className="px-1 text-[13px] text-fg" data-testid="adult-selected-count">{t("adult.select.count", { count: selected.size })}</span>
          <Button variant="ghost" onClick={() => setSelected(new Set(items.map((i) => i.id)))}>{t("adult.select.all")}</Button>
          {selected.size > 0 ? <Button variant="ghost" onClick={() => setSelected(new Set())}>{t("adult.select.none")}</Button> : null}
          <span className="flex-1" />
          <Button disabled={!selected.size} busy={bulk.isPending && bulk.variables?.action === "monitor"} onClick={() => run("monitor")} data-testid="adult-bulk-monitor">
            <Eye className="size-3.5" aria-hidden /> {t("adult.bulk.monitor")}
          </Button>
          <Button disabled={!selected.size} busy={bulk.isPending && bulk.variables?.action === "unmonitor"} onClick={() => run("unmonitor")} data-testid="adult-bulk-unmonitor">
            <EyeOff className="size-3.5" aria-hidden /> {t("adult.bulk.unmonitor")}
          </Button>
          <Button variant="danger" disabled={!selected.size} onClick={() => setRemoving(true)} data-testid="adult-bulk-remove">
            <Trash2 className="size-3.5" aria-hidden /> {t("adult.bulk.remove")}
          </Button>
          <Button variant="ghost" onClick={stop} aria-label={t("common.cancel")}><X className="size-3.5" aria-hidden /></Button>
          {bulk.error && !removing ? <div className="w-full"><ErrorAlert error={bulk.error} /></div> : null}
        </div>
      ) : null}

      {removing ? (
        <RemoveDialog title={t("adult.bulk.removeTitle", { count: selected.size })} busy={bulk.isPending} error={bulk.error}
          onClose={() => setRemoving(false)} onConfirm={() => run("remove")}>
          <p className="text-fg">{t("adult.bulk.removeQuestion", { count: selected.size })}</p>
          <RemoveChoices value={choice} onChange={setChoice} files={anyFile} />
        </RemoveDialog>
      ) : null}
    </div>
  );
}
