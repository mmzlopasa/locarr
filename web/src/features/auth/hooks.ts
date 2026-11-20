import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, fetchMe, fetchSystemInfo, queryKeys, unwrap, type Language, type User } from "@/api/client";
import { setLanguage } from "@/i18n";

export function useSystemInfo() {
  return useQuery({ queryKey: queryKeys.info, queryFn: fetchSystemInfo, staleTime: 60_000 });
}

export function useMe() {
  return useQuery({ queryKey: queryKeys.me, queryFn: fetchMe, staleTime: 60_000 });
}

/** The signed-in user; only for components rendered inside the signed-in layout. */
export function useCurrentUser(): User {
  const { data } = useMe();
  if (!data) throw new Error("useCurrentUser outside the signed-in layout");
  return data;
}

export const isAdmin = (u: User | null | undefined) => u?.role === "admin";

/**
 * Switches the UI language. When someone is signed in it is also saved to their
 * account, so it follows them to other browsers and the apps.
 */
export function useChangeLanguage() {
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: async (language: Language) => unwrap(await api.PATCH("/auth/me", { body: { language } })),
    onSuccess: (user) => qc.setQueryData(queryKeys.me, user),
  });
  return (lang: Language) => {
    setLanguage(lang);
    const me = qc.getQueryData<User | null>(queryKeys.me);
    if (me && me.language !== lang) save.mutate(lang);
  };
}

export function useSignOut() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      // Signing out locally must work even if the server is unreachable.
      await api.POST("/auth/logout").catch(() => undefined);
    },
    onSettled: () => {
      qc.clear();
      qc.setQueryData(queryKeys.me, null);
    },
  });
}
