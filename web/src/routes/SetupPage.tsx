import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, deviceInfo, queryKeys, unwrap, type Language, type SystemInfo } from "@/api/client";
import { AuthLayout } from "@/components/AuthLayout";
import { Alert, Button, ErrorAlert, SelectInput, TextInput } from "@/components/ui";
import { SUPPORTED_LANGUAGES, currentLanguage, isLanguage, setLanguage } from "@/i18n";

const MIN_PASSWORD = 8;

/** First run: create the administrator, protected by the code from the server log. */
export function SetupPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [form, setForm] = useState({ setupCode: "", username: "", password: "", confirm: "", instanceName: "" });
  const [language, setLang] = useState<Language>(currentLanguage());
  const [localError, setLocalError] = useState<string | null>(null);
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });

  const setup = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/setup", {
          body: {
            setupCode: form.setupCode.trim(),
            username: form.username.trim(),
            password: form.password,
            language,
            instanceName: form.instanceName.trim() || undefined,
            device: deviceInfo(),
            useCookie: true,
          },
        }),
      ),
    onSuccess: async (res) => {
      qc.setQueryData<SystemInfo>(queryKeys.info, (old) => (old ? { ...old, setupRequired: false } : old));
      qc.setQueryData(queryKeys.me, res.user);
      setLanguage(res.user.language);
      await navigate({ to: "/welcome" });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (form.password.length < MIN_PASSWORD) return setLocalError(t("setup.passwordTooShort", { min: MIN_PASSWORD }));
    if (form.password !== form.confirm) return setLocalError(t("setup.passwordMismatch"));
    setLocalError(null);
    setup.mutate();
  };

  return (
    <AuthLayout title={t("setup.title")} subtitle={t("setup.subtitle")}>
      <form onSubmit={submit} className="flex flex-col gap-3" noValidate>
        <Alert tone="info">
          {t("setup.codeHint")} <code className="font-mono text-[12px]">docker compose logs locarr</code>
        </Alert>
        <TextInput label={t("setup.code")} name="setupCode" autoComplete="off" required autoFocus spellCheck={false}
          className="font-mono uppercase tracking-widest" value={form.setupCode} onChange={set("setupCode")} />
        <TextInput label={t("setup.username")} name="username" autoComplete="username" required value={form.username}
          onChange={set("username")} />
        <TextInput label={t("setup.password")} name="password" type="password" autoComplete="new-password" required
          hint={t("setup.passwordHint", { min: MIN_PASSWORD })} value={form.password} onChange={set("password")} />
        <TextInput label={t("setup.confirmPassword")} name="confirm" type="password" autoComplete="new-password" required
          value={form.confirm} onChange={set("confirm")} />
        <SelectInput
          label={t("setup.language")}
          name="language"
          value={language}
          onChange={(e) => {
            if (!isLanguage(e.target.value)) return;
            setLang(e.target.value);
            setLanguage(e.target.value);
          }}
        >
          {SUPPORTED_LANGUAGES.map((l) => (
            <option key={l} value={l}>
              {t(`languages.${l}`)}
            </option>
          ))}
        </SelectInput>
        <TextInput label={t("setup.instanceName")} name="instanceName" placeholder="Locarr" hint={t("setup.instanceNameHint")}
          value={form.instanceName} onChange={set("instanceName")} />
        {localError ? <Alert>{localError}</Alert> : <ErrorAlert error={setup.error} />}
        <Button type="submit" variant="primary" busy={setup.isPending}
          disabled={!form.setupCode.trim() || !form.username.trim() || !form.password}>
          {t("setup.submit")}
        </Button>
      </form>
    </AuthLayout>
  );
}
