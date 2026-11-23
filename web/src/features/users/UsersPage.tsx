import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type Permissions, type Role, type User } from "@/api/client";
import { formatDateTime, formatRelative } from "@/components/format";
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  ConfirmButton,
  ErrorAlert,
  PageHeader,
  Panel,
  SelectInput,
  Spinner,
  TextInput,
} from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";
import { InvitesPanel, QuotaEditor } from "./InvitesPanel";
import { StreameroLink } from "./StreameroLink";
import { PERMISSION_KEYS, ROLES, defaultPermissions } from "./permissions";

export function UsersPage() {
  const { t } = useTranslation();
  const [creating, setCreating] = useState(false);
  const users = useQuery({ queryKey: queryKeys.users, queryFn: async () => unwrap(await api.GET("/users")) });
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("users.title")}
        description={t("users.subtitle")}
        actions={
          !creating ? (
            <Button variant="primary" onClick={() => setCreating(true)}>
              {t("users.add")}
            </Button>
          ) : null
        }
      />
      {creating ? <CreateUserForm onDone={() => setCreating(false)} /> : null}
      {users.isPending ? <Spinner /> : null}
      <ErrorAlert error={users.error} />
      <div className="flex flex-col gap-2" data-testid="user-list">
        {users.data?.map((u) => <UserRow key={u.id} user={u} />)}
      </div>
      <InvitesPanel />
    </div>
  );
}

function PermissionFields({ value, onChange, disabled }: { value: Permissions; onChange: (p: Permissions) => void; disabled?: boolean }) {
  const { t } = useTranslation();
  return (
    <fieldset className="grid gap-2 sm:grid-cols-2" disabled={disabled}>
      <legend className="mb-1 text-[12px] font-medium text-muted">{t("users.permissions")}</legend>
      {PERMISSION_KEYS.map((k) => (
        <Checkbox key={k} label={t(`permissions.${k}.label`)} description={t(`permissions.${k}.hint`)} checked={value[k]}
          onChange={(e) => onChange({ ...value, [k]: e.target.checked })} />
      ))}
      {disabled ? <p className="text-[12px] text-faint sm:col-span-2">{t("users.adminHasAll")}</p> : null}
    </fieldset>
  );
}

function RoleSelect({ value, onChange }: { value: Role; onChange: (r: Role) => void }) {
  const { t } = useTranslation();
  return (
    <SelectInput label={t("users.role")} value={value} onChange={(e) => onChange(e.target.value as Role)}>
      {ROLES.map((r) => (
        <option key={r} value={r}>
          {t(`roles.${r}`)}
        </option>
      ))}
    </SelectInput>
  );
}

function CreateUserForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("requester");
  const [perms, setPerms] = useState<Permissions>(defaultPermissions("requester"));
  const create = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/users", { body: { username: username.trim(), password, role, permissions: perms } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.users });
      onDone();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  return (
    <Panel title={t("users.newUser")}>
      <form onSubmit={submit} className="flex flex-col gap-4" data-testid="create-user-form">
        <div className="grid gap-3 sm:grid-cols-3">
          <TextInput label={t("users.username")} name="new-username" autoComplete="off" value={username}
            onChange={(e) => setUsername(e.target.value)} />
          <TextInput label={t("users.password")} name="new-password" type="password" autoComplete="new-password"
            hint={t("setup.passwordHint", { min: 8 })} value={password} onChange={(e) => setPassword(e.target.value)} />
          <RoleSelect
            value={role}
            onChange={(r) => {
              setRole(r);
              setPerms(defaultPermissions(r));
            }}
          />
        </div>
        <PermissionFields value={perms} onChange={setPerms} disabled={role === "admin"} />
        <ErrorAlert error={create.error} />
        <div className="flex gap-2">
          <Button type="submit" variant="primary" busy={create.isPending} disabled={!username.trim() || password.length < 8}>
            {t("users.create")}
          </Button>
          <Button variant="ghost" onClick={onDone}>
            {t("common.cancel")}
          </Button>
        </div>
      </form>
    </Panel>
  );
}

