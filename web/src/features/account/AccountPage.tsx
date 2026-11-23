import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type CreatedApiKey, type TotpSetup } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import { LanguagePicker } from "@/components/LanguagePicker";
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  ConfirmButton,
  EmptyState,
  ErrorAlert,
  PageHeader,
  Panel,
  Spinner,
  TextInput,
} from "@/components/ui";
import { useChangeLanguage, useCurrentUser } from "@/features/auth/hooks";
import { LinkedAccountsPanel, MyNotificationsPanel } from "./ConnectedPanels";

export function AccountPage() {
  const { t } = useTranslation();
  const me = useCurrentUser();
  return (
    <div>
      <PageHeader title={t("account.title")} description={t("account.subtitle", { username: me.username })} />
      <div className="grid gap-4 lg:grid-cols-2">
        <PreferencesPanel />
        <PasswordPanel />
        <TwoFactorPanel />
        <SessionsPanel />
        <div className="lg:col-span-2">
          <MyNotificationsPanel />
        </div>
        <LinkedAccountsPanel />
        <div className="lg:col-span-2">
          <ApiKeysPanel />
        </div>
      </div>
    </div>
  );
}

function PreferencesPanel() {
  const { t } = useTranslation();
  const change = useChangeLanguage();
  return (
    <Panel title={t("account.preferences")} description={t("account.languageHint")}>
      <div className="flex items-center gap-3">
        <span className="text-[13px] text-muted">{t("common.language")}</span>
        <LanguagePicker onChange={change} />
      </div>
    </Panel>
  );
}

function PasswordPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [mismatch, setMismatch] = useState(false);
  const change = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/auth/password", { body: { currentPassword: current, newPassword: next } })),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setConfirm("");
      void qc.invalidateQueries({ queryKey: queryKeys.sessions });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (next !== confirm) return setMismatch(true);
    setMismatch(false);
    change.mutate();
  };
  return (
    <Panel title={t("account.password.title")} description={t("account.password.hint")}>
      <form onSubmit={submit} className="flex flex-col gap-3">
        <TextInput label={t("account.password.current")} type="password" autoComplete="current-password" value={current}
          onChange={(e) => setCurrent(e.target.value)} />
        <TextInput label={t("account.password.new")} type="password" autoComplete="new-password" minLength={8} value={next}
          onChange={(e) => setNext(e.target.value)} />
        <TextInput label={t("account.password.confirm")} type="password" autoComplete="new-password" value={confirm}
          onChange={(e) => setConfirm(e.target.value)} />
        {mismatch ? <Alert>{t("setup.passwordMismatch")}</Alert> : <ErrorAlert error={change.error} />}
        {change.isSuccess ? <Alert tone="success">{t("account.password.changed")}</Alert> : null}
        <div>
          <Button type="submit" variant="primary" busy={change.isPending} disabled={!current || next.length < 8}>
            {t("account.password.submit")}
          </Button>
        </div>
      </form>
    </Panel>
  );
}

function RecoveryCodeList({ codes }: { codes: string[] }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2" data-testid="recovery-codes">
      <Alert tone="warning">{t("account.totp.codesOnce")}</Alert>
      <ul className="grid grid-cols-2 gap-1 rounded-ui border border-line bg-surface-2 p-3 font-mono text-[12px]">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div>
        <CopyButton text={codes.join("\n")} />
      </div>
    </div>
  );
}

function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  return (
    <Button
      onClick={() => {
        void navigator.clipboard?.writeText(text).then(() => setCopied(true), () => setCopied(false));
      }}
    >
      {copied ? t("common.copied") : t("common.copy")}
    </Button>
  );
}

function TwoFactorPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const me = useCurrentUser();
  const [pending, setPending] = useState<TotpSetup | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const refreshMe = () => qc.invalidateQueries({ queryKey: queryKeys.me });

  const start = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/setup")),
    onSuccess: (s) => {
      setPending(s);
      setCodes(null);
    },
  });
  const enable = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/enable", { body: { code: code.trim() } })),
    onSuccess: (r) => {
      setCodes(r.codes);
      setPending(null);
      setCode("");
      void refreshMe();
    },
  });
  const disable = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/disable", { body: { password } })),
    onSuccess: () => {
      setPassword("");
      setCodes(null);
      void refreshMe();
    },
  });
  const regenerate = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/recovery-codes", { body: { password } })),
    onSuccess: (r) => {
      setPassword("");
      setCodes(r.codes);
    },
  });

  return (
    <Panel
      title={t("account.totp.title")}
      description={t("account.totp.hint")}
      actions={me.totpEnabled ? <Badge tone="success">{t("account.totp.on")}</Badge> : <Badge>{t("account.totp.off")}</Badge>}
    >
      <div className="flex flex-col gap-3">
        {codes ? <RecoveryCodeList codes={codes} /> : null}
        {!me.totpEnabled && !pending ? (
          <div>
            <ErrorAlert error={start.error} />
            <Button variant="primary" busy={start.isPending} onClick={() => start.mutate()}>
              {t("account.totp.start")}
            </Button>
          </div>
        ) : null}
        {pending ? (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              enable.mutate();
            }}
          >
            <p className="text-[13px] text-muted">{t("account.totp.addToApp")}</p>
            <div className="rounded-ui border border-line bg-surface-2 p-3">
              <p className="text-[11px] uppercase tracking-wide text-faint">{t("account.totp.secret")}</p>
              <p className="mt-1 break-all font-mono text-[13px] text-fg" data-testid="totp-secret">{pending.secret}</p>
              <p className="mt-2 text-[11px] uppercase tracking-wide text-faint">{t("account.totp.uri")}</p>
              <p className="mt-1 break-all font-mono text-[11px] text-muted">{pending.uri}</p>
            </div>
            <TextInput label={t("account.totp.code")} inputMode="numeric" autoComplete="one-time-code" value={code}
              onChange={(e) => setCode(e.target.value)} />
            <ErrorAlert error={enable.error} />
            <div className="flex gap-2">
              <Button type="submit" variant="primary" busy={enable.isPending} disabled={!code.trim()}>
                {t("account.totp.enable")}
              </Button>
              <Button variant="ghost" onClick={() => setPending(null)}>
                {t("common.cancel")}
              </Button>
            </div>
          </form>
        ) : null}
        {me.totpEnabled ? (
          <div className="flex flex-col gap-3">
            <TextInput label={t("account.totp.confirmPassword")} type="password" autoComplete="current-password" value={password}
              onChange={(e) => setPassword(e.target.value)} />
            <ErrorAlert error={disable.error ?? regenerate.error} />
            <div className="flex flex-wrap gap-2">
              <Button busy={regenerate.isPending} disabled={!password} onClick={() => regenerate.mutate()}>
                {t("account.totp.regenerate")}
              </Button>
              <Button variant="danger" busy={disable.isPending} disabled={!password} onClick={() => disable.mutate()}>
                {t("account.totp.disable")}
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </Panel>
  );
}

