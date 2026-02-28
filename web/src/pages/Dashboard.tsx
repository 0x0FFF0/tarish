import { usePoll } from "@/hooks/use-poll"
import { api, type Overview, type HashrateHistory } from "@/lib/api"
import { formatHashrate, displayName } from "@/lib/utils"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Activity, Cpu, TrendingUp, Zap } from "lucide-react"
import { Link } from "react-router-dom"
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts"

export default function Dashboard() {
  const { data: overview } = usePoll<Overview>(() => api.getOverview(), 10000)
  const { data: history } = usePoll<HashrateHistory[]>(() => api.getHashrateHistory(undefined, 6), 30000)

  const chartData = aggregateHistory(history ?? [])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
        <p className="text-muted-foreground">Mining fleet overview</p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="Total Hashrate"
          value={formatHashrate(overview?.total_hashrate ?? 0)}
          icon={<Zap className="h-4 w-4 text-primary" />}
        />
        <StatCard
          title="Average Hashrate"
          value={formatHashrate(overview?.average_hashrate ?? 0)}
          icon={<TrendingUp className="h-4 w-4 text-chart-2" />}
        />
        <StatCard
          title="Active Miners"
          value={String(overview?.active_miners ?? 0)}
          subtitle={`of ${overview?.total_miners ?? 0} total`}
          icon={<Activity className="h-4 w-4 text-green-400" />}
        />
        <StatCard
          title="Fleet Cores"
          value={String((overview?.top_miners ?? []).reduce((s, m) => s + m.cores, 0))}
          icon={<Cpu className="h-4 w-4 text-chart-3" />}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">Hashrate (6h)</CardTitle>
          </CardHeader>
          <CardContent>
            {chartData.length > 0 ? (
              <ResponsiveContainer width="100%" height={280}>
                <AreaChart data={chartData}>
                  <defs>
                    <linearGradient id="hrGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="oklch(0.7 0.15 160)" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="oklch(0.7 0.15 160)" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="time" tick={{ fill: "#888", fontSize: 12 }} tickLine={false} axisLine={false} />
                  <YAxis tick={{ fill: "#888", fontSize: 12 }} tickLine={false} axisLine={false} tickFormatter={(v: number) => formatHashrate(v)} />
                  <Tooltip
                    contentStyle={{ background: "#1a1a1a", border: "1px solid #333", borderRadius: 8 }}
                    labelStyle={{ color: "#aaa" }}
                    formatter={(v: number | undefined) => [formatHashrate(v ?? 0), "Hashrate"]}
                  />
                  <Area type="monotone" dataKey="hashrate" stroke="oklch(0.7 0.15 160)" fill="url(#hrGrad)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-[280px] items-center justify-center text-muted-foreground">
                No hashrate data yet
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Top Miners</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {(overview?.top_miners ?? []).map((m, i) => (
                <Link
                  key={m.id}
                  to={`/miners/${encodeURIComponent(m.id)}`}
                  className="flex items-center justify-between rounded-lg p-2 transition-colors hover:bg-secondary"
                >
                  <div className="flex items-center gap-3">
                    <span className="flex h-6 w-6 items-center justify-center rounded-full bg-secondary text-xs font-bold">
                      {i + 1}
                    </span>
                    <div>
                      <p className="text-sm font-medium leading-none">{displayName(m)}</p>
                      <p className="text-xs text-muted-foreground">{m.ip}</p>
                    </div>
                  </div>
                  <div className="text-right">
                    <p className="text-sm font-mono font-medium text-primary">
                      {formatHashrate(m.hashrate?.current ?? 0)}
                    </p>
                    <Badge variant={m.status === "online" ? "success" : m.status === "stale" ? "warning" : "destructive"} className="text-[10px]">
                      {m.status}
                    </Badge>
                  </div>
                </Link>
              ))}
              {(overview?.top_miners ?? []).length === 0 && (
                <p className="text-sm text-muted-foreground text-center py-4">No miners reporting yet</p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function StatCard({ title, value, subtitle, icon }: { title: string; value: string; subtitle?: string; icon: React.ReactNode }) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
        {icon}
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{value}</div>
        {subtitle && <p className="text-xs text-muted-foreground">{subtitle}</p>}
      </CardContent>
    </Card>
  )
}

function aggregateHistory(history: HashrateHistory[]): { time: string; hashrate: number }[] {
  const buckets = new Map<string, number>()

  for (const h of history) {
    const d = new Date(h.timestamp)
    const key = `${d.getHours().toString().padStart(2, "0")}:${(Math.floor(d.getMinutes() / 5) * 5).toString().padStart(2, "0")}`
    buckets.set(key, (buckets.get(key) ?? 0) + h.current)
  }

  return Array.from(buckets.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([time, hashrate]) => ({ time, hashrate }))
}
