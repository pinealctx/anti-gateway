import { Outlet, useNavigate, useLocation } from "react-router-dom";
import { Layout, Menu, Button, Typography } from "antd";
import {
  DashboardOutlined,
  CloudServerOutlined,
  KeyOutlined,
  BarChartOutlined,
  GithubOutlined,
  ThunderboltOutlined,
  LogoutOutlined,
} from "@ant-design/icons";

const { Header, Sider, Content } = Layout;

const menuItems = [
  { key: "/", icon: <DashboardOutlined />, label: "仪表盘" },
  { key: "/providers", icon: <CloudServerOutlined />, label: "Provider" },
  { key: "/keys", icon: <KeyOutlined />, label: "API Keys" },
  { key: "/usage", icon: <BarChartOutlined />, label: "用量统计" },
  { key: "/copilot", icon: <GithubOutlined />, label: "Copilot" },
  { key: "/kiro", icon: <ThunderboltOutlined />, label: "Kiro" },
];

interface Props {
  onLogout: () => void;
}

export default function AdminLayout({ onLogout }: Props) {
  const navigate = useNavigate();
  const location = useLocation();

  // Map current path to menu key
  const selectedKey = menuItems.find(
    (item) => item.key !== "/" && location.pathname.startsWith(item.key),
  )?.key ?? "/";

  return (
    <Layout className="min-h-screen">
      <Sider theme="light" width={220} className="border-r border-gray-200">
        <div className="h-16 flex items-center justify-center border-b border-gray-200">
          <Typography.Title level={4} className="!mb-0">
            ⚡ AntiGateway
          </Typography.Title>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          className="border-r-0"
        />
      </Sider>
      <Layout>
        <Header className="!bg-white !px-6 flex items-center justify-end border-b border-gray-200">
          <Button
            type="text"
            icon={<LogoutOutlined />}
            onClick={onLogout}
            danger
          >
            退出
          </Button>
        </Header>
        <Content className="p-6 bg-gray-50">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
