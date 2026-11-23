import { useEffect, useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";

/**
 * A dialog over the page: centred on wide screens, a bottom sheet on phones.
 * Escape and the backdrop close it; focus moves into it when it opens.
 */
export function Modal({ title, onClose, children, footer, testId, wide }:
  { title: string; onClose: () => void; children: ReactNode; footer?: ReactNode; testId?: string; wide?: boolean }) {
  const { t } = useTranslation();
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    const first = box.current?.querySelector<HTMLElement>("input, select, textarea, button:not([data-close])");
    first?.focus();
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center sm:items-center sm:p-6" role="dialog" aria-modal="true" aria-label={title} data-testid={testId}>
      <button type="button" data-close className="absolute inset-0 bg-black/60 backdrop-blur-[2px]" aria-label={t("common.close")} onClick={onClose} />
      <div ref={box} className={`relative flex max-h-[90vh] w-full flex-col rounded-t-xl border border-line bg-surface shadow-2xl sm:rounded-xl ${wide ? "sm:max-w-2xl" : "sm:max-w-lg"}`}>
        <div className="flex h-12 shrink-0 items-center justify-between border-b border-line px-4">
          <h2 className="truncate text-[14px] font-semibold text-fg">{title}</h2>
          <button type="button" data-close onClick={onClose} className="text-muted hover:text-fg" aria-label={t("common.close")}>
            <X className="size-4" />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-4">{children}</div>
        {footer ? <div className="flex flex-wrap justify-end gap-2 border-t border-line px-4 py-3">{footer}</div> : null}
      </div>
    </div>
  );
}
