import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type User } from "@/api/client";
import type { components } from "@/api/schema.gen";

type S = components["schemas"];
export type AdultItem = S["AdultItem"];
export type AdultStudio = S["AdultStudio"];
export type AdultPerformer = S["AdultPerformer"];
export type AdultSettings = S["AdultSettings"];
export type AdultSettingsUpdate = S["AdultSettingsUpdate"];
export type AdultLookupResult = S["AdultLookupResult"];
export type AdultBlockTerm = S["AdultBlockTerm"];
export type AdultExclusion = S["AdultExclusion"];
export type AdultLookupPage = S["AdultLookupPage"];
export type AdultGalleryRequest = S["AdultGalleryRequest"];
export type AdultGalleryImage = S["AdultGalleryImage"];

/** Every adult query lives under this key, so disabling drops them all at once. */
export const adultKey = ["adult"] as const;

/** The adult section is shown only when the server says this session may see it. */
export const canSeeAdult = (me: User | null | undefined) => !!me?.adultVisible;

export function useAdultSettings(enabled = true) {
  return useQuery({
    queryKey: [...adultKey, "settings"],
    queryFn: async () => unwrap(await api.GET("/adult/settings")),
    enabled,
    retry: false,
  });
}

export function useSaveAdultSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: AdultSettingsUpdate) => unwrap(await api.PUT("/adult/settings", { body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: adultKey });
      void qc.invalidateQueries({ queryKey: ["auth", "me"] });
    },
  });
}

export type ItemQuery = {
  kind?: "scene" | "movie" | "gallery";
  studioId?: number;
  sceneId?: number;
  performerId?: number;
  missing?: boolean;
  term?: string;
  status?: S["AdultItemStatus"];
  monitored?: boolean;
  sort?: "date" | "added" | "title";
};

export const ITEM_PAGE = 120;

export function useAdultItems(query: ItemQuery, enabled = true) {
  return useQuery({
    enabled,
    queryKey: [...adultKey, "items", query],
    queryFn: async () => unwrap(await api.GET("/adult/items", { params: { query: { ...query, limit: 200 } } })),
  });
}

/** Items page by page ("Load more"). */
export function useAdultItemPages(query: ItemQuery) {
  return useInfiniteQuery({
    queryKey: [...adultKey, "items", "pages", query],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/adult/items", { params: { query: { ...query, limit: ITEM_PAGE, offset: pageParam } } })),
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
    refetchInterval: 15_000,
  });
}

export function useAdultCalendar(start: string, end: string) {
  return useQuery({
    queryKey: [...adultKey, "calendar", start, end],
    queryFn: async () => unwrap(await api.GET("/adult/calendar", { params: { query: { start, end } } })),
  });
}

export type LookupQuery = {
  source: "stashdb" | "tpdb";
  type: "scene" | "movie" | "performer" | "studio";
  term: string;
  sort: "newest" | "oldest" | "relevance" | "title";
};

/** Search results page by page; scenes and movies in the chosen order. */
export function useAdultLookup(q: LookupQuery | null) {
  return useInfiniteQuery({
    queryKey: [...adultKey, "lookup", q],
    enabled: !!q,
    initialPageParam: 1,
    queryFn: async ({ pageParam }) => {
      const query = q!;
      const ordered = query.type === "scene" || query.type === "movie";
      return unwrap(await api.GET("/adult/lookup", {
        params: { query: { source: query.source, type: query.type, term: query.term, page: pageParam, ...(ordered ? { sort: query.sort } : {}) } },
      }));
    },
    getNextPageParam: (last) => (last.more ? last.page + 1 : undefined),
    retry: false,
    staleTime: 60_000,
  });
}

export type RemoveChoice = { deleteFiles: boolean; exclude: boolean };

export function useRemoveItem() {
  const invalidate = useAdultInvalidate();
  return useMutation({
    mutationFn: async ({ id, ...q }: { id: number } & RemoveChoice) =>
      unwrap(await api.DELETE("/adult/items/{id}", { params: { path: { id }, query: q } })),
    onSuccess: () => invalidate(),
  });
}

export function useBulkItems() {
  const invalidate = useAdultInvalidate();
  return useMutation({
    mutationFn: async (body: S["AdultBulkRequest"]) => unwrap(await api.POST("/adult/items/bulk", { body })),
    onSuccess: () => invalidate(),
  });
}

