const BASE = ""

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, init)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
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

export const api = {
  getOverview: () => fetchJSON<Overview>("/api/overview"),
  getMiners: () => fetchJSON<Miner[]>("/api/miners"),
  getMiner: (id: string) => fetchJSON<Miner>(`/api/miners/${encodeURIComponent(id)}`),
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
}
