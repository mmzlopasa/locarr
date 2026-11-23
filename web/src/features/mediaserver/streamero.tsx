import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Play } from "lucide-react";
import { api, queryKeys, unwrap } from "@/api/client";

/** Only http(s) links from the server are followed (never javascript: or data:). */
export function safeExternal(url: string | undefined): string {
  if (!url) return "";
  try {
    const u = new URL(url);
    return u.protocol === "http:" || u.protocol === "https:" ? u.href : "";
  } catch {
    return "";
  }
}

/** The ways to sign in (public). */
export function useAuthProviders() {
  return useQuery({
    queryKey: queryKeys.authProviders,
    queryFn: async () => unwrap(await api.GET("/auth/providers")),
    staleTime: 5 * 60_000,
  });
}

/** Whether "Sign in with Streamero" is offered. */
export function useStreameroSignIn(): boolean {
  const q = useAuthProviders();
  return !!q.data?.some((p) => p.id === "streamero");
}

/** A small "In Streamero" pill for titles that are already watchable. */
export function InStreameroBadge({ url, overlay, testId = "in-streamero" }: { url?: string; overlay?: boolean; testId?: string }) {
  const { t } = useTranslation();
  if (!safeExternal(url)) return null;
  return (
    <span data-testid={testId}
      className={`inline-flex h-5 items-center gap-1 rounded-full border px-2 text-[10.5px] font-semibold uppercase tracking-wide ${
        overlay ? "border-transparent bg-info text-bg" : "border-info/30 bg-info-bg text-info"}`}>
      <Play className="size-2.5 fill-current" aria-hidden />
      {t("streamero.inStreamero")}
    </span>
  );
}

/** "Watch in Streamero": opens the title in Streamero's web client in a new tab. */
export function WatchInStreamero({ url, size = "md", testId = "watch-in-streamero" }:
  { url?: string; size?: "sm" | "md"; testId?: string }) {
  const { t } = useTranslation();
  const href = safeExternal(url);
  if (!href) return null;
  const cls = size === "md"
    ? "h-9 rounded-full px-4 text-[13px]"
    : "h-8 rounded-ui px-3 text-[13px]";
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" data-testid={testId}
      className={`inline-flex items-center gap-1.5 border border-transparent bg-info font-semibold text-bg hover:opacity-90 ${cls}`}>
      <Play className="size-4 fill-current" aria-hidden />
      {t("streamero.watch")}
    </a>
  );
}
