import { useState, useCallback, useEffect } from "react";
import { Routes, Route, Navigate, useNavigate, useLocation } from "react-router-dom";
import { ConfigProvider, App as AntApp, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import enUS from "antd/locale/en_US";
import AdminLayout from "./layouts/AdminLayout";
import LoginPage from "./pages/Login";
import DashboardPage from "./pages/Dashboard";
import ProvidersPage from "./pages/Providers";
import KeysPage from "./pages/Keys";
import { isAuthenticated, clearAdminKey } from "./stores/auth";
import { I18nProvider, useI18n } from "./locales";

// Theme storage key
const THEME_KEY = "antigateway-theme";

type ThemeMode = "light" | "dark";

function getInitialTheme(): ThemeMode {
  if (typeof window !== "undefined") {
    const stored = localStorage.getItem(THEME_KEY) as ThemeMode | null;
    if (stored === "light" || stored === "dark") return stored;
    // Follow system preference
    if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
      return "dark";
    }
  }
  return "light";
}

function AppContent() {
  const [authed, setAuthed] = useState(isAuthenticated);
  const [themeMode, setThemeMode] = useState<ThemeMode>(getInitialTheme);
  const navigate = useNavigate();
  const location = useLocation();
  const { lang } = useI18n();

  // Apply theme to document
  useEffect(() => {
    document.documentElement.setAttribute("data-theme", themeMode);
    localStorage.setItem(THEME_KEY, themeMode);
  }, [themeMode]);

  // Listen for system theme changes
  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    const handleChange = (e: MediaQueryListEvent) => {
      const stored = localStorage.getItem(THEME_KEY);
      if (!stored) {
        setThemeMode(e.matches ? "dark" : "light");
      }
    };
    mediaQuery.addEventListener("change", handleChange);
    return () => mediaQuery.removeEventListener("change", handleChange);
  }, []);

  const onLogin = useCallback(() => {
    setAuthed(true);
    navigate("/");
  }, [navigate]);

  const onLogout = useCallback(() => {
    clearAdminKey();
    setAuthed(false);
    navigate("/login");
  }, [navigate]);

  const toggleTheme = useCallback(() => {
    setThemeMode((prev) => (prev === "light" ? "dark" : "light"));
  }, []);

  // Redirect to login if not authenticated (except login page itself)
  if (!authed && location.pathname !== "/login") {
    return <Navigate to="/login" replace />;
  }

  return (
    <ConfigProvider
      locale={lang === "zh" ? zhCN : enUS}
      theme={{
        algorithm: themeMode === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: "#2563eb",
          colorPrimaryHover: "#1d4ed8",
          colorPrimaryActive: "#1e40af",
          colorSuccess: "#10b981",
          colorWarning: "#f59e0b",
          colorError: "#ef4444",
          colorInfo: "#3b82f6",
          borderRadius: 8,
          fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
          fontSize: 14,
        },
        components: {
          Layout: {
            headerBg: themeMode === "dark" ? "#1e293b" : "#ffffff",
            siderBg: themeMode === "dark" ? "#1e293b" : "#ffffff",
            bodyBg: themeMode === "dark" ? "#0f172a" : "#f8fafc",
          },
          Card: {
            borderRadiusLG: 12,
          },
          Table: {
            headerBg: themeMode === "dark" ? "#1e293b" : "#f8fafc",
            rowHoverBg: themeMode === "dark" ? "#334155" : "#f1f5f9",
          },
          Menu: {
            itemBorderRadius: 8,
            subMenuItemBorderRadius: 8,
          },
          Button: {
            borderRadius: 8,
            controlHeight: 36,
          },
          Input: {
            borderRadius: 8,
          },
          Select: {
            borderRadius: 8,
          },
          Modal: {
            borderRadiusLG: 12,
          },
        },
      }}
    >
      <AntApp>
        <Routes>
          <Route path="/login" element={<LoginPage onSuccess={onLogin} />} />
          <Route element={<AdminLayout onLogout={onLogout} themeMode={themeMode} onToggleTheme={toggleTheme} />}>
            <Route index element={<DashboardPage />} />
            <Route path="providers" element={<ProvidersPage />} />
            <Route path="keys" element={<KeysPage />} />
          </Route>
          {/* Redirect old routes */}
          <Route path="/copilot" element={<Navigate to="/providers" replace />} />
          <Route path="/kiro" element={<Navigate to="/providers" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AntApp>
    </ConfigProvider>
  );
}

export default function App() {
  return (
    <I18nProvider>
      <AppContent />
    </I18nProvider>
  );
}
