import type { DiscoverItem, MediaRequest, RequestSummary, TitleDetail, User } from "@/api/client";
import { testUser } from "@/test-utils";

const now = new Date().toISOString();

export const item = (over: Partial<DiscoverItem> = {}): DiscoverItem => ({
  mediaType: "movie", tmdbId: 133701, title: "Tears of Steel", year: 2012, overview: "Warriors and scientists.", poster: "poster133701.png",
  backdrop: "backdrop133701.png", status: "none", status4k: "none", ...over,
});

export const mediaRequest = (over: Partial<MediaRequest> = {}): MediaRequest => ({
  id: 7, kind: "movie", is4k: false, status: "pending", tmdbId: 133701, title: "Tears of Steel", year: 2012, poster: "poster133701.png",
  seasons: [], allSeasons: false, futureSeasons: false, requestedBy: { id: 2, username: "mia" }, createdAt: now, updatedAt: now, tags: [],
  commentCount: 0, canCancel: false, autoApproved: false, ...over,
});

export const titleDetail = (over: Partial<TitleDetail> = {}): TitleDetail => ({
  item: item(), genres: ["Science Fiction"], cast: [{ personId: 5, name: "Derek de Lint", character: "Old Thom" }], seasons: [], albums: [],
  requests: [], runtime: 12, trailerUrl: "https://www.youtube.com/watch?v=abc",
  ability: { canRequest: true, canRequest4k: false, autoApprove: false, autoApprove4k: false, canSetOptions: false, canReportIssue: false },
  ...over,
});

export const summary = (over: Partial<RequestSummary> = {}): RequestSummary => {
  const q = { limit: 5, days: 7, used: 2, remaining: 3, unlimited: false, custom: false };
  return { pending: 1, processing: 0, failed: 0, openIssues: 0, quotas: { movie: q, series: q, music: q }, ...over };
};

/** A family member: may request, nothing else. */
export const requester = (): User => testUser({
  id: 2, username: "mia", role: "requester",
  permissions: { requestMovies: true, requestSeries: true, requestMusic: true, autoApprove: false, request4k: false, manageLibrary: false,
    manageRequests: false, autoApproveMovies: false, autoApproveSeries: false, autoApproveMusic: false, autoApprove4k: false },
});

export const emptyNotifications = { status: 200, body: { items: [], unread: 0, latestId: 0 } };
