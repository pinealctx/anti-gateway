import { getAdminKey } from "@/stores/auth";

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
  id: number;
  name: string;
  type: string;
  weight: number;
  enabled: boolean;
  base_url?: string;
  api_key?: string;
  github_tokens?: string[];
  models?: string[];
  default_model?: string;
  healthy?: boolean;
  created_at?: string;
}

export const listProviders = () =>
  request<{ providers: ProviderRecord[]; total: number }>("GET", "/admin/providers");

export const getProvider = (id: number) =>
  request<ProviderRecord>("GET", `/admin/providers/${id}`);

export const createProvider = (data: Partial<ProviderRecord>) =>
  request<ProviderRecord>("POST", "/admin/providers", data);

export const updateProvider = (id: number, data: Partial<ProviderRecord>) =>
  request<ProviderRecord>("PUT", `/admin/providers/${id}`, data);

export const deleteProvider = (id: number) =>
  request<{ deleted: boolean }>("DELETE", `/admin/providers/${id}`);

// --- API Keys ---
export interface ApiKey {
  id: number;
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

export const getKey = (id: number) =>
  request<ApiKey>("GET", `/admin/keys/${id}`);

export const createKey = (data: Partial<ApiKey>) =>
  request<ApiKey>("POST", "/admin/keys", data);

export const updateKey = (id: number, data: Partial<ApiKey>) =>
  request<ApiKey>("PUT", `/admin/keys/${id}`, data);

export const deleteKey = (id: number) =>
  request<{ deleted: boolean }>("DELETE", `/admin/keys/${id}`);

// --- Usage ---
export interface UsageRecord {
  key_id: number;
  key_name: string;
  model: string;
  requests: number;
  tokens: number;
}

export const getUsage = (params?: {
  key_id?: number;
  model?: string;
  from?: string;
  to?: string;
}) => {
  const qs = new URLSearchParams();
  if (params?.key_id) qs.set("key_id", String(params.key_id));
  if (params?.model) qs.set("model", params.model);
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

export interface CopilotAccount {
  username: string;
  token_prefix: string;
  added_at: string;
  copilot_token_expires: string;
}

export const startDeviceFlow = () =>
  request<DeviceFlowSession>("POST", "/admin/auth/device-code");

export const pollDeviceFlow = (id: string) =>
  request<{ id: string; status: string; error?: string }>("GET", `/admin/auth/poll/${id}`);

export const completeDeviceFlow = (id: string) =>
  request<{ message: string }>("POST", `/admin/auth/complete/${id}`);

export const listCopilotAccounts = () =>
  request<{ accounts: CopilotAccount[]; total: number }>("GET", "/admin/copilot/accounts");

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

export const startKiroLogin = (port?: number) =>
  request<KiroLoginSession>("POST", "/admin/kiro/login", port ? { port } : undefined);

export const getKiroLoginStatus = (id: string) =>
  request<{ id: string; status: string; error?: string }>("GET", `/admin/kiro/login/${id}`);

export const completeKiroLogin = (id: string) =>
  request<{ message: string }>("POST", `/admin/kiro/login/complete/${id}`);

export const getKiroStatus = () =>
  request<KiroStatus>("GET", "/admin/kiro/status");

export const refreshKiroToken = () =>
  request<KiroStatus>("POST", "/admin/kiro/refresh");
