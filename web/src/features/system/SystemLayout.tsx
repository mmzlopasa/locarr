import { Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Tabs } from "@/components/Tabs";

export function SystemLayout() {
  const { t } = useTranslation();
  return (
    <div>
      <Tabs
        label={t("nav.system")}
        tabs={[
          { to: "/system/status", label: t("system.tabs.status") },
          { to: "/system/health", label: t("system.tabs.health") },
          { to: "/system/tasks", label: t("system.tabs.tasks") },
          { to: "/system/logs", label: t("system.tabs.logs") },
          { to: "/system/backups", label: t("system.tabs.backups") },
        ]}
      />
      <Outlet />
    </div>
  );
}
