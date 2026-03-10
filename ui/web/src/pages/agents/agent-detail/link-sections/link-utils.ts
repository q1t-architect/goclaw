import type { AgentLinkData, AgentLinkSettings } from "@/types/agent";

export const DIRECTION_OPTIONS = [
  { value: "outbound", label: "Outbound", desc: "This agent can delegate to the target" },
  { value: "inbound", label: "Inbound", desc: "The target can delegate to this agent" },
  { value: "bidirectional", label: "Bidirectional", desc: "Both agents can delegate to each other" },
] as const;

export function directionBadgeVariant(direction: string) {
  switch (direction) {
    case "outbound": return "info" as const;
    case "inbound": return "warning" as const;
    case "bidirectional": return "success" as const;
    default: return "outline" as const;
  }
}

export function parseCommaSeparated(val: string): string[] {
  return val.split(",").map((s) => s.trim()).filter(Boolean);
}

export function joinArray(arr?: string[]): string {
  return (arr ?? []).join(", ");
}

export function linkTargetName(link: AgentLinkData, currentAgentId: string): string {
  if (link.source_agent_id === currentAgentId) {
    return link.target_display_name || link.target_agent_key || link.target_agent_id;
  }
  return link.source_agent_key || link.source_agent_id;
}

export function effectiveDirection(link: AgentLinkData, currentAgentId: string): string {
  if (link.direction === "bidirectional") return "bidirectional";
  if (link.source_agent_id === currentAgentId) return link.direction;
  return link.direction === "outbound" ? "inbound" : "outbound";
}

export function buildSettings(userAllow: string, userDeny: string): AgentLinkSettings | undefined {
  const allow = parseCommaSeparated(userAllow);
  const deny = parseCommaSeparated(userDeny);
  if (allow.length === 0 && deny.length === 0) return undefined;
  return {
    ...(allow.length > 0 ? { user_allow: allow } : {}),
    ...(deny.length > 0 ? { user_deny: deny } : {}),
  };
}
