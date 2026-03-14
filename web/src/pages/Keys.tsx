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
  Empty,
  Card,
  Alert,
  Tooltip,
} from "antd";
import {
  PlusOutlined,
  ReloadOutlined,
  CopyOutlined,
  EditOutlined,
  DeleteOutlined,
  KeyOutlined,
  BarChartOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  listKeys,
  createKey,
  updateKey,
  deleteKey,
  type ApiKey,
} from "@/services/api";
import UsageModal from "@/components/UsageModal";
import { useT } from "@/locales";

const { Title, Text } = Typography;

// Status Badge Component
function StatusBadge({ status, label }: { status: boolean; label: string }) {
  return (
    <span className={`status-badge ${status ? "success" : "error"}`}>
      {label}
    </span>
  );
}

export default function KeysPage() {
  const [data, setData] = useState<ApiKey[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ApiKey | null>(null);
  const [newKeyValue, setNewKeyValue] = useState("");
  const [form] = Form.useForm();
  const { message } = App.useApp();
  const t = useT();

  // Usage modal state
  const [usageModalOpen, setUsageModalOpen] = useState(false);
  const [selectedKey, setSelectedKey] = useState<ApiKey | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listKeys();
      setData(res.keys);
    } catch {
      message.error(t.keys.loadError);
    } finally {
      setLoading(false);
    }
  }, [message, t]);

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

  const openUsage = (record: ApiKey) => {
    setSelectedKey(record);
    setUsageModalOpen(true);
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
        message.success(t.keys.updateSuccess);
        setModalOpen(false);
      } else {
        const res = await createKey(payload);
        if (res.key) {
          setNewKeyValue(res.key);
          message.success(t.keys.createSuccess);
        } else {
          setModalOpen(false);
        }
      }
      fetchData();
    } catch {
      // validation error
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteKey(id);
      message.success(t.keys.deleteSuccess);
      fetchData();
    } catch {
      message.error(t.keys.deleteError);
    }
  };

  const handleCopyKey = () => {
    navigator.clipboard.writeText(newKeyValue);
    message.success(t.common.copied);
  };

  const columns: ColumnsType<ApiKey> = [
    {
      title: t.common.id,
      dataIndex: "id",
      width: 100,
      render: (id: string) => (
        <Tooltip title={id}>
          <Tag
            className="font-mono text-xs cursor-pointer hover:opacity-80 transition-opacity"
            onClick={() => {
              navigator.clipboard.writeText(id);
              message.success(t.common.copied);
            }}
          >
            {id ? id.slice(0, 8) : "—"}
          </Tag>
        </Tooltip>
      ),
    },
    {
      title: t.common.name,
      dataIndex: "name",
      width: 200,
      render: (name: string) => <Text strong>{name}</Text>,
    },
    {
      title: t.keys.keyPrefix,
      dataIndex: "key_prefix",
      width: 150,
      render: (v: string) => (
        <Tooltip title={t.keys.keyPrefixTooltip}>
          <Text code className="text-xs">
            {v}...
          </Text>
        </Tooltip>
      ),
    },
    {
      title: t.common.enabled,
      dataIndex: "enabled",
      width: 80,
      align: "center",
      render: (v: boolean) => <StatusBadge status={v} label={v ? t.common.yes : t.common.no} />,
    },
    {
      title: "QPM",
      dataIndex: "qpm",
      width: 80,
      align: "center",
      render: (v: number) => <Text code>{v}</Text>,
    },
    {
      title: "TPM",
      dataIndex: "tpm",
      width: 80,
      align: "center",
      render: (v: number) => <Text code>{v?.toLocaleString()}</Text>,
    },
    {
      title: t.keys.fieldDefaultProvider,
      dataIndex: "default_provider",
      ellipsis: true,
      render: (v: string) =>
        v ? <Tag>{v}</Tag> : <Text type="secondary">-</Text>,
    },
    {
      title: t.common.actions,
      width: 100,
      fixed: "right",
      render: (_, record) => (
        <Space size="small">
          <Tooltip title={t.keys.usage}>
            <Button
              type="text"
              size="small"
              icon={<BarChartOutlined />}
              onClick={() => openUsage(record)}
              className="text-blue-500"
            />
          </Tooltip>
          <Button
            type="text"
            size="small"
            icon={<EditOutlined />}
            onClick={() => openEdit(record)}
          />
          <Popconfirm
            title={t.keys.deleteConfirm}
            description={t.keys.deleteDesc}
            onConfirm={() => handleDelete(record.id)}
            okButtonProps={{ danger: true }}
          >
            <Button type="text" size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 mb-6">
        <Title level={4} className="!mb-0">
          {t.keys.title}
        </Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchData} loading={loading}>
            {t.common.refresh}
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            {t.keys.createKey}
          </Button>
        </Space>
      </div>

      <Card className="overflow-hidden">
        <Table
          rowKey="id"
          columns={columns}
          dataSource={data}
          loading={loading}
          pagination={false}
          size="middle"
          scroll={{ x: 550 }}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={t.empty.noKeys}
              >
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                  {t.empty.createFirstKey}
                </Button>
              </Empty>
            ),
          }}
        />
      </Card>

      {/* Key Form Modal */}
      <Modal
        title={editing ? t.keys.editKey : t.keys.createKey}
        open={modalOpen}
        onOk={newKeyValue ? () => setModalOpen(false) : handleSubmit}
        onCancel={() => setModalOpen(false)}
        okText={newKeyValue ? t.common.complete : editing ? t.common.save : t.common.create}
        cancelText={t.common.cancel}
        width={520}
        destroyOnClose
      >
        {newKeyValue ? (
          <div className="py-4">
            <Alert
              type="warning"
              showIcon
              message={t.keys.copyWarning}
              description={t.keys.copyWarningDesc}
              className="mb-4"
            />
            <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <KeyOutlined className="text-blue-500" />
                <Text strong>API Key</Text>
              </div>
              <Input.Search
                value={newKeyValue}
                readOnly
                enterButton={<CopyOutlined />}
                onSearch={handleCopyKey}
                className="font-mono"
              />
            </div>
          </div>
        ) : (
          <Form form={form} layout="vertical" className="mt-4">
            <Form.Item
              name="name"
              label={t.keys.fieldName}
              rules={[{ required: true, message: t.common.required }]}
            >
              <Input placeholder={t.keys.fieldNamePlaceholder} />
            </Form.Item>
            {editing && (
              <Form.Item name="enabled" label={t.keys.fieldEnabled} valuePropName="checked">
                <Switch />
              </Form.Item>
            )}
            <Form.Item name="default_provider" label={t.keys.fieldDefaultProvider}>
              <Input placeholder={t.keys.fieldDefaultProviderPlaceholder} />
            </Form.Item>
            <Form.Item
              name="allowed_models"
              label={t.keys.fieldAllowedModels}
              tooltip={t.keys.fieldAllowedModelsTooltip}
            >
              <Input placeholder={t.keys.fieldAllowedModelsPlaceholder} />
            </Form.Item>
            <Form.Item
              name="allowed_providers"
              label={t.keys.fieldAllowedProviders}
              tooltip={t.keys.fieldAllowedProvidersTooltip}
            >
              <Input placeholder={t.keys.fieldAllowedProvidersPlaceholder} />
            </Form.Item>
            <Form.Item
              name="qpm"
              label={t.keys.fieldQpm}
              tooltip={t.keys.fieldQpmTooltip}
            >
              <InputNumber min={0} className="w-full" />
            </Form.Item>
            <Form.Item
              name="tpm"
              label={t.keys.fieldTpm}
              tooltip={t.keys.fieldTpmTooltip}
            >
              <InputNumber min={0} className="w-full" />
            </Form.Item>
          </Form>
        )}
      </Modal>

      {/* Usage Modal */}
      {selectedKey && (
        <UsageModal
          open={usageModalOpen}
          keyId={selectedKey.id}
          keyName={selectedKey.name}
          onClose={() => {
            setUsageModalOpen(false);
            setSelectedKey(null);
          }}
        />
      )}
    </div>
  );
}
