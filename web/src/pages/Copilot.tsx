import { useEffect, useState, useCallback, useRef } from "react";
import {
  Button,
  Card,
  Table,
  Tag,
  Typography,
  Space,
  App,
  Spin,
} from "antd";
import { GithubOutlined, ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  startDeviceFlow,
  pollDeviceFlow,
  completeDeviceFlow,
  listCopilotAccounts,
  type CopilotAccount,
  type DeviceFlowSession,
} from "@/services/api";

export default function CopilotPage() {
  const [accounts, setAccounts] = useState<CopilotAccount[]>([]);
  const [loading, setLoading] = useState(false);
  const [flow, setFlow] = useState<DeviceFlowSession | null>(null);
  const [flowLoading, setFlowLoading] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);
  const { message } = App.useApp();

  const fetchAccounts = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listCopilotAccounts();
      setAccounts(res.accounts ?? []);
    } catch {
      // Copilot provider may not be configured
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAccounts();
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [fetchAccounts]);

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
            message.success("Copilot 账户添加成功");
            setFlow(null);
            fetchAccounts();
          } else if (status.status === "error" || status.error) {
            clearInterval(pollRef.current);
            message.error(status.error || "授权失败");
            setFlow(null);
          }
        } catch {
          clearInterval(pollRef.current);
          setFlow(null);
        }
      }, (session.interval || 5) * 1000);
    } catch {
      message.error("启动设备授权流程失败，请确认 Copilot Provider 已配置");
    } finally {
      setFlowLoading(false);
    }
  };

  const columns: ColumnsType<CopilotAccount> = [
    { title: "用户名", dataIndex: "username", width: 200 },
    {
      title: "Token 前缀",
      dataIndex: "token_prefix",
      width: 200,
      render: (v: string) => <code>{v}...</code>,
    },
    { title: "添加时间", dataIndex: "added_at", width: 200 },
    {
      title: "Copilot Token 过期",
      dataIndex: "copilot_token_expires",
      width: 200,
    },
  ];

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <Typography.Title level={4} className="!mb-0">
          Copilot 管理
        </Typography.Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchAccounts} loading={loading}>
            刷新
          </Button>
          <Button
            type="primary"
            icon={<GithubOutlined />}
            onClick={handleStartFlow}
            loading={flowLoading}
            disabled={!!flow}
          >
            添加 GitHub 账户
          </Button>
        </Space>
      </div>

      {flow && (
        <Card className="mb-4">
          <div className="text-center">
            <Spin className="mr-3" />
            <Typography.Text>
              请在浏览器中访问{" "}
              <Typography.Link href={flow.verification_uri} target="_blank">
                {flow.verification_uri}
              </Typography.Link>{" "}
              并输入代码：
            </Typography.Text>
            <div className="mt-2">
              <Tag color="blue" className="text-2xl px-4 py-1">
                {flow.user_code}
              </Tag>
            </div>
          </div>
        </Card>
      )}

      <Table
        rowKey="username"
        columns={columns}
        dataSource={accounts}
        loading={loading}
        pagination={false}
        size="middle"
      />
    </div>
  );
}
