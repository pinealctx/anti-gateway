import { useEffect, useState, useCallback } from "react";
import { Table, Typography, Spin, App } from "antd";
import type { ColumnsType } from "antd/es/table";
import { getUsage, type UsageRecord } from "@/services/api";

export default function UsagePage() {
  const [data, setData] = useState<UsageRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const { message } = App.useApp();

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getUsage();
      setData(res.usage ?? []);
    } catch {
      message.error("加载用量数据失败");
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const columns: ColumnsType<UsageRecord> = [
    { title: "Key ID", dataIndex: "key_id", width: 80 },
    { title: "Key 名称", dataIndex: "key_name", width: 150 },
    { title: "模型", dataIndex: "model", width: 200 },
    {
      title: "请求数",
      dataIndex: "requests",
      width: 100,
      sorter: (a, b) => a.requests - b.requests,
    },
    {
      title: "Token 用量",
      dataIndex: "tokens",
      width: 120,
      render: (v: number) => v?.toLocaleString() ?? "-",
      sorter: (a, b) => a.tokens - b.tokens,
    },
  ];

  if (loading && data.length === 0) {
    return (
      <div className="flex justify-center py-20">
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div>
      <Typography.Title level={4} className="!mb-6">
        用量统计
      </Typography.Title>
      <Table
        rowKey={(r) => `${r.key_id}-${r.model}`}
        columns={columns}
        dataSource={data}
        loading={loading}
        pagination={false}
        size="middle"
      />
    </div>
  );
}
