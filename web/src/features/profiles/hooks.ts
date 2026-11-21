import { useQuery } from "@tanstack/react-query";
import { api, queryKeys, unwrap } from "@/api/client";

export const useQualities = () =>
  useQuery({ queryKey: queryKeys.qualities, queryFn: async () => unwrap(await api.GET("/qualities")), staleTime: Infinity });

export const useQualityProfiles = () =>
  useQuery({ queryKey: queryKeys.qualityProfiles, queryFn: async () => unwrap(await api.GET("/qualityprofiles")) });

export const useQualityDefinitions = () =>
  useQuery({ queryKey: queryKeys.qualityDefinitions, queryFn: async () => unwrap(await api.GET("/qualitydefinitions")) });

export const useCustomFormats = () =>
  useQuery({ queryKey: queryKeys.customFormats, queryFn: async () => unwrap(await api.GET("/customformats")) });

export const useCustomFormatSchema = () =>
  useQuery({ queryKey: queryKeys.customFormatSchema, queryFn: async () => unwrap(await api.GET("/customformats/schema")), staleTime: Infinity });

export const useLanguages = () =>
  useQuery({ queryKey: queryKeys.languages, queryFn: async () => unwrap(await api.GET("/languages")), staleTime: Infinity });

export const useReleaseProfiles = () =>
  useQuery({ queryKey: queryKeys.releaseProfiles, queryFn: async () => unwrap(await api.GET("/releaseprofiles")) });

export const useDelayProfiles = () =>
  useQuery({ queryKey: queryKeys.delayProfiles, queryFn: async () => unwrap(await api.GET("/delayprofiles")) });

/** Quality names by id. */
export function useQualityNames(): Map<string, string> {
  const q = useQualities();
  return new Map((q.data ?? []).map((x) => [x.id, x.name]));
}
