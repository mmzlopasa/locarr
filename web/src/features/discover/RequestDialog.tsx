import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  api,
  queryKeys,
  unwrap,
  type CreateRequest,
  type MediaRequest,
  type RequestKind,
  type SeasonAvailability,
  type TitleDetail,
} from "@/api/client";
import { Modal } from "@/components/Modal";
import { Alert, Button, Checkbox, ErrorAlert, SelectInput, TextInput } from "@/components/ui";
import { QuotaLine, useRequestSummary } from "@/features/requests/common";
import { AvailabilityBadge } from "./common";

const libraryKind = (k: RequestKind) => (k === "movie" ? "movies" : k === "series" ? "series" : "music");

/** A season that may not be asked for again on this track (it is here or on its way). */
export function seasonTaken(s: SeasonAvailability, is4k: boolean): boolean {
  const st = is4k ? s.status4k : s.status;
  return st === "available" || st === "pending" || st === "processing";
}

/** The request body the dialog sends (exported for tests). */
export function buildRequest(o: {
  kind: RequestKind; tmdbId?: number; mbid?: string; is4k: boolean; seasons: number[]; allSeasons: boolean; futureSeasons: boolean;
  comment: string; profileId?: number; rootFolderId?: number; tags: string;
}): CreateRequest {
  const body: CreateRequest = { kind: o.kind };
  if (o.tmdbId) body.tmdbId = o.tmdbId;
  if (o.mbid) body.mbid = o.mbid;
  if (o.is4k) body.is4k = true;
  if (o.kind === "series") {
    if (o.allSeasons) body.allSeasons = true;
    else body.seasons = [...o.seasons].sort((a, b) => a - b);
    if (o.futureSeasons) body.futureSeasons = true;
  }
  if (o.comment.trim()) body.comment = o.comment.trim();
  const tags = o.tags.split(",").map((s) => s.trim()).filter(Boolean);
  if (o.profileId || o.rootFolderId || tags.length) {
    body.options = {};
    if (o.profileId) body.options.qualityProfileId = o.profileId;
    if (o.rootFolderId) body.options.rootFolderId = o.rootFolderId;
    if (tags.length) body.options.tags = tags;
  }
  return body;
}

