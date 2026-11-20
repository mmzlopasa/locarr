import { useTranslation } from "react-i18next";
import { SUPPORTED_LANGUAGES, currentLanguage, isLanguage } from "@/i18n";
import type { Language } from "@/api/client";

/** A compact language switch; `onChange` defaults to changing only this browser. */
export function LanguagePicker({ onChange, className = "" }: { onChange: (l: Language) => void; className?: string }) {
  const { t, i18n } = useTranslation();
  const value = isLanguage(i18n.language) ? i18n.language : currentLanguage();
  return (
    <label className={`inline-flex items-center gap-1.5 text-[12px] text-muted ${className}`}>
      <span className="sr-only">{t("common.language")}</span>
      <select
        aria-label={t("common.language")}
        value={value}
        onChange={(e) => isLanguage(e.target.value) && onChange(e.target.value)}
        className="h-7 rounded-ui border border-line bg-surface-2 px-1.5 text-[12px] text-fg"
      >
        {SUPPORTED_LANGUAGES.map((l) => (
          <option key={l} value={l}>
            {t(`languages.${l}`)}
          </option>
        ))}
      </select>
    </label>
  );
}
