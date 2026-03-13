import { useEffect, useState, useCallback } from "react";
import {
  Table,
  Button,
  Modal,
  Form,
  Input,
  InputNumber,
  Switch,
  Tag,
  Space,
  App,
  Typography,
  Popconfirm,
} from "antd";
import { PlusOutlined, ReloadOutlined, CopyOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  listKeys,
  createKey,
  updateKey,
  deleteKey,
  type ApiKey,
} from "@/services/api";

export default function KeysPage() {
  const [data, setData] = useState<ApiKey[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ApiKey | null>(null);
  const [newKeyValue, setNewKeyValue] = useState("");
  const [form] = Form.useForm();
  const { message } = App.useApp();

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listKeys();
      setData(res.keys);
    } catch {
      message.error("加载 Key 列表失败");
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const openCreate = () => {
    setEditing(null);
    setNewKeyValue("");
    form.resetFields();
    form.setFieldsValue({ enabled: true, qpm: 60, tpm: 100000 });
    setModalOpen(true);
  };

  const openEdit = (record: ApiKey) => {
    setEditing(record);
    setNewKeyValue("");
    form.setFieldsValue({
      ...record,
      allowed_models: record.allowed_models?.join(", ") ?? "",
      allowed_providers: record.allowed_providers?.join(", ") ?? "",
    });
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      const parseList = (v: string | undefined) =>
        v
          ? String(v)
              .split(",")
              .map((s: string) => s.trim())
              .filter(Boolean)
          : undefined;

      const payload = {
        ...values,
        allowed_models: parseList(values.allowed_models),
        allowed_providers: parseList(values.allowed_providers),
      };

      if (editing) {
        await updateKey(editing.id, payload);
        message.success("Key 已更新");
        setModalOpen(false);
      } else {
        const res = await createKey(payload);
        if (res.key) {
          setNewKeyValue(res.key);
          message.success("Key 已创建，请复制保存");
        } else {
          setModalOpen(false);
        }
      }
      fetchData();
    } catch {
      // validation error
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await deleteKey(id);
      message.success("Key 已删除");
      fetchData();
    } catch {
      message.error("删除失败");
    }
  };

  const columns: ColumnsType<ApiKey> = [
    { title: "ID", dataIndex: "id", width: 60 },
    { title: "名称", dataIndex: "name", width: 150 },
    {
      title: "Key",
      dataIndex: "key_prefix",
      width: 150,
      render: (v: string) => <code>{v}...</code>,
    },
    {
      title: "启用",
      dataIndex: "enabled",
      width: 80,
      render: (v: boolean) =>
        v ? <Tag color="green">是</Tag> : <Tag color="red">否</Tag>,
    },
    { title: "QPM", dataIndex: "qpm", width: 80 },
    { title: "TPM", dataIndex: "tpm", width: 100 },
    {
      title: "默认 Provider",
      dataIndex: "default_provider",
      width: 130,
      render: (v: string) => v || "-",
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
            title="确定删除此 Key?"
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

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <Typography.Title level={4} className="!mb-0">
          API Key 管理
        </Typography.Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchData} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            创建 Key
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
        title={editing ? "编辑 Key" : "创建 Key"}
        open={modalOpen}
        onOk={newKeyValue ? () => setModalOpen(false) : handleSubmit}
        onCancel={() => setModalOpen(false)}
        okText={newKeyValue ? "完成" : "确定"}
        width={520}
        destroyOnClose
      >
        {newKeyValue ? (
          <div className="py-4">
            <Typography.Paragraph type="warning">
              请立即复制此 Key，关闭后将无法再次查看完整值：
            </Typography.Paragraph>
            <Input.Search
              value={newKeyValue}
              readOnly
              enterButton={<CopyOutlined />}
              onSearch={() => {
                navigator.clipboard.writeText(newKeyValue);
                message.success("已复制");
              }}
            />
          </div>
        ) : (
          <Form form={form} layout="vertical" className="mt-4">
            <Form.Item
              name="name"
              label="名称"
              rules={[{ required: true, message: "请输入名称" }]}
            >
              <Input placeholder="my-api-key" />
            </Form.Item>
            {editing && (
              <Form.Item name="enabled" label="启用" valuePropName="checked">
                <Switch />
              </Form.Item>
            )}
            <Form.Item name="default_provider" label="默认 Provider">
              <Input placeholder="留空使用全局默认" />
            </Form.Item>
            <Form.Item name="allowed_models" label="允许的模型（逗号分隔，留空=所有）">
              <Input placeholder="gpt-4, claude-sonnet-4-20250514" />
            </Form.Item>
            <Form.Item name="allowed_providers" label="允许的 Provider（逗号分隔，留空=所有）">
              <Input placeholder="openai, anthropic" />
            </Form.Item>
            <Form.Item name="qpm" label="QPM（每分钟请求数）">
              <InputNumber min={0} className="w-full" />
            </Form.Item>
            <Form.Item name="tpm" label="TPM（每分钟 Token 数）">
              <InputNumber min={0} className="w-full" />
            </Form.Item>
          </Form>
        )}
      </Modal>
    </div>
  );
}
