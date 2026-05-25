const BASE = ""

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, init)
  if (!res.ok) {
    const message = (await res.text()).trim()
    throw new Error(message || `${res.status} ${res.statusText}`)
  }
  return res.json()
}

export interface HashrateData {
  current: number
  average: number
  max: number
}

export interface Miner {
  id: string
  miner_id: string
  worker_id: string
  hostname: string
  ip: string
  cpu_model: string
  cpu_family: string
  cores: number
  os: string
  arch: string
  xmrig_version: string
  tarish_version: string
  uptime_seconds: number
  hashrate: HashrateData | null
  config: Record<string, unknown> | null
  last_seen: string
  status: string
}

export interface Overview {
  total_hashrate: number
  average_hashrate: number
  active_miners: number
  total_miners: number
  top_miners: Miner[]
}

export interface HashrateHistory {
  miner_id: string
  timestamp: string
  current: number
  average: number
  max: number
}

export interface GuideLink {
  label: string
  url: string
}

export interface GuideDocument {
  id: string
  slug: string
  title: string
  summary: string
  category: "general" | "script" | "terminal" | string
  content_type: "text" | "code" | string
  content: string
  links: GuideLink[]
  created_at: string
  updated_at: string
  revision_count: number
  can_rollback: boolean
}

export interface GuideDocumentInput {
  title: string
  summary: string
  category: string
  content_type: string
  content: string
  links: GuideLink[]
}

export interface GuideEditChallenge {
  challenge_id: string
  prompt: string
  expires_at: string
}

export interface GuideEditSession {
  token: string
  expires_at: string
}

export interface BarkSettingsView {
  enabled: boolean
  token_set: boolean
  token_last4?: string
  check_interval_s: number
  throttle_minutes: number
  notify_recovery: boolean
  mute_until?: string | null
  mute_forever?: boolean
  updated_at: string
}

export interface BarkSettingsInput {
  enabled?: boolean
  token?: string
  check_interval_s?: number
  throttle_minutes?: number
  notify_recovery?: boolean
}

export interface AlertLogEntry {
  id: number
  miner_id?: string
  kind: "offline" | "recovery" | "test" | "offline_suppressed" | string
  title: string
  body: string
  ok: boolean
  error?: string
  created_at: string
}

export const api = {
  getOverview: () => fetchJSON<Overview>("/api/overview"),
  getMiners: () => fetchJSON<Miner[]>("/api/miners"),
  getMiner: (id: string) => fetchJSON<Miner>(`/api/miners/${encodeURIComponent(id)}`),
  deleteMiner: (id: string) =>
    fetchJSON<{ ok: boolean }>(`/api/miners/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  setConfig: (id: string, config: Record<string, unknown>) =>
    fetchJSON<{ ok: boolean }>(`/api/miners/${encodeURIComponent(id)}/config`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(config),
    }),
  deleteConfig: (id: string) =>
    fetchJSON<{ ok: boolean }>(`/api/miners/${encodeURIComponent(id)}/config`, {
      method: "DELETE",
    }),
  getHashrateHistory: (minerID?: string, hours = 24) => {
    const params = new URLSearchParams({ hours: String(hours) })
    if (minerID) params.set("miner_id", minerID)
    return fetchJSON<HashrateHistory[]>(`/api/hashrate/history?${params}`)
  },
  getGuideDocuments: () => fetchJSON<GuideDocument[]>("/api/guides/documents"),
  startGuideEditChallenge: () =>
    fetchJSON<GuideEditChallenge>("/api/guides/edit-challenge", {
      method: "POST",
    }),
  createGuideEditSession: (challenge_id: string, answer: string) =>
    fetchJSON<GuideEditSession>("/api/guides/edit-session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ challenge_id, answer }),
    }),
  createGuideDocument: (input: GuideDocumentInput, token: string) =>
    fetchJSON<GuideDocument>("/api/guides/documents", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Guide-Edit-Token": token,
      },
      body: JSON.stringify({ ...input, confirmed: true }),
    }),
  updateGuideDocument: (id: string, input: GuideDocumentInput, token: string) =>
    fetchJSON<GuideDocument>(`/api/guides/documents/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-Guide-Edit-Token": token,
      },
      body: JSON.stringify({ ...input, confirmed: true }),
    }),
  rollbackGuideDocument: (id: string, token: string) =>
    fetchJSON<GuideDocument>(`/api/guides/documents/${encodeURIComponent(id)}/rollback`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Guide-Edit-Token": token,
      },
      body: JSON.stringify({ confirmed: true }),
    }),
  getBarkSettings: () => fetchJSON<BarkSettingsView>("/api/settings/bark"),
  updateBarkSettings: (input: BarkSettingsInput) =>
    fetchJSON<BarkSettingsView>("/api/settings/bark", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
    }),
  testBark: (token?: string) =>
    fetchJSON<{ ok: boolean; error?: string }>("/api/settings/bark/test", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(token ? { token } : {}),
    }),
  setBarkMute: (req: { minutes?: number; permanent?: boolean }) =>
    fetchJSON<BarkSettingsView>("/api/settings/bark/mute", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
    }),
  clearBarkMute: () =>
    fetchJSON<BarkSettingsView>("/api/settings/bark/mute", {
      method: "DELETE",
    }),
  getRecentAlerts: (limit = 25) =>
    fetchJSON<AlertLogEntry[]>(`/api/settings/alerts/recent?limit=${limit}`),
}
