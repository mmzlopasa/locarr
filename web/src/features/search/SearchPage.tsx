import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp, Check, Download, ExternalLink, Magnet } from "lucide-react";
import { api, unwrap, type MediaKind, type Release, type SearchIndexerResult, type SearchType } from "@/api/client";
import { errorMessage } from "@/components/errors";
import { formatBytes, formatRelative } from "@/components/format";
import { Badge, Button, EmptyState, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { streamSearch } from "./stream";

const TYPES: SearchType[] = ["search", "movie", "tv", "music", "book"];

/** Top-level Newznab categories offered as filters. */
export const CATEGORY_FILTERS = [
  { id: 2000, key: "movies" },
  { id: 5000, key: "tv" },
  { id: 3000, key: "audio" },
  { id: 7000, key: "books" },
  { id: 1000, key: "console" },
  { id: 4000, key: "pc" },
  { id: 8000, key: "other" },
] as const;

type SortKey = "seeders" | "size" | "age" | "title" | "indexer";

function statusTone(s: SearchIndexerResult["status"]) {
  if (s === "ok") return "success" as const;
  if (s === "error") return "danger" as const;
  if (s === "backoff" || s === "rate_limited") return "warning" as const;
  return "neutral" as const;
}

export function SearchPage() {
  const { t, i18n } = useTranslation();
  const me = useCurrentUser();
  const canGrabLinks = me.role === "admin" || me.role === "manager";
  const [q, setQ] = useState("");
  const [type, setType] = useState<SearchType>("search");
  const [cats, setCats] = useState<number[]>([]);
  const [releases, setReleases] = useState<Release[]>([]);
  const [indexers, setIndexers] = useState<SearchIndexerResult[]>([]);
  const [running, setRunning] = useState(false);
  const [done, setDone] = useState<{ total: number; durationMs: number } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: "seeders", desc: true });
  const abort = useRef<AbortController | null>(null);

  useEffect(() => () => abort.current?.abort(), []);

  const run = async (e: FormEvent) => {
    e.preventDefault();
    abort.current?.abort();
    const ctl = new AbortController();
    abort.current = ctl;
    setReleases([]);
    setIndexers([]);
    setDone(null);
    setError(null);
    setRunning(true);
    try {
      await streamSearch(
        { q, type, categories: cats },
        {
          onIndexer: (ev) => {
            setIndexers((list) => [...list, ev.indexer]);
            if (ev.releases.length) setReleases((list) => [...list, ...ev.releases]);
          },
          onDone: (d) => setDone(d),
        },
        ctl.signal,
      );
    } catch (err) {
      if (!(err instanceof DOMException && err.name === "AbortError")) setError(err);
    } finally {
      if (abort.current === ctl) setRunning(false);
    }
  };

  const sorted = useMemo(() => {
    const val = (r: Release): number | string => {
      switch (sort.key) {
        case "seeders":
          return r.seeders ?? -1;
        case "size":
          return r.size ?? -1;
        case "age":
          return r.publishDate ? -new Date(r.publishDate).getTime() : -Infinity;
        case "title":
          return r.title.toLowerCase();
        case "indexer":
          return r.indexerName.toLowerCase();
      }
    };
    const out = [...releases];
    out.sort((a, b) => {
      const x = val(a);
      const y = val(b);
      const c = x < y ? -1 : x > y ? 1 : 0;
      return sort.desc ? -c : c;
    });
    return out;
  }, [releases, sort]);

  const header = (key: SortKey, label: string, className = "") => {
    const active = sort.key === key;
    const Arrow = sort.desc ? ArrowDown : ArrowUp;
    return (
      <th className={`px-2 py-1.5 font-medium ${className}`} aria-sort={active ? (sort.desc ? "descending" : "ascending") : "none"}>
        <button type="button" className={`inline-flex items-center gap-1 hover:text-fg ${active ? "text-fg" : ""}`}
          onClick={() => setSort({ key, desc: active ? !sort.desc : key !== "title" && key !== "indexer" })}>
          {label}
          {active ? <Arrow className="size-3" aria-hidden /> : null}
        </button>
      </th>
    );
  };

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("search.title")} description={t("search.subtitle")} />
      <Panel>
        <form onSubmit={run} className="flex flex-col gap-3" data-testid="search-form">
          <div className="grid gap-3 sm:grid-cols-[1fr_12rem_auto] sm:items-end">
            <TextInput label={t("search.query")} name="q" value={q} placeholder={t("search.queryPlaceholder")}
              onChange={(e) => setQ(e.target.value)} />
            <SelectInput label={t("search.type")} value={type} onChange={(e) => setType(e.target.value as SearchType)}>
              {TYPES.map((ty) => (
                <option key={ty} value={ty}>
                  {t(`search.types.${ty}`)}
                </option>
              ))}
            </SelectInput>
            <Button type="submit" variant="primary" busy={running}>
              {t("search.submit")}
            </Button>
          </div>
          <fieldset className="flex flex-wrap items-center gap-1.5">
            <legend className="sr-only">{t("search.categories")}</legend>
            <span className="mr-1 text-[12px] text-muted">{t("search.categories")}</span>
            {CATEGORY_FILTERS.map((c) => {
              const on = cats.includes(c.id);
              return (
                <button key={c.id} type="button" aria-pressed={on}
                  onClick={() => setCats(on ? cats.filter((x) => x !== c.id) : [...cats, c.id])}
                  className={`h-6 rounded border px-2 text-[12px] ${on ? "border-accent/50 bg-success-bg text-accent" : "border-line-strong text-muted hover:text-fg"}`}>
                  {t(`search.cats.${c.key}`)}
                </button>
              );
            })}
          </fieldset>
        </form>
      </Panel>

      <ErrorAlert error={error} />

      {indexers.length || running ? (
        <div className="flex flex-wrap items-center gap-1.5" data-testid="search-indexers" aria-live="polite">
          {indexers.map((ir) => (
            <span key={ir.indexerId} title={ir.error ?? undefined}
              className="inline-flex items-center gap-1.5 rounded border border-line bg-surface px-2 py-0.5 text-[12px] text-muted">
              <span className="text-fg">{ir.indexerName}</span>
              <Badge tone={statusTone(ir.status)}>{t(`search.status.${ir.status}`)}</Badge>
              <span>{ir.cached ? t("search.cached") : `${ir.durationMs} ms`}</span>
              {ir.status === "ok" ? <span>· {ir.count}</span> : null}
            </span>
          ))}
          {running ? <Spinner label={t("search.searching")} /> : null}
          {done ? (
            <span className="text-[12px] text-faint" data-testid="search-summary">
              {t("search.summary", { n: done.total, ms: done.durationMs })}
            </span>
          ) : null}
        </div>
      ) : null}

      {releases.length ? (
        <div className="overflow-x-auto rounded-ui border border-line bg-surface">
          <table className="w-full min-w-[56rem] text-left text-[12px]" data-testid="search-results">
            <thead className="border-b border-line text-muted">
              <tr>
                {header("title", t("search.col.title"))}
                {header("indexer", t("search.col.indexer"))}
                {header("size", t("search.col.size"), "text-right")}
                {header("seeders", t("search.col.peers"), "text-right")}
                {header("age", t("search.col.age"))}
                <th className="px-2 py-1.5 font-medium">
                  <span className="sr-only">{t("search.col.actions")}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((r) => (
                <tr key={`${r.indexerId}:${r.guid}`} className="border-b border-line last:border-b-0 hover:bg-surface-2">
                  <td className="max-w-[28rem] px-2 py-1.5">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="break-all text-fg">{r.title}</span>
                      {r.flags.map((f) => (
                        <Badge key={f} tone={f.includes("free") ? "success" : "info"}>
                          {t(`search.flags.${f}`, { defaultValue: f })}
                        </Badge>
                      ))}
                    </div>
                  </td>
                  <td className="px-2 py-1.5 text-muted">
                    {r.indexerName}
                    <span className="ml-1 text-faint">{r.protocol === "usenet" ? t("search.usenet") : t("search.torrent")}</span>
                  </td>
                  <td className="px-2 py-1.5 text-right font-mono">{formatBytes(r.size, i18n.language)}</td>
                  <td className="px-2 py-1.5 text-right font-mono">
                    {r.protocol === "torrent" ? `${r.seeders ?? "–"} / ${r.leechers ?? "–"}` : (r.grabs ?? "–")}
                  </td>
                  <td className="whitespace-nowrap px-2 py-1.5 text-muted">{formatRelative(r.publishDate, i18n.language)}</td>
                  <td className="px-2 py-1.5">
                    <div className="flex items-center justify-end gap-1">
                      {r.infoUrl ? (
                        <a href={r.infoUrl} target="_blank" rel="noreferrer noopener" className="text-muted hover:text-fg"
                          aria-label={t("search.details")} title={t("search.details")}>
                          <ExternalLink className="size-3.5" />
                        </a>
                      ) : null}
                      {canGrabLinks && r.magnetUrl ? (
                        <a href={r.magnetUrl} className="text-muted hover:text-fg" aria-label={t("search.magnet")} title={t("search.magnet")}>
                          <Magnet className="size-3.5" />
                        </a>
                      ) : null}
                      {canGrabLinks && r.downloadLink ? (
                        <a href={r.downloadLink} className="text-muted hover:text-fg" aria-label={t("search.download")}
                          title={t("search.download")}>
                          <Download className="size-3.5" />
                        </a>
                      ) : null}
                      {canGrabLinks ? <GrabButton release={r} mediaKind={KIND_BY_TYPE[type]} /> : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : done && !running ? (
        <EmptyState>{indexers.length ? t("search.noResults") : t("search.noIndexers")}</EmptyState>
      ) : null}
    </div>
  );
}

const KIND_BY_TYPE: Partial<Record<SearchType, MediaKind>> = { movie: "movies", tv: "series", music: "music" };

/** The search result's download token, from its server download link. */
function downloadToken(link: string | undefined): string | undefined {
  if (!link) return undefined;
  return new URL(link, "http://localhost").searchParams.get("token") ?? undefined;
}

/** Sends one release to a download client through the server. */
function GrabButton({ release: r, mediaKind }: { release: Release; mediaKind?: MediaKind }) {
  const { t } = useTranslation();
  const token = downloadToken(r.downloadLink);
  const grab = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/queue/grab", {
          body: {
            protocol: r.protocol,
            title: r.title,
            indexerId: r.indexerId,
            guid: r.guid,
            ...(token ? { downloadToken: token } : { downloadUrl: r.magnetUrl }),
            ...(mediaKind ? { mediaKind } : {}),
            ...(r.publishDate ? { publishedAt: r.publishDate } : {}),
          },
        }),
      ),
  });
  if (!token && !r.magnetUrl) return null;
  if (grab.isSuccess) {
    return (
      <span className="inline-flex h-6 items-center gap-1 px-2 text-[12px] text-success" role="status">
        <Check className="size-3.5" />
        {t("search.grabbed")}
      </span>
    );
  }
  return (
    <span title={grab.error ? errorMessage(t, grab.error) : undefined} className="inline-flex items-center gap-1">
      <Button variant="ghost" busy={grab.isPending} onClick={() => grab.mutate()} aria-label={`${t("search.grab")}: ${r.title}`}
        className={`h-6 px-2 text-[12px] ${grab.isError ? "text-danger" : ""}`}>
        {grab.isError ? t("search.grabRetry") : t("search.grab")}
      </Button>
    </span>
  );
}
