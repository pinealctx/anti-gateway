import { useEffect, useState } from "react";
import { Modal, Table, Typography, Tag, Empty, Spin } from "antd";
import { BarChartOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { getUsage, type UsageRecord } from "@/services/api";
import { useT } from "@/locales";

const { Text } = Typography;

// Format number with locale
function formatNumber(num: number | undefined): string {
  if (num === undefined || num === null) return "-";
  return num.toLocaleString();
}

interface Props {
  open: boolean;
  keyId?: number;
  keyName?: string;
  onClose: () => void;
}

export default function UsageModal({ open, keyId, keyName, onClose }: Props) {
  const [data, setData] = useState<UsageRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const t = useT();

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = await getUsage(keyId ? { key_id: keyId } : undefined);
      setData(res.usage ?? []);
    } catch {
      // Error handled by empty state
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) {
      fetchData();
    }
  }, [open, keyId]);

  const columns: ColumnsType<UsageRecord> = [
    {
      title: t.usage.modelName,
      dataIndex: "model",
      ellipsis: true,
      render: (v: string) => (
        <Tag color="blue" className="font-mono text-xs">
          {v}
        </Tag>
      ),
    },
    {
      title: t.usage.requests,
      dataIndex: "requests",
      width: 100,
      align: "right",
      sorter: (a, b) => a.requests - b.requests,
      render: (v: number) => (
        <Text strong className="font-mono">
          {formatNumber(v)}
        </Text>
      ),
    },
    {
      title: t.usage.tokens,
      dataIndex: "tokens",
      width: 120,
      align: "right",
      sorter: (a, b) => a.tokens - b.tokens,
      render: (v: number) => (
        <Text className="font-mono text-blue-500">{formatNumber(v)}</Text>
      ),
    },
    {
      title: t.usage.avgTokens,
      width: 130,
      align: "right",
      render: (_, record) => {
        const avg = record.requests > 0 ? Math.round(record.tokens / record.requests) : 0;
        return <Text type="secondary" className="font-mono">{formatNumber(avg)}</Text>;
      },
    },
  ];

  // Calculate totals
  const totals = data.reduce(
    (acc, item) => ({
      requests: acc.requests + (item.requests || 0),
      tokens: acc.tokens + (item.tokens || 0),
    }),
    { requests: 0, tokens: 0 }
  );

  const title = keyName ? `${t.usage.title} - ${keyName}` : t.usage.title;

  return (
    <Modal
      title={
        <span>
          <BarChartOutlined className="mr-2" />
          {title}
        </span>
      }
      open={open}
      onCancel={onClose}
      footer={null}
      width={600}
      destroyOnClose
    >
      {loading ? (
        <div className="flex justify-center py-10">
          <Spin />
        </div>
      ) : data.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={t.usage.noData}
          className="py-8"
        />
      ) : (
        <>
          {/* Summary */}
          <div className="grid grid-cols-3 gap-3 mb-4">
            <div className="text-center p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <Text type="secondary" className="text-xs">{t.dashboard.providers}</Text>
              <div className="text-lg font-semibold mt-1">{data.length}</div>
            </div>
            <div className="text-center p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <Text type="secondary" className="text-xs">{t.usage.totalRequests}</Text>
              <div className="text-lg font-semibold mt-1 font-mono">{formatNumber(totals.requests)}</div>
            </div>
            <div className="text-center p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <Text type="secondary" className="text-xs">{t.usage.totalTokens}</Text>
              <div className="text-lg font-semibold mt-1 font-mono text-blue-500">{formatNumber(totals.tokens)}</div>
            </div>
          </div>

          {/* Table */}
          <Table
            rowKey={(r) => `${r.key_id}-${r.model}`}
            columns={columns}
            dataSource={data}
            pagination={false}
            size="small"
            scroll={{ y: 300 }}
          />
        </>
      )}
    </Modal>
  );
}
