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
          { to: "/settings/profiles", label: t("settings.tabs.profiles") },
          { to: "/settings/quality", label: t("settings.tabs.quality") },
          { to: "/settings/customformats", label: t("settings.tabs.customFormats") },
          { to: "/settings/rules", label: t("settings.tabs.rules") },
          { to: "/settings/tester", label: t("settings.tabs.tester") },
          { to: "/settings/users", label: t("settings.tabs.users") },
        ]}
      />
      <Outlet />
    </div>
  );
}
