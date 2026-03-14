import { useState, useRef, useEffect } from "react";
import { Modal, Typography, Button, Tag, Space, App, Spin } from "antd";
import { GithubOutlined, ReloadOutlined, LinkOutlined, CheckCircleOutlined, CloseCircleOutlined } from "@ant-design/icons";
import {
  startDeviceFlow,
  pollDeviceFlow,
  completeDeviceFlow,
  getCopilotStatus,
  type CopilotStatus,
  type DeviceFlowSession,
} from "@/services/api";
import { useT } from "@/locales";

const { Text, Link } = Typography;

interface Props {
  open: boolean;
  providerName: string;
  onClose: () => void;
}

export default function CopilotAuthModal({ open, providerName, onClose }: Props) {
  const [status, setStatus] = useState<CopilotStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [flow, setFlow] = useState<DeviceFlowSession | null>(null);
  const [flowLoading, setFlowLoading] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);
  const { message } = App.useApp();
  const t = useT();

  const fetchStatus = async () => {
    setLoading(true);
    try {
      const res = await getCopilotStatus(providerName);
      setStatus(res);
    } catch {
      setStatus(null);
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
  }, [open, providerName]);

  const handleStartFlow = async () => {
    setFlowLoading(true);
    try {
      const session = await startDeviceFlow(providerName);
      setFlow(session);

      // Start polling
      pollRef.current = setInterval(async () => {
        try {
          const pollStatus = await pollDeviceFlow(session.id, providerName);
          if (pollStatus.status === "completed") {
            clearInterval(pollRef.current);
            await completeDeviceFlow(session.id, providerName);
            message.success(t.copilot.authSuccess);
            setFlow(null);
            fetchStatus();
          } else if (pollStatus.status === "error" || pollStatus.error) {
            clearInterval(pollRef.current);
            message.error(pollStatus.error || t.copilot.authFailed);
            setFlow(null);
          }
        } catch {
          clearInterval(pollRef.current);
          setFlow(null);
        }
      }, (session.interval || 5) * 1000);
    } catch {
      message.error(t.copilot.startError);
    } finally {
      setFlowLoading(false);
    }
  };

  const handleClose = () => {
    if (pollRef.current) clearInterval(pollRef.current);
    setFlow(null);
    onClose();
  };

  const renderStatus = () => {
    if (loading) {
      return (
        <div className="flex justify-center py-8">
          <Spin />
        </div>
      );
    }

    if (!status) {
      return (
        <div className="text-center py-6">
          <CloseCircleOutlined className="text-4xl text-gray-300 mb-3" />
          <Text type="secondary">{t.copilot.noProvider}</Text>
        </div>
      );
    }

    const isExpired = status.token_expires ? new Date(status.token_expires) < new Date() : false;

    return (
      <div className="space-y-4">
        {/* Account Info */}
        <div className="grid grid-cols-2 gap-4">
          <div className="p-4 bg-gray-50 dark:bg-gray-800 rounded-lg">
            <Text type="secondary" className="text-xs block mb-1">{t.copilot.username}</Text>
            <div className="flex items-center gap-2">
              <GithubOutlined className="text-lg" />
              <Text strong>{status.username || "-"}</Text>
            </div>
          </div>
          <div className="p-4 bg-gray-50 dark:bg-gray-800 rounded-lg">
            <Text type="secondary" className="text-xs block mb-1">{t.copilot.status}</Text>
            <Tag color={status.healthy && status.has_token ? "green" : "red"}>
              {status.healthy && status.has_token ? t.copilot.healthy : t.copilot.unhealthy}
            </Tag>
          </div>
        </div>

        {/* Token Status */}
        <div className="p-4 bg-gray-50 dark:bg-gray-800 rounded-lg">
          <Text type="secondary" className="text-xs block mb-1">{t.copilot.tokenStatus}</Text>
          <div className="flex items-center gap-3">
            {status.has_token ? (
              <>
                <CheckCircleOutlined className="text-green-500 text-lg" />
                <div className="flex-1">
                  <Text>{t.copilot.tokenActive}</Text>
                  {status.token_expires && (
                    <Tag
                      color={isExpired ? "red" : "blue"}
                      className="ml-2"
                    >
                      {isExpired ? t.copilot.expired : `${t.copilot.tokenExpires}: ${status.token_expires}`}
                    </Tag>
                  )}
                </div>
              </>
            ) : (
              <>
                <CloseCircleOutlined className="text-gray-400 text-lg" />
                <Text type="secondary">{t.copilot.noToken}</Text>
              </>
            )}
          </div>
        </div>
      </div>
    );
  };

  return (
    <Modal
      title={
        <Space>
          <GithubOutlined />
          <span>{t.copilot.title} - {providerName}</span>
        </Space>
      }
      open={open}
      onCancel={handleClose}
      footer={null}
      width={520}
      destroyOnClose
    >
      {/* Device Flow Section */}
      {flow && (
        <div className="text-center py-6 mb-4 bg-blue-50 dark:bg-blue-900/20 rounded-lg">
          <div className="flex items-center justify-center gap-2 mb-4">
            <div className="w-6 h-6 rounded-full border-2 border-blue-500 border-t-transparent animate-spin" />
            <Text>{t.copilot.pendingAuth}</Text>
          </div>
          <Text type="secondary" className="block mb-3">
            {t.copilot.visitUrl}
          </Text>
          <Link href={flow.verification_uri} target="_blank" className="text-base">
            {flow.verification_uri}
          </Link>
          <div className="mt-4 inline-flex flex-col items-center bg-white dark:bg-gray-800 rounded-lg px-6 py-3 shadow-sm">
            <Text type="secondary" className="text-xs mb-1">{t.copilot.verificationCode}</Text>
            <Text className="text-2xl font-mono font-bold tracking-widest text-blue-600">
              {flow.user_code}
            </Text>
          </div>
        </div>
      )}

      {/* Status Section */}
      {renderStatus()}

      {/* Actions */}
      <div className="flex items-center justify-end gap-2 mt-6 pt-4 border-t border-gray-200 dark:border-gray-700">
        <Button
          icon={<ReloadOutlined />}
          onClick={fetchStatus}
          loading={loading}
        >
          {t.common.refresh}
        </Button>
        <Button
          type="primary"
          icon={<LinkOutlined />}
          onClick={handleStartFlow}
          loading={flowLoading}
          disabled={!!flow}
        >
          {status?.has_token ? t.copilot.updateToken : t.copilot.authorize}
        </Button>
      </div>
    </Modal>
  );
}
