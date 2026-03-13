import { useEffect, useState, useCallback, useRef } from "react";
import {
  Button,
  Card,
  Descriptions,
  Tag,
  Typography,
  Space,
  App,
  Spin,
} from "antd";
import { ThunderboltOutlined, ReloadOutlined, SyncOutlined } from "@ant-design/icons";
import {
  startKiroLogin,
  getKiroLoginStatus,
  completeKiroLogin,
  getKiroStatus,
  refreshKiroToken,
  type KiroStatus,
} from "@/services/api";

export default function KiroPage() {
  const [status, setStatus] = useState<KiroStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [loginUrl, setLoginUrl] = useState("");
  const [loginLoading, setLoginLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);
  const { message } = App.useApp();

  const fetchStatus = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getKiroStatus();
      setStatus(res);
    } catch {
      // Kiro provider may not be configured
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [fetchStatus]);

  const handleLogin = async () => {
    setLoginLoading(true);
    try {
      const session = await startKiroLogin();
      setLoginUrl(session.auth_url);

      // Open auth URL
      window.open(session.auth_url, "_blank");

      // Poll for completion
      pollRef.current = setInterval(async () => {
        try {
          const res = await getKiroLoginStatus(session.id);
          if (res.status === "completed") {
            clearInterval(pollRef.current);
            await completeKiroLogin(session.id);
            message.success("Kiro 登录成功");
            setLoginUrl("");
            fetchStatus();
          } else if (res.status === "error" || res.error) {
            clearInterval(pollRef.current);
            message.error(res.error || "登录失败");
            setLoginUrl("");
          }
        } catch {
          clearInterval(pollRef.current);
          setLoginUrl("");
        }
      }, 3000);
    } catch {
      message.error("启动 Kiro 登录失败，请确认 Kiro Provider 已配置");
    } finally {
      setLoginLoading(false);
    }
  };

  const handleRefreshToken = async () => {
    setRefreshing(true);
    try {
      const res = await refreshKiroToken();
      setStatus(res);
      message.success("Token 刷新成功");
    } catch {
      message.error("Token 刷新失败");
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <Typography.Title level={4} className="!mb-0">
          Kiro 管理
        </Typography.Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchStatus} loading={loading}>
            刷新状态
          </Button>
          <Button
            icon={<SyncOutlined />}
            onClick={handleRefreshToken}
            loading={refreshing}
            disabled={!status?.has_login}
          >
            强制刷新 Token
          </Button>
          <Button
            type="primary"
            icon={<ThunderboltOutlined />}
            onClick={handleLogin}
            loading={loginLoading}
            disabled={!!loginUrl}
          >
            PKCE 登录
          </Button>
        </Space>
      </div>

      {loginUrl && (
        <Card className="mb-4">
          <div className="text-center">
            <Spin className="mr-3" />
            <Typography.Text>
              正在等待浏览器完成授权...{" "}
              <Typography.Link href={loginUrl} target="_blank">
                重新打开
              </Typography.Link>
            </Typography.Text>
          </div>
        </Card>
      )}

      <Card>
        {loading && !status ? (
          <div className="flex justify-center py-10">
            <Spin />
          </div>
        ) : status ? (
          <Descriptions column={2} bordered size="small">
            <Descriptions.Item label="Login Token">
              {status.has_login ? (
                <Tag color="green">已配置</Tag>
              ) : (
                <Tag color="red">未配置</Tag>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="当前 Token">
              {status.has_current ? (
                <Tag color="green">有效</Tag>
              ) : (
                <Tag color="orange">无</Tag>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="外部 IdP">
              {status.is_external_idp ? (
                <Tag color="blue">是</Tag>
              ) : (
                <Tag>否</Tag>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="过期时间">
              {status.expires_at || "-"}
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Typography.Text type="secondary">
            Kiro Provider 未配置
          </Typography.Text>
        )}
      </Card>
    </div>
  );
}
