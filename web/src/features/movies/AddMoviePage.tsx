import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { api, queryKeys, unwrap, type MinimumAvailability, type MovieLookup } from "@/api/client";
import { Alert, Button, Checkbox, ErrorAlert, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { AVAILABILITIES, Poster, useQualityProfiles, useRootFolders } from "./common";

export type AddDefaults = {
  rootFolderId: number;
  qualityProfileId: number;
  minimumAvailability: MinimumAvailability;
  monitored: boolean;
  tags: string;
  searchOnAdd: boolean;
};

/** Root folder, profile, availability, monitoring, tags and "search now" for new movies. */
export function AddOptionsForm({ value, onChange }: { value: AddDefaults; onChange: (v: AddDefaults) => void }) {
  const { t } = useTranslation();
  const roots = useRootFolders();
  const profiles = useQualityProfiles();
  const movieRoots = (roots.data ?? []).filter((r) => r.kind === "movies");
  const movieProfiles = (profiles.data ?? []).filter((p) => p.kind === "movies");
  useEffect(() => {
    if (!value.rootFolderId && movieRoots[0]) onChange({ ...value, rootFolderId: movieRoots[0].id });
    if (!value.qualityProfileId && movieProfiles[0]) onChange({ ...value, qualityProfileId: movieProfiles[0].id });
  }, [movieRoots, movieProfiles, value, onChange]);
  if (roots.data && movieRoots.length === 0) {
    return (
      <Alert tone="warning">
        {t("addMovie.noRoot")} <Link to="/settings/media" className="underline">{t("settings.tabs.media")}</Link>
      </Alert>
    );
  }
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <SelectInput label={t("addMovie.rootFolder")} value={value.rootFolderId} onChange={(e) => onChange({ ...value, rootFolderId: Number(e.target.value) })}>
        {movieRoots.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
      </SelectInput>
      <SelectInput label={t("addMovie.profile")} value={value.qualityProfileId} onChange={(e) => onChange({ ...value, qualityProfileId: Number(e.target.value) })}>
        {movieProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </SelectInput>
      <SelectInput label={t("addMovie.availability")} value={value.minimumAvailability}
        onChange={(e) => onChange({ ...value, minimumAvailability: e.target.value as MinimumAvailability })}>
        {AVAILABILITIES.map((a) => <option key={a} value={a}>{t(`availability.${a}`)}</option>)}
      </SelectInput>
      <TextInput label={t("addMovie.tags")} hint={t("addMovie.tagsHint")} value={value.tags} onChange={(e) => onChange({ ...value, tags: e.target.value })} />
      <Checkbox label={t("addMovie.monitored")} description={t("addMovie.monitoredHint")} checked={value.monitored}
        onChange={(e) => onChange({ ...value, monitored: e.target.checked })} />
      <Checkbox label={t("addMovie.searchOnAdd")} description={t("addMovie.searchOnAddHint")} checked={value.searchOnAdd}
        onChange={(e) => onChange({ ...value, searchOnAdd: e.target.checked })} />
    </div>
  );
}

export function splitTags(s: string): string[] {
  return s.split(",").map((x) => x.trim().toLowerCase()).filter(Boolean);
}

export function useAddDefaults() {
  const mm = useQuery({ queryKey: queryKeys.mediaManagement, queryFn: async () => unwrap(await api.GET("/settings/mediamanagement")), retry: false });
  const [v, setV] = useState<AddDefaults>({ rootFolderId: 0, qualityProfileId: 0, minimumAvailability: "released", monitored: true, tags: "", searchOnAdd: true });
  useEffect(() => {
    if (mm.data) setV((x) => ({ ...x, searchOnAdd: mm.data.searchOnAddDefault }));
  }, [mm.data]);
  return [v, setV] as const;
}

export function AddMoviePage() {
  const { t } = useTranslation();
  const [term, setTerm] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [picked, setPicked] = useState<MovieLookup | null>(null);
  const [opts, setOpts] = useAddDefaults();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const lookup = useQuery({
    queryKey: ["movies", "lookup", submitted],
    queryFn: async () => unwrap(await api.GET("/movies/lookup", { params: { query: { term: submitted } } })),
    enabled: submitted.length > 0,
  });
  const add = useMutation({
    mutationFn: async (m: MovieLookup) =>
      unwrap(await api.POST("/movies", { body: { tmdbId: m.tmdbId, rootFolderId: opts.rootFolderId, qualityProfileId: opts.qualityProfileId,
        minimumAvailability: opts.minimumAvailability, monitored: opts.monitored, tags: splitTags(opts.tags), searchOnAdd: opts.searchOnAdd } })),
    onSuccess: (m) => {
      void qc.invalidateQueries({ queryKey: queryKeys.movies });
      void navigate({ to: "/movies/$id", params: { id: String(m.id) } });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setPicked(null);
    setSubmitted(term.trim());
  };
  return (
    <div>
      <PageHeader title={t("addMovie.title")} description={t("addMovie.description")} />
      <form onSubmit={submit} className="mb-4 flex items-end gap-2" role="search">
        <div className="flex-1">
          <TextInput label={t("addMovie.search")} placeholder={t("addMovie.searchHint")} value={term} onChange={(e) => setTerm(e.target.value)}
            autoFocus data-testid="add-movie-term" />
        </div>
        <Button type="submit" variant="primary" disabled={!term.trim()}>
          <Search className="size-3.5" /> {t("addMovie.searchButton")}
        </Button>
      </form>
      {lookup.isFetching ? <Spinner /> : null}
      <ErrorAlert error={lookup.error} />
      {lookup.data && lookup.data.length === 0 ? <p className="text-[13px] text-muted">{t("addMovie.noResults")}</p> : null}
      {picked ? (
        <Panel title={`${picked.title}${picked.year ? ` (${picked.year})` : ""}`} description={t("addMovie.optionsDescription")}
          actions={<Button variant="ghost" onClick={() => setPicked(null)}>{t("common.back")}</Button>}>
          <div className="flex gap-4">
            <Poster file={picked.poster} title={picked.title} size="w185" className="hidden w-28 shrink-0 sm:block" />
            <div className="flex-1">
              <AddOptionsForm value={opts} onChange={setOpts} />
              <div className="mt-4 flex items-center gap-2">
                <Button variant="primary" busy={add.isPending} onClick={() => add.mutate(picked)} disabled={!opts.rootFolderId || !opts.qualityProfileId}
                  data-testid="add-movie-confirm">
                  {t("addMovie.add")}
                </Button>
                <ErrorAlert error={add.error} />
              </div>
            </div>
          </div>
        </Panel>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {(lookup.data ?? []).map((m) => (
            <li key={m.tmdbId} className="flex gap-3 rounded-ui border border-line bg-surface p-3">
              <Poster file={m.poster} title={m.title} size="w185" className="w-20 shrink-0" />
              <div className="flex min-w-0 flex-1 flex-col">
                <div className="flex items-baseline gap-2">
                  <h2 className="truncate text-[14px] font-semibold text-fg">{m.title}</h2>
                  <span className="text-[12px] tabular-nums text-muted">{m.year || "–"}</span>
                </div>
                {m.originalTitle && m.originalTitle !== m.title ? <p className="truncate text-[12px] text-faint">{m.originalTitle}</p> : null}
                <p className="mt-1 line-clamp-3 text-[12px] text-muted">{m.overview}</p>
                <div className="mt-auto pt-2">
                  {m.movieId ? (
                    <Link to="/movies/$id" params={{ id: String(m.movieId) }} className="text-[13px] text-accent hover:underline">
                      {t("addMovie.inLibrary")}
                    </Link>
                  ) : (
                    <Button variant="secondary" onClick={() => setPicked(m)} data-testid={`pick-${m.tmdbId}`}>{t("addMovie.choose")}</Button>
                  )}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
