import { useState, useMemo } from "react"
import { usePoll } from "@/hooks/use-poll"
import { api, type Miner } from "@/lib/api"
import { formatHashrate, formatUptime, formatTimeAgo, displayName, friendlyCPU } from "@/lib/utils"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ArrowUpDown, Search } from "lucide-react"
import { Link } from "react-router-dom"

type SortKey = "hashrate" | "name" | "cores" | "uptime" | "last_seen"
type SortDir = "asc" | "desc"

export default function Miners() {
  const { data: miners } = usePoll<Miner[]>(() => api.getMiners(), 10000)
  const [search, setSearch] = useState("")
  const [sortKey, setSortKey] = useState<SortKey>("hashrate")
  const [sortDir, setSortDir] = useState<SortDir>("desc")

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir(d => (d === "asc" ? "desc" : "asc"))
    } else {
      setSortKey(key)
      setSortDir("desc")
    }
  }

  const filtered = useMemo(() => {
    if (!miners) return []
    const q = search.toLowerCase()
    let list = miners.filter(m => {
      if (!q) return true
      return (
        m.hostname.toLowerCase().includes(q) ||
        m.ip.includes(q) ||
        m.cpu_family.toLowerCase().includes(q) ||
        m.miner_id.toLowerCase().includes(q) ||
        m.cpu_model.toLowerCase().includes(q)
      )
    })

    list.sort((a, b) => {
      let cmp = 0
      switch (sortKey) {
        case "hashrate":
          cmp = (a.hashrate?.current ?? 0) - (b.hashrate?.current ?? 0)
          break
        case "name":
          cmp = displayName(a).localeCompare(displayName(b))
          break
        case "cores":
          cmp = a.cores - b.cores
          break
        case "uptime":
          cmp = a.uptime_seconds - b.uptime_seconds
          break
        case "last_seen":
          cmp = new Date(a.last_seen).getTime() - new Date(b.last_seen).getTime()
          break
      }
      return sortDir === "asc" ? cmp : -cmp
    })

    return list
  }, [miners, search, sortKey, sortDir])

  const SortBtn = ({ k, children }: { k: SortKey; children: React.ReactNode }) => (
    <Button variant="ghost" size="sm" className="h-8 px-2 text-xs font-medium text-muted-foreground" onClick={() => toggleSort(k)}>
      {children}
      <ArrowUpDown className="ml-1 h-3 w-3" />
      {sortKey === k && <span className="ml-0.5 text-primary">{sortDir === "asc" ? "\u2191" : "\u2193"}</span>}
    </Button>
  )

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Miners</h1>
          <p className="text-muted-foreground">{filtered.length} miner{filtered.length !== 1 ? "s" : ""}</p>
        </div>
        <div className="relative">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search miners..."
            className="h-10 w-64 rounded-md border border-input bg-background pl-10 pr-4 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
        </div>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Fleet</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-border">
                  <th className="px-4 py-2 text-left"><span className="text-xs font-medium text-muted-foreground">#</span></th>
                  <th className="px-4 py-2 text-left"><SortBtn k="name">Miner</SortBtn></th>
                  <th className="px-4 py-2 text-left"><span className="text-xs font-medium text-muted-foreground">IP</span></th>
                  <th className="px-4 py-2 text-left"><span className="text-xs font-medium text-muted-foreground">CPU</span></th>
                  <th className="px-4 py-2 text-right"><SortBtn k="cores">Cores</SortBtn></th>
                  <th className="px-4 py-2 text-right"><SortBtn k="hashrate">Hashrate</SortBtn></th>
                  <th className="px-4 py-2 text-right"><SortBtn k="uptime">Uptime</SortBtn></th>
                  <th className="px-4 py-2 text-center"><span className="text-xs font-medium text-muted-foreground">Status</span></th>
                  <th className="px-4 py-2 text-right"><SortBtn k="last_seen">Last Seen</SortBtn></th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((m, i) => (
                  <tr key={m.id} className="border-b border-border/50 transition-colors hover:bg-secondary/50">
                    <td className="px-4 py-3 text-sm text-muted-foreground">{i + 1}</td>
                    <td className="px-4 py-3">
                      <Link to={`/miners/${encodeURIComponent(m.id)}`} className="font-medium text-sm hover:text-primary transition-colors">
                        {displayName(m)}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-sm font-mono text-muted-foreground">{m.ip}</td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">{friendlyCPU(m.cpu_family)}</td>
                    <td className="px-4 py-3 text-sm text-right">{m.cores}</td>
                    <td className="px-4 py-3 text-right">
                      <span className="font-mono text-sm font-medium text-primary">{formatHashrate(m.hashrate?.current ?? 0)}</span>
                      <span className="block text-xs text-muted-foreground">avg {formatHashrate(m.hashrate?.average ?? 0)}</span>
                    </td>
                    <td className="px-4 py-3 text-sm text-right text-muted-foreground">{formatUptime(m.uptime_seconds)}</td>
                    <td className="px-4 py-3 text-center">
                      <Badge variant={m.status === "online" ? "success" : m.status === "stale" ? "warning" : "destructive"}>
                        {m.status}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-sm text-right text-muted-foreground">{formatTimeAgo(m.last_seen)}</td>
                  </tr>
                ))}
                {filtered.length === 0 && (
                  <tr>
                    <td colSpan={9} className="px-4 py-12 text-center text-muted-foreground">
                      {miners === null ? "Loading..." : "No miners found"}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
