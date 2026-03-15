import { getAdminKey, clearAdminKey } from "@/stores/auth";

const BASE = "";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  const key = getAdminKey();
  if (key) headers["Authorization"] = `Bearer ${key}`;

  const resp = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  if (!resp.ok) {
    // Handle 401 Unauthorized - clear auth and redirect to login
    if (resp.status === 401) {
      clearAdminKey();
      window.location.href = "/ui/login";
      throw new ApiError(resp.status, "Unauthorized");
    }
    const data = await resp.json().catch(() => ({}));
    throw new ApiError(
      resp.status,
      data?.error?.message ?? `Request failed (${resp.status})`,
    );
  }

  if (resp.status === 204) return undefined as T;
  return resp.json();
}

// --- Health ---
export const getHealth = () => request<{ status: string; version: string }>("GET", "/health");

// --- Providers ---
export interface ProviderRecord {
  id: string;
  name: string;
  type: string;
  weight: number;
  enabled: boolean;
  base_url?: string;
  api_key?: string;
  github_token?: string;
  models?: string[];
  default_model?: string;
  healthy?: boolean;
  created_at?: string;
}

export const listProviders = () =>
  request<{ providers: ProviderRecord[]; total: number }>("GET", "/admin/providers");

export const getProvider = (id: string) =>
  request<ProviderRecord>("GET", `/admin/providers/${id}`);

export const createProvider = (data: Partial<ProviderRecord>) =>
  request<ProviderRecord>("POST", "/admin/providers", data);

export const updateProvider = (id: string, data: Partial<ProviderRecord>) =>
  request<ProviderRecord>("PUT", `/admin/providers/${id}`, data);

export const deleteProvider = (id: string) =>
  request<{ deleted: boolean }>("DELETE", `/admin/providers/${id}`);

// --- API Keys ---
export interface ApiKey {
  id: string;
  key?: string;
  key_prefix?: string;
  name: string;
  enabled: boolean;
  allowed_models?: string[];
  allowed_providers?: string[];
  default_provider?: string;
  qpm?: number;
  tpm?: number;
  created_at?: string;
}

export const listKeys = () =>
  request<{ keys: ApiKey[]; total: number }>("GET", "/admin/keys");

export const getKey = (id: string) =>
  request<ApiKey>("GET", `/admin/keys/${id}`);

export const createKey = (data: Partial<ApiKey>) =>
  request<ApiKey>("POST", "/admin/keys", data);

export const updateKey = (id: string, data: Partial<ApiKey>) =>
  request<ApiKey>("PUT", `/admin/keys/${id}`, data);

export const deleteKey = (id: string) =>
  request<{ deleted: boolean }>("DELETE", `/admin/keys/${id}`);

// --- Usage ---
export interface UsageRecord {
  key_id: string;
  key_name: string;
  model?: string;
  provider?: string;
  total_requests: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
}

export const getUsage = (params?: {
  key_id?: string;
  model?: string;
  provider?: string;
  group_by?: "key" | "model" | "provider" | "key_model" | "key_provider";
  from?: string;
  to?: string;
}) => {
  const qs = new URLSearchParams();
  if (params?.key_id) qs.set("key_id", params.key_id);
  if (params?.model) qs.set("model", params.model);
  if (params?.provider) qs.set("provider", params.provider);
  if (params?.group_by) qs.set("group_by", params.group_by);
  if (params?.from) qs.set("from", params.from);
  if (params?.to) qs.set("to", params.to);
  const q = qs.toString();
  return request<{ usage: UsageRecord[] }>("GET", `/admin/usage${q ? `?${q}` : ""}`);
};

// --- Copilot ---
export interface DeviceFlowSession {
  id: string;
  user_code: string;
  verification_uri: string;
  expires_at: string;
  interval: number;
  status: string;
}

export interface CopilotStatus {
  username?: string;
  healthy: boolean;
  has_token: boolean;
  token_expires?: string;
}

export const startDeviceFlow = (provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<DeviceFlowSession>("POST", `/admin/auth/device-code${qs}`);
};

export const pollDeviceFlow = (id: string, provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<{ id: string; status: string; error?: string }>("GET", `/admin/auth/poll/${id}${qs}`);
};

export const completeDeviceFlow = (id: string, provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<{ message: string; provider: string }>("POST", `/admin/auth/complete/${id}${qs}`);
};

export const getCopilotStatus = (provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<CopilotStatus>("GET", `/admin/copilot/status${qs}`);
};

// --- Kiro ---
export interface KiroLoginSession {
  id: string;
  auth_url: string;
  port: number;
  status: string;
}

export interface KiroStatus {
  has_login: boolean;
  has_current: boolean;
  is_external_idp: boolean;
  expires_at?: string;
}

export const startKiroLogin = (provider?: string, port?: number) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<KiroLoginSession>("POST", `/admin/kiro/login${qs}`, port ? { port } : undefined);
};

export const getKiroLoginStatus = (id: string, provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<{ id: string; status: string; error?: string }>("GET", `/admin/kiro/login/${id}${qs}`);
};

export const completeKiroLogin = (id: string, provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<{ message: string; provider: string }>("POST", `/admin/kiro/login/complete/${id}${qs}`);
};

export const getKiroStatus = (provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<KiroStatus>("GET", `/admin/kiro/status${qs}`);
};

export const refreshKiroToken = (provider?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<KiroStatus>("POST", `/admin/kiro/refresh${qs}`);
};

export const importKiroLocal = (provider?: string, dbPath?: string) => {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
  return request<{ message: string; provider: string; is_external_idp: boolean; has_refresh: boolean; profile_arn: string; expires_at?: string }>(
    "POST",
    `/admin/kiro/import-local${qs}`,
    dbPath ? { db_path: dbPath } : undefined
  );
};
