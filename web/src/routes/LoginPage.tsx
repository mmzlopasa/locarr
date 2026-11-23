import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouter, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ApiError, api, deviceInfo, queryKeys, unwrap } from "@/api/client";
import { AuthLayout } from "@/components/AuthLayout";
import { Button, ErrorAlert, TextInput } from "@/components/ui";
import { setLanguage } from "@/i18n";
import { useAuthProviders } from "@/features/mediaserver/streamero";

/** Only same-site paths are followed after sign-in (never another host). */
export function safeRedirect(target: string | undefined): string {
  if (!target || !target.startsWith("/") || target.startsWith("//") || target.startsWith("/\\")) return "/";
  if (target.startsWith("/login") || target.startsWith("/setup")) return "/";
  return target;
}

export function LoginPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const router = useRouter();
  const navigate = useNavigate();
  const search = useSearch({ from: "/login" });
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [needsCode, setNeedsCode] = useState(false);
  const [useRecovery, setUseRecovery] = useState(false);
  const [code, setCode] = useState("");
  const providers = useAuthProviders();
  const others = (providers.data ?? []).filter((p) => p.id !== "local" && p.kind === "password");
  const [provider, setProvider] = useState("local");
  const external = provider !== "local";

  const login = useMutation({
    mutationFn: async () => {
      if (external) {
        const totp = needsCode && code.trim() ? { totpCode: code.trim() } : {};
        return unwrap(await api.POST("/auth/providers/{id}/login", { params: { path: { id: provider } },
          body: { username, password, device: deviceInfo(), useCookie: true, ...totp } }));
      }
      const second = needsCode && code.trim() ? (useRecovery ? { recoveryCode: code.trim() } : { totpCode: code.trim() }) : {};
      return unwrap(
        await api.POST("/auth/login", { body: { username, password, device: deviceInfo(), useCookie: true, ...second } }),
      );
    },
    onSuccess: async (res) => {
      qc.setQueryData(queryKeys.me, res.user);
      setLanguage(res.user.language);
      const target = safeRedirect(search.redirect);
      if (target === "/") await navigate({ to: "/" });
      else router.history.push(target);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === "totp_required") setNeedsCode(true);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  // The first "totp_required" answer is the next step, not an error.
  const shownError = login.error instanceof ApiError && login.error.code === "totp_required" ? null : login.error;

  const choose = (id: string) => {
    setProvider(id);
    setNeedsCode(false);
    setCode("");
    setUseRecovery(false);
    login.reset();
  };
  const providerName = others.find((p) => p.id === provider)?.name ?? provider;

  return (
    <AuthLayout title={t("login.title")}
      subtitle={needsCode ? (external ? t("login.providerTotpHint", { name: providerName }) : t("login.totpHint"))
        : external ? t("login.providerSubtitle", { name: providerName }) : t("login.subtitle")}>
      {others.length > 0 && !needsCode ? (
        <div role="tablist" aria-label={t("login.method")} className="mb-4 grid grid-flow-col gap-1 rounded-ui border border-line bg-surface-2 p-1"
          data-testid="login-providers">
          {[{ id: "local", name: t("login.localAccount") }, ...others.map((p) => ({ id: p.id, name: p.name }))].map((p) => (
            <button key={p.id} type="button" role="tab" aria-selected={provider === p.id} onClick={() => choose(p.id)}
              data-testid={`provider-${p.id}`}
              className={`h-8 rounded-[5px] px-3 text-[13px] transition-colors ${provider === p.id
                ? "bg-surface-3 font-semibold text-fg shadow-sm" : "text-muted hover:text-fg"}`}>
              {p.name}
            </button>
          ))}
        </div>
      ) : null}
      <form onSubmit={submit} className="flex flex-col gap-3" noValidate>
        {!needsCode ? (
          <>
            <TextInput label={t("login.username")} name="username" autoComplete="username" required autoFocus
              value={username} onChange={(e) => setUsername(e.target.value)} />
            <TextInput label={t("login.password")} name="password" type="password" autoComplete="current-password" required
              value={password} onChange={(e) => setPassword(e.target.value)} />
          </>
        ) : (
          <>
            <TextInput
              key={useRecovery ? "recovery" : "totp"}
              label={external ? t("login.streameroCode") : useRecovery ? t("login.recoveryCode") : t("login.totpCode")}
              name="code"
              autoComplete="one-time-code"
              inputMode={useRecovery ? "text" : "numeric"}
              autoFocus
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
            {!external ? <button
              type="button"
              className="self-start text-[12px] text-accent hover:underline"
              onClick={() => {
                setUseRecovery(!useRecovery);
                setCode("");
              }}
            >
              {useRecovery ? t("login.useTotp") : t("login.useRecovery")}
            </button> : null}
          </>
        )}
        <ErrorAlert error={shownError} />
        <Button type="submit" variant="primary" busy={login.isPending} disabled={!username || !password || (needsCode && !code.trim())}>
          {needsCode ? t("login.verify") : external ? t("login.submitWith", { name: providerName }) : t("login.submit")}
        </Button>
        {needsCode ? (
          <Button
            variant="ghost"
            onClick={() => {
              setNeedsCode(false);
              setCode("");
              login.reset();
            }}
          >
            {t("common.back")}
          </Button>
        ) : null}
      </form>
    </AuthLayout>
  );
}
