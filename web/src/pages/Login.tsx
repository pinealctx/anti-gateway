import { useState } from "react";
import { Card, Input, Button, Typography, App } from "antd";
import { LockOutlined, ThunderboltOutlined } from "@ant-design/icons";
import { setAdminKey } from "@/stores/auth";
import { listProviders } from "@/services/api";

interface Props {
  onSuccess: () => void;
}

export default function LoginPage({ onSuccess }: Props) {
  const [key, setKey] = useState("");
  const [loading, setLoading] = useState(false);
  const { message } = App.useApp();

  const handleLogin = async () => {
    if (!key.trim()) {
      message.warning("请输入 Admin Key");
      return;
    }
    setLoading(true);
    try {
      setAdminKey(key.trim());
      // Use an admin-authed endpoint to verify the key
      await listProviders();
      message.success("连接成功");
      onSuccess();
    } catch {
      setAdminKey("");
      message.error("验证失败，请检查 Admin Key");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100">
      <Card className="w-96 shadow-lg">
        <div className="text-center mb-8">
          <ThunderboltOutlined className="text-5xl text-blue-500 mb-4" />
          <Typography.Title level={3}>AntiGateway Admin</Typography.Title>
          <Typography.Text type="secondary">输入 Admin Key 以访问管理面板</Typography.Text>
        </div>
        <Input.Password
          size="large"
          prefix={<LockOutlined />}
          placeholder="Admin Key"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          onPressEnter={handleLogin}
        />
        <Button
          type="primary"
          size="large"
          block
          loading={loading}
          onClick={handleLogin}
          className="mt-4"
        >
          连接
        </Button>
      </Card>
    </div>
  );
}
