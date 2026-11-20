import "@testing-library/jest-dom/vitest";
import { afterEach, vi } from "vitest";
import i18n from "./i18n";

afterEach(() => {
  vi.unstubAllGlobals();
  void i18n.changeLanguage("en");
  try {
    localStorage.clear();
  } catch {
    // jsdom always has storage; nothing to clear otherwise.
  }
});
