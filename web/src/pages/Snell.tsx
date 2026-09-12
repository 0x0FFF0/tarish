import { useMemo, useState } from "react"
import { usePoll } from "@/hooks/use-poll"
import {
  api,
  type SnellCatalog,
  type SnellNodeView,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { formatTimeAgo } from "@/lib/utils"
import { Link } from "react-router-dom"

const inputClass =
  "h-10 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"

export default function Snell() {
  const { data, refresh } = usePoll<SnellCatalog>(() => api.getSnellCatalog(), 10000)
  const [busy, setBusy] = useState("")
  const [msg, setMsg] = useState("")
  const [sourceURL, setSourceURL] = useState("")
  const [sourceName, setSourceName] = useState("")
  const [manualName, setManualName] = useState("")
  const [manualHost, setManualHost] = useState("")
  const [manualPort, setManualPort] = useState("440")
  const [manualPSK, setManualPSK] = useState("")

  const sources = data?.sources ?? []
  const nodes = data?.nodes ?? []
  const pool = data?.pool
  const machines = data?.machines ?? []
  const seed = data?.seed

  const selectedSources = new Set(pool?.include_source_ids ?? [])
  const selectedManual = new Set(pool?.include_manual_ids ?? [])

  const catalogNodes = useMemo(
    () => nodes.filter(n => !n.builtin),
    [nodes]
  )

  const flash = (text: string) => {
    setMsg(text)
    setTimeout(() => setMsg(""), 4000)
  }

  const run = async (key: string, fn: () => Promise<void>) => {
    setBusy(key)
    try {
      await fn()
      await refresh()
    } catch (e) {
      flash(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy("")
    }
  }

  const savePool = async (nextSources: string[], nextManual: string[], enabled: boolean) => {
    await api.updateSnellPool({
      include_source_ids: nextSources,
      include_manual_ids: nextManual,
      managed_proxy_enabled: enabled,
    })
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Proxy</h1>
        <p className="text-muted-foreground">Subscriptions, node pool, and miner assignment</p>
        {msg ? <p className="mt-2 text-sm text-destructive">{msg}</p> : null}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Sources</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-[1fr_160px_auto_auto]">
            <input
              className={inputClass}
              placeholder="https://…/subscribe?format=surge"
              value={sourceURL}
              onChange={e => setSourceURL(e.target.value)}
              type="url"
            />
            <input
              className={inputClass}
              placeholder="Name"
              value={sourceName}
              onChange={e => setSourceName(e.target.value)}
            />
            <Button
              variant="outline"
              disabled={busy !== "" || !sourceURL}
              onClick={() =>
                run("test", async () => {
                  const res = await api.testSnellSource({ url: sourceURL, format: "surge" })
                  flash(res.ok ? `OK · ${res.nodes ?? 0} nodes` : res.error || "test failed")
                })
              }
            >
              Test
            </Button>
            <Button
              disabled={busy !== "" || !sourceURL}
              onClick={() =>
                run("add", async () => {
                  await api.addSnellSource({ url: sourceURL, name: sourceName, format: "surge" })
                  setSourceURL("")
                })
              }
            >
              Add
            </Button>
          </div>
          <div className="space-y-2">
            {sources.length === 0 ? (
              <p className="text-sm text-muted-foreground">No sources.</p>
            ) : (
              sources.map(src => (
                <div key={src.id} className="flex min-w-0 items-center gap-2 rounded-md border border-border px-3 py-2 text-sm">
                  <div className="shrink-0 font-medium">{src.name || src.id.slice(0, 8)}</div>
                  <div className="min-w-0 flex-1 truncate text-muted-foreground" title={src.url}>
                    {src.url}
                  </div>
                  <Badge variant="secondary" className="shrink-0">
                    {src.node_count}
                  </Badge>
                  <span className="shrink-0 whitespace-nowrap text-muted-foreground">
                    {src.last_success_at ? formatTimeAgo(src.last_success_at) : "—"}
                  </span>
                  {showSourceError(src) ? (
                    <span className="max-w-24 shrink-0 truncate text-destructive" title={src.last_error}>
                      {src.last_error}
                    </span>
                  ) : null}
                  <div className="flex shrink-0 gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy !== ""}
                      onClick={() =>
                        run("sync-"+src.id, async () => {
                          const res = await api.syncSnellSource(src.id)
                          if (!res.ok) flash(res.error || "sync failed")
                        })
                      }
                    >
                      Sync
                    </Button>
                    <Button
                      size="sm"
                      variant="destructive"
                      disabled={busy !== ""}
                      onClick={() => run("del-"+src.id, () => api.deleteSnellSource(src.id).then(() => undefined))}
                    >
                      Delete
                    </Button>
                  </div>
                </div>
              ))
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Nodes</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-5">
            <input className={inputClass} placeholder="Name" value={manualName} onChange={e => setManualName(e.target.value)} />
            <input className={inputClass} placeholder="Host" value={manualHost} onChange={e => setManualHost(e.target.value)} />
            <input className={inputClass} placeholder="Port" value={manualPort} onChange={e => setManualPort(e.target.value)} />
            <input className={inputClass} placeholder="PSK" type="password" value={manualPSK} onChange={e => setManualPSK(e.target.value)} />
            <Button
              disabled={busy !== "" || !manualHost || !manualPSK}
              onClick={() =>
                run("manual", async () => {
                  await api.addSnellNode({
                    name: manualName || "manual",
                    host: manualHost,
                    port: Number(manualPort) || 440,
                    psk: manualPSK,
                    version: "v5",
                  })
                  setManualPSK("")
                })
              }
            >
              Add
            </Button>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-muted-foreground">
                <tr>
                  <th className="py-2 pr-4 font-medium">Name</th>
                  <th className="pr-4 font-medium">Source</th>
                  <th className="pr-4 font-medium">Ver</th>
                  <th className="pr-4 font-medium">On</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {catalogNodes.map((n: SnellNodeView) => (
                  <tr key={n.id} className="border-t border-border">
                    <td className="py-2 pr-4">{n.name}</td>
                    <td className="pr-4">{nodeSourceLabel(n)}</td>
                    <td className="pr-4">{n.version}</td>
                    <td className="pr-4">
                      <Switch
                        checked={n.enabled}
                        onCheckedChange={on =>
                          run("en-"+n.id, () => api.updateSnellNode(n.id, { enabled: on }).then(() => undefined))
                        }
                      />
                    </td>
                    <td>
                      {n.manual ? (
                        <Button size="sm" variant="ghost" onClick={() => run("dn-"+n.id, () => api.deleteSnellNode(n.id).then(() => undefined))}>
                          Delete
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Default pool</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-3">
            <Switch
              checked={!!pool?.managed_proxy_enabled}
              onCheckedChange={on =>
                run("pool-en", () =>
                  savePool([...selectedSources], [...selectedManual], on)
                )
              }
            />
            <span className="text-sm">Managed proxy</span>
            <Badge variant={pool?.valid ? "success" : "warning"}>{pool?.valid ? "Ready" : "Empty"}</Badge>
            <span className="text-sm text-muted-foreground">v{pool?.policy_version ?? 0} · {pool?.node_count ?? 0}</span>
          </div>
          <p className="text-sm text-muted-foreground">
            Fallback {seed?.id ? seed.id.slice(0, 8) : "builtin"} · out of pool
          </p>
          <div className="grid gap-2 md:grid-cols-2">
            <div>
              <div className="mb-2 text-sm font-medium">Sources</div>
              {sources.map(src => (
                <label key={src.id} className="flex items-center gap-2 py-1 text-sm">
                  <input
                    type="checkbox"
                    checked={selectedSources.has(src.id)}
                    onChange={e => {
                      const next = new Set(selectedSources)
                      if (e.target.checked) next.add(src.id)
                      else next.delete(src.id)
                      run("pool-src", () => savePool([...next], [...selectedManual], !!pool?.managed_proxy_enabled))
                    }}
                  />
                  {src.name || src.id.slice(0, 8)}
                </label>
              ))}
            </div>
            <div>
              <div className="mb-2 text-sm font-medium">Manual</div>
              {catalogNodes.filter(n => n.manual).map(n => (
                <label key={n.id} className="flex items-center gap-2 py-1 text-sm">
                  <input
                    type="checkbox"
                    checked={selectedManual.has(n.id)}
                    onChange={e => {
                      const next = new Set(selectedManual)
                      if (e.target.checked) next.add(n.id)
                      else next.delete(n.id)
                      run("pool-man", () => savePool([...selectedSources], [...next], !!pool?.managed_proxy_enabled))
                    }}
                  />
                  {n.name}
                </label>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Fleet</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-muted-foreground">
                <tr>
                  <th className="pb-2 pr-4 font-medium">Miner</th>
                  <th className="pb-2 pr-4 font-medium">Mode</th>
                  <th className="pb-2 pr-4 font-medium">Expected</th>
                  <th className="pb-2 pr-4 font-medium">Applied</th>
                  <th className="pb-2 pr-4 font-medium">Node</th>
                  <th className="pb-2 pr-4 font-medium">Error</th>
                  <th className="pb-2 pl-4 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody>
                {machines.map(m => (
                  <tr key={m.miner_id} className="border-t border-border">
                    <td className="py-3 pr-4 align-middle">
                      <Link className="hover:underline" to={`/miners/${encodeURIComponent(m.miner_id)}`}>
                        {m.hostname || m.miner_id}
                      </Link>
                    </td>
                    <td className="py-3 pr-4 align-middle">
                      {modeLabel(m.mode)}
                      {!m.capable ? <span className="text-muted-foreground"> (old client)</span> : null}
                    </td>
                    <td className="py-3 pr-4 align-middle">{m.expected_version}</td>
                    <td className="py-3 pr-4 align-middle">{m.applied_version}</td>
                    <td className="py-3 pr-4 align-middle font-mono text-xs">{m.selected_node || "—"}</td>
                    <td className="py-3 pr-4 align-middle text-destructive">{m.apply_error || ""}</td>
                    <td className="py-3 pl-4 align-middle text-right whitespace-nowrap">
                      {m.mode === "local" ? (
                        <Button size="sm" variant="outline" onClick={() => run("enroll-"+m.miner_id, () => api.enrollSnellMiners([m.miner_id]).then(() => undefined))}>
                          Enroll
                        </Button>
                      ) : null}
                      {m.mode !== "local" ? (
                        <Button size="sm" variant="ghost" onClick={() => run("exit-"+m.miner_id, () => api.exitSnellManaged(m.miner_id).then(() => undefined))}>
                          Local
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function nodeSourceLabel(n: SnellNodeView) {
  if (n.builtin || n.role === "内置兜底") return "fallback"
  return n.source
}

function showSourceError(src: { last_error?: string; last_success_at?: string | null; last_failure_at?: string | null }) {
  if (!src.last_error) return false
  if (!src.last_success_at) return true
  if (!src.last_failure_at) return false
  return Date.parse(src.last_failure_at) > Date.parse(src.last_success_at)
}

export function modeLabel(mode: string) {
  switch (mode) {
    case "inherit":
      return "Inherit"
    case "custom":
      return "Custom"
    case "local":
      return "Local"
    default:
      return mode
  }
}
