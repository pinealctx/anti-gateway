import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Card, Row, Col, Typography, Skeleton, Empty, Tag } from "antd";
import {
  CloudServerOutlined,
  KeyOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  ThunderboltOutlined,
  BarChartOutlined,
} from "@ant-design/icons";
import { getHealth, listProviders, listKeys, getUsage } from "@/services/api";
import { useT } from "@/locales";

const { Title, Text } = Typography;

// Stat Card Component
interface StatCardProps {
  title: string;
  value: string | number;
  icon: React.ReactNode;
  suffix?: React.ReactNode;
  loading?: boolean;
  iconColor?: string;
}

function StatCard({ title, value, icon, suffix, loading, iconColor = "#2563eb" }: StatCardProps) {
  return (
    <Card className="h-full hover:shadow-md transition-shadow duration-200">
      {loading ? (
        <div className="space-y-3">
          <Skeleton.Input active size="small" style={{ width: 80 }} />
          <Skeleton.Input active size="small" style={{ width: 120 }} />
        </div>
      ) : (
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <Text type="secondary" className="text-sm">
              {title}
            </Text>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-semibold">{value}</span>
              {suffix}
            </div>
          </div>
          <div
            className="w-12 h-12 rounded-xl flex items-center justify-center"
            style={{ backgroundColor: `${iconColor}15` }}
          >
            <span style={{ color: iconColor, fontSize: 24 }}>{icon}</span>
          </div>
        </div>
      )}
    </Card>
  );
}

// Skeleton Card
function SkeletonCard() {
  return (
    <Card className="h-full">
      <div className="space-y-3">
        <Skeleton.Input active size="small" style={{ width: 80 }} />
        <div className="flex items-center gap-2">
          <Skeleton.Avatar active size={48} shape="square" />
          <Skeleton.Input active size="small" style={{ width: 100 }} />
        </div>
      </div>
    </Card>
  );
}

export default function DashboardPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [version, setVersion] = useState("");
  const [providerCount, setProviderCount] = useState(0);
  const [healthyCount, setHealthyCount] = useState(0);
  const [keyCount, setKeyCount] = useState(0);
  const [totalRequests, setTotalRequests] = useState(0);
  const [totalTokens, setTotalTokens] = useState(0);
  const [error, setError] = useState(false);
  const t = useT();

  useEffect(() => {
    (async () => {
      try {
        const [health, providers, keys, usage] = await Promise.all([
          getHealth(),
          listProviders(),
          listKeys(),
          getUsage(),
        ]);
        setVersion(health.version);
        setProviderCount(providers.total);
        setHealthyCount(providers.providers.filter((p) => p.healthy).length);
        setKeyCount(keys.total);

        // Calculate usage totals
        const usageData = usage.usage ?? [];
        const requests = usageData.reduce((acc, item) => acc + (item.requests || 0), 0);
        const tokens = usageData.reduce((acc, item) => acc + (item.tokens || 0), 0);
        setTotalRequests(requests);
        setTotalTokens(tokens);
      } catch {
        setError(true);
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  if (error) {
    return (
      <div>
        <Title level={4} className="!mb-6">
          {t.dashboard.title}
        </Title>
        <Empty description={t.empty.loadFailed} className="py-12" />
      </div>
    );
  }

  const unhealthyCount = providerCount - healthyCount;

  const quickNavItems = [
    {
      key: "/providers",
      icon: <CloudServerOutlined />,
      label: t.dashboard.manageProviders,
      desc: t.dashboard.manageProvidersDesc,
    },
    {
      key: "/keys",
      icon: <KeyOutlined />,
      label: t.dashboard.manageKeys,
      desc: t.dashboard.manageKeysDesc,
    },
    {
      key: "/keys",
      icon: <BarChartOutlined />,
      label: t.dashboard.viewUsage,
      desc: t.dashboard.viewUsageDesc,
    },
  ];

  return (
    <div>
      <Title level={4} className="!mb-6">
        {t.dashboard.title}
      </Title>

      {/* Main Stats */}
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          {loading ? (
            <SkeletonCard />
          ) : (
            <StatCard
              title={t.dashboard.version}
              value={version || "-"}
              icon={<ThunderboltOutlined />}
              iconColor="#3b82f6"
              suffix={
                <Tag color="blue" className="ml-1">
                  {t.dashboard.latest}
                </Tag>
              }
            />
          )}
        </Col>

        <Col xs={24} sm={12} lg={6}>
          {loading ? (
            <SkeletonCard />
          ) : (
            <StatCard
              title={t.dashboard.providers}
              value={providerCount}
              icon={<CloudServerOutlined />}
              iconColor="#8b5cf6"
              suffix={
                <span className="text-sm text-gray-400 flex items-center gap-1">
                  <span className="flex items-center gap-1 text-green-500">
                    <CheckCircleOutlined />
                    {healthyCount}
                  </span>
                  {unhealthyCount > 0 && (
                    <span className="flex items-center gap-1 text-red-400 ml-1">
                      <CloseCircleOutlined />
                      {unhealthyCount}
                    </span>
                  )}
                </span>
              }
            />
          )}
        </Col>

        <Col xs={24} sm={12} lg={6}>
          {loading ? (
            <SkeletonCard />
          ) : (
            <StatCard
              title={t.dashboard.apiKeys}
              value={keyCount}
              icon={<KeyOutlined />}
              iconColor="#f97316"
            />
          )}
        </Col>

        <Col xs={24} sm={12} lg={6}>
          {loading ? (
            <SkeletonCard />
          ) : (
            <StatCard
              title={t.dashboard.healthRate}
              value={
                providerCount > 0
                  ? `${Math.round((healthyCount / providerCount) * 100)}%`
                  : "-"
              }
              icon={<CheckCircleOutlined />}
              iconColor={
                healthyCount === providerCount && providerCount > 0
                  ? "#10b981"
                  : "#f59e0b"
              }
            />
          )}
        </Col>
      </Row>

      {/* Usage Stats */}
      <Title level={5} className="!mb-4 !mt-8">
        {t.dashboard.usageStats}
      </Title>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12}>
          {loading ? (
            <SkeletonCard />
          ) : (
            <StatCard
              title={t.dashboard.totalRequests}
              value={totalRequests.toLocaleString()}
              icon={<BarChartOutlined />}
              iconColor="#06b6d4"
            />
          )}
        </Col>
        <Col xs={24} sm={12}>
          {loading ? (
            <SkeletonCard />
          ) : (
            <StatCard
              title={t.dashboard.totalTokens}
              value={totalTokens.toLocaleString()}
              icon={<ThunderboltOutlined />}
              iconColor="#10b981"
            />
          )}
        </Col>
      </Row>

      {/* Quick Actions */}
      <Title level={5} className="!mb-4 !mt-8">
        {t.dashboard.quickNav}
      </Title>
      <Card>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          {quickNavItems.map((item, index) => (
            <Card
              key={index}
              hoverable
              className="cursor-pointer transition-all duration-200 hover:shadow-md"
              onClick={() => navigate(item.key)}
            >
              <div className="flex items-start gap-3">
                <div className="w-10 h-10 rounded-lg bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center text-blue-500">
                  {item.icon}
                </div>
                <div>
                  <Text strong>{item.label}</Text>
                  <br />
                  <Text type="secondary" className="text-xs">
                    {item.desc}
                  </Text>
                </div>
              </div>
            </Card>
          ))}
        </div>
      </Card>
    </div>
  );
}
