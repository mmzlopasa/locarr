import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import {
  api,
  queryKeys,
  unwrap,
  type NotificationChannel,
  type NotificationChannelInput,
  type NotificationChannelKind,
  type NotificationChannelSecrets,
  type NotificationChannelSettings,
  type NotificationEventType,
  type NotificationTestResult,
} from "@/api/client";
import { formatRelative } from "@/components/format";
import { useMe } from "@/features/auth/hooks";
import { canSeeAdult } from "@/features/adult/api";
import { Modal } from "@/components/Modal";
import { Alert, Badge, Button, Checkbox, ConfirmButton, EmptyState, ErrorAlert, SelectInput, Spinner, TextInput } from "@/components/ui";

export const ALL_KINDS: NotificationChannelKind[] = ["webhook", "ntfy", "gotify", "discord", "telegram", "pushover", "email", "apprise"];
/** Kinds a person can use for their own channel (no webhooks or Apprise: those reach internal services). */
export const PERSONAL_KINDS: NotificationChannelKind[] = ["ntfy", "gotify", "discord", "telegram", "pushover", "email"];

export const EVENT_GROUPS: { key: string; personal: boolean; adult?: boolean; events: NotificationEventType[] }[] = [
  { key: "requests", personal: true, events: ["request.created", "request.approved", "request.declined", "request.available",
    "request.partial", "request.failed", "request.comment"] },
  { key: "issues", personal: true, events: ["issue.created", "issue.comment", "issue.resolved", "issue.reopened"] },
  { key: "library", personal: false, events: ["library.grabbed", "library.imported", "library.upgraded", "library.download_failed",
    "library.import_failed"] },
  { key: "subtitles", personal: false, events: ["subtitles.downloaded", "subtitles.missing"] },
  { key: "system", personal: false, events: ["health.warning", "migrate.finished", "migrate.failed"] },
  // Adult events are never chosen by default and only listed while the adult library is visible.
  { key: "adult", personal: false, adult: true, events: ["adult.grabbed", "adult.imported", "adult.upgraded", "adult.download_failed",
    "adult.import_failed"] },
];

type SettingKey = "url" | "method" | "topic" | "priority" | "tags" | "tag" | "chatId" | "sound" | "device" | "to" | "username";
type SecretKey = keyof NotificationChannelSecrets;
type FieldDef =
  | { setting: SettingKey; label: string; hint?: string; type?: "text" | "url" | "number" | "list" | "method"; required?: boolean;
      min?: number; max?: number; placeholder?: string }
  | { secret: SecretKey; label: string; hint?: string; required?: boolean };

/** The fields of each kind; only these are shown and sent. */
export const KIND_FIELDS: Record<NotificationChannelKind, FieldDef[]> = {
  webhook: [
    { setting: "url", label: "url", type: "url", required: true, placeholder: "https://example.com/hook" },
    { setting: "method", label: "method", type: "method" },
    { secret: "secret", label: "hmacSecret", hint: "hmacSecretHint" },
  ],
  ntfy: [
    { setting: "url", label: "ntfyServer", type: "url", placeholder: "https://ntfy.sh", hint: "ntfyServerHint" },
    { setting: "topic", label: "topic", required: true },
    { secret: "token", label: "ntfyToken", hint: "ntfyTokenHint" },
    { setting: "username", label: "ntfyUsername" },
    { secret: "password", label: "ntfyPassword" },
    { setting: "priority", label: "priority", type: "number", min: 1, max: 5, hint: "ntfyPriorityHint" },
    { setting: "tags", label: "tags", type: "list", hint: "tagsHint" },
  ],
  gotify: [
    { setting: "url", label: "gotifyServer", type: "url", required: true, placeholder: "https://gotify.example.com" },
    { secret: "token", label: "gotifyToken", required: true, hint: "gotifyTokenHint" },
    { setting: "priority", label: "priority", type: "number", min: 0, max: 10 },
  ],
  discord: [
    { secret: "webhookUrl", label: "discordWebhook", required: true, hint: "discordWebhookHint" },
    { setting: "username", label: "discordName", placeholder: "Locarr" },
  ],
  telegram: [
    { secret: "botToken", label: "botToken", required: true, hint: "botTokenHint" },
    { setting: "chatId", label: "chatId", required: true, hint: "chatIdHint" },
  ],
  pushover: [
    { secret: "token", label: "pushoverToken", required: true },
    { secret: "userKey", label: "userKey", required: true },
    { setting: "priority", label: "priority", type: "number", min: -2, max: 2 },
    { setting: "sound", label: "sound" },
    { setting: "device", label: "device" },
  ],
  email: [{ setting: "to", label: "to", type: "list", required: true, hint: "toHint" }],
  apprise: [
    { setting: "url", label: "appriseUrl", type: "url", required: true, placeholder: "http://apprise:8000/notify/apprise" },
    { setting: "tag", label: "appriseTag" },
  ],
};

