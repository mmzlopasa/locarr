import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type Invite, type Permissions, type QuotaOverrides, type QuotaRule, type Role } from "@/api/client";
import { formatDateTime } from "@/components/format";
import { Alert, Badge, Button, Checkbox, ConfirmButton, EmptyState, ErrorAlert, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { QuotaFields } from "@/features/settings/RequestSettingsPage";
import { PERMISSION_KEYS, defaultPermissions } from "./permissions";

type QKind = "movie" | "series" | "music";
const QKINDS: QKind[] = ["movie", "series", "music"];

/** Invite links that let family and friends make their own accounts. */
export function InvitesPanel() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: queryKeys.invites, queryFn: async () => unwrap(await api.GET("/invites")) });
  const [creating, setCreating] = useState(false);
  const [link, setLink] = useState("");
  const [copied, setCopied] = useState(false);
  const revoke = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/invites/{id}", { params: { path: { id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.invites }),
  });
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <Panel title={t("invites.title")} description={t("invites.subtitle")}
      actions={!creating ? <Button variant="primary" onClick={() => { setCreating(true); setLink(""); }} data-testid="invite-new">{t("invites.new")}</Button> : null}>
      <div className="flex flex-col gap-3" data-testid="invites">
        {link ? (
          <Alert tone="success">
            <span className="flex flex-col gap-2">
              <span>{t("invites.created")}</span>
              <span className="flex flex-wrap items-center gap-2">
                <code className="break-all rounded bg-surface-3 px-1.5 py-0.5 font-mono text-[12px] text-fg" data-testid="invite-link">{link}</code>
                <Button onClick={() => void copy()}>{copied ? t("common.copied") : t("common.copy")}</Button>
              </span>
              <span className="text-[12px] text-muted">{t("invites.onlyOnce")}</span>
            </span>
          </Alert>
        ) : null}
        {creating ? <InviteForm onCancel={() => setCreating(false)} onCreated={(code) => {
          setLink(`${window.location.origin}/join#${code}`);
          setCopied(false);
          setCreating(false);
          void qc.invalidateQueries({ queryKey: queryKeys.invites });
        }} /> : null}
        {list.isPending ? <Spinner /> : null}
        <ErrorAlert error={list.error ?? revoke.error} />
        {list.data && list.data.length === 0 && !creating ? <EmptyState>{t("invites.none")}</EmptyState> : null}
        <ul className="flex flex-col gap-2">
          {list.data?.map((inv) => <InviteRow key={inv.id} inv={inv} locale={i18n.language} onRevoke={() => revoke.mutate(inv.id)} busy={revoke.isPending} />)}
        </ul>
      </div>
    </Panel>
  );
}

function InviteRow({ inv, locale, onRevoke, busy }: { inv: Invite; locale: string; onRevoke: () => void; busy: boolean }) {
  const { t } = useTranslation();
  const dead = inv.revoked || inv.expired;
  return (
    <li className="flex flex-wrap items-center justify-between gap-2 rounded-ui border border-line bg-surface-2 px-3 py-2" data-testid={`invite-${inv.id}`}>
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2 text-[13px] text-fg">
          <span className="font-medium">{inv.label || t("invites.unnamed")}</span>
          <Badge tone={inv.role === "manager" ? "info" : "neutral"}>{t(`roles.${inv.role}`)}</Badge>
          {inv.revoked ? <Badge tone="danger">{t("invites.revoked")}</Badge> : inv.expired ? <Badge tone="warning">{t("invites.expired")}</Badge> : <Badge tone="success">{t("invites.active")}</Badge>}
        </p>
        <p className="text-[12px] text-faint">
          {t("invites.uses", { uses: inv.uses, max: inv.maxUses === 0 ? "∞" : inv.maxUses })}
          {inv.expiresAt ? ` · ${t("invites.expires", { when: formatDateTime(inv.expiresAt, locale) })}` : ""}
          {inv.createdBy ? ` · ${t("invites.by", { user: inv.createdBy })}` : ""}
        </p>
      </div>
      {!dead ? <ConfirmButton label={t("invites.revoke")} confirmLabel={t("invites.revokeConfirm")} onConfirm={onRevoke} busy={busy} testId={`invite-revoke-${inv.id}`} /> : null}
    </li>
  );
}

