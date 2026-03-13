import { useState, useCallback } from "react";
import { Routes, Route, Navigate, useNavigate, useLocation } from "react-router-dom";
import { ConfigProvider, App as AntApp, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import AdminLayout from "./layouts/AdminLayout";
import LoginPage from "./pages/Login";
import DashboardPage from "./pages/Dashboard";
import ProvidersPage from "./pages/Providers";
import KeysPage from "./pages/Keys";
import UsagePage from "./pages/Usage";
import CopilotPage from "./pages/Copilot";
import KiroPage from "./pages/Kiro";
import { isAuthenticated, clearAdminKey } from "./stores/auth";

export default function App() {
  const [authed, setAuthed] = useState(isAuthenticated);
  const navigate = useNavigate();
  const location = useLocation();

  const onLogin = useCallback(() => {
    setAuthed(true);
    navigate("/");
  }, [navigate]);

  const onLogout = useCallback(() => {
    clearAdminKey();
    setAuthed(false);
    navigate("/login");
  }, [navigate]);

  // Redirect to login if not authenticated (except login page itself)
  if (!authed && location.pathname !== "/login") {
    return <Navigate to="/login" replace />;
  }

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: { colorPrimary: "#1677ff" },
      }}
    >
      <AntApp>
        <Routes>
          <Route path="/login" element={<LoginPage onSuccess={onLogin} />} />
          <Route element={<AdminLayout onLogout={onLogout} />}>
            <Route index element={<DashboardPage />} />
            <Route path="providers" element={<ProvidersPage />} />
            <Route path="keys" element={<KeysPage />} />
            <Route path="usage" element={<UsagePage />} />
            <Route path="copilot" element={<CopilotPage />} />
            <Route path="kiro" element={<KiroPage />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AntApp>
    </ConfigProvider>
  );
}
