import type { ReactNode } from "react";
import { LanguagePicker } from "./LanguagePicker";
import { setLanguage } from "@/i18n";
import { Logo } from "./Logo";

/** The centred card used by the setup and sign-in pages. */
export function AuthLayout({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <main className="flex min-h-full flex-col items-center justify-center px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center justify-between">
          <Logo />
          <LanguagePicker onChange={setLanguage} />
        </div>
        <div className="rounded-ui border border-line bg-surface p-5 shadow-[0_8px_40px_-12px_rgba(0,0,0,0.6)]">
          <h1 className="text-[16px] font-semibold text-fg">{title}</h1>
          {subtitle ? <p className="mt-1 text-[13px] text-muted">{subtitle}</p> : null}
          <div className="mt-4">{children}</div>
        </div>
      </div>
    </main>
  );
}
