import { useState } from "react"
import { useParams, Link, useNavigate } from "react-router-dom"
import { usePoll } from "@/hooks/use-poll"
import { api, type Miner, type HashrateHistory, type SnellCatalog } from "@/lib/api"
import { formatHashrate, formatUptime, formatTimeAgo, displayName, friendlyCPU } from "@/lib/utils"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { ArrowLeft, Cpu, Globe, HardDrive, Clock, Gauge, Trash2 } from "lucide-react"
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts"
import ConfigEditor from "@/components/ConfigEditor"

export default function MinerDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data: miner, refresh } = usePoll<Miner>(() => api.getMiner(id!), 10000)
  const { data: history } = usePoll<HashrateHistory[]>(() => api.getHashrateHistory(id, 6), 30000)
  const { data: catalog } = usePoll<SnellCatalog>(() => api.getSnellCatalog(), 15000)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [snellBusy, setSnellBusy] = useState(false)

  if (!miner) {
    return <div className="flex h-64 items-center justify-center text-muted-foreground">Loading...</div>
  }

  const chartData = (history ?? [])
    .filter(h => h.miner_id === id)
    .map(h => ({
      time: new Date(h.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
      current: h.current,
      average: h.average,
    }))

  const handleDeleteMiner = async () => {
    const confirmed = window.confirm(
      `Delete ${displayName(miner)} from the dashboard? This also removes its stored hashrate history and pending config overrides.`
    )
    if (!confirmed) return

    setDeleting(true)
    setDeleteError(null)
    try {
      await api.deleteMiner(miner.id)
      navigate("/miners")
    } catch (error) {
      setDeleteError(error instanceof Error ? error.message : "Failed to delete miner")
      setDeleting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/miners" className="rounded-md p-2 hover:bg-secondary transition-colors">
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{displayName(miner)}</h1>
          <p className="text-muted-foreground">{miner.ip} &middot; {miner.miner_id}</p>
        </div>
        <div className="ml-auto flex items-center gap-3">
          <Button variant="destructive" size="sm" onClick={handleDeleteMiner} disabled={deleting}>
            <Trash2 className="mr-2 h-4 w-4" />
            {deleting ? "Deleting..." : "Delete Miner"}
          </Button>
          <Badge variant={miner.status === "online" ? "success" : miner.status === "stale" ? "warning" : "destructive"}>
            {miner.status}
          </Badge>
        </div>
      </div>

      {deleteError ? (
        <Card className="border-destructive/40 bg-destructive/5">
          <CardContent className="py-4 text-sm text-destructive">
            Failed to delete miner: {deleteError}
          </CardContent>
        </Card>
      ) : null}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <InfoCard icon={<Gauge className="h-4 w-4 text-primary" />} label="Current Hashrate" value={formatHashrate(miner.hashrate?.current ?? 0)} />
        <InfoCard icon={<Gauge className="h-4 w-4 text-chart-2" />} label="Avg / Max" value={`${formatHashrate(miner.hashrate?.average ?? 0)} / ${formatHashrate(miner.hashrate?.max ?? 0)}`} />
        <InfoCard icon={<Clock className="h-4 w-4 text-chart-3" />} label="Uptime" value={formatUptime(miner.uptime_seconds)} />
        <InfoCard icon={<Cpu className="h-4 w-4 text-chart-4" />} label="Last Seen" value={formatTimeAgo(miner.last_seen)} />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">Hashrate (6h)</CardTitle>
          </CardHeader>
          <CardContent>
            {chartData.length > 0 ? (
              <ResponsiveContainer width="100%" height={260}>
                <AreaChart data={chartData}>
                  <defs>
                    <linearGradient id="hrGradDetail" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="oklch(0.7 0.15 160)" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="oklch(0.7 0.15 160)" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="time" tick={{ fill: "#888", fontSize: 12 }} tickLine={false} axisLine={false} />
                  <YAxis tick={{ fill: "#888", fontSize: 12 }} tickLine={false} axisLine={false} tickFormatter={(v: number) => formatHashrate(v)} />
                  <Tooltip contentStyle={{ background: "#1a1a1a", border: "1px solid #333", borderRadius: 8 }} formatter={(v: number | undefined) => [formatHashrate(v ?? 0), ""]} />
                  <Area type="monotone" dataKey="current" stroke="oklch(0.7 0.15 160)" fill="url(#hrGradDetail)" strokeWidth={2} name="Current" />
                  <Area type="monotone" dataKey="average" stroke="oklch(0.7 0.15 200)" fill="none" strokeWidth={1.5} strokeDasharray="4 4" name="Average" />
                </AreaChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-[260px] items-center justify-center text-muted-foreground">No history data</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">System Info</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <InfoRow icon={<Cpu className="h-4 w-4" />} label="CPU" value={miner.cpu_model || friendlyCPU(miner.cpu_family)} />
            <InfoRow icon={<Cpu className="h-4 w-4" />} label="Family" value={friendlyCPU(miner.cpu_family)} />
            <InfoRow icon={<HardDrive className="h-4 w-4" />} label="Cores" value={String(miner.cores)} />
            <InfoRow icon={<Globe className="h-4 w-4" />} label="OS / Arch" value={`${miner.os} / ${miner.arch}`} />
            <Separator />
            <InfoRow label="XMRig" value={miner.xmrig_version || "—"} />
            <InfoRow label="Tarish" value={miner.tarish_version || "—"} />
            <InfoRow label="Hostname" value={miner.hostname || "—"} />
            <InfoRow label="Worker ID" value={miner.worker_id || "—"} />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Proxy</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          {!miner.snell?.capable ? (
            <p className="text-muted-foreground">Old client — enroll is available; restart tarish to apply snapshots.</p>
          ) : null}
          {(miner.snell?.capable || miner.snell) && (
            <>
              <div className="flex flex-wrap gap-2">
                {([
                  ["inherit", "Inherit"],
                  ["custom", "Custom"],
                  ["local", "Local"],
                ] as const).map(([mode, label]) => (
                  <Button
                    key={mode}
                    size="sm"
                    variant={miner.snell?.mode === mode ? "default" : "outline"}
                    disabled={snellBusy}
                    onClick={async () => {
                      setSnellBusy(true)
                      try {
                        if (mode === "local" && miner.snell?.mode !== "local") {
                          await api.exitSnellManaged(miner.id)
                        } else if (mode === "inherit") {
                          await api.setMinerSnell(miner.id, {
                            mode: "inherit",
                            custom_node_ids: miner.snell?.custom_node_ids,
                          })
                        } else {
                          await api.setMinerSnell(miner.id, {
                            mode,
                            custom_node_ids: miner.snell?.custom_node_ids,
                            enabled: miner.snell?.enabled ?? true,
                          })
                        }
                        await refresh()
                      } finally {
                        setSnellBusy(false)
                      }
                    }}
                  >
                    {label}
                  </Button>
                ))}
              </div>
              <p className="text-muted-foreground">
                expected {miner.snell?.expected_version ?? 0} · applied {miner.snell?.applied_version ?? 0}
                {miner.snell?.apply_error ? ` · ${miner.snell.apply_error}` : ""}
              </p>
              {miner.snell?.mode === "inherit" ? (
                <div className="flex flex-wrap items-center gap-3">
                  <label className="flex items-center gap-2">
                    <Switch
                      checked={miner.snell.enabled ?? !!catalog?.pool.managed_proxy_enabled}
                      disabled={snellBusy}
                      onCheckedChange={async on => {
                        setSnellBusy(true)
                        try {
                          await api.setMinerSnell(miner.id, {
                            mode: "inherit",
                            enabled: on,
                            custom_node_ids: miner.snell?.custom_node_ids,
                          })
                          await refresh()
                        } finally {
                          setSnellBusy(false)
                        }
                      }}
                    />
                    <span>Override</span>
                  </label>
                  {miner.snell.enabled != null ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={snellBusy}
                      onClick={async () => {
                        setSnellBusy(true)
                        try {
                          await api.setMinerSnell(miner.id, {
                            mode: "inherit",
                            custom_node_ids: miner.snell?.custom_node_ids,
                          })
                          await refresh()
                        } finally {
                          setSnellBusy(false)
                        }
                      }}
                    >
                      Follow pool
                    </Button>
                  ) : (
                    <span className="text-muted-foreground">Follow pool</span>
                  )}
                </div>
              ) : null}
              {miner.snell?.mode === "custom" ? (
                <div className="space-y-3">
                  <label className="flex items-center gap-2">
                    <Switch
                      checked={miner.snell.enabled ?? true}
                      disabled={snellBusy}
                      onCheckedChange={async on => {
                        setSnellBusy(true)
                        try {
                          await api.setMinerSnell(miner.id, {
                            mode: "custom",
                            enabled: on,
                            custom_node_ids: miner.snell?.custom_node_ids,
                          })
                          await refresh()
                        } finally {
                          setSnellBusy(false)
                        }
                      }}
                    />
                    <span>Enable</span>
                  </label>
                  <div className="grid gap-1">
                    {(catalog?.nodes ?? []).filter(n => !n.builtin).map(n => {
                      const checked = (miner.snell?.custom_node_ids ?? []).includes(n.id)
                      return (
                        <label key={n.id} className="flex items-center gap-2">
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={async e => {
                              const ids = new Set(miner.snell?.custom_node_ids ?? [])
                              if (e.target.checked) ids.add(n.id)
                              else ids.delete(n.id)
                              await api.setMinerSnell(miner.id, {
                                mode: "custom",
                                enabled: miner.snell?.enabled ?? true,
                                custom_node_ids: [...ids],
                              })
                              await refresh()
                            }}
                          />
                          {n.name}
                        </label>
                      )
                    })}
                  </div>
                </div>
              ) : null}
            </>
          )}
        </CardContent>
      </Card>

      <ConfigEditor miner={miner} onApplied={refresh} />
    </div>
  )
}

function InfoCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{label}</CardTitle>
        {icon}
      </CardHeader>
      <CardContent>
        <div className="text-lg font-bold">{value}</div>
      </CardContent>
    </Card>
  )
}

function InfoRow({ icon, label, value }: { icon?: React.ReactNode; label: string; value: string }) {
  return (
    <div className="flex items-center justify-between text-sm">
      <span className="flex items-center gap-2 text-muted-foreground">
        {icon}
        {label}
      </span>
      <span className="font-medium truncate ml-2 max-w-[200px]">{value}</span>
    </div>
  )
}
