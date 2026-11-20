import { Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Tabs } from "@/components/Tabs";

export function SettingsLayout() {
  const { t } = useTranslation();
  return (
    <div>
      <Tabs
        label={t("nav.settings")}
        tabs={[
          { to: "/settings", label: t("settings.tabs.general") },
          { to: "/settings/media", label: t("settings.tabs.media") },
          { to: "/settings/users", label: t("settings.tabs.users") },
        ]}
      />
      <Outlet />
    </div>
  );
}
