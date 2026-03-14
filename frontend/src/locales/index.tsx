import { createContext, useContext, useState, useCallback, useEffect, type ReactNode } from "react";
import en from "./en";
import zh from "./zh";

type Lang = "en" | "zh";
type Translations = typeof en;

const translations: Record<Lang, Translations> = { en, zh };

const LANG_KEY = "antigateway-lang";

function getInitialLang(): Lang {
  if (typeof window !== "undefined") {
    const stored = localStorage.getItem(LANG_KEY) as Lang | null;
    if (stored === "en" || stored === "zh") return stored;
    // Detect browser language
    const browserLang = navigator.language.toLowerCase();
    if (browserLang.startsWith("zh")) return "zh";
  }
  return "en";
}

interface I18nContextType {
  lang: Lang;
  t: Translations;
  setLang: (lang: Lang) => void;
  toggleLang: () => void;
}

const I18nContext = createContext<I18nContextType | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(getInitialLang);

  useEffect(() => {
    document.documentElement.setAttribute("lang", lang);
    localStorage.setItem(LANG_KEY, lang);
  }, [lang]);

  const setLang = useCallback((newLang: Lang) => {
    setLangState(newLang);
  }, []);

  const toggleLang = useCallback(() => {
    setLangState((prev) => (prev === "en" ? "zh" : "en"));
  }, []);

  const value: I18nContextType = {
    lang,
    t: translations[lang],
    setLang,
    toggleLang,
  };

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextType {
  const context = useContext(I18nContext);
  if (!context) {
    throw new Error("useI18n must be used within I18nProvider");
  }
  return context;
}

// Shorthand hook for translations only
export function useT(): Translations {
  return useI18n().t;
}
