import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ExternalLink, Plus, UserRound } from "lucide-react";
import { api, imageUrl, queryKeys, unwrap, type DiscoverItem, type RequestKind, type TitleDetail } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { Button, EmptyState, ErrorAlert, Spinner } from "@/components/ui";
import { RequestStatusBadge, ProgressBar, requestExplanation, requestScope } from "@/features/requests/common";
import { AvailabilityBadge, itemKey, PosterGrid, PosterImage } from "./common";
import { IssueDialog } from "./IssueDialog";
import { RequestDialog } from "./RequestDialog";

type Kind = "movie" | "series" | "artist";

function useTitle(kind: Kind, id: string) {
  return useQuery({
    queryKey: queryKeys.title(kind, id),
    queryFn: async (): Promise<TitleDetail> => {
      if (kind === "movie") return unwrap(await api.GET("/discover/movies/{tmdbId}", { params: { path: { tmdbId: Number(id) } } }));
      if (kind === "series") return unwrap(await api.GET("/discover/series/{tmdbId}", { params: { path: { tmdbId: Number(id) } } }));
      return unwrap(await api.GET("/discover/artists/{mbid}", { params: { path: { mbid: id } } }));
    },
    // While something is on its way, the page follows it.
    refetchInterval: (q) => (q.state.data?.requests.some((r) => ["approved", "processing", "partial"].includes(r.status)) ? 15_000 : false),
  });
}

export function MovieTitlePage() {
  const { tmdbId } = useParams({ from: "/app/discover/movie/$tmdbId" });
  return <TitlePage kind="movie" id={tmdbId} />;
}

export function SeriesTitlePage() {
  const { tmdbId } = useParams({ from: "/app/discover/series/$tmdbId" });
  return <TitlePage kind="series" id={tmdbId} />;
}

export function ArtistTitlePage() {
  const { mbid } = useParams({ from: "/app/discover/artist/$mbid" });
  return <TitlePage kind="artist" id={mbid} />;
}

type Dialog = { type: "request"; kind: RequestKind; target?: { mbid?: string; title: string } } | { type: "issue"; target?: { mbid: string; title: string } } | null;

