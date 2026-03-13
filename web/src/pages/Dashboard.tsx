import { useEffect, useState } from "react";
import { Card, Statistic, Row, Col, Spin, Tag, Typography } from "antd";
import {
  CloudServerOutlined,
  KeyOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
} from "@ant-design/icons";
import { getHealth, listProviders, listKeys } from "@/services/api";

export default function DashboardPage() {
  const [loading, setLoading] = useState(true);
  const [version, setVersion] = useState("");
  const [providerCount, setProviderCount] = useState(0);
  const [healthyCount, setHealthyCount] = useState(0);
  const [keyCount, setKeyCount] = useState(0);

  useEffect(() => {
    (async () => {
      try {
        const [health, providers, keys] = await Promise.all([
          getHealth(),
          listProviders(),
          listKeys(),
        ]);
        setVersion(health.version);
        setProviderCount(providers.total);
        setHealthyCount(providers.providers.filter((p) => p.healthy).length);
        setKeyCount(keys.total);
      } catch {
        // ignore
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div>
      <Typography.Title level={4} className="!mb-6">
        仪表盘
      </Typography.Title>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="版本"
              value={version || "-"}
              prefix={<Tag color="blue">v</Tag>}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="Provider"
              value={providerCount}
              prefix={<CloudServerOutlined />}
              suffix={
                <span className="text-sm text-gray-400">
                  {" "}
                  ({healthyCount} <CheckCircleOutlined className="text-green-500" /> /{" "}
                  {providerCount - healthyCount} <CloseCircleOutlined className="text-red-400" />)
                </span>
              }
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="API Keys" value={keyCount} prefix={<KeyOutlined />} />
          </Card>
        </Col>
      </Row>
    </div>
  );
}
