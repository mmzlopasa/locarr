import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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

export function useAdultItems(query: { kind?: "scene" | "movie"; studioId?: number; performerId?: number; missing?: boolean; term?: string }) {
  return useQuery({
    queryKey: [...adultKey, "items", query],
    queryFn: async () => unwrap(await api.GET("/adult/items", { params: { query: { ...query, limit: 200 } } })),
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
