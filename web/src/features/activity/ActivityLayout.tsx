import { Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Tabs } from "@/components/Tabs";
import { PageHeader } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";

/** Activity: the queue, why items did not land, history, the blocklist and statistics (library managers and administrators). */
export function ActivityLayout() {
  const { t } = useTranslation();
  const me = useCurrentUser();
  const allowed = me.role === "admin" || me.permissions.manageLibrary;
  return (
    <div>
      <PageHeader title={t("nav.activity")} />
      {allowed ? (
        <>
          <Tabs
            label={t("nav.activity")}
            tabs={[
              { to: "/activity/queue", label: t("activity.tabs.queue") },
              { to: "/activity/board", label: t("activity.tabs.board") },
              { to: "/activity/history", label: t("activity.tabs.history") },
              { to: "/activity/blocklist", label: t("activity.tabs.blocklist") },
              { to: "/activity/stats", label: t("activity.tabs.stats") },
            ]}
          />
          <Outlet />
        </>
      ) : (
        <p className="rounded-ui border border-line bg-surface p-6 text-[13px] text-muted" data-testid="activity-forbidden">
          {t("activity.forbidden")}
        </p>
      )}
    </div>
  );
}
