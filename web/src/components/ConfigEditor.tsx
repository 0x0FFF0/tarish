import { useState, useEffect } from "react"
import { api, type Miner } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Slider } from "@/components/ui/slider"
import { Switch } from "@/components/ui/switch"
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { Settings, Check, RotateCcw } from "lucide-react"

interface Props {
  miner: Miner
  onApplied?: () => void
}

export default function ConfigEditor({ miner, onApplied }: Props) {
  const cpuConfig = (miner.config?.cpu ?? {}) as Record<string, unknown>

  const currentThreadsHint = (cpuConfig["max-threads-hint"] as number) ?? 100
  const currentRx = (cpuConfig["rx"] as number[]) ?? Array.from({ length: miner.cores }, (_, i) => i)
  const currentPriority = (cpuConfig["priority"] as number) ?? 5
  const currentHugePages = (cpuConfig["huge-pages"] as boolean) ?? true

  const [threadsHint, setThreadsHint] = useState(currentThreadsHint)
  const [activeCores, setActiveCores] = useState<Set<number>>(new Set(currentRx))
  const [priority, setPriority] = useState(String(currentPriority))
  const [hugePages, setHugePages] = useState(currentHugePages)
  const [applying, setApplying] = useState(false)
  const [applied, setApplied] = useState(false)

  const configKey = `${miner.id}:${currentThreadsHint}:${currentRx.join(",")}:${currentPriority}:${currentHugePages}`

  useEffect(() => {
    setThreadsHint(currentThreadsHint)
    setActiveCores(new Set(currentRx))
    setPriority(String(currentPriority))
    setHugePages(currentHugePages)
  }, [configKey])

  const hasChanges =
    threadsHint !== currentThreadsHint ||
    !setsEqual(activeCores, new Set(currentRx)) ||
    Number(priority) !== currentPriority ||
    hugePages !== currentHugePages

  const toggleCore = (core: number) => {
    setActiveCores(prev => {
      const next = new Set(prev)
      if (next.has(core)) {
        if (next.size > 1) next.delete(core)
      } else {
        next.add(core)
      }
      return next
    })
  }

  const handleApply = async () => {
    setApplying(true)
    setApplied(false)
    try {
      const fullConfig = structuredClone(miner.config ?? {})
      const cpu = ((fullConfig.cpu as Record<string, unknown>) ?? {})
      cpu["max-threads-hint"] = threadsHint
      cpu["rx"] = Array.from(activeCores).sort((a, b) => a - b)
      cpu["priority"] = Number(priority)
      cpu["huge-pages"] = hugePages
      fullConfig.cpu = cpu
      await api.setConfig(miner.id, fullConfig)
      setApplied(true)
      onApplied?.()
      setTimeout(() => setApplied(false), 3000)
    } catch {
      // silently fail
    } finally {
      setApplying(false)
    }
  }

  const handleReset = () => {
    setThreadsHint(currentThreadsHint)
    setActiveCores(new Set(currentRx))
    setPriority(String(currentPriority))
    setHugePages(currentHugePages)
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2 text-base">
              <Settings className="h-4 w-4" />
              Mining Configuration
            </CardTitle>
            <CardDescription>
              Changes are queued and applied on the miner's next heartbeat (~30s)
            </CardDescription>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={handleReset} disabled={!hasChanges}>
              <RotateCcw className="mr-1 h-3 w-3" />
              Reset
            </Button>
            <Button size="sm" onClick={handleApply} disabled={!hasChanges || applying}>
              {applied ? <Check className="mr-1 h-3 w-3" /> : null}
              {applying ? "Applying..." : applied ? "Applied" : "Apply Changes"}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-8">
        {/* CPU Leverage (max-threads-hint) */}
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <label className="text-sm font-medium">CPU Leverage</label>
            <span className="text-sm font-mono text-primary">{threadsHint}%</span>
          </div>
          <Slider
            value={[threadsHint]}
            onValueChange={([v]) => setThreadsHint(v)}
            min={10}
            max={100}
            step={5}
          />
          <p className="text-xs text-muted-foreground">
            Controls <code className="rounded bg-secondary px-1">max-threads-hint</code> &mdash; percentage of available CPU threads to use
          </p>
        </div>

        {/* Active Cores (rx array) */}
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <label className="text-sm font-medium">Active Cores</label>
            <span className="text-sm text-muted-foreground">{activeCores.size} / {miner.cores}</span>
          </div>
          <div className="flex flex-wrap gap-2">
            {Array.from({ length: miner.cores }, (_, i) => (
              <button
                key={i}
                onClick={() => toggleCore(i)}
                className={cn(
                  "flex h-9 w-9 items-center justify-center rounded-md border text-xs font-mono transition-colors",
                  activeCores.has(i)
                    ? "border-primary bg-primary/20 text-primary"
                    : "border-border bg-secondary/50 text-muted-foreground hover:border-muted-foreground"
                )}
              >
                {i}
              </button>
            ))}
          </div>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" className="text-xs h-7" onClick={() => setActiveCores(new Set(Array.from({ length: miner.cores }, (_, i) => i)))}>
              All
            </Button>
            <Button variant="outline" size="sm" className="text-xs h-7" onClick={() => setActiveCores(new Set(Array.from({ length: miner.cores }, (_, i) => i).filter(i => i % 2 === 0)))}>
              Even
            </Button>
            <Button variant="outline" size="sm" className="text-xs h-7" onClick={() => setActiveCores(new Set(Array.from({ length: miner.cores }, (_, i) => i).filter(i => i % 2 !== 0)))}>
              Odd
            </Button>
            <Button variant="outline" size="sm" className="text-xs h-7" onClick={() => setActiveCores(new Set(Array.from({ length: Math.ceil(miner.cores / 2) }, (_, i) => i)))}>
              First Half
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            Controls <code className="rounded bg-secondary px-1">cpu.rx</code> &mdash; which CPU core indices are used for mining
          </p>
        </div>

        {/* Priority & Huge Pages */}
        <div className="grid gap-6 md:grid-cols-2">
          <div className="space-y-3">
            <label className="text-sm font-medium">Process Priority</label>
            <Select value={priority} onValueChange={setPriority}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="1">1 - Lowest</SelectItem>
                <SelectItem value="2">2 - Low</SelectItem>
                <SelectItem value="3">3 - Normal</SelectItem>
                <SelectItem value="4">4 - High</SelectItem>
                <SelectItem value="5">5 - Highest</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Controls <code className="rounded bg-secondary px-1">cpu.priority</code>
            </p>
          </div>

          <div className="space-y-3">
            <label className="text-sm font-medium">Huge Pages</label>
            <div className="flex items-center gap-3 pt-1">
              <Switch checked={hugePages} onCheckedChange={setHugePages} />
              <span className="text-sm text-muted-foreground">{hugePages ? "Enabled" : "Disabled"}</span>
            </div>
            <p className="text-xs text-muted-foreground">
              Controls <code className="rounded bg-secondary px-1">cpu.huge-pages</code> &mdash; improves RandomX performance
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function setsEqual<T>(a: Set<T>, b: Set<T>): boolean {
  if (a.size !== b.size) return false
  for (const v of a) if (!b.has(v)) return false
  return true
}