function TitlePage({ kind, id }: { kind: Kind; id: string }) {
  const { t, i18n } = useTranslation();
  const q = useTitle(kind, id);
  const [dialog, setDialog] = useState<Dialog>(null);
  if (q.isPending) return <Spinner />;
  if (q.error) return <ErrorAlert error={q.error} />;
  const d = q.data;
  const it = d.item;
  const ab = d.ability;
  const backdrop = it.backdrop ? imageUrl(it.backdrop, "w1280") : "";
  const facts = [it.year ? String(it.year) : "", d.runtime ? t("title.minutes", { n: d.runtime }) : "", d.certification ?? "",
    d.network ?? d.studio ?? ""].filter(Boolean);
  const requestKind: RequestKind = kind === "artist" ? "artist" : kind;
  const issueKind = kind === "artist" ? null : kind;
  const anyStatus = it.status !== "none" ? it.status : it.status4k;

  return (
    <div className="flex flex-col gap-6" data-testid="title-page" data-kind={kind}>
      <section className="relative -mx-4 -mt-5 overflow-hidden md:-mx-6">
        {backdrop ? <img src={backdrop} alt="" className="absolute inset-0 size-full object-cover opacity-35" decoding="async" /> : null}
        <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/85 to-bg/40" />
        <div className="relative flex flex-col gap-5 px-4 pb-6 pt-10 sm:flex-row sm:items-end md:px-6 md:pt-16">
          <PosterImage item={it} size="large" className="w-36 shrink-0 border border-line shadow-2xl sm:w-48" />
          <div className="flex min-w-0 flex-col gap-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <AvailabilityBadge status={it.status} testId="title-status" />
              {it.status4k !== "none" ? <AvailabilityBadge status={it.status4k} is4k testId="title-status-4k" /> : null}
            </div>
            <h1 className="text-[26px] font-semibold leading-tight tracking-tight text-fg md:text-[32px]" data-testid="title-name">{it.title}</h1>
            {it.subtitle ? <p className="text-[13px] text-muted">{it.subtitle}</p> : null}
            {facts.length ? <p className="text-[13px] text-muted">{facts.join(" · ")}</p> : null}
            {d.genres.length ? (
              <div className="flex flex-wrap gap-1.5">
                {d.genres.map((g) => <span key={g} className="rounded-full border border-line-strong px-2.5 py-0.5 text-[11.5px] text-muted">{g}</span>)}
              </div>
            ) : null}
            <div className="mt-1 flex flex-wrap items-center gap-2">
              {ab.canRequest || ab.canRequest4k ? (
                <Button variant="primary" className="h-9 rounded-full px-4" onClick={() => setDialog({ type: "request", kind: requestKind })}
                  data-testid="request-button">
                  <Plus className="size-4" />
                  {kind === "artist" ? t("title.requestArtist") : anyStatus === "partial" ? t("title.requestMore") : t("title.request")}
                </Button>
              ) : null}
              {d.trailerUrl ? (
                <a href={d.trailerUrl} target="_blank" rel="noopener noreferrer" data-testid="trailer-link"
                  className="inline-flex h-9 items-center gap-1.5 rounded-full border border-line-strong px-4 text-[13px] text-fg hover:bg-surface-3">
                  <ExternalLink className="size-4" />
                  {t("title.trailer")}
                </a>
              ) : null}
              {ab.canReportIssue && issueKind ? (
                <Button variant="ghost" className="h-9 rounded-full" onClick={() => setDialog({ type: "issue" })} data-testid="report-issue">
                  <AlertTriangle className="size-4" />
                  {t("title.reportIssue")}
                </Button>
              ) : null}
            </div>
          </div>
        </div>
      </section>

      {d.tagline ? <p className="text-[14px] italic text-muted">{d.tagline}</p> : null}
      {it.overview ? <p className="max-w-3xl text-[14px] leading-relaxed text-fg/90" data-testid="title-overview">{it.overview}</p> : null}

      {d.requests.length > 0 ? (
        <section className="flex flex-col gap-2" data-testid="title-requests">
          <h2 className="text-[15px] font-semibold text-fg">{t("title.requests")}</h2>
          <ul className="flex flex-col gap-2">
            {d.requests.map((r) => (
              <li key={r.id} className="flex flex-col gap-1.5 rounded-ui border border-line bg-surface px-3 py-2.5">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Link to="/requests/$id" params={{ id: String(r.id) }} className="text-[13px] text-fg hover:underline">
                    {requestScope(t, r)}
                  </Link>
                  <RequestStatusBadge status={r.status} />
                </div>
                <p className="text-[12px] text-faint">
                  {t("title.requestedBy", { user: r.requestedBy.username, when: formatDateTime(r.createdAt, i18n.language) })}
                </p>
                {r.progress && r.progress.downloads > 0 ? <ProgressBar value={r.progress.percent} label={t("requests.progress")} /> : null}
                {r.explanation && r.status !== "available" ? <p className="text-[12px] text-muted">{requestExplanation(t, r, i18n.language)}</p> : null}
                {r.declineReason ? <p className="text-[12px] text-danger">{t("requests.declinedBecause", { reason: r.declineReason })}</p> : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {kind === "series" && d.seasons.length > 0 ? (
        <section className="flex flex-col gap-2" data-testid="title-seasons">
          <h2 className="text-[15px] font-semibold text-fg">{t("title.seasons")}</h2>
          <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {d.seasons.map((s) => (
              <li key={s.number} className="flex items-center gap-3 rounded-ui border border-line bg-surface p-2" data-testid={`season-${s.number}`}>
                <PosterImage item={{ mediaType: "series", poster: s.poster, title: s.title }} className="w-12 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[13px] font-medium text-fg">{s.title || t("request.season", { n: s.number })}</p>
                  <p className="text-[12px] text-faint">
                    {t("request.episodes", { count: s.episodeCount })}
                    {s.episodesAvailable ? ` · ${t("title.episodesHere", { n: s.episodesAvailable })}` : ""}
                    {s.airDate ? ` · ${s.airDate.slice(0, 4)}` : ""}
                  </p>
                </div>
                <div className="flex flex-col items-end gap-1">
                  <AvailabilityBadge status={s.status} testId={`season-${s.number}-status`} />
                  {s.status4k !== "none" ? <AvailabilityBadge status={s.status4k} is4k /> : null}
                </div>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {kind === "artist" ? (
        <section className="flex flex-col gap-2" data-testid="title-albums">
          <h2 className="text-[15px] font-semibold text-fg">{t("title.albums")}</h2>
          {d.albums.length === 0 ? <EmptyState>{t("title.noAlbums")}</EmptyState> : null}
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-3">
            {d.albums.map((a) => <AlbumTile key={itemKey(a)} album={a} canRequest={ab.canRequest} canReport={ab.canReportIssue === true}
              onRequest={() => setDialog({ type: "request", kind: "album", target: { mbid: a.mbid, title: a.title } })}
              onReport={() => setDialog({ type: "issue", target: { mbid: a.mbid ?? "", title: a.title } })} />)}
          </ul>
        </section>
      ) : null}

      {d.cast.length > 0 ? (
        <section className="flex flex-col gap-2" data-testid="title-cast">
          <h2 className="text-[15px] font-semibold text-fg">{t("title.cast")}</h2>
          <div className="-mx-1 flex snap-x gap-3 overflow-x-auto px-1 pb-2">
            {d.cast.map((c, i) => {
              const inner = (
                <>
                  <div className="relative aspect-[2/3] w-[92px] overflow-hidden rounded-ui border border-line bg-surface-2">
                    <div className="flex size-full items-center justify-center text-faint"><UserRound className="size-6" aria-hidden /></div>
                    {c.profile ? <img src={imageUrl(c.profile, "w185")} alt="" loading="lazy" className="absolute inset-0 size-full object-cover" /> : null}
                  </div>
                  <p className="mt-1 w-[92px] truncate text-[12px] text-fg">{c.name}</p>
                  {c.character ? <p className="w-[92px] truncate text-[11px] text-faint">{c.character}</p> : null}
                </>
              );
              return c.personId ? (
                <Link key={`${c.personId}-${i}`} to="/discover/person/$tmdbId" params={{ tmdbId: String(c.personId) }} className="shrink-0 snap-start rounded-ui">
                  {inner}
                </Link>
              ) : <div key={`${c.name}-${i}`} className="shrink-0 snap-start">{inner}</div>;
            })}
          </div>
        </section>
      ) : null}

      {dialog?.type === "request" ? (
        <RequestDialog detail={d} kind={dialog.kind} target={dialog.target} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.type === "issue" && (issueKind || dialog.target) ? (
        <IssueDialog detail={d} kind={dialog.target ? "album" : (issueKind as "movie" | "series")} target={dialog.target} onClose={() => setDialog(null)} />
      ) : null}
    </div>
  );
}

function AlbumTile({ album, canRequest, canReport, onRequest, onReport }:
  { album: DiscoverItem; canRequest: boolean; canReport: boolean; onRequest: () => void; onReport: () => void }) {
  const { t } = useTranslation();
  const status = album.status;
  const requestable = canRequest && (status === "none" || status === "in_library" || status === "partial");
  return (
    <li className="flex flex-col gap-1.5" data-testid={`album-${album.mbid}`}>
      <div className="relative">
        <PosterImage item={album} className="border border-line" />
        <div className="absolute left-1.5 top-1.5"><AvailabilityBadge status={status} testId="album-status" /></div>
      </div>
      <p className="truncate text-[12.5px] font-medium text-fg" title={album.title}>{album.title}</p>
      <p className="text-[11.5px] text-faint">{[album.subtitle, album.year || ""].filter(Boolean).join(" · ")}</p>
      <div className="flex gap-1">
        {requestable ? <Button className="h-7 flex-1 px-2 text-[12px]" onClick={onRequest} data-testid={`request-album-${album.mbid}`}>{t("title.request")}</Button> : null}
        {canReport && status === "available" ? (
          <Button variant="ghost" className="h-7 px-2 text-[12px]" onClick={onReport} aria-label={t("title.reportIssue")}><AlertTriangle className="size-3.5" /></Button>
        ) : null}
      </div>
    </li>
  );
}

export function PersonPage() {
  const { t } = useTranslation();
  const { tmdbId } = useParams({ from: "/app/discover/person/$tmdbId" });
  const id = Number(tmdbId);
  const q = useQuery({ queryKey: queryKeys.person(id), queryFn: async () =>
    unwrap(await api.GET("/discover/people/{tmdbId}", { params: { path: { tmdbId: id } } })) });
  if (q.isPending) return <Spinner />;
  if (q.error) return <ErrorAlert error={q.error} />;
  const p = q.data;
  return (
    <div className="flex flex-col gap-6" data-testid="person-page">
      <div className="flex flex-col gap-4 sm:flex-row">
        <PosterImage item={{ mediaType: "person", poster: p.profile, title: p.name }} size="large" className="w-36 shrink-0 border border-line sm:w-44" />
        <div className="flex min-w-0 flex-col gap-2">
          <h1 className="text-[26px] font-semibold tracking-tight text-fg">{p.name}</h1>
          <p className="text-[13px] text-muted">{[p.department, p.birthday].filter(Boolean).join(" · ")}</p>
          {p.biography ? <p className="max-w-3xl whitespace-pre-line text-[13.5px] leading-relaxed text-fg/90 line-clamp-[12]">{p.biography}</p> : null}
        </div>
      </div>
      <section className="flex flex-col gap-2">
        <h2 className="text-[15px] font-semibold text-fg">{t("title.knownFor")}</h2>
        {p.knownFor.length === 0 ? <EmptyState>{t("discover.emptyRow")}</EmptyState> : <PosterGrid items={p.knownFor} />}
      </section>
    </div>
  );
}