type Form = {
  name: string;
  kind: NotificationChannelKind;
  enabled: boolean;
  events: NotificationEventType[];
  settings: NotificationChannelSettings;
  /** undefined = keep what is stored; "" = remove. */
  secrets: Partial<Record<SecretKey, string>>;
};

function initialForm(ch: NotificationChannel | undefined, personal: boolean, lang: string): Form {
  if (ch) return { name: ch.name, kind: ch.kind, enabled: ch.enabled, events: ch.events, settings: { ...ch.settings }, secrets: {} };
  const events = EVENT_GROUPS.filter((g) => (!personal || g.personal) && !g.adult).flatMap((g) => g.events)
    .filter((e) => (personal ? e !== "request.created" && e !== "issue.created" : true));
  return { name: "", kind: personal ? "ntfy" : "webhook", enabled: true, events, settings: { language: lang === "hr" ? "hr" : "en" }, secrets: {} };
}

/** Builds the request body: only the fields of the chosen kind, and only the secrets that were typed or removed. */
export function toInput(f: Form, personal: boolean): NotificationChannelInput {
  const settings: NotificationChannelSettings = {};
  const secrets: NotificationChannelSecrets = {};
  for (const def of KIND_FIELDS[f.kind]) {
    if ("setting" in def) {
      const v = f.settings[def.setting];
      if (v === undefined || v === "" || (Array.isArray(v) && v.length === 0)) continue;
      (settings as Record<string, unknown>)[def.setting] = v;
    } else {
      const v = f.secrets[def.secret];
      if (v !== undefined) secrets[def.secret] = v;
    }
  }
  if (f.settings.language) settings.language = f.settings.language;
  const allowed = new Set(EVENT_GROUPS.filter((g) => !personal || g.personal).flatMap((g) => g.events));
  return { name: f.name.trim(), kind: f.kind, enabled: f.enabled, personal, events: f.events.filter((e) => allowed.has(e)),
    settings, ...(Object.keys(secrets).length ? { secrets } : {}) };
}

/** The list of channels with add, edit, test and delete. */
export function ChannelList({ personal }: { personal: boolean }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const key = queryKeys.channels(personal);
  const q = useQuery({
    queryKey: key,
    queryFn: async () => unwrap(await api.GET("/notificationchannels", { params: { query: personal ? { personal: true } : {} } })),
  });
  const [editing, setEditing] = useState<NotificationChannel | "new" | null>(null);
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/notificationchannels/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
  return (
    <div className="flex flex-col gap-3" data-testid={personal ? "personal-channels" : "shared-channels"}>
      <div className="flex justify-end">
        <Button variant="primary" onClick={() => setEditing("new")} data-testid="add-channel">
          <Plus className="size-4" />
          {t("channels.add")}
        </Button>
      </div>
      {q.isPending ? <Spinner /> : null}
      <ErrorAlert error={q.error ?? remove.error} />
      {q.data && q.data.length === 0 ? <EmptyState>{personal ? t("channels.emptyPersonal") : t("channels.empty")}</EmptyState> : null}
      <ul className="flex flex-col gap-2">
        {q.data?.map((c) => (
          <li key={c.id} className="flex flex-wrap items-center justify-between gap-3 rounded-ui border border-line bg-surface px-3 py-2.5"
            data-testid={`channel-${c.id}`}>
            <div className="min-w-0">
              <p className="flex flex-wrap items-center gap-2 text-[13px] text-fg">
                <span className="font-medium">{c.name}</span>
                <Badge tone="info">{t(`channels.kinds.${c.kind}`)}</Badge>
                {!c.enabled ? <Badge>{t("channels.off")}</Badge> : null}
                {c.failures > 0 ? <Badge tone="danger">{t("channels.failing", { count: c.failures })}</Badge> : null}
              </p>
              <p className="text-[12px] text-faint">
                {t("channels.eventCount", { count: c.events.length })}
                {" · "}
                {c.lastSentAt ? t("channels.lastSent", { when: formatRelative(c.lastSentAt, i18n.language) }) : t("channels.neverSent")}
              </p>
              {c.failures > 0 && c.lastError ? (
                <p className="mt-0.5 text-[12px] text-danger" data-testid="channel-error">
                  {c.lastErrorAt ? `${formatRelative(c.lastErrorAt, i18n.language)}: ` : ""}{c.lastError}
                </p>
              ) : null}
            </div>
            <div className="flex items-center gap-1">
              <Button variant="ghost" onClick={() => setEditing(c)} data-testid={`edit-channel-${c.id}`}>{t("channels.edit")}</Button>
              <ConfirmButton label={t("common.delete")} confirmLabel={t("channels.deleteConfirm", { name: c.name })}
                busy={remove.isPending} onConfirm={() => remove.mutate(c.id)} testId={`delete-channel-${c.id}`} />
            </div>
          </li>
        ))}
      </ul>
      {editing ? (
        <ChannelEditor channel={editing === "new" ? undefined : editing} personal={personal} onClose={() => setEditing(null)} />
      ) : null}
    </div>
  );
}

