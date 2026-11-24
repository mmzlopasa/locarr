import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, ThumbsDown, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { formatRelative } from "@/components/format";
import { Alert, Button, Checkbox, EmptyState, ErrorAlert, PageHeader, SelectInput, Spinner } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { musicKeys } from "./common";
import {
  AddSuggestionsDialog, Attribution, discoveryKeys, InLibraryLink, PROVIDERS, SuggestionCard, useArtistSuggestions, useExclude,
  type ArtistSuggestion, type DiscoveryProviderId,
} from "./discovery";

const PAGE = 48;
const MIN_SCORES = [0, 25, 50, 75];

export type DiscoverArtistsSearch = { seed?: string[] };

export function validateDiscoverArtistsSearch(s: Record<string, unknown>): DiscoverArtistsSearch {
  const raw = Array.isArray(s.seed) ? s.seed : typeof s.seed === "string" ? s.seed.split(",") : [];
  const seed = raw.filter((x): x is string => typeof x === "string" && /^[0-9a-fA-F-]{36}$/.test(x)).slice(0, 50);
  return seed.length ? { seed } : {};
}

/** Music → Discover artists: artists similar to the library's, to add in one click. */
export function DiscoverArtistsPage() {
  const { t, i18n } = useTranslation();
  const me = useCurrentUser();
  const search = useSearch({ from: "/app/music/discover" });
  const navigate = useNavigate();
  const seeds = search.seed ?? [];
  const [tag, setTag] = useState("");
  const [minScore, setMinScore] = useState(0);
  const [providers, setProviders] = useState<DiscoveryProviderId[]>([]);
  const [limit, setLimit] = useState(PAGE);
  const [selected, setSelected] = useState<Record<string, ArtistSuggestion>>({});
  const [adding, setAdding] = useState<ArtistSuggestion[] | null>(null);
  const q = { seed: seeds, tag, minScore, provider: providers, limit };
  const list = useArtistSuggestions(q);
  const exclude = useExclude();
  const qc = useQueryClient();
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/music/discover/refresh", { body: seeds.length ? { seeds } : {} })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: discoveryKeys.all }),
  });
  const library = useQuery({
    queryKey: [...musicKeys.artists, "seed-picker"],
    queryFn: async () => unwrap(await api.GET("/music/artists", { params: { query: { sort: "name", limit: 1000 } } })),
    staleTime: 60_000,
  });
  const data = list.data;
  const active = (data?.providers ?? []).filter((p) => p.enabled && p.configured).map((p) => p.id);
  const used = useMemo(() => {
    const s = new Set<DiscoveryProviderId>();
    for (const it of data?.items ?? []) for (const p of it.providers) s.add(p);
    return PROVIDERS.filter((p) => s.has(p));
  }, [data]);
  const spotify = data?.providers.find((p) => p.id === "spotify");
  const picked = Object.values(selected);
  const seedNames = (library.data?.items ?? []).filter((a) => seeds.includes(a.mbid.toLowerCase()) || seeds.includes(a.mbid));
  const setSeeds = (next: string[]) => void navigate({ to: "/music/discover", search: next.length ? { seed: next } : {} });
  const toggleProvider = (p: DiscoveryProviderId, on: boolean) =>
    setProviders((cur) => (on ? [...cur, p] : cur.filter((x) => x !== p)));
  return (
    <div>
      <PageHeader title={t("discovery.title")} description={t("discovery.description")}
        actions={(
          <>
            {picked.length ? (
              <Button variant="primary" onClick={() => setAdding(picked)} data-testid="bulk-add">
                <Plus className="size-3.5" /> {t("discovery.addCount", { count: picked.length })}
              </Button>
            ) : null}
            <Button onClick={() => refresh.mutate()} busy={refresh.isPending} data-testid="discovery-refresh">
              <RefreshCw className="size-3.5" /> {t("discovery.refresh")}
            </Button>
          </>
        )} />
      <div className="mb-3 grid grid-cols-2 gap-2 lg:grid-cols-4">
        <SelectInput label={t("discovery.basedOn")} value="" data-testid="seed-picker"
          onChange={(e) => e.target.value && setSeeds([...seeds, e.target.value])}>
          <option value="">{seeds.length ? t("discovery.addSeed") : t("discovery.allArtists")}</option>
          {(library.data?.items ?? []).filter((a) => !seeds.includes(a.mbid)).map((a) => <option key={a.id} value={a.mbid}>{a.name}</option>)}
        </SelectInput>
        <SelectInput label={t("discovery.tag")} value={tag} onChange={(e) => setTag(e.target.value)} data-testid="tag-filter">
          <option value="">{t("discovery.anyTag")}</option>
          {(data?.tags ?? []).map((x) => <option key={x.name} value={x.name}>{x.name} ({x.count})</option>)}
          {tag && !(data?.tags ?? []).some((x) => x.name === tag) ? <option value={tag}>{tag}</option> : null}
        </SelectInput>
        <SelectInput label={t("discovery.minScore")} value={minScore} onChange={(e) => setMinScore(Number(e.target.value))}>
          {MIN_SCORES.map((m) => <option key={m} value={m}>{m === 0 ? t("discovery.anyScore") : t("discovery.atLeast", { score: m })}</option>)}
        </SelectInput>
        <fieldset className="flex flex-col gap-1">
          <legend className="mb-1 text-[12px] font-medium text-muted">{t("discovery.providersLabel")}</legend>
          <div className="flex flex-wrap gap-x-3 gap-y-1">
            {PROVIDERS.filter((p) => active.includes(p)).map((p) => (
              <Checkbox key={p} label={t(`discovery.providers.${p}`)} checked={providers.length === 0 || providers.includes(p)}
                onChange={(e) => {
                  if (providers.length === 0) setProviders(active.filter((x) => x !== p || e.target.checked));
                  else toggleProvider(p, e.target.checked);
                }} />
            ))}
          </div>
        </fieldset>
      </div>
      {seeds.length ? (
        <div className="mb-3 flex flex-wrap items-center gap-1.5" data-testid="seed-chips">
          <span className="text-[12px] text-muted">{t("discovery.basedOnThese")}</span>
          {seeds.map((id) => (
            <span key={id} className="inline-flex h-7 items-center gap-1 rounded-full border border-line-strong bg-surface-2 pl-3 pr-1.5 text-[12px] text-fg">
              {seedNames.find((a) => a.mbid === id)?.name ?? id.slice(0, 8)}
              <button type="button" onClick={() => setSeeds(seeds.filter((x) => x !== id))} aria-label={t("discovery.removeSeed")}
                className="rounded-full p-0.5 text-muted hover:text-fg"><X className="size-3" /></button>
            </span>
          ))}
          <Button variant="ghost" onClick={() => setSeeds([])}>{t("discovery.clearSeeds")}</Button>
        </div>
      ) : null}
      <ErrorAlert error={list.error ?? refresh.error ?? exclude.error} />
      {data && active.length === 0 ? (
        <Alert tone="info">
          {t("discovery.noProviders")}{" "}
          {me.role === "admin" ? <Link to="/settings/metadata" className="underline">{t("settings.nav.items.metadata")}</Link> : null}
        </Alert>
      ) : null}
      {data && (data.refreshing || data.pending > 0) ? (
        <p role="status" className="mb-3 flex items-center gap-2 text-[13px] text-muted" data-testid="discovery-working">
          <RefreshCw className="size-3.5 animate-spin motion-reduce:animate-none" aria-hidden /> {t("discovery.working", { count: data.pending })}
        </p>
      ) : null}
      {spotify?.enabled && spotify.configured && spotify.relatedArtists === "restricted" ? (
        <p className="mb-3 text-[12px] text-faint">{t("discovery.spotifyRestricted")}</p>
      ) : null}
      {list.isPending ? <Spinner /> : null}
      {data && data.items.length === 0 && !data.refreshing && data.pending === 0 && active.length > 0 ? (
        <EmptyState>{data.lastRefreshAt ? t("discovery.empty") : t("discovery.emptyNever")}</EmptyState>
      ) : null}
      {data && data.items.length ? (
        <>
          <p className="mb-2 text-[12px] text-faint">
            {t("discovery.count", { count: data.total })}
            {data.lastRefreshAt ? ` · ${t("discovery.lastRefresh", { when: formatRelative(data.lastRefreshAt, i18n.language) })}` : ""}
          </p>
          <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-7" data-testid="suggestions">
            {data.items.map((s) => (
              <SuggestionCard key={s.mbid} s={s} selected={!!selected[s.mbid]}
                onSelect={s.artistId ? undefined : (on) => setSelected((cur) => {
                  const next = { ...cur };
                  if (on) next[s.mbid] = s;
                  else delete next[s.mbid];
                  return next;
                })}
                actions={s.artistId ? <InLibraryLink id={s.artistId} /> : (
                  <>
                    <Button variant="primary" onClick={() => setAdding([s])} data-testid={`add-${s.mbid}`}>
                      <Plus className="size-3.5" /> {t("discovery.add")}
                    </Button>
                    <Button variant="ghost" onClick={() => exclude.mutate(s)} busy={exclude.isPending && exclude.variables?.mbid === s.mbid}
                      data-testid={`exclude-${s.mbid}`} title={t("discovery.notInterestedHint")}>
                      <ThumbsDown className="size-3.5" /> {t("discovery.notInterested")}
                    </Button>
                  </>
                )} />
            ))}
          </ul>
          {data.total > data.items.length ? (
            <div className="mt-4 flex justify-center">
              <Button onClick={() => setLimit((n) => Math.min(n + PAGE, 100))} disabled={limit >= 100}>{t("discovery.more")}</Button>
            </div>
          ) : null}
        </>
      ) : null}
      <div className="mt-6"><Attribution providers={used} /></div>
      <ExclusionsPanel />
      {adding ? (
        <AddSuggestionsDialog items={adding} onClose={() => setAdding(null)} onAdded={() => setSelected({})} />
      ) : null}
    </div>
  );
}

