export interface ChannelInstanceData {
  id: string;
  name: string;
  display_name: string;
  channel_type: string;
  agent_id: string;
  config: Record<string, unknown> | null;
  enabled: boolean;
  is_default: boolean;
  has_credentials: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ChannelRuntimeStatus {
  enabled: boolean;
  running: boolean;
  state?:
    | "registered"
    | "starting"
    | "healthy"
    | "degraded"
    | "failed"
    | "stopped";
  summary?: string;
  detail?: string;
  failure_kind?: "auth" | "config" | "network" | "unknown";
  retryable?: boolean;
  checked_at?: string;
  failure_count?: number;
  consecutive_failures?: number;
  first_failed_at?: string;
  last_failed_at?: string;
  last_healthy_at?: string;
  remediation?: {
    code: "reauth" | "open_credentials" | "open_advanced" | "check_network";
    headline: string;
    hint?: string;
    target?: "credentials" | "advanced" | "reauth" | "details";
  };
}

export interface ChannelInstanceInput {
  name: string;
  display_name?: string;
  channel_type: string;
  agent_id: string;
  credentials?: Record<string, unknown>;
  config?: Record<string, unknown>;
  enabled?: boolean;
}

export interface ChannelContextData {
  scope_type: "channel" | "group" | "user" | "role";
  scope_key: string;
  display_name: string;
  source: string;
  live_members_supported: boolean;
  member_count?: number;
  last_seen_at?: string;
}

export interface ChannelContextMember {
  platform_id: string;
  user_id?: string;
  display_name?: string;
  username?: string;
  source: string;
  last_seen_at?: string;
}

export interface ChannelCapability {
  type: "mcp_server" | "secure_cli";
  id: string;
  name: string;
  display_name?: string;
  enabled: boolean;
  source: string;
  tool_allow?: string[];
  tool_deny?: string[];
  credential_source: string;
  has_credential: boolean;
  context_grant_configured: boolean;
  context_credentials_configured: boolean;
}