/** Add or edit one channel; shows only the chosen kind's fields. */
export function ChannelEditor({ channel, personal, onClose }: { channel?: NotificationChannel; personal: boolean; onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const [f, setF] = useState<Form>(() => initialForm(channel, personal, i18n.language));
  const [result, setResult] = useState<NotificationTestResult | null>(null);
  const kinds = personal ? PERSONAL_KINDS : ALL_KINDS;
  const set = (patch: Partial<Form>) => {
    setF({ ...f, ...patch });
    setResult(null);
  };
  const setSetting = (k: keyof NotificationChannelSettings, v: unknown) => set({ settings: { ...f.settings, [k]: v } });
  const save = useMutation({
    mutationFn: async () => {
      const body = toInput(f, personal);
      return channel
        ? unwrap(await api.PUT("/notificationchannels/{id}", { params: { path: { id: channel.id } }, body }))
        : unwrap(await api.POST("/notificationchannels", { body }));
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: queryKeys.channels(personal) });
      onClose();
    },
  });
  const test = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/notificationchannels/test", { body: { ...(channel ? { id: channel.id } : {}), channel: toInput(f, personal) } })),
    onSuccess: setResult,
  });
  const groups = EVENT_GROUPS.filter((g) => (!personal || g.personal) && (!g.adult || canSeeAdult(me)));
  const toggleEvent = (e: NotificationEventType, on: boolean) =>
    set({ events: on ? [...f.events.filter((x) => x !== e), e] : f.events.filter((x) => x !== e) });

  return (
    <Modal title={channel ? t("channels.editTitle", { name: channel.name }) : t("channels.addTitle")} onClose={onClose} wide
      testId="channel-editor"
      footer={
        <>
          <Button onClick={() => test.mutate()} busy={test.isPending} data-testid="test-channel">{t("channels.test")}</Button>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending} disabled={!f.name.trim() || f.events.length === 0}
            data-testid="save-channel">
            {t("common.save")}
          </Button>
        </>
      }>
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <TextInput label={t("channels.name")} value={f.name} onChange={(e) => set({ name: e.target.value })} data-testid="channel-name" />
          <SelectInput label={t("channels.kind")} value={f.kind} disabled={!!channel} data-testid="channel-kind"
            onChange={(e) => set({ kind: e.target.value as NotificationChannelKind, secrets: {} })}>
            {kinds.map((k) => <option key={k} value={k}>{t(`channels.kinds.${k}`)}</option>)}
          </SelectInput>
        </div>
        <p className="text-[12px] text-faint">{t(`channels.kindHints.${f.kind}`)}</p>
        <div className="grid gap-3 sm:grid-cols-2" data-testid="channel-fields">
          {KIND_FIELDS[f.kind].map((def) =>
            "setting" in def ? (
              <SettingField key={def.setting} def={def} value={f.settings[def.setting]} onChange={(v) => setSetting(def.setting, v)} />
            ) : (
              <SecretField key={def.secret} label={t(`channels.fields.${def.label}`)} hint={def.hint ? t(`channels.fields.${def.hint}`) : undefined}
                name={def.secret} stored={!!channel?.secretsSet.includes(def.secret)} value={f.secrets[def.secret]}
                onChange={(v) => set({ secrets: { ...f.secrets, [def.secret]: v } })} />
            ),
          )}
          <SelectInput label={t("channels.language")} value={f.settings.language ?? "en"}
            onChange={(e) => setSetting("language", e.target.value)}>
            <option value="en">English</option>
            <option value="hr">Hrvatski</option>
          </SelectInput>
        </div>
        <fieldset className="flex flex-col gap-3">
          <legend className="mb-1 text-[12px] font-medium text-muted">{personal ? t("channels.eventsPersonal") : t("channels.events")}</legend>
          <div className="grid gap-3 sm:grid-cols-2">
            {groups.map((g) => (
              <div key={g.key} className="flex flex-col gap-1.5 rounded-ui border border-line p-2.5">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">{t(`channels.groups.${g.key}`)}</p>
                {g.events.map((e) => (
                  <Checkbox key={e} label={t(`channels.eventNames.${e.replace(".", "_")}`)} checked={f.events.includes(e)}
                    data-testid={`event-${e}`} onChange={(ev) => toggleEvent(e, ev.target.checked)} />
                ))}
              </div>
            ))}
          </div>
        </fieldset>
        <Checkbox label={t("channels.enabled")} checked={f.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
        <ErrorAlert error={save.error ?? test.error} />
        {result ? (
          <Alert tone={result.ok ? "success" : "danger"}>
            <span data-testid="channel-test-result">{result.ok ? t("channels.testOk") : t("channels.testFailed", { message: result.message ?? "" })}</span>
          </Alert>
        ) : null}
      </div>
    </Modal>
  );
}