function ExclusionsPanel() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const qc = useQueryClient();
  const list = useQuery({ queryKey: discoveryKeys.exclusions, enabled: open,
    queryFn: async () => unwrap(await api.GET("/music/discover/exclusions")) });
  const undo = useMutation({
    mutationFn: async (mbid: string) => unwrap(await api.DELETE("/music/discover/exclusions/{mbid}", { params: { path: { mbid } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: discoveryKeys.all }),
  });
  return (
    <section className="mt-6 rounded-ui border border-line bg-surface" aria-labelledby="excluded-title">
      <div className="flex items-start justify-between gap-3 p-4">
        <div>
          <h2 id="excluded-title" className="text-[14px] font-semibold text-fg">{t("discovery.excluded")}</h2>
          <p className="text-[12.5px] text-muted">{t("discovery.excludedHint")}</p>
        </div>
        <Button variant="ghost" onClick={() => setOpen(!open)} aria-expanded={open} data-testid="toggle-exclusions">
          {open ? t("discovery.hide") : t("discovery.show")}
        </Button>
      </div>
      {open ? (
        <div className="border-t border-line px-4 py-2">
          {list.isPending ? <Spinner /> : (list.data ?? []).length === 0 ? <EmptyState>{t("discovery.noneExcluded")}</EmptyState> : (
            <ul className="divide-y divide-line" data-testid="exclusions">
              {(list.data ?? []).map((e) => (
                <li key={e.mbid} className="flex items-center justify-between gap-2 py-1.5 text-[13px]">
                  <span className="truncate text-fg">{e.name || e.mbid}</span>
                  <Button variant="ghost" onClick={() => undo.mutate(e.mbid)} busy={undo.isPending && undo.variables === e.mbid}>
                    {t("discovery.suggestAgain")}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <ErrorAlert error={list.error ?? undo.error} />
        </div>
      ) : null}
    </section>
  );
}