export function RequestDialog({ detail, kind, target, onClose }: {
  detail: TitleDetail;
  kind: RequestKind;
  /** The album or artist asked for (music); defaults to the page's item. */
  target?: { mbid?: string; tmdbId?: number; title: string };
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const ability = detail.ability;
  const summary = useRequestSummary();
  const seasons = detail.seasons.filter((s) => s.number > 0 || detail.seasons.every((x) => x.number === 0));
  const [is4k, setIs4k] = useState(false);
  const [picked, setPicked] = useState<number[]>(() => seasons.filter((s) => !seasonTaken(s, false)).map((s) => s.number));
  const [allSeasons, setAllSeasons] = useState(false);
  const [futureSeasons, setFutureSeasons] = useState(false);
  const [comment, setComment] = useState("");
  const [profileId, setProfileId] = useState(0);
  const [rootFolderId, setRootFolderId] = useState(0);
  const [tags, setTags] = useState("");
  const [done, setDone] = useState<MediaRequest | null>(null);
  const options = ability.canSetOptions === true;
  const profiles = useQuery({ queryKey: queryKeys.qualityProfiles, enabled: options,
    queryFn: async () => unwrap(await api.GET("/qualityprofiles")) });
  const roots = useQuery({ queryKey: queryKeys.rootFolders, enabled: options, queryFn: async () => unwrap(await api.GET("/rootfolders")) });
  const lk = libraryKind(kind);
  const title = target?.title ?? detail.item.title;

  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/requests", { body: buildRequest({
      kind, tmdbId: target ? target.tmdbId : detail.item.tmdbId, mbid: target ? target.mbid : detail.item.mbid, is4k,
      seasons: picked, allSeasons, futureSeasons, comment, profileId, rootFolderId, tags,
    }) })),
    onSuccess: (r) => {
      setDone(r);
      void qc.invalidateQueries({ queryKey: ["discover"] });
      void qc.invalidateQueries({ queryKey: queryKeys.requests });
    },
  });

  const toggle = (n: number, on: boolean) => setPicked((p) => (on ? [...new Set([...p, n])] : p.filter((x) => x !== n)));
  const nothing = kind === "series" && !allSeasons && picked.length === 0;
  const auto = done && (done.autoApproved || done.status !== "pending");

  return (
    <Modal title={t("request.title", { title })} onClose={onClose} testId="request-dialog" wide={kind === "series"}
      footer={done ? (
        <Button variant="primary" onClick={onClose} data-testid="request-done">{t("common.done")}</Button>
      ) : (
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" busy={create.isPending} disabled={nothing} onClick={() => create.mutate()} data-testid="request-confirm">
            {t("request.send")}
          </Button>
        </>
      )}>
      {done ? (
        <Alert tone="success">
          <span data-testid="request-result" data-status={done.status}>{auto ? t("request.autoApproved") : t("request.sent")}</span>
        </Alert>
      ) : (
        <div className="flex flex-col gap-4">
          <QuotaLine kind={kind} summary={summary.data} />
          {ability.canRequest4k && (kind === "movie" || kind === "series") ? (
            <Checkbox label={t("request.fourK")} description={t("request.fourKHint")} checked={is4k}
              onChange={(e) => {
                setIs4k(e.target.checked);
                setPicked(seasons.filter((s) => !seasonTaken(s, e.target.checked)).map((s) => s.number));
              }} data-testid="request-4k" />
          ) : null}
          {kind === "series" ? (
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-[12px] font-medium text-muted">{t("request.seasons")}</legend>
              <Checkbox label={t("request.allSeasons")} description={t("request.allSeasonsHint")} checked={allSeasons}
                onChange={(e) => setAllSeasons(e.target.checked)} data-testid="request-all-seasons" />
              <Checkbox label={t("request.futureSeasons")} description={t("request.futureSeasonsHint")} checked={futureSeasons}
                onChange={(e) => setFutureSeasons(e.target.checked)} data-testid="request-future-seasons" />
              {!allSeasons ? (
                <ul className="mt-1 grid gap-1.5 sm:grid-cols-2" data-testid="request-season-list">
                  {seasons.map((s) => {
                    const taken = seasonTaken(s, is4k);
                    return (
                      <li key={s.number} className="flex items-center justify-between gap-2 rounded-ui border border-line bg-surface-2 px-2.5 py-1.5">
                        <label className={`flex min-w-0 items-center gap-2 text-[13px] ${taken ? "text-faint" : "text-fg"}`}>
                          <input type="checkbox" className="size-4 accent-[var(--lc-accent)]" disabled={taken}
                            checked={!taken && picked.includes(s.number)} onChange={(e) => toggle(s.number, e.target.checked)}
                            data-testid={`request-season-${s.number}`} />
                          <span className="truncate">{s.number === 0 ? t("request.specials") : t("request.season", { n: s.number })}</span>
                          <span className="shrink-0 text-[11px] text-faint">{t("request.episodes", { count: s.episodeCount })}</span>
                        </label>
                        <AvailabilityBadge status={is4k ? s.status4k : s.status} />
                      </li>
                    );
                  })}
                </ul>
              ) : null}
            </fieldset>
          ) : null}
          {options ? (
            <fieldset className="grid gap-3 rounded-ui border border-line p-3 sm:grid-cols-2">
              <legend className="px-1 text-[12px] font-medium text-muted">{t("request.options")}</legend>
              <SelectInput label={t("request.profile")} value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
                <option value={0}>{t("request.useDefault")}</option>
                {profiles.data?.filter((p) => p.kind === lk).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </SelectInput>
              <SelectInput label={t("request.rootFolder")} value={rootFolderId} onChange={(e) => setRootFolderId(Number(e.target.value))}>
                <option value={0}>{t("request.useDefault")}</option>
                {roots.data?.filter((r) => r.kind === lk).map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
              </SelectInput>
              <div className="sm:col-span-2">
                <TextInput label={t("request.tags")} hint={t("request.tagsHint")} value={tags} onChange={(e) => setTags(e.target.value)} />
              </div>
            </fieldset>
          ) : null}
          <div className="flex flex-col gap-1">
            <label htmlFor="request-comment" className="text-[12px] font-medium text-muted">{t("request.comment")}</label>
            <textarea id="request-comment" rows={2} maxLength={2000} value={comment} onChange={(e) => setComment(e.target.value)}
              placeholder={t("request.commentHint")}
              className="w-full rounded-ui border border-line-strong bg-surface-2 px-2.5 py-1.5 text-[13px] text-fg placeholder:text-faint focus:border-info focus:outline-none" />
          </div>
          <ErrorAlert error={create.error} />
        </div>
      )}
    </Modal>
  );
}
