import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ApiError, api, queryKeys, unwrap } from "@/api/client";
import { formatRelative } from "@/components/format";
import { Button, ConfirmButton, EmptyState, ErrorAlert, Panel, Spinner, TextInput } from "@/components/ui";
import { ChannelList } from "@/features/notifications/channels";
import { useStreameroSignIn } from "@/features/mediaserver/streamero";

/** The caller's own notification channels (their requests and problem reports). */
export function MyNotificationsPanel() {
  const { t } = useTranslation();
  return (
    <Panel title={t("account.notifications.title")} description={t("account.notifications.hint")}>
      <ChannelList personal />
    </Panel>
  );
}

/** Accounts at other services linked to the caller (Streamero). */
export function LinkedAccountsPanel() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const offered = useStreameroSignIn();
  const q = useQuery({ queryKey: queryKeys.identities, queryFn: async () => unwrap(await api.GET("/auth/identities")) });
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [needsCode, setNeedsCode] = useState(false);
  const link = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/auth/identities/{provider}", { params: { path: { provider: "streamero" } },
        body: { username, password, ...(needsCode && code.trim() ? { totpCode: code.trim() } : {}) } })),
    onSuccess: async () => {
      setUsername("");
      setPassword("");
      setCode("");
      setNeedsCode(false);
      await qc.invalidateQueries({ queryKey: queryKeys.identities });
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === "totp_required") setNeedsCode(true);
    },
  });
  const unlink = useMutation({
    mutationFn: async (provider: string) =>
      unwrap(await api.DELETE("/auth/identities/{provider}", { params: { path: { provider } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.identities }),
  });
  if (!offered && !q.data?.length) return null;
  const streamero = q.data?.find((i) => i.provider === "streamero");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    link.mutate();
  };
  const shown = link.error instanceof ApiError && link.error.code === "totp_required" ? null : link.error;
  return (
    <Panel title={t("account.linked.title")} description={t("account.linked.hint")}>
      <div className="flex flex-col gap-3" data-testid="linked-accounts">
        {q.isPending ? <Spinner /> : null}
        <ErrorAlert error={q.error ?? unlink.error} />
        {q.data?.map((i) => (
          <div key={i.provider} className="flex flex-wrap items-center justify-between gap-2 rounded-ui border border-line px-3 py-2"
            data-testid={`identity-${i.provider}`}>
            <div>
              <p className="text-[13px] text-fg">{t("account.linked.as", { provider: t(`account.linked.providers.${i.provider}`, { defaultValue: i.provider }), username: i.username || i.subject })}</p>
              <p className="text-[12px] text-faint">
                {i.lastLoginAt ? t("account.linked.lastUsed", { when: formatRelative(i.lastLoginAt, i18n.language) }) : t("account.linked.notUsed")}
              </p>
            </div>
            <ConfirmButton label={t("account.linked.unlink")} confirmLabel={t("account.linked.unlinkConfirm")} busy={unlink.isPending}
              onConfirm={() => unlink.mutate(i.provider)} testId={`unlink-${i.provider}`} />
          </div>
        ))}
        {q.data && q.data.length === 0 ? <EmptyState>{t("account.linked.none")}</EmptyState> : null}
        {offered && !streamero ? (
          <form onSubmit={submit} className="flex flex-col gap-3 border-t border-line pt-3" data-testid="link-streamero">
            <p className="text-[12px] text-muted">{t("account.linked.linkHint")}</p>
            {!needsCode ? (
              <div className="grid gap-3 sm:grid-cols-2">
                <TextInput label={t("account.linked.streameroUsername")} autoComplete="off" value={username}
                  onChange={(e) => setUsername(e.target.value)} />
                <TextInput label={t("account.linked.streameroPassword")} type="password" autoComplete="off" value={password}
                  onChange={(e) => setPassword(e.target.value)} />
              </div>
            ) : (
              <TextInput label={t("login.streameroCode")} inputMode="numeric" autoComplete="one-time-code" autoFocus value={code}
                onChange={(e) => setCode(e.target.value)} />
            )}
            <ErrorAlert error={shown} />
            <div>
              <Button type="submit" busy={link.isPending} disabled={!username || !password || (needsCode && !code.trim())}
                data-testid="link-streamero-submit">
                {t("account.linked.link")}
              </Button>
            </div>
          </form>
        ) : null}
      </div>
    </Panel>
  );
}
