import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { formatRelative } from "@/components/format";
import { Alert, Badge, Button, ErrorAlert, Panel, TextInput } from "@/components/ui";

type State = "connected" | "missing" | "unauthorized" | "error";

const stateTone = { connected: "success", missing: "warning", unauthorized: "danger", error: "danger" } as const;

/** Settings → Indexers → "Browser for protected sites": the companion browser's state, test and connection. */
export function BrowserPanel() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: queryKeys.indexerBrowser,
    queryFn: async () => unwrap(await api.GET("/indexers/browser")),
    refetchInterval: 30_000,
  });
  const settings = useQuery({ queryKey: queryKeys.indexerSettings, queryFn: async () => unwrap(await api.GET("/indexers/settings")) });
  const [url, setUrl] = useState<string | undefined>(undefined);
  const [token, setToken] = useState("");
  const [testUrl, setTestUrl] = useState("");
  const save = useMutation({
    mutationFn: async (body: { browserUrl?: string; browserToken?: string }) =>
      unwrap(await api.PATCH("/indexers/settings", { body })),
    onSuccess: (s) => {
      qc.setQueryData(queryKeys.indexerSettings, s);
      setUrl(undefined);
      setToken("");
      void qc.invalidateQueries({ queryKey: queryKeys.indexerBrowser });
    },
  });
  const test = useMutation({
    mutationFn: async (body: { url?: string; variant?: "js" | "human" }) =>
      unwrap(await api.POST("/indexers/browser/test", { body })),
    onSettled: () => void qc.invalidateQueries({ queryKey: queryKeys.indexerBrowser }),
  });

  const st = status.data;
  const s = settings.data;
  const state = (st?.state ?? "missing") as State;
  return (
    <Panel title={t("indexers.browser.title")} description={t("indexers.browser.subtitle")}>
      <div className="flex flex-col gap-3" data-testid="browser-panel">
        <ErrorAlert error={status.error} />
        {st ? (
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2 text-[13px]">
              <Badge tone={stateTone[state]}>{t(`indexers.browser.state.${state}`)}</Badge>
              <span className="font-mono text-[12px] text-fg-muted">{st.url}</span>
            </div>
            {state === "connected" ? (
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-[13px] sm:grid-cols-4" data-testid="browser-facts">
                <Fact label={t("indexers.browser.version")} value={st.version ?? "—"} />
                <Fact label={t("indexers.browser.chromium")} value={st.chromium ?? t("indexers.browser.notStarted")} />
                <Fact label={t("indexers.browser.tabs")} value={t("indexers.browser.tabsValue", { inUse: st.tabsInUse ?? 0, tabs: st.tabs ?? 0 })} />
                <Fact label={t("indexers.browser.sandbox")} value={st.chromium ? t(st.sandbox ? "common.yes" : "common.no") : "—"} />
              </dl>
            ) : null}
            {state === "missing" ? (
              <Alert tone="warning">
                <p>{t("indexers.browser.missing")}</p>
                <code className="mt-1 block select-all rounded bg-surface-2 px-2 py-1 font-mono text-[12px] text-fg">{st.command}</code>
              </Alert>
            ) : null}
            {state === "unauthorized" ? <Alert tone="danger">{t("indexers.browser.unauthorized")}</Alert> : null}
            {state === "error" && st.message ? <Alert tone="danger">{st.message}</Alert> : null}
            {st.lastSolve ? (
              <p className="text-[12px] text-fg-muted" data-testid="browser-last-solve">
                {t("indexers.browser.lastSolve", {
                  host: st.lastSolve.host,
                  outcome: t(`indexers.browser.outcome.${st.lastSolve.outcome}`, { defaultValue: st.lastSolve.outcome }),
                  when: formatRelative(st.lastSolve.at, i18n.language),
                  seconds: (st.lastSolve.elapsedMs / 1000).toFixed(1),
                })}
              </p>
            ) : null}
          </div>
        ) : null}

        <div className="flex flex-col gap-2 border-t border-line pt-3">
          <div className="flex flex-wrap items-end gap-2">
            <Button variant="primary" busy={test.isPending && !test.variables?.url && test.variables?.variant !== "human"}
              disabled={test.isPending} onClick={() => test.mutate({})}>
              {t("indexers.browser.test")}
            </Button>
            <Button busy={test.isPending && test.variables?.variant === "human"} disabled={test.isPending}
              onClick={() => test.mutate({ variant: "human" })}>
              {t("indexers.browser.testHuman")}
            </Button>
          </div>
          <form className="flex flex-wrap items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (testUrl.trim()) test.mutate({ url: testUrl.trim() });
            }}>
            <div className="min-w-[16rem] flex-1">
              <TextInput label={t("indexers.browser.testUrl")} value={testUrl} placeholder="https://"
                hint={t("indexers.browser.testUrlHint")} onChange={(e) => setTestUrl(e.target.value)} />
            </div>
            <Button type="submit" busy={test.isPending && !!test.variables?.url} disabled={test.isPending || !testUrl.trim()}>
              {t("indexers.browser.open")}
            </Button>
          </form>
          {test.isPending ? <p className="text-[12px] text-fg-muted">{t("indexers.browser.testing")}</p> : null}
          {test.data ? (
            <Alert tone={test.data.passed ? "success" : test.data.outcome === "human" ? "warning" : "danger"}>
              <span data-testid="browser-test-result">
                {t(test.data.passed ? "indexers.browser.testPassed" : "indexers.browser.testFailed", {
                  outcome: t(`indexers.browser.outcome.${test.data.outcome}`, { defaultValue: test.data.outcome }),
                  seconds: (test.data.elapsedMs / 1000).toFixed(1),
                  cookies: test.data.cookies,
                })}
              </span>
              {test.data.message ? <span className="block text-[12px] opacity-80">{test.data.message}</span> : null}
            </Alert>
          ) : null}
          <ErrorAlert error={test.error} />
        </div>

        {s ? (
          <form className="flex flex-col gap-2 border-t border-line pt-3"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate({ browserUrl: url, browserToken: token ? token : undefined });
            }}>
            <div className="grid gap-3 sm:grid-cols-2">
              <TextInput label={t("indexers.browser.url")} value={url ?? s.browserUrl} placeholder={s.browserDefaultUrl}
                hint={t("indexers.browser.urlHint")} onChange={(e) => setUrl(e.target.value)} />
              <TextInput label={t("indexers.browser.token")} type="password" autoComplete="off" value={token}
                placeholder={s.browserToken.configured ? t("indexers.browser.tokenSaved")
                  : s.browserTokenFromEnv ? t("indexers.browser.tokenFromEnv") : ""}
                hint={t("indexers.browser.tokenHint")} onChange={(e) => setToken(e.target.value)} />
            </div>
            <ErrorAlert error={save.error} />
            {save.isSuccess ? <Alert tone="success">{t("common.saved")}</Alert> : null}
            <div className="flex flex-wrap gap-2">
              <Button type="submit" variant="primary" busy={save.isPending} disabled={url === undefined && !token}>
                {t("common.save")}
              </Button>
              {s.browserToken.configured ? (
                <Button busy={save.isPending} onClick={() => save.mutate({ browserToken: "" })}>
                  {t("indexers.browser.forgetToken")}
                </Button>
              ) : null}
            </div>
          </form>
        ) : null}
      </div>
    </Panel>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-[11px] uppercase tracking-wide text-fg-muted">{label}</dt>
      <dd className="text-fg">{value}</dd>
    </div>
  );
}
