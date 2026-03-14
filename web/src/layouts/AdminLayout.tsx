import { useState, useEffect } from "react";
import { Outlet, useNavigate, useLocation } from "react-router-dom";
import { Layout, Menu, Button, Typography, Tooltip, Drawer } from "antd";
import {
  DashboardOutlined,
  CloudServerOutlined,
  KeyOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SunOutlined,
  MoonOutlined,
  GlobalOutlined,
} from "@ant-design/icons";
import { useI18n } from "@/locales";

const { Header, Sider, Content } = Layout;

// Logo SVG Component - Lightning bolt icon
function LogoIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      width="24"
      height="24"
    >
      <path
        d="M13 2L3 14H12L11 22L21 10H12L13 2Z"
        fill="currentColor"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

interface Props {
  onLogout: () => void;
  themeMode: "light" | "dark";
  onToggleTheme: () => void;
}

export default function AdminLayout({ onLogout, themeMode, onToggleTheme }: Props) {
  const navigate = useNavigate();
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const { t, lang, toggleLang } = useI18n();

  // Menu items with translations
  const menuItems = [
    { key: "/", icon: <DashboardOutlined />, label: t.nav.dashboard },
    { key: "/providers", icon: <CloudServerOutlined />, label: t.nav.providers },
    { key: "/keys", icon: <KeyOutlined />, label: t.nav.keys },
  ];

  // Detect mobile screen
  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth < 768);
      if (window.innerWidth >= 768) {
        setMobileOpen(false);
      }
    };
    checkMobile();
    window.addEventListener("resize", checkMobile);
    return () => window.removeEventListener("resize", checkMobile);
  }, []);

  // Map current path to menu key
  const selectedKey =
    menuItems.find((item) => item.key !== "/" && location.pathname.startsWith(item.key))?.key ?? "/";

  const handleMenuClick = (key: string) => {
    navigate(key);
    if (isMobile) {
      setMobileOpen(false);
    }
  };

  // Logo component
  const Logo = (
    <div className="flex items-center gap-2 px-4">
      <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-blue-500 to-blue-600 flex items-center justify-center text-white shadow-sm">
        <LogoIcon className="w-5 h-5" />
      </div>
      {(!collapsed || isMobile) && (
        <Typography.Title level={5} className="!mb-0 !text-inherit transition-all duration-200">
          AntiGateway
        </Typography.Title>
      )}
    </div>
  );

  const menuContent = (
    <Menu
      mode="inline"
      selectedKeys={[selectedKey]}
      items={menuItems}
      onClick={({ key }) => handleMenuClick(key)}
      className="border-r-0"
      style={{ border: "none" }}
    />
  );

  return (
    <Layout className="min-h-screen">
      {/* Desktop Sider */}
      {!isMobile && (
        <Sider
          theme={themeMode === "dark" ? "dark" : "light"}
          width={220}
          collapsedWidth={72}
          collapsible
          collapsed={collapsed}
          onCollapse={setCollapsed}
          trigger={null}
          className="border-r"
          style={{
            borderColor: themeMode === "dark" ? "#334155" : "#e2e8f0",
            position: "sticky",
            top: 0,
            height: "100vh",
            overflow: "auto",
          }}
        >
          <div
            className="h-16 flex items-center border-b px-2"
            style={{ borderColor: themeMode === "dark" ? "#334155" : "#e2e8f0" }}
          >
            {Logo}
          </div>
          <div className="py-2">{menuContent}</div>
        </Sider>
      )}

      {/* Mobile Drawer */}
      {isMobile && (
        <Drawer
          placement="left"
          closable={false}
          onClose={() => setMobileOpen(false)}
          open={mobileOpen}
          width={260}
          styles={{
            body: { padding: 0 },
            header: { display: "none" },
          }}
          className={themeMode === "dark" ? "dark-drawer" : ""}
        >
          <div
            className="h-16 flex items-center border-b px-2"
            style={{ borderColor: themeMode === "dark" ? "#334155" : "#e2e8f0" }}
          >
            {Logo}
          </div>
          <div className="py-2">{menuContent}</div>
        </Drawer>
      )}

      <Layout>
        <Header
          className="!px-4 md:!px-6 flex items-center justify-between border-b"
          style={{
            borderColor: themeMode === "dark" ? "#334155" : "#e2e8f0",
            position: "sticky",
            top: 0,
            zIndex: 10,
            background: themeMode === "dark" ? "#1e293b" : "#ffffff",
          }}
        >
          <div className="flex items-center gap-2">
            {/* Mobile menu toggle */}
            {isMobile && (
              <Button
                type="text"
                icon={<MenuUnfoldOutlined />}
                onClick={() => setMobileOpen(true)}
                className="!flex md:!hidden"
              />
            )}
            {/* Desktop collapse toggle */}
            {!isMobile && (
              <Button
                type="text"
                icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                onClick={() => setCollapsed(!collapsed)}
              />
            )}
          </div>

          <div className="flex items-center gap-2">
            {/* Language toggle */}
            <Tooltip title={lang === "zh" ? "Switch to English" : "切换到中文"}>
              <Button
                type="text"
                icon={<GlobalOutlined />}
                onClick={toggleLang}
                className="font-medium"
              >
                {lang === "zh" ? "EN" : "中文"}
              </Button>
            </Tooltip>

            {/* Theme toggle */}
            <Tooltip title={themeMode === "dark" ? t.nav.themeLight : t.nav.themeDark}>
              <Button
                type="text"
                icon={themeMode === "dark" ? <SunOutlined /> : <MoonOutlined />}
                onClick={onToggleTheme}
              />
            </Tooltip>

            {/* Logout */}
            <Tooltip title={t.nav.logout}>
              <Button type="text" icon={<LogoutOutlined />} onClick={onLogout} danger />
            </Tooltip>
          </div>
        </Header>

        <Content
          className="p-4 md:p-6"
          style={{
            background: themeMode === "dark" ? "#0f172a" : "#f8fafc",
            minHeight: "calc(100vh - 64px)",
          }}
        >
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
