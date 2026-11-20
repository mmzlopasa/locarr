import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";
import hr from "./locales/hr.json";

/** English is the primary language; Croatian is a full translation of it. */
export const SUPPORTED_LANGUAGES = ["en", "hr"] as const;
export type Language = (typeof SUPPORTED_LANGUAGES)[number];
const STORAGE_KEY = "locarr.lang";

export function isLanguage(v: unknown): v is Language {
  return v === "en" || v === "hr";
}

function initialLanguage(): Language {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (isLanguage(saved)) return saved;
  } catch {
    // Storage can be unavailable (private mode); English is the default.
  }
  return "en";
}

/** Switches the UI language and remembers it in this browser. */
export function setLanguage(lang: Language) {
  void i18n.changeLanguage(lang);
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    // Not remembered when storage is unavailable; the switch still applies.
  }
}

export function currentLanguage(): Language {
  return isLanguage(i18n.language) ? i18n.language : "en";
}

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, hr: { translation: hr } },
  lng: initialLanguage(),
  fallbackLng: "en",
  interpolation: { escapeValue: false },
});

const syncDocumentLang = (lng: string) => {
  if (typeof document !== "undefined") document.documentElement.lang = lng;
};
syncDocumentLang(i18n.language);
i18n.on("languageChanged", syncDocumentLang);

export default i18n;
