import { useEffect, useState, useCallback } from "react";
import {
  Table,
  Button,
  Modal,
  Form,
  Input,
  InputNumber,
  Select,
  Switch,
  Tag,
  Space,
  App,
  Typography,
  Popconfirm,
} from "antd";
import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  type ProviderRecord,
} from "@/services/api";

const PROVIDER_TYPES = [
  { label: "Kiro", value: "kiro" },
  { label: "OpenAI", value: "openai" },
  { label: "OpenAI Compatible", value: "openai-compat" },
  { label: "Copilot", value: "copilot" },
  { label: "Anthropic", value: "anthropic" },
];

export default function ProvidersPage() {
  const [data, setData] = useState<ProviderRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ProviderRecord | null>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listProviders();
      setData(res.providers);
    } catch {
      message.error("加载 Provider 列表失败");
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ weight: 100, enabled: true });
    setModalOpen(true);
  };

  const openEdit = (record: ProviderRecord) => {
    setEditing(record);
    form.setFieldsValue({
      ...record,
      models: record.models?.join(", ") ?? "",
      github_tokens: record.github_tokens?.join("\n") ?? "",
    });
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      // Parse comma-separated models
      const models = values.models
        ? String(values.models)
            .split(",")
            .map((s: string) => s.trim())
            .filter(Boolean)
        : undefined;
      // Parse newline-separated github tokens
      const github_tokens = values.github_tokens
        ? String(values.github_tokens)
            .split("\n")
            .map((s: string) => s.trim())
            .filter(Boolean)
        : undefined;

      const payload = { ...values, models, github_tokens };

      if (editing) {
        await updateProvider(editing.id, payload);
        message.success("Provider 已更新");
      } else {
        await createProvider(payload);
        message.success("Provider 已创建");
      }
      setModalOpen(false);
      fetchData();
    } catch {
      // validation error, ignore
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await deleteProvider(id);
      message.success("Provider 已删除");
      fetchData();
    } catch {
      message.error("删除失败");
    }
  };

  const columns: ColumnsType<ProviderRecord> = [
    { title: "ID", dataIndex: "id", width: 60 },
    { title: "名称", dataIndex: "name", width: 150 },
    {
      title: "类型",
      dataIndex: "type",
      width: 120,
      render: (t: string) => <Tag>{t}</Tag>,
    },
    { title: "权重", dataIndex: "weight", width: 80 },
    {
      title: "启用",
      dataIndex: "enabled",
      width: 80,
      render: (v: boolean) =>
        v ? <Tag color="green">是</Tag> : <Tag color="red">否</Tag>,
    },
    {
      title: "健康",
      dataIndex: "healthy",
      width: 80,
      render: (v: boolean) =>
        v ? <Tag color="green">健康</Tag> : <Tag color="red">异常</Tag>,
    },
    {
      title: "模型",
      dataIndex: "models",
      ellipsis: true,
      render: (models: string[]) =>
        models?.length ? models.map((m) => <Tag key={m}>{m}</Tag>) : <Tag>所有</Tag>,
    },
    {
      title: "操作",
      width: 150,
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" onClick={() => openEdit(record)}>
            编辑
          </Button>
          <Popconfirm
            title="确定删除此 Provider?"
            onConfirm={() => handleDelete(record.id)}
          >
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const selectedType = Form.useWatch("type", form);

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <Typography.Title level={4} className="!mb-0">
          Provider 管理
        </Typography.Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchData} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            添加 Provider
          </Button>
        </Space>
      </div>

      <Table
        rowKey="id"
        columns={columns}
        dataSource={data}
        loading={loading}
        pagination={false}
        size="middle"
      />

      <Modal
        title={editing ? "编辑 Provider" : "添加 Provider"}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
        width={560}
        destroyOnClose
      >
        <Form form={form} layout="vertical" className="mt-4">
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: "请输入名称" }]}
          >
            <Input placeholder="my-provider" disabled={!!editing} />
          </Form.Item>
          <Form.Item
            name="type"
            label="类型"
            rules={[{ required: true, message: "请选择类型" }]}
          >
            <Select options={PROVIDER_TYPES} placeholder="选择 Provider 类型" disabled={!!editing} />
          </Form.Item>
          <Form.Item name="weight" label="权重">
            <InputNumber min={0} max={10000} className="w-full" />
          </Form.Item>
          {editing && (
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          )}
          {(selectedType === "openai" ||
            selectedType === "openai-compat" ||
            selectedType === "anthropic") && (
            <>
              <Form.Item name="base_url" label="Base URL">
                <Input placeholder="https://api.openai.com/v1" />
              </Form.Item>
              <Form.Item name="api_key" label="API Key">
                <Input.Password placeholder="sk-..." />
              </Form.Item>
            </>
          )}
          {selectedType === "copilot" && (
            <Form.Item name="github_tokens" label="GitHub Tokens（每行一个）">
              <Input.TextArea rows={3} placeholder="gho_xxx&#10;gho_yyy" />
            </Form.Item>
          )}
          <Form.Item name="models" label="模型（逗号分隔，留空=所有）">
            <Input placeholder="gpt-4, claude-sonnet-4-20250514" />
          </Form.Item>
          <Form.Item name="default_model" label="默认模型">
            <Input placeholder="gpt-4" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
