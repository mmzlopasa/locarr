import { useEffect, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";

/** A side panel over the page (Escape or the backdrop closes it). */
export function Drawer({ title, onClose, children, testId }: { title: string; onClose: () => void; children: ReactNode; testId?: string }) {
  const { t } = useTranslation();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-40 flex justify-end" role="dialog" aria-modal="true" aria-label={title} data-testid={testId}>
      <button type="button" className="absolute inset-0 bg-black/50" aria-label={t("common.close")} onClick={onClose} />
      <div className="relative flex h-full w-full max-w-5xl flex-col border-l border-line bg-bg shadow-2xl">
        <div className="flex h-12 shrink-0 items-center justify-between border-b border-line px-4">
          <h2 className="truncate text-[14px] font-semibold text-fg">{title}</h2>
          <button type="button" onClick={onClose} className="text-muted hover:text-fg" aria-label={t("common.close")}>
            <X className="size-4" />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-4">{children}</div>
      </div>
    </div>
  );
}
