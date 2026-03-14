import { useState, useRef, useEffect } from "react";
import { Modal, Typography, Button, Descriptions, Tag, Space, App, Empty } from "antd";
import { ThunderboltOutlined, ReloadOutlined, SyncOutlined, CheckCircleOutlined, CloseCircleOutlined, ClockCircleOutlined } from "@ant-design/icons";
import {
  startKiroLogin,
  getKiroLoginStatus,
  completeKiroLogin,
  getKiroStatus,
  refreshKiroToken,
  type KiroStatus,
} from "@/services/api";
import { useT } from "@/locales";

const { Text, Link } = Typography;

interface Props {
  open: boolean;
  providerName: string;
  onClose: () => void;
}

export default function KiroAuthModal({ open, providerName, onClose }: Props) {
  const [status, setStatus] = useState<KiroStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [loginUrl, setLoginUrl] = useState("");
  const [loginLoading, setLoginLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);
  const { message } = App.useApp();
  const t = useT();

  const fetchStatus = async () => {
    setLoading(true);
    try {
      const res = await getKiroStatus();
      setStatus(res);
    } catch {
      // Kiro provider may not be configured
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) {
      fetchStatus();
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [open]);

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
            message.success(t.kiro.loginSuccess);
            setLoginUrl("");
            fetchStatus();
          } else if (res.status === "error" || res.error) {
            clearInterval(pollRef.current);
            message.error(res.error || t.kiro.loginFailed);
            setLoginUrl("");
          }
        } catch {
          clearInterval(pollRef.current);
          setLoginUrl("");
        }
      }, 3000);
    } catch {
      message.error(t.kiro.startError);
    } finally {
      setLoginLoading(false);
    }
  };

  const handleRefreshToken = async () => {
    setRefreshing(true);
    try {
      const res = await refreshKiroToken();
      setStatus(res);
      message.success(t.kiro.tokenRefreshSuccess);
    } catch {
      message.error(t.kiro.tokenRefreshFailed);
    } finally {
      setRefreshing(false);
    }
  };

  const handleClose = () => {
    if (pollRef.current) clearInterval(pollRef.current);
    setLoginUrl("");
    onClose();
  };

  return (
    <Modal
      title={
        <Space>
          <ThunderboltOutlined />
          <span>{t.kiro.title} - {providerName}</span>
        </Space>
      }
      open={open}
      onCancel={handleClose}
      footer={null}
      width={560}
      destroyOnClose
    >
      {/* Login Pending Section */}
      {loginUrl && (
        <div className="text-center py-6 mb-4 bg-blue-50 dark:bg-blue-900/20 rounded-lg">
          <div className="flex items-center justify-center gap-2 mb-3">
            <div className="w-6 h-6 rounded-full border-2 border-blue-500 border-t-transparent animate-spin" />
            <Text>{t.kiro.pendingAuth}</Text>
          </div>
          <Text type="secondary" className="block mb-2">
            {t.common.refresh}
          </Text>
          <Link href={loginUrl} target="_blank" className="text-blue-600">
            {t.kiro.openAuthPage}
          </Link>
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center justify-between mb-4">
        <Text type="secondary">{t.kiro.statusTitle}</Text>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchStatus} loading={loading} size="small">
            {t.common.refresh}
          </Button>
          <Button
            icon={<SyncOutlined />}
            onClick={handleRefreshToken}
            loading={refreshing}
            disabled={!status?.has_login}
            size="small"
          >
            {t.kiro.refreshToken}
          </Button>
          <Button
            type="primary"
            icon={<ThunderboltOutlined />}
            onClick={handleLogin}
            loading={loginLoading}
            disabled={!!loginUrl}
            size="small"
          >
            {t.kiro.pkceLogin}
          </Button>
        </Space>
      </div>

      {/* Status Display */}
      {loading && !status ? (
        <div className="flex justify-center py-10">
          <div className="w-8 h-8 rounded-full border-2 border-blue-500 border-t-transparent animate-spin" />
        </div>
      ) : status ? (
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item
            label={
              <Space size="small">
                <CheckCircleOutlined />
                <Text strong>{t.kiro.loginToken}</Text>
              </Space>
            }
          >
            {status.has_login ? (
              <Tag color="green" icon={<CheckCircleOutlined />}>{t.kiro.configured}</Tag>
            ) : (
              <Tag color="red" icon={<CloseCircleOutlined />}>{t.kiro.notConfigured}</Tag>
            )}
          </Descriptions.Item>
          <Descriptions.Item
            label={
              <Space size="small">
                <CheckCircleOutlined />
                <Text strong>{t.kiro.currentToken}</Text>
              </Space>
            }
          >
            {status.has_current ? (
              <Tag color="green" icon={<CheckCircleOutlined />}>{t.kiro.valid}</Tag>
            ) : (
              <Tag color="orange" icon={<CloseCircleOutlined />}>{t.kiro.none}</Tag>
            )}
          </Descriptions.Item>
          <Descriptions.Item
            label={
              <Space size="small">
                <ThunderboltOutlined />
                <Text strong>{t.kiro.externalIdp}</Text>
              </Space>
            }
          >
            {status.is_external_idp ? <Tag color="blue">{t.kiro.yes}</Tag> : <Tag>{t.kiro.no}</Tag>}
          </Descriptions.Item>
          <Descriptions.Item
            label={
              <Space size="small">
                <ClockCircleOutlined />
                <Text strong>{t.kiro.expiresAt}</Text>
              </Space>
            }
          >
            {status.expires_at ? <Text code>{status.expires_at}</Text> : <Text type="secondary">-</Text>}
          </Descriptions.Item>
        </Descriptions>
      ) : (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={t.kiro.statusError}
        >
          <Text type="secondary">{t.kiro.confirmConfig}</Text>
        </Empty>
      )}
    </Modal>
  );
}