function UserRow({ user }: { user: User }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const me = useCurrentUser();
  const isSelf = me.id === user.id;
  const [editing, setEditing] = useState(false);
  const [role, setRole] = useState<Role>(user.role);
  const [perms, setPerms] = useState<Permissions>(user.permissions);
  const [newPassword, setNewPassword] = useState("");
  const refresh = () => qc.invalidateQueries({ queryKey: queryKeys.users });
  const path = { params: { path: { id: user.id } } };

  const update = useMutation({
    mutationFn: async (body: { role?: Role; permissions?: Permissions; disabled?: boolean }) =>
      unwrap(await api.PATCH("/users/{id}", { ...path, body })),
    onSuccess: () => {
      void refresh();
      setEditing(false);
    },
  });
  const resetPassword = useMutation({
    mutationFn: async () => unwrap(await api.POST("/users/{id}/password", { ...path, body: { password: newPassword } })),
    onSuccess: () => setNewPassword(""),
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/users/{id}", path)),
    onSuccess: () => refresh(),
  });
  const locked = user.lockedUntil && new Date(user.lockedUntil).getTime() > Date.now();

  return (
    <div className="rounded-ui border border-line bg-surface" data-testid={`user-${user.username}`}>
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5">
        <div className="min-w-0">
          <p className="flex flex-wrap items-center gap-2 text-[13px] text-fg">
            <span className="font-medium">{user.username}</span>
            <Badge tone={user.role === "admin" ? "warning" : user.role === "manager" ? "info" : "neutral"}>{t(`roles.${user.role}`)}</Badge>
            {user.disabled ? <Badge tone="danger">{t("users.disabled")}</Badge> : null}
            {locked ? <Badge tone="danger">{t("users.locked")}</Badge> : null}
            {user.totpEnabled ? <Badge tone="success">2FA</Badge> : null}
            {isSelf ? <Badge>{t("users.you")}</Badge> : null}
          </p>
          <p className="text-[12px] text-faint">
            {t("users.created", { when: formatDateTime(user.createdAt, i18n.language) })} ·{" "}
            {user.lastLoginAt ? t("users.lastLogin", { when: formatRelative(user.lastLoginAt, i18n.language) }) : t("users.neverSignedIn")}
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" onClick={() => setEditing(!editing)} aria-expanded={editing}>
            {editing ? t("common.close") : t("users.edit")}
          </Button>
          {!isSelf ? (
            <ConfirmButton label={t("users.delete")} confirmLabel={t("users.deleteConfirm", { username: user.username })}
              busy={remove.isPending} onConfirm={() => remove.mutate()} />
          ) : null}
        </div>
      </div>
      {remove.error && !editing ? (
        <div className="px-4 pb-3">
          <ErrorAlert error={remove.error} />
        </div>
      ) : null}
      {editing ? (
        <div className="flex flex-col gap-4 border-t border-line px-4 py-3">
          <div className="max-w-xs">
            <RoleSelect value={role} onChange={setRole} />
          </div>
          <PermissionFields value={perms} onChange={setPerms} disabled={role === "admin"} />
          <ErrorAlert error={update.error} />
          <div className="flex flex-wrap gap-2">
            <Button variant="primary" busy={update.isPending} onClick={() => update.mutate({ role, permissions: perms })}>
              {t("common.save")}
            </Button>
            {!isSelf ? (
              <Button variant={user.disabled ? "secondary" : "danger"} busy={update.isPending}
                onClick={() => update.mutate({ disabled: !user.disabled })}>
                {user.disabled ? t("users.enable") : t("users.disable")}
              </Button>
            ) : null}
          </div>
          {user.role !== "admin" ? <QuotaEditor userId={user.id} /> : null}
          <StreameroLink user={user} />
          <form
            className="flex flex-wrap items-end gap-2 border-t border-line pt-3"
            onSubmit={(e) => {
              e.preventDefault();
              resetPassword.mutate();
            }}
          >
            <div className="min-w-48 flex-1">
              <TextInput label={t("users.resetPassword")} type="password" autoComplete="new-password" value={newPassword}
                hint={t("users.resetPasswordHint")} onChange={(e) => setNewPassword(e.target.value)} />
            </div>
            <Button type="submit" busy={resetPassword.isPending} disabled={newPassword.length < 8}>
              {t("users.setPassword")}
            </Button>
          </form>
          <ErrorAlert error={resetPassword.error ?? remove.error} />
          {resetPassword.isSuccess ? <Alert tone="success">{t("users.passwordSet")}</Alert> : null}
        </div>
      ) : null}
    </div>
  );
}