export function useRemoveStudio() {
  const invalidate = useAdultInvalidate();
  return useMutation({
    mutationFn: async ({ id, keepScenes, ...q }: { id: number; keepScenes: boolean } & RemoveChoice) =>
      unwrap(await api.DELETE("/adult/studios/{id}", { params: { path: { id }, query: { keepScenes, ...q } } })),
    onSuccess: () => invalidate(),
  });
}

export function useRemovePerformer() {
  const invalidate = useAdultInvalidate();
  return useMutation({
    mutationFn: async ({ id, removeScenes, ...q }: { id: number; removeScenes: boolean } & RemoveChoice) =>
      unwrap(await api.DELETE("/adult/performers/{id}", { params: { path: { id }, query: { removeScenes, ...q } } })),
    onSuccess: () => invalidate(),
  });
}

export function useAdultExclusions(enabled = true) {
  return useQuery({
    queryKey: [...adultKey, "exclusions"],
    queryFn: async () => unwrap(await api.GET("/adult/exclusions")),
    enabled,
    retry: false,
  });
}

export function useDeleteExclusion() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/adult/exclusions/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...adultKey, "exclusions"] }),
  });
}

export function useAdultItem(id: number) {
  return useQuery({
    queryKey: [...adultKey, "item", id],
    queryFn: async () => unwrap(await api.GET("/adult/items/{id}", { params: { path: { id } } })),
    refetchInterval: 10_000,
  });
}

export function useAdultStudios() {
  return useQuery({ queryKey: [...adultKey, "studios"], queryFn: async () => unwrap(await api.GET("/adult/studios")) });
}

export function useAdultStudio(id: number) {
  return useQuery({
    queryKey: [...adultKey, "studio", id],
    queryFn: async () => unwrap(await api.GET("/adult/studios/{id}", { params: { path: { id } } })),
  });
}

export function useAdultPerformers() {
  return useQuery({ queryKey: [...adultKey, "performers"], queryFn: async () => unwrap(await api.GET("/adult/performers")) });
}

export function useAdultPerformer(id: number) {
  return useQuery({
    queryKey: [...adultKey, "performer", id],
    queryFn: async () => unwrap(await api.GET("/adult/performers/{id}", { params: { path: { id } } })),
  });
}

export function useAdultPreferences() {
  return useQuery({ queryKey: [...adultKey, "preferences"], queryFn: async () => unwrap(await api.GET("/adult/preferences")) });
}

export function useSetBlur() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (blur: boolean) => unwrap(await api.PUT("/adult/preferences", { body: { blur } })),
    // Flip the checkbox right away; the server value replaces it once saved.
    onMutate: async (blur: boolean) => {
      const key = [...adultKey, "preferences"];
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData(key);
      qc.setQueryData(key, (old: object | undefined) => ({ ...(old ?? {}), blur }));
      return { prev };
    },
    onError: (_err, _blur, ctx) => {
      if (ctx?.prev !== undefined) qc.setQueryData([...adultKey, "preferences"], ctx.prev);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: [...adultKey, "preferences"] }),
  });
}

export function useAdultRootFolders() {
  return useQuery({ queryKey: [...adultKey, "roots"], queryFn: async () => unwrap(await api.GET("/adult/rootfolders")) });
}

/** Invalidate everything adult after a change. */
export function useAdultInvalidate() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: adultKey });
}

/** The images of a gallery (empty until a set is imported). */
export function useGalleryImages(id: number, enabled = true) {
  return useQuery({
    queryKey: [...adultKey, "item", id, "images"],
    queryFn: async () => unwrap(await api.GET("/adult/items/{id}/images", { params: { path: { id } } })),
    enabled,
  });
}

/** Add a gallery: by hand, a scene's photo set or a performer's set. */
export function useAddGallery() {
  const invalidate = useAdultInvalidate();
  return useMutation({
    mutationFn: async (body: AdultGalleryRequest) => unwrap(await api.POST("/adult/galleries", { body })),
    onSuccess: () => invalidate(),
  });
}

/** Path of one gallery image (fetched with the session, never a public URL). */
export const galleryImagePath = (id: number, index: number, thumb = false) =>
  `/api/v1/adult/items/${id}/images/${index}${thumb ? "?thumb=true" : ""}`;
