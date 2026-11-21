import { Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Tabs } from "@/components/Tabs";
import { PageHeader } from "@/components/ui";
import { useCurrentUser } from "@/features/auth/hooks";

/** Activity: the download queue and the blocklist (library managers and administrators). */
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
              { to: "/activity/blocklist", label: t("activity.tabs.blocklist") },
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
