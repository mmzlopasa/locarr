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
          { to: "/system/tasks", label: t("system.tabs.tasks") },
        ]}
      />
      <Outlet />
    </div>
  );
}
