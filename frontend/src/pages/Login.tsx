import { useState } from "react";
import { Card, Input, Button, Typography, App } from "antd";
import { LockOutlined } from "@ant-design/icons";
import { setAdminKey } from "@/stores/auth";
import { listProviders } from "@/services/api";
import { useT } from "@/locales";

interface Props {
  onSuccess: () => void;
}

// Logo SVG Component
function LogoIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      width="48"
      height="48"
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

export default function LoginPage({ onSuccess }: Props) {
  const [key, setKey] = useState("");
  const [loading, setLoading] = useState(false);
  const { message } = App.useApp();
  const t = useT();

  const handleLogin = async () => {
    if (!key.trim()) {
      message.warning(t.login.warning);
      return;
    }
    setLoading(true);
    try {
      setAdminKey(key.trim());
      // Use an admin-authed endpoint to verify the key
      await listProviders();
      message.success(t.login.success);
      onSuccess();
    } catch {
      setAdminKey("");
      message.error(t.login.error);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-50 to-slate-100 dark:from-slate-900 dark:to-slate-800 p-4">
      <Card className="w-full max-w-md shadow-lg border-0">
        <div className="text-center mb-8">
          {/* Logo */}
          <div className="inline-flex items-center justify-center w-16 h-16 rounded-2xl bg-gradient-to-br from-blue-500 to-blue-600 text-white shadow-lg mb-4">
            <LogoIcon className="w-8 h-8" />
          </div>
          <Typography.Title level={3} className="!mb-2">
            {t.login.title}
          </Typography.Title>
          <Typography.Text type="secondary">
            {t.login.subtitle}
          </Typography.Text>
        </div>

        <div className="space-y-4">
          <Input.Password
            size="large"
            prefix={<LockOutlined className="text-gray-400" />}
            placeholder={t.login.placeholder}
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
          >
            {t.login.button}
          </Button>
        </div>

        <div className="mt-6 pt-6 border-t border-gray-100 dark:border-gray-700">
          <Typography.Text type="secondary" className="text-center block text-sm">
            API Gateway Management Console
          </Typography.Text>
        </div>
      </Card>
    </div>
  );
}
