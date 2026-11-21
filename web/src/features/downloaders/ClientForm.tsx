import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  api,
  queryKeys,
  unwrap,
  type DownloadClient,
  type DownloadClientField,
  type DownloadClientImplementation,
  type DownloadClientInput,
  type DownloadClientTestResult,
  type QueuePriority,
} from "@/api/client";
import { formatBytes } from "@/components/format";
import { Alert, Button, Checkbox, ConfirmButton, ErrorAlert, Panel, SelectInput, TextInput } from "@/components/ui";

const PRIORITIES: QueuePriority[] = ["normal", "top", "last"];

type FormState = {
  name: string;
  enabled: boolean;
  priority: string;
  host: string;
  port: string;
  useTls: boolean;
  allowSelfSigned: boolean;
  urlBase: string;
  username: string;
  password: string;
  apiKey: string;
  directory: string;
  movies: string;
  series: string;
  music: string;
  recentPriority: QueuePriority;
  olderPriority: QueuePriority;
  initialState: "start" | "pause";
  removeCompleted: boolean;
  sequential: boolean;
  firstLastPiece: boolean;
  tags: string;
};

function initialState(impl: DownloadClientImplementation, c?: DownloadClient): FormState {
  return {
    name: c?.name ?? impl.displayName,
    enabled: c?.enabled ?? true,
    priority: String(c?.priority ?? 1),
    host: c?.host ?? "",
    port: String(c?.port ?? impl.defaultPort),
    useTls: c?.useTls ?? false,
    allowSelfSigned: c?.allowSelfSigned ?? false,
    urlBase: c?.urlBase ?? impl.defaultUrlBase,
    username: c?.username ?? impl.defaultUsername,
    password: "",
    apiKey: "",
    directory: c?.directory ?? "",
    movies: c?.categories.movies ?? "movies",
    series: c?.categories.series ?? "tv",
    music: c?.categories.music ?? "music",
    recentPriority: c?.recentPriority ?? "normal",
    olderPriority: c?.olderPriority ?? "normal",
    initialState: c?.initialState ?? "start",
    removeCompleted: c?.removeCompleted ?? false,
    sequential: c?.sequential ?? false,
    firstLastPiece: c?.firstLastPiece ?? false,
    tags: (c?.tags ?? []).join(", "),
  };
}

/** Builds the request body; secrets are sent only when typed (empty = keep the stored one). */
export function toInput(impl: DownloadClientImplementation, f: FormState): DownloadClientInput {
  const has = (x: DownloadClientField) => impl.fields.includes(x);
  const body: DownloadClientInput = {
    name: f.name.trim(),
    implementation: impl.name,
    enabled: f.enabled,
    priority: Number(f.priority) || 1,
    host: f.host.trim(),
    port: Number(f.port) || impl.defaultPort,
    initialState: f.initialState,
    removeCompleted: f.removeCompleted,
    categories: { movies: f.movies.trim(), series: f.series.trim(), music: f.music.trim() },
    tags: f.tags.split(",").map((s) => s.trim()).filter(Boolean),
    useTls: has("useTls") && f.useTls,
    allowSelfSigned: has("allowSelfSigned") && f.useTls && f.allowSelfSigned,
    sequential: impl.capabilities.sequential && f.sequential,
    firstLastPiece: impl.capabilities.firstLastPiece && f.firstLastPiece,
  };
  if (has("urlBase")) body.urlBase = f.urlBase.trim();
  if (has("username")) body.username = f.username.trim();
  if (has("password") && f.password) body.password = f.password;
  if (has("apiKey") && f.apiKey) body.apiKey = f.apiKey;
  if (has("directory")) body.directory = f.directory.trim();
  if (impl.capabilities.priority) {
    body.recentPriority = f.recentPriority;
    body.olderPriority = f.olderPriority;
  }
  return body;
}

/** Shows the outcome of a connection test in the user's language. */
export function TestOutcome({ result }: { result: DownloadClientTestResult }) {
  const { t, i18n } = useTranslation();
  if (result.ok && !result.missingCategories.length) {
    return (
      <Alert tone="success">
        <span data-testid="test-ok">
          {t("downloaders.test.ok", { version: result.version ?? "?" })}
          {result.freeSpaceBytes !== undefined
            ? " " + t("downloaders.test.freeSpace", { free: formatBytes(result.freeSpaceBytes, i18n.language) })
            : ""}
        </span>
      </Alert>
    );
  }
  if (result.ok) {
    return (
      <Alert tone="warning">
        <span data-testid="test-missing">
          {t("downloaders.test.missingCategories", { categories: result.missingCategories.join(", ") })}{" "}
          {result.canCreateCategories ? t("downloaders.test.createdOnGrab") : t("downloaders.test.createByHand")}
        </span>
      </Alert>
    );
  }
  return (
    <Alert>
      <span data-testid="test-failed">{t(`downloaders.testErrors.${result.errorCode ?? "client_error"}`, { defaultValue: t("downloaders.testErrors.client_error") })}</span>
      {result.message ? <span className="mt-1 block break-words font-mono text-[11px] opacity-80">{result.message}</span> : null}
    </Alert>
  );
}

