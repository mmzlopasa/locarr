import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, deviceInfo, queryKeys, unwrap, type InviteCheck, type Language } from "@/api/client";
import { AuthLayout } from "@/components/AuthLayout";
import { Alert, Button, ErrorAlert, TextInput } from "@/components/ui";
import { setLanguage } from "@/i18n";

/** The invite code travels in the URL fragment (#code), which browsers never send to the server. */
export function codeFromHash(hash: string): string {
  return decodeURIComponent(hash.replace(/^#/, "")).trim();
}

export function JoinPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [code, setCode] = useState(() => (typeof window !== "undefined" ? codeFromHash(window.location.hash) : ""));
  const [invite, setInvite] = useState<InviteCheck | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");

  const check = useMutation({
    mutationFn: async (c: string) => unwrap(await api.POST("/auth/invite", { body: { code: c } })),
    onSuccess: (res) => setInvite(res),
  });
  useEffect(() => {
    if (code) check.mutate(code);
    // Only once, for a code that came with the link.
  }, []);

  const signUp = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/signup", { body: { code, username: username.trim(), password,
      language: (i18n.language.startsWith("hr") ? "hr" : "en") as Language, device: deviceInfo(), useCookie: true } })),
    onSuccess: async (res) => {
      // The code is spent; drop it from the address bar.
      window.history.replaceState(null, "", "/join");
      qc.setQueryData(queryKeys.me, res.user);
      setLanguage(res.user.language);
      await navigate({ to: "/discover" });
    },
  });

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    if (code.trim()) check.mutate(code.trim());
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    signUp.mutate();
  };
  const mismatch = repeat !== "" && repeat !== password;

  if (!invite) {
    return (
      <AuthLayout title={t("join.title")} subtitle={t("join.codeHint")}>
        <form onSubmit={submitCode} className="flex flex-col gap-3" noValidate>
          <TextInput label={t("join.code")} name="code" autoComplete="off" value={code} onChange={(e) => setCode(e.target.value)}
            data-testid="join-code" autoFocus={!code} />
          <ErrorAlert error={check.error} />
          <Button type="submit" variant="primary" busy={check.isPending} disabled={!code.trim()} data-testid="join-check">
            {t("join.continue")}
          </Button>
        </form>
      </AuthLayout>
    );
  }
  return (
    <AuthLayout title={t("join.welcome", { name: invite.instanceName })}
      subtitle={t("join.as", { role: t(`roles.${invite.role}`) })}>
      <form onSubmit={submit} className="flex flex-col gap-3" noValidate data-testid="join-form">
        {invite.label ? <Alert tone="info">{invite.label}</Alert> : null}
        <TextInput label={t("join.username")} name="username" autoComplete="username" required autoFocus value={username}
          onChange={(e) => setUsername(e.target.value)} data-testid="join-username" />
        <TextInput label={t("join.password")} name="password" type="password" autoComplete="new-password" required
          hint={t("setup.passwordHint", { min: 8 })} value={password} onChange={(e) => setPassword(e.target.value)} data-testid="join-password" />
        <TextInput label={t("join.repeat")} name="repeat" type="password" autoComplete="new-password" required value={repeat}
          onChange={(e) => setRepeat(e.target.value)} data-testid="join-repeat" />
        {mismatch ? <Alert>{t("join.mismatch")}</Alert> : null}
        <ErrorAlert error={signUp.error} />
        <Button type="submit" variant="primary" busy={signUp.isPending} data-testid="join-submit"
          disabled={!username.trim() || password.length < 8 || password !== repeat}>
          {t("join.create")}
        </Button>
      </form>
    </AuthLayout>
  );
}
