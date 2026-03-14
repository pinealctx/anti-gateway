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
  Empty,
  Tooltip,
  Card,
} from "antd";
import {
  PlusOutlined,
  ReloadOutlined,
  EditOutlined,
  DeleteOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  GithubOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  type ProviderRecord,
} from "@/services/api";
import CopilotAuthModal from "@/components/CopilotAuthModal";
import KiroAuthModal from "@/components/KiroAuthModal";
import { useT } from "@/locales";

const { Title, Text } = Typography;

const PROVIDER_TYPES = [
  { label: "Kiro", value: "kiro" },
  { label: "OpenAI", value: "openai" },
  { label: "OpenAI Compatible", value: "openai-compat" },
  { label: "Copilot", value: "copilot" },
  { label: "Anthropic", value: "anthropic" },
];

// Status Badge Component
function StatusBadge({ status, label }: { status: boolean; label: string }) {
  return (
    <span className={`status-badge ${status ? "success" : "error"}`}>
      {label}
    </span>
  );
}

export default function ProvidersPage() {
  const [data, setData] = useState<ProviderRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ProviderRecord | null>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  const t = useT();

  // Auth modal states
  const [copilotModalOpen, setCopilotModalOpen] = useState(false);
  const [kiroModalOpen, setKiroModalOpen] = useState(false);
  const [selectedProvider, setSelectedProvider] = useState<ProviderRecord | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listProviders();
      setData(res.providers);
    } catch {
      message.error(t.providers.loadError);
    } finally {
      setLoading(false);
    }
  }, [message, t]);

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
    });
    setModalOpen(true);
  };

  const openCopilotAuth = (record: ProviderRecord) => {
    setSelectedProvider(record);
    setCopilotModalOpen(true);
  };

  const openKiroAuth = (record: ProviderRecord) => {
    setSelectedProvider(record);
    setKiroModalOpen(true);
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

      const payload = { ...values, models };

      if (editing) {
        await updateProvider(editing.id, payload);
        message.success(t.providers.updateSuccess);
      } else {
        await createProvider(payload);
        message.success(t.providers.createSuccess);
      }
      setModalOpen(false);
      fetchData();
    } catch {
      // validation error, ignore
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteProvider(id);
      message.success(t.providers.deleteSuccess);
      fetchData();
    } catch {
      message.error(t.providers.deleteError);
    }
  };

  const columns: ColumnsType<ProviderRecord> = [
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
      title: t.common.type,
      dataIndex: "type",
      width: 110,
      render: (type: string) => (
        <Tag color="blue" className="font-mono text-xs">
          {type}
        </Tag>
      ),
    },
    {
      title: t.providers.fieldWeight,
      dataIndex: "weight",
      width: 80,
      align: "center",
      render: (v: number) => <Text code>{v}</Text>,
    },
    {
      title: t.common.enabled,
      dataIndex: "enabled",
      width: 80,
      align: "center",
      render: (v: boolean) => <StatusBadge status={v} label={v ? t.common.yes : t.common.no} />,
    },
    {
      title: t.common.status,
      dataIndex: "healthy",
      width: 80,
      align: "center",
      render: (v: boolean) =>
        v ? (
          <Tooltip title={t.providers.healthy}>
            <CheckCircleOutlined className="text-green-500 text-lg" />
          </Tooltip>
        ) : (
          <Tooltip title={t.providers.unhealthy}>
            <CloseCircleOutlined className="text-red-400 text-lg" />
          </Tooltip>
        ),
    },
    {
      title: t.providers.fieldModels,
      dataIndex: "models",
      ellipsis: true,
      render: (models: string[]) =>
        models?.length ? (
          <div className="flex flex-wrap gap-1">
            {models.slice(0, 3).map((m) => (
              <Tag key={m} className="text-xs">
                {m}
              </Tag>
            ))}
            {models.length > 3 && (
              <Tag className="text-xs">+{models.length - 3}</Tag>
            )}
          </div>
        ) : (
          <Tag>{t.common.all}</Tag>
        ),
    },
    {
      title: t.common.actions,
      ellipsis: true,
      fixed: "right",
      render: (_, record) => (
        <Space size="small">
          {/* Type-specific auth actions */}
          {record.type === "copilot" && (
            <Tooltip title={t.copilot.authorize}>
              <Button
                type="link"
                size="small"
                icon={<GithubOutlined />}
                onClick={() => openCopilotAuth(record)}
                className="text-purple-500"
              >
                {t.providers.authorize}
              </Button>
            </Tooltip>
          )}
          {record.type === "kiro" && (
            <Tooltip title={t.kiro.pkceLogin}>
              <Button
                type="link"
                size="small"
                icon={<ThunderboltOutlined />}
                onClick={() => openKiroAuth(record)}
                className="text-orange-500"
              >
                {t.providers.authorize}
              </Button>
            </Tooltip>
          )}
          <Button
            type="text"
            size="small"
            icon={<EditOutlined />}
            onClick={() => openEdit(record)}
          />
          <Popconfirm
            title={t.providers.deleteConfirm}
            description={t.providers.deleteDesc}
            onConfirm={() => handleDelete(record.id)}
            okButtonProps={{ danger: true }}
          >
            <Button type="text" size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const selectedType = Form.useWatch("type", form);

  return (
    <div>
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 mb-6">
        <Title level={4} className="!mb-0">
          {t.providers.title}
        </Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchData} loading={loading}>
            {t.common.refresh}
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            {t.providers.addProvider}
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
          scroll={{ x: 600 }}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={t.empty.noProviders}
              >
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                  {t.empty.createFirstProvider}
                </Button>
              </Empty>
            ),
          }}
        />
      </Card>

      {/* Provider Form Modal */}
      <Modal
        title={editing ? t.providers.editProvider : t.providers.addProvider}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
        okText={editing ? t.common.save : t.common.create}
        cancelText={t.common.cancel}
        width={560}
        destroyOnClose
      >
        <Form form={form} layout="vertical" className="mt-4">
          <Form.Item
            name="name"
            label={t.providers.fieldName}
            rules={[{ required: true, message: t.common.required }]}
          >
            <Input placeholder={t.providers.fieldNamePlaceholder} disabled={!!editing} />
          </Form.Item>
          <Form.Item
            name="type"
            label={t.providers.fieldType}
            rules={[{ required: true, message: t.common.required }]}
          >
            <Select options={PROVIDER_TYPES} placeholder={t.providers.fieldTypePlaceholder} disabled={!!editing} />
          </Form.Item>
          <Form.Item name="weight" label={t.providers.fieldWeight} tooltip={t.providers.fieldWeightTooltip}>
            <InputNumber min={0} max={10000} className="w-full" />
          </Form.Item>
          {editing && (
            <Form.Item name="enabled" label={t.providers.fieldEnabled} valuePropName="checked">
              <Switch />
            </Form.Item>
          )}
          {(selectedType === "openai" ||
            selectedType === "openai-compat" ||
            selectedType === "anthropic") && (
            <>
              <Form.Item name="base_url" label={t.providers.fieldBaseUrl}>
                <Input placeholder={t.providers.fieldBaseUrlPlaceholder} />
              </Form.Item>
              <Form.Item name="api_key" label={t.providers.fieldApiKey}>
                <Input.Password placeholder={t.providers.fieldApiKeyPlaceholder} />
              </Form.Item>
            </>
          )}
          {selectedType === "copilot" && (
            <Form.Item
              name="github_token"
              label={t.providers.fieldGithubToken}
            >
              <Input.Password placeholder={t.providers.fieldGithubTokenPlaceholder} />
            </Form.Item>
          )}
          <Form.Item name="models" label={t.providers.fieldModels}>
            <Input placeholder={t.providers.fieldModelsPlaceholder} />
          </Form.Item>
          <Form.Item name="default_model" label={t.providers.fieldDefaultModel}>
            <Input placeholder={t.providers.fieldDefaultModelPlaceholder} />
          </Form.Item>
        </Form>
      </Modal>

      {/* Copilot Auth Modal */}
      {selectedProvider?.type === "copilot" && (
        <CopilotAuthModal
          open={copilotModalOpen}
          providerName={selectedProvider.name}
          onClose={() => {
            setCopilotModalOpen(false);
            setSelectedProvider(null);
            fetchData();
          }}
        />
      )}

      {/* Kiro Auth Modal */}
      {selectedProvider?.type === "kiro" && (
        <KiroAuthModal
          open={kiroModalOpen}
          providerName={selectedProvider.name}
          onClose={() => {
            setKiroModalOpen(false);
            setSelectedProvider(null);
            fetchData();
          }}
        />
      )}
    </div>
  );
}
