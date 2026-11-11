// Thin fetch wrapper for /admin/api. Sends the CSRF header the server requires.

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const resp = await fetch(`/admin/api${path}`, {
    method: init.method ?? "GET",
    headers: { "Content-Type": "application/json", "X-Requested-With": "rdm" },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
    credentials: "same-origin",
  });
  if (resp.status === 204) return undefined as T;
  const text = await resp.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!resp.ok) {
    if (resp.status === 401 && !path.startsWith("/auth/")) window.dispatchEvent(new Event("rdm:unauthorized"));
    throw new ApiError(resp.status, data?.error ?? `HTTP ${resp.status}`);
  }
  return data as T;
}

export function qs(params: Record<string, string | number | undefined>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") p.set(k, String(v));
  const s = p.toString();
  return s ? `?${s}` : "";
}

export type Paged<T> = { total: number; data: T[] };
export type Ref = { id: number; name: string };
export type User = { id: number; name: string; display_name: string; email: string; is_admin: boolean; totp_enabled: boolean };

export type ServerSettings = {
  require_registered: boolean;
  accept_recordings: boolean;
  public_url: string;
  oidc_redirect_url: string;
};

export type OIDCProvider = {
  id: number;
  name: string;
  issuer: string;
  client_id: string;
  has_secret: boolean;
  scopes: string;
  auto_create: boolean;
  trust_email: boolean;
  enabled: boolean;
  created_at: string;
};

export type DeployToken = { id: number; name: string; prefix: string; expires_at: string | null; created_at: string };

export type Recording = {
  id: number;
  file: string;
  device_id: string;
  device_name: string;
  incoming: boolean;
  size: number;
  completed: boolean;
  created_at: string;
};

export type Device = {
  id: number;
  rustdesk_id: string;
  hostname: string;
  reported_hostname: string;
  custom_hostname: string;
  os: string;
  cpu: string;
  memory: string;
  username: string;
  version: string;
  note: string;
  group: Ref | null;
  user: Ref | null;
  strategy: Ref | null;
  online: boolean;
  active_conns: number[];
  last_seen_at: string | null;
  last_ip: string;
  created_at: string;
};

export type DeviceGroup = { id: number; name: string; note: string; device_count: number; strategy: Ref | null; created_at: string };

export type Session = {
  id: number;
  guid: string;
  device_id: string;
  device_name: string;
  conn_id: number;
  ip: string;
  peer_id: string;
  peer_name: string;
  type: number;
  primary_auth: number;
  two_factor: number;
  note: string;
  started_at: string;
  authed_at: string | null;
  closed_at: string | null;
};

export type FileAudit = {
  id: number;
  device_id: string;
  device_name: string;
  peer_id: string;
  type: number;
  path: string;
  is_file: boolean;
  info: { ip?: string; name?: string; num?: number; files?: [string, number][] } | string;
  created_at: string;
};

export type Alarm = {
  id: number;
  device_id: string;
  device_name: string;
  type: number;
  info: Record<string, unknown> | string;
  created_at: string;
};

export type AdminUser = {
  id: number;
  name: string;
  display_name: string;
  email: string;
  note: string;
  is_admin: boolean;
  totp_enabled: boolean;
  status: number; // 1 enabled, 0 disabled
  group: Ref | null;
  device_count: number;
  last_login_at: string | null;
  created_at: string;
};

export type UserGroup = {
  id: number;
  name: string;
  note: string;
  member_count: number;
  device_groups: Ref[];
  created_at: string;
};

export type LoginSession = {
  id: number;
  kind: "client" | "web";
  device_id: string;
  device_info?: { os?: string; type?: string; name?: string };
  user_agent?: string;
  created_at: string;
  last_used_at: string;
  current: boolean;
};

export type AbShare = { id: number; user: Ref | null; user_group: Ref | null; rule: number };

export type AddressBook = {
  id: number;
  guid: string;
  name: string;
  personal: boolean;
  owner: Ref;
  note: string;
  peer_count: number;
  shares: AbShare[];
  updated_at: string;
};

export type AbTag = { name: string; color: number };

export type AbPeer = {
  id: string;
  username: string;
  hostname: string;
  platform: string;
  alias: string;
  tags: string[];
  note: string;
  loginName: string;
  device_group_name: string;
  same_server: boolean;
  has_password: boolean;
};

export type Strategy = {
  id: number;
  name: string;
  is_default: boolean;
  options: Record<string, string>;
  device_count: number;
  groups: Ref[];
  updated_at: string;
};

export type EffectiveStrategy = {
  strategy: Ref | null;
  source: "device" | "group" | "default" | "none";
  group?: Ref;
  status: "applied" | "pending" | "unknown";
};
