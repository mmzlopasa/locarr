import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap } from "@/api/client";
import { Badge, ErrorAlert, PageHeader, Panel, Spinner } from "@/components/ui";

const STORAGE = "locarr.migrate.checklist";

function loadTicks(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem(STORAGE) ?? "{}") as Record<string, boolean>;
  } catch {
    return {};
  }
}

/** What to do after importing so Locarr takes over from the old applications. */
export function ChecklistPage() {
  const { t } = useTranslation();
  const list = useQuery({ queryKey: queryKeys.migrateChecklist, queryFn: async () => unwrap(await api.GET("/migrate/checklist")) });
  const [ticks, setTicks] = useState(loadTicks);
  const tick = (key: string, on: boolean) => {
    const next = { ...ticks, [key]: on };
    setTicks(next);
    try {
      localStorage.setItem(STORAGE, JSON.stringify(next));
    } catch {
      // Private windows may refuse storage; the ticks then last for this visit.
    }
  };
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("migrate.checklist.title")} description={t("migrate.checklist.subtitle")}
        actions={<Link to="/migrate" className="text-[13px] text-info hover:underline">{t("migrate.checklist.back")}</Link>} />
      <Panel>
        {list.isPending ? <Spinner /> : null}
        <ErrorAlert error={list.error} />
        <ol className="flex flex-col gap-3" data-testid="migrate-checklist">
          {list.data?.map((c, i) => {
            const done = c.manual ? !!ticks[c.key] : c.done;
            return (
              <li key={c.key} className="flex items-start gap-3 rounded-ui border border-line px-3 py-2" data-testid={`check-${c.key}`}>
                <span className="mt-0.5 w-5 text-right text-[12px] tabular-nums text-faint">{i + 1}.</span>
                <div className="min-w-0 flex-1">
                  <p className="flex flex-wrap items-center gap-2 text-[13px] font-medium text-fg">
                    {t(`migrate.checklist.items.${c.key}.title`, c.params)}
                    <Badge tone={done ? "success" : c.manual ? "info" : "warning"}>
                      {done ? t("migrate.checklist.done") : c.manual ? t("migrate.checklist.manual") : t("migrate.checklist.todo")}
                    </Badge>
                  </p>
                  <p className="mt-0.5 text-[12px] text-muted">{t(`migrate.checklist.items.${c.key}.body`, c.params)}</p>
                  {c.manual ? (
                    <label className="mt-1 flex items-center gap-2 text-[12px] text-muted">
                      <input type="checkbox" checked={!!ticks[c.key]} onChange={(e) => tick(c.key, e.target.checked)} />
                      {t("migrate.checklist.tick")}
                    </label>
                  ) : null}
                  <CheckLink k={c.key} />
                </div>
              </li>
            );
          })}
        </ol>
      </Panel>
    </div>
  );
}

function CheckLink({ k }: { k: string }) {
  const { t } = useTranslation();
  const to = ({ download_clients: "/downloaders", rss: "/indexers", test_search: "/search", secrets: "/migrate", import_done: "/migrate" } as Record<string, string>)[k];
  if (!to) return null;
  return (
    <Link to={to} className="mt-1 inline-block text-[12px] text-info hover:underline">
      {t(`migrate.checklist.go.${k}`)}
    </Link>
  );
}