function SettingField({ def, value, onChange }:
  { def: Extract<FieldDef, { setting: SettingKey }>; value: unknown; onChange: (v: unknown) => void }) {
  const { t } = useTranslation();
  const label = t(`channels.fields.${def.label}`) + (def.required ? " *" : "");
  const hint = def.hint ? t(`channels.fields.${def.hint}`) : undefined;
  const testId = `field-${def.setting}`;
  if (def.type === "method") {
    return (
      <SelectInput label={label} value={(value as string) || "POST"} onChange={(e) => onChange(e.target.value)} data-testid={testId}>
        <option value="POST">POST</option>
        <option value="PUT">PUT</option>
      </SelectInput>
    );
  }
  if (def.type === "list") {
    const text = Array.isArray(value) ? value.join(", ") : "";
    return (
      <ListInput label={label} hint={hint} initial={text} testId={testId} onChange={onChange} />
    );
  }
  if (def.type === "number") {
    return (
      <TextInput label={label} hint={hint} type="number" min={def.min} max={def.max} data-testid={testId}
        value={typeof value === "number" ? value : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} />
    );
  }
  return (
    <TextInput label={label} hint={hint} type={def.type === "url" ? "url" : "text"} placeholder={def.placeholder} data-testid={testId}
      value={(value as string) ?? ""} onChange={(e) => onChange(e.target.value)} />
  );
}

/** A comma- or line-separated list kept as text while typing. */
function ListInput({ label, hint, initial, testId, onChange }:
  { label: string; hint?: string; initial: string; testId: string; onChange: (v: string[]) => void }) {
  const [text, setText] = useState(initial);
  return (
    <TextInput label={label} hint={hint} value={text} data-testid={testId}
      onChange={(e) => {
        setText(e.target.value);
        onChange(e.target.value.split(/[,\n]/).map((s) => s.trim()).filter(Boolean));
      }} />
  );
}

/** A write-only value: shows whether one is stored; typing replaces it, "Remove" clears it. */
export function SecretField({ label, hint, name, stored, value, onChange }:
  { label: string; hint?: string; name: string; stored: boolean; value: string | undefined; onChange: (v: string | undefined) => void }) {
  const { t } = useTranslation();
  const removing = value === "" && stored;
  return (
    <div className="flex flex-col gap-1">
      <TextInput label={label} hint={hint} type="password" autoComplete="new-password" data-testid={`secret-${name}`}
        placeholder={stored && !removing ? t("channels.secretSet") : ""} value={value ?? ""}
        onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)} />
      {stored ? (
        <div className="flex items-center gap-2 text-[12px]">
          {removing ? <span className="text-warning">{t("channels.secretWillRemove")}</span> : <Badge tone="success">{t("channels.set")}</Badge>}
          <button type="button" className="text-accent hover:underline" data-testid={`remove-secret-${name}`}
            onClick={() => onChange(removing ? undefined : "")}>
            {removing ? t("channels.keepSecret") : t("channels.removeSecret")}
          </button>
        </div>
      ) : null}
    </div>
  );
}
