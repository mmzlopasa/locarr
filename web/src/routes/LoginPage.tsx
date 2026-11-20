import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouter, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ApiError, api, deviceInfo, queryKeys, unwrap } from "@/api/client";
import { AuthLayout } from "@/components/AuthLayout";
import { Button, ErrorAlert, TextInput } from "@/components/ui";
import { setLanguage } from "@/i18n";

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

  const login = useMutation({
    mutationFn: async () => {
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

  return (
    <AuthLayout title={t("login.title")} subtitle={needsCode ? t("login.totpHint") : t("login.subtitle")}>
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
              label={useRecovery ? t("login.recoveryCode") : t("login.totpCode")}
              name="code"
              autoComplete="one-time-code"
              inputMode={useRecovery ? "text" : "numeric"}
              autoFocus
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
            <button
              type="button"
              className="self-start text-[12px] text-accent hover:underline"
              onClick={() => {
                setUseRecovery(!useRecovery);
                setCode("");
              }}
            >
              {useRecovery ? t("login.useTotp") : t("login.useRecovery")}
            </button>
          </>
        )}
        <ErrorAlert error={shownError} />
        <Button type="submit" variant="primary" busy={login.isPending} disabled={!username || !password || (needsCode && !code.trim())}>
          {needsCode ? t("login.verify") : t("login.submit")}
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
