import { Outlet } from "@tanstack/react-router";
import { useCurrentUser } from "@/features/auth/hooks";
import { SettingsNav, SettingsNavBar } from "./SettingsNav";

/**
 * Settings: grouped sections in a sticky column beside the page on wide screens,
 * a "Settings › Section" bar that opens them as a sheet on narrower ones.
 */
export function SettingsLayout() {
  const me = useCurrentUser();
  return (
    <div className="lg:flex lg:items-start lg:gap-6 xl:gap-8">
      <aside className="sticky top-5 hidden max-h-[calc(100vh-2.5rem)] w-52 shrink-0 overflow-y-auto overscroll-contain pr-1 lg:block xl:w-56">
        <SettingsNav me={me} idPrefix="rail" />
      </aside>
      <div className="sticky top-0 z-30 -mx-4 -mt-5 mb-4 border-b border-line bg-bg/90 px-4 py-2 backdrop-blur md:-mx-6 md:px-6 lg:hidden">
        <SettingsNavBar me={me} />
      </div>
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}