function SessionsPanel() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const sessions = useQuery({ queryKey: queryKeys.sessions, queryFn: async () => unwrap(await api.GET("/auth/sessions")) });
  const revoke = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/auth/sessions/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.sessions }),
  });
  return (
    <Panel title={t("account.sessions.title")} description={t("account.sessions.hint")}>
      {sessions.isPending ? <Spinner /> : null}
      <ErrorAlert error={sessions.error ?? revoke.error} />
      {sessions.data?.length === 0 ? <EmptyState>{t("account.sessions.none")}</EmptyState> : null}
      <ul className="divide-y divide-line">
        {sessions.data?.map((s) => (
          <li key={s.id} className="flex items-center justify-between gap-3 py-2">
            <div className="min-w-0">
              <p className="flex items-center gap-2 truncate text-[13px] text-fg">
                {s.deviceName || t("account.sessions.unknownDevice")}
                {s.current ? <Badge tone="success">{t("account.sessions.current")}</Badge> : null}
              </p>
              <p className="truncate text-[12px] text-faint">
                {[s.client, s.ip, t("account.sessions.lastSeen", { when: formatRelative(s.lastSeenAt, i18n.language) })]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
            </div>
            {!s.current ? (
              <ConfirmButton label={t("account.sessions.revoke")} confirmLabel={t("account.sessions.revokeConfirm")}
                busy={revoke.isPending} onConfirm={() => revoke.mutate(s.id)} />
            ) : null}
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function ApiKeysPanel() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [readOnly, setReadOnly] = useState(true);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const keys = useQuery({ queryKey: queryKeys.apiKeys, queryFn: async () => unwrap(await api.GET("/auth/apikeys")) });
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/apikeys", { body: { name: name.trim(), readOnly } })),
    onSuccess: (k) => {
      setCreated(k);
      setName("");
      void qc.invalidateQueries({ queryKey: queryKeys.apiKeys });
    },
  });
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/auth/apikeys/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.apiKeys }),
  });
  return (
    <Panel title={t("account.apiKeys.title")} description={t("account.apiKeys.hint")}>
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <div className="min-w-48 flex-1">
          <TextInput label={t("account.apiKeys.name")} value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="pb-1.5">
          <Checkbox label={t("account.apiKeys.readOnly")} checked={readOnly} onChange={(e) => setReadOnly(e.target.checked)} />
        </div>
        <Button type="submit" variant="primary" busy={create.isPending} disabled={!name.trim()}>
          {t("account.apiKeys.create")}
        </Button>
      </form>
      <div className="mt-3 flex flex-col gap-2">
        <ErrorAlert error={create.error ?? keys.error ?? remove.error} />
        {created ? (
          <div className="flex flex-col gap-2 rounded-ui border border-warning/40 bg-warning-bg p-3" data-testid="created-api-key">
            <p className="text-[13px] text-warning">{t("account.apiKeys.shownOnce")}</p>
            <code className="break-all rounded bg-bg px-2 py-1 font-mono text-[12px] text-fg">{created.key}</code>
            <div className="flex gap-2">
              <CopyButton text={created.key} />
              <Button variant="ghost" onClick={() => setCreated(null)}>
                {t("common.done")}
              </Button>
            </div>
          </div>
        ) : null}
      </div>
      {keys.isPending ? <Spinner /> : null}
      {keys.data?.length === 0 ? <EmptyState>{t("account.apiKeys.none")}</EmptyState> : null}
      {keys.data && keys.data.length > 0 ? (
        <div className="mt-3 overflow-x-auto">
          <table className="w-full text-left text-[13px]">
            <thead className="text-[11px] uppercase tracking-wide text-faint">
              <tr>
                <th className="py-1.5 pr-3 font-medium">{t("account.apiKeys.name")}</th>
                <th className="py-1.5 pr-3 font-medium">{t("account.apiKeys.key")}</th>
                <th className="py-1.5 pr-3 font-medium">{t("account.apiKeys.access")}</th>
                <th className="py-1.5 pr-3 font-medium">{t("account.apiKeys.created")}</th>
                <th className="py-1.5 pr-3 font-medium">{t("account.apiKeys.lastUsed")}</th>
                <th />
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {keys.data.map((k) => (
                <tr key={k.id}>
                  <td className="py-1.5 pr-3 text-fg">{k.name}</td>
                  <td className="py-1.5 pr-3 font-mono text-[12px] text-muted">{k.prefix}…</td>
                  <td className="py-1.5 pr-3">
                    {k.readOnly ? <Badge tone="info">{t("account.apiKeys.readOnlyBadge")}</Badge> : <Badge tone="warning">{t("account.apiKeys.fullBadge")}</Badge>}
                  </td>
                  <td className="py-1.5 pr-3 text-muted">{formatDateTime(k.createdAt, i18n.language)}</td>
                  <td className="py-1.5 pr-3 text-muted">{k.lastUsedAt ? formatRelative(k.lastUsedAt, i18n.language) : t("common.never")}</td>
                  <td className="py-1.5 text-right">
                    <ConfirmButton label={t("account.apiKeys.revoke")} confirmLabel={t("account.apiKeys.revokeConfirm")}
                      busy={remove.isPending} onConfirm={() => remove.mutate(k.id)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </Panel>
  );
}