function InviteForm({ onCancel, onCreated }: { onCancel: () => void; onCreated: (code: string) => void }) {
  const { t } = useTranslation();
  const [label, setLabel] = useState("");
  const [role, setRole] = useState<Extract<Role, "manager" | "requester">>("requester");
  const [perms, setPerms] = useState<Permissions>(defaultPermissions("requester"));
  const [maxUses, setMaxUses] = useState(1);
  const [days, setDays] = useState(7);
  const [custom, setCustom] = useState(false);
  const [quotas, setQuotas] = useState<Record<QKind, QuotaRule>>({ movie: { limit: 0, days: 7 }, series: { limit: 0, days: 7 }, music: { limit: 0, days: 7 } });
  const create = useMutation({
    mutationFn: async () => {
      const q: QuotaOverrides | undefined = custom ? quotas : undefined;
      return unwrap(await api.POST("/invites", { body: { label: label.trim() || undefined, role, permissions: perms, maxUses, expiresInDays: days,
        ...(q ? { quotas: q } : {}) } }));
    },
    onSuccess: (res) => onCreated(res.code),
  });
  return (
    <form className="flex flex-col gap-3 rounded-ui border border-line p-3" data-testid="invite-form" onSubmit={(e) => {
      e.preventDefault();
      create.mutate();
    }}>
      <div className="grid gap-3 sm:grid-cols-4">
        <TextInput label={t("invites.label")} placeholder={t("invites.labelHint")} value={label} maxLength={100} onChange={(e) => setLabel(e.target.value)} data-testid="invite-label" />
        <SelectInput label={t("users.role")} value={role} onChange={(e) => {
          const r = e.target.value as "manager" | "requester";
          setRole(r);
          setPerms(defaultPermissions(r));
        }}>
          <option value="requester">{t("roles.requester")}</option>
          <option value="manager">{t("roles.manager")}</option>
        </SelectInput>
        <TextInput label={t("invites.maxUses")} type="number" min={0} max={1000} value={maxUses} hint={t("invites.maxUsesHint")}
          onChange={(e) => setMaxUses(Math.max(0, Number(e.target.value) || 0))} />
        <TextInput label={t("invites.days")} type="number" min={1} max={365} value={days}
          onChange={(e) => setDays(Math.min(365, Math.max(1, Number(e.target.value) || 1)))} />
      </div>
      <fieldset className="grid gap-2 sm:grid-cols-2">
        <legend className="mb-1 text-[12px] font-medium text-muted">{t("users.permissions")}</legend>
        {PERMISSION_KEYS.map((k) => (
          <Checkbox key={k} label={t(`permissions.${k}.label`)} checked={perms[k]} onChange={(e) => setPerms({ ...perms, [k]: e.target.checked })} />
        ))}
      </fieldset>
      <Checkbox label={t("invites.customQuotas")} description={t("invites.customQuotasHint")} checked={custom} onChange={(e) => setCustom(e.target.checked)} />
      {custom ? (
        <div className="grid gap-3 sm:grid-cols-3">
          {QKINDS.map((k) => <QuotaFields key={k} label={t(`requestSettings.kinds.${k}`)} value={quotas[k]}
            onChange={(p) => setQuotas({ ...quotas, [k]: { ...quotas[k], ...p } })} />)}
        </div>
      ) : null}
      <ErrorAlert error={create.error} />
      <div className="flex gap-2">
        <Button type="submit" variant="primary" busy={create.isPending} data-testid="invite-create">{t("invites.create")}</Button>
        <Button variant="ghost" onClick={onCancel}>{t("common.cancel")}</Button>
      </div>
    </form>
  );
}

/** A user's own request quotas (or the defaults), with what is used now. */
export function QuotaEditor({ userId }: { userId: number }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: queryKeys.userQuotas(userId), queryFn: async () =>
    unwrap(await api.GET("/users/{id}/quotas", { params: { path: { id: userId } } })) });
  const [draft, setDraft] = useState<QuotaOverrides | null>(null);
  const save = useMutation({
    mutationFn: async (body: QuotaOverrides) => unwrap(await api.PUT("/users/{id}/quotas", { params: { path: { id: userId } }, body })),
    onSuccess: (res) => {
      qc.setQueryData(queryKeys.userQuotas(userId), res);
      setDraft(null);
    },
  });
  if (q.isPending) return <Spinner />;
  if (q.error) return <ErrorAlert error={q.error} />;
  const value = draft ?? q.data.overrides;
  return (
    <div className="flex flex-col gap-2 border-t border-line pt-3" data-testid="quota-editor">
      <p className="text-[12px] font-medium text-muted">{t("invites.quotasTitle")}</p>
      <div className="grid gap-3 sm:grid-cols-3">
        {QKINDS.map((k) => {
          const st = q.data[k];
          const own = value[k];
          return (
            <div key={k} className="flex flex-col gap-2 rounded-ui border border-line p-3">
              <p className="text-[12.5px] font-medium text-fg">{t(`requestSettings.kinds.${k}`)}</p>
              <p className="text-[12px] text-faint" data-testid={`quota-used-${k}`}>
                {st.unlimited ? t("invites.usedUnlimited", { used: st.used }) : t("invites.used", { used: st.used, limit: st.limit, days: st.days })}
              </p>
              <Checkbox label={t("invites.ownRule")} checked={!!own} onChange={(e) => {
                const next = { ...value };
                if (e.target.checked) next[k] = { limit: st.limit, days: st.days };
                else delete next[k];
                setDraft(next);
              }} data-testid={`quota-own-${k}`} />
              {own ? <QuotaFields label={t("invites.rule")} value={own} onChange={(p) => setDraft({ ...value, [k]: { ...own, ...p } })} testId={`quota-user-${k}`} /> : null}
            </div>
          );
        })}
      </div>
      <ErrorAlert error={save.error} />
      <div>
        <Button busy={save.isPending} disabled={!draft} onClick={() => draft && save.mutate(draft)} data-testid="quota-save">{t("invites.saveQuotas")}</Button>
      </div>
    </div>
  );
}
