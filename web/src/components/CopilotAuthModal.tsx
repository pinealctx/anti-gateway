import { useState, useRef, useEffect } from "react";
import { Modal, Typography, Button, Table, Tag, Space, App, Empty, Popconfirm } from "antd";
import { GithubOutlined, ReloadOutlined, PlusOutlined, DeleteOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  startDeviceFlow,
  pollDeviceFlow,
  completeDeviceFlow,
  listCopilotAccounts,
  deleteCopilotAccount,
  type CopilotAccount,
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
  const [accounts, setAccounts] = useState<CopilotAccount[]>([]);
  const [loading, setLoading] = useState(false);
  const [flow, setFlow] = useState<DeviceFlowSession | null>(null);
  const [flowLoading, setFlowLoading] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);
  const { message } = App.useApp();
  const t = useT();

  const fetchAccounts = async () => {
    setLoading(true);
    try {
      const res = await listCopilotAccounts();
      setAccounts(res.accounts ?? []);
    } catch {
      // Copilot provider may not be configured
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) {
      fetchAccounts();
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [open]);

  const handleStartFlow = async () => {
    setFlowLoading(true);
    try {
      const session = await startDeviceFlow();
      setFlow(session);

      // Start polling
      pollRef.current = setInterval(async () => {
        try {
          const status = await pollDeviceFlow(session.id);
          if (status.status === "complete") {
            clearInterval(pollRef.current);
            await completeDeviceFlow(session.id);
            message.success(t.copilot.addSuccess);
            setFlow(null);
            fetchAccounts();
          } else if (status.status === "error" || status.error) {
            clearInterval(pollRef.current);
            message.error(status.error || t.copilot.authFailed);
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

  const handleDeleteAccount = async (username: string) => {
    try {
      await deleteCopilotAccount(username);
      message.success(t.copilot.deleteSuccess);
      fetchAccounts();
    } catch {
      message.error(t.copilot.deleteError);
    }
  };

  const columns: ColumnsType<CopilotAccount> = [
    {
      title: t.copilot.username,
      dataIndex: "username",
      width: 160,
      render: (v: string) => (
        <Space>
          <GithubOutlined className="text-lg" />
          <Text strong>{v}</Text>
        </Space>
      ),
    },
    {
      title: t.copilot.tokenPrefix,
      dataIndex: "token_prefix",
      width: 140,
      render: (v: string) => <Text code className="text-xs">{v}...</Text>,
    },
    {
      title: t.copilot.addedAt,
      dataIndex: "added_at",
      width: 150,
      render: (v: string) => <Text type="secondary">{v || "-"}</Text>,
    },
    {
      title: t.copilot.tokenExpires,
      dataIndex: "copilot_token_expires",
      width: 150,
      render: (v: string) => {
        if (!v) return <Text type="secondary">-</Text>;
        const isExpired = new Date(v) < new Date();
        return <Tag color={isExpired ? "red" : "green"}>{isExpired ? t.copilot.expired : v}</Tag>;
      },
    },
    {
      title: "",
      width: 60,
      render: (_, record) => (
        <Popconfirm
          title={t.copilot.deleteConfirm}
          description={t.copilot.deleteDesc.replace("{username}", record.username)}
          onConfirm={() => handleDeleteAccount(record.username)}
          okButtonProps={{ danger: true }}
        >
          <Button type="text" size="small" danger icon={<DeleteOutlined />} />
        </Popconfirm>
      ),
    },
  ];

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
      width={680}
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

      {/* Actions */}
      <div className="flex items-center justify-between mb-4">
        <Text type="secondary">
          {t.copilot.accounts} {accounts.length} {t.copilot.authorized}
        </Text>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchAccounts} loading={loading} size="small">
            {t.common.refresh}
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={handleStartFlow}
            loading={flowLoading}
            disabled={!!flow}
            size="small"
          >
            {t.copilot.addAccount}
          </Button>
        </Space>
      </div>

      {/* Accounts Table */}
      <Table
        rowKey="username"
        columns={columns}
        dataSource={accounts}
        loading={loading}
        pagination={false}
        size="small"
        scroll={{ y: 300 }}
        locale={{
          emptyText: (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={t.copilot.noAccounts}
            >
              <Button type="primary" icon={<PlusOutlined />} onClick={handleStartFlow} disabled={!!flow}>
                {t.copilot.addFirst}
              </Button>
            </Empty>
          ),
        }}
      />
    </Modal>
  );
}