export function ClientForm({ impl, client, onDone }:
  { impl: DownloadClientImplementation; client?: DownloadClient; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [f, setF] = useState<FormState>(() => initialState(impl, client));
  const [result, setResult] = useState<DownloadClientTestResult | null>(null);
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((cur) => ({ ...cur, [k]: v }));
  const has = (x: DownloadClientField) => impl.fields.includes(x);
  const required = (x: DownloadClientField) => impl.required.includes(x);
  const caps = impl.capabilities;

  const test = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/downloadclients/test", { body: { client: toInput(impl, f), ...(client ? { id: client.id } : {}) } })),
    onSuccess: setResult,
  });
  const save = useMutation({
    mutationFn: async () => {
      const body = toInput(impl, f);
      return client
        ? unwrap(await api.PUT("/downloadclients/{id}", { params: { path: { id: client.id } }, body }))
        : unwrap(await api.POST("/downloadclients", { body }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.downloadClients });
      onDone();
    },
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/downloadclients/{id}", { params: { path: { id: client!.id } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.downloadClients });
      onDone();
    },
  });
  const createCats = useMutation({
    mutationFn: async () => unwrap(await api.POST("/downloadclients/{id}/categories", { params: { path: { id: client!.id } } })),
    onSuccess: () => test.mutate(),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const secretHint = (set: boolean | undefined) => (client && set ? t("downloaders.form.secretKeep") : undefined);

  return (
    <Panel title={client ? t("downloaders.form.editTitle", { name: client.name }) : t("downloaders.form.addTitle", { name: impl.displayName })}
      description={t(`downloaders.protocols.${impl.protocol}`)}>
      <form onSubmit={submit} className="flex flex-col gap-4" data-testid="client-form">
        <div className="grid gap-3 sm:grid-cols-2">
          <TextInput label={t("downloaders.form.name")} value={f.name} onChange={(e) => set("name", e.target.value)} required maxLength={100} />
          <TextInput label={t("downloaders.form.priority")} type="number" min={1} max={50} value={f.priority}
            onChange={(e) => set("priority", e.target.value)} hint={t("downloaders.form.priorityHint")} />
          {has("host") ? (
            <TextInput label={t("downloaders.fields.host")} value={f.host} onChange={(e) => set("host", e.target.value)} required
              placeholder="192.168.1.10" className="font-mono" hint={t("downloaders.fields.hostHint")} />
          ) : null}
          {has("port") ? (
            <TextInput label={t("downloaders.fields.port")} type="number" min={1} max={65535} value={f.port}
              onChange={(e) => set("port", e.target.value)} />
          ) : null}
          {has("urlBase") ? (
            <TextInput label={t("downloaders.fields.urlBase")} value={f.urlBase} onChange={(e) => set("urlBase", e.target.value)}
              className="font-mono" hint={t("downloaders.fields.urlBaseHint")} />
          ) : null}
          {has("username") ? (
            <TextInput label={t("downloaders.fields.username")} value={f.username} onChange={(e) => set("username", e.target.value)}
              autoComplete="off" />
          ) : null}
          {has("password") ? (
            <TextInput label={t("downloaders.fields.password")} type="password" value={f.password} autoComplete="new-password"
              onChange={(e) => set("password", e.target.value)} placeholder={secretHint(client?.password.configured)}
              required={required("password") && !client?.password.configured} />
          ) : null}
          {has("apiKey") ? (
            <TextInput label={t("downloaders.fields.apiKey")} type="password" value={f.apiKey} autoComplete="off"
              onChange={(e) => set("apiKey", e.target.value)} placeholder={secretHint(client?.apiKey.configured)}
              required={required("apiKey") && !client?.apiKey.configured} />
          ) : null}
          {has("directory") ? (
            <TextInput label={t("downloaders.fields.directory")} value={f.directory} onChange={(e) => set("directory", e.target.value)}
              className="font-mono" placeholder="/data/downloads/torrents" hint={t("downloaders.fields.directoryHint")}
              required={required("directory")} />
          ) : null}
        </div>
        <div className="flex flex-wrap gap-x-6 gap-y-2">
          <Checkbox label={t("downloaders.form.enabled")} checked={f.enabled} onChange={(e) => set("enabled", e.target.checked)} />
          {has("useTls") ? (
            <Checkbox label={t("downloaders.fields.useTls")} checked={f.useTls} onChange={(e) => set("useTls", e.target.checked)} />
          ) : null}
          {has("allowSelfSigned") && f.useTls ? (
            <Checkbox label={t("downloaders.fields.allowSelfSigned")} description={t("downloaders.fields.allowSelfSignedHint")}
              checked={f.allowSelfSigned} onChange={(e) => set("allowSelfSigned", e.target.checked)} />
          ) : null}
        </div>

        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 text-[12px] font-semibold uppercase tracking-wide text-muted">{t("downloaders.form.categories")}</legend>
          {caps.categories || caps.directoryCategories ? (
            <>
              <p className="text-[12px] text-faint">
                {caps.directoryCategories ? t("downloaders.form.directoryCategories") : t("downloaders.form.categoriesHint")}
              </p>
              <div className="grid gap-3 sm:grid-cols-3">
                <TextInput label={t("mediaKinds.movies")} value={f.movies} onChange={(e) => set("movies", e.target.value)} />
                <TextInput label={t("mediaKinds.series")} value={f.series} onChange={(e) => set("series", e.target.value)} />
                <TextInput label={t("mediaKinds.music")} value={f.music} onChange={(e) => set("music", e.target.value)} />
              </div>
            </>
          ) : (
            <p className="text-[12px] text-faint">{t("downloaders.form.noCategories")}</p>
          )}
        </fieldset>

        <fieldset className="flex flex-col gap-3">
          <legend className="mb-1 text-[12px] font-semibold uppercase tracking-wide text-muted">{t("downloaders.form.behaviour")}</legend>
          <div className="grid gap-3 sm:grid-cols-3">
            {caps.priority ? (
              <>
                <SelectInput label={t("downloaders.form.recentPriority")} value={f.recentPriority}
                  onChange={(e) => set("recentPriority", e.target.value as QueuePriority)}>
                  {PRIORITIES.map((p) => <option key={p} value={p}>{t(`downloaders.priorities.${p}`)}</option>)}
                </SelectInput>
                <SelectInput label={t("downloaders.form.olderPriority")} value={f.olderPriority}
                  onChange={(e) => set("olderPriority", e.target.value as QueuePriority)}>
                  {PRIORITIES.map((p) => <option key={p} value={p}>{t(`downloaders.priorities.${p}`)}</option>)}
                </SelectInput>
              </>
            ) : null}
            <SelectInput label={t("downloaders.form.initialState")} value={f.initialState}
              onChange={(e) => set("initialState", e.target.value as "start" | "pause")}>
              <option value="start">{t("downloaders.form.initialStart")}</option>
              <option value="pause">{t("downloaders.form.initialPause")}</option>
            </SelectInput>
          </div>
          <Checkbox label={t("downloaders.form.removeCompleted")} description={t("downloaders.form.removeCompletedHint")}
            checked={f.removeCompleted} onChange={(e) => set("removeCompleted", e.target.checked)} />
          {caps.sequential ? (
            <Checkbox label={t("downloaders.form.sequential")} checked={f.sequential} onChange={(e) => set("sequential", e.target.checked)} />
          ) : null}
          {caps.firstLastPiece ? (
            <Checkbox label={t("downloaders.form.firstLastPiece")} checked={f.firstLastPiece}
              onChange={(e) => set("firstLastPiece", e.target.checked)} />
          ) : null}
          <TextInput label={t("downloaders.form.tags")} value={f.tags} onChange={(e) => set("tags", e.target.value)}
            hint={t("downloaders.form.tagsHint")} />
        </fieldset>

        {result ? <TestOutcome result={result} /> : null}
        {client && result?.ok && result.missingCategories.length > 0 && result.canCreateCategories ? (
          <div>
            <Button onClick={() => createCats.mutate()} busy={createCats.isPending}>{t("downloaders.test.createCategories")}</Button>
          </div>
        ) : null}
        <ErrorAlert error={test.error ?? save.error ?? remove.error ?? createCats.error} />
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex gap-2">
            <Button variant="primary" type="submit" busy={save.isPending}>{t("common.save")}</Button>
            <Button onClick={() => test.mutate()} busy={test.isPending}>{t("downloaders.test.button")}</Button>
            <Button variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
          </div>
          {client ? (
            <ConfirmButton label={t("downloaders.form.delete")} confirmLabel={t("downloaders.form.deleteConfirm")}
              busy={remove.isPending} onConfirm={() => remove.mutate()} testId="delete-client" />
          ) : null}
        </div>
      </form>
    </Panel>
  );
}
