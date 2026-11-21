import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { Button, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";
import { AddOptionsForm, splitTags, useAddDefaults } from "./AddMoviePage";
import { Poster } from "./common";

export function CollectionPage() {
  const { id } = useParams({ from: "/app/movies/collections/$id" });
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [opts, setOpts] = useAddDefaults();
  const col = useQuery({
    queryKey: ["collections", id],
    queryFn: async () => unwrap(await api.GET("/movies/collections/{tmdbId}", { params: { path: { tmdbId: Number(id) } } })),
  });
  const add = useMutation({
    mutationFn: async () => unwrap(await api.POST("/movies/collections/{tmdbId}", { params: { path: { tmdbId: Number(id) } }, body: {
      rootFolderId: opts.rootFolderId, qualityProfileId: opts.qualityProfileId, minimumAvailability: opts.minimumAvailability,
      monitored: opts.monitored, tags: splitTags(opts.tags), searchOnAdd: opts.searchOnAdd } })),
    onSuccess: () => {
      void col.refetch();
      void qc.invalidateQueries({ queryKey: queryKeys.movies });
    },
  });
  if (col.isLoading) return <Spinner />;
  if (!col.data) return <ErrorAlert error={col.error} />;
  const missing = col.data.members.filter((m) => !m.movieId).length;
  return (
    <div>
      <PageHeader title={col.data.name} description={col.data.overview} />
      <ul className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
        {col.data.members.map((m) => (
          <li key={m.tmdbId} className={m.movieId ? "" : "opacity-60"}>
            <Poster file={m.poster} title={m.title} />
            <div className="mt-1 truncate text-[13px] text-fg">
              {m.movieId ? <Link to="/movies/$id" params={{ id: String(m.movieId) }} className="hover:text-accent">{m.title}</Link> : m.title}
            </div>
            <div className="text-[12px] text-muted">{m.year || "–"} · {m.movieId ? t("collection.inLibrary") : t("collection.missing")}</div>
          </li>
        ))}
      </ul>
      {missing > 0 ? (
        <Panel title={t("collection.addMissing", { count: missing })}>
          <AddOptionsForm value={opts} onChange={setOpts} />
          <div className="mt-3 flex gap-2">
            <Button variant="primary" onClick={() => add.mutate()} busy={add.isPending}>{t("collection.addButton", { count: missing })}</Button>
            <ErrorAlert error={add.error} />
          </div>
        </Panel>
      ) : null}
    </div>
  );
}
