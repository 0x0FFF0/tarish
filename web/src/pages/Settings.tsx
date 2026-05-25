import { useEffect, useMemo, useState } from "react"
import { usePoll } from "@/hooks/use-poll"
import {
  api,
  type AlertLogEntry,
  type BarkSettingsView,
} from "@/lib/api"
import { cn, formatTimeAgo } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { Slider } from "@/components/ui/slider"
import { Switch } from "@/components/ui/switch"
import { BellOff, BellRing, KeyRound, Send, Timer } from "lucide-react"

type FormState = {
  enabled: boolean
  token: string
  checkIntervalS: number
  throttleMinutes: number
  notifyRecovery: boolean
}

const muteQuickOptions: { label: string; minutes?: number; permanent?: boolean }[] = [
  { label: "1h", minutes: 60 },
  { label: "4h", minutes: 240 },
  { label: "24h", minutes: 1440 },
  { label: "Forever", permanent: true },
]

export default function Settings() {
  const { data: settings, refresh } = usePoll<BarkSettingsView>(
    () => api.getBarkSettings(),
    15000
  )
  const { data: alerts } = usePoll<AlertLogEntry[]>(
    () => api.getRecentAlerts(25),
    30000
  )

  const [form, setForm] = useState<FormState | null>(null)
  const [busy, setBusy] = useState("")
  const [statusMsg, setStatusMsg] = useState("")
  const [statusOK, setStatusOK] = useState(true)

  // Hydrate the form once we have server data; preserve in-progress edits.
  useEffect(() => {
    if (!settings) return
    setForm(prev =>
      prev ?? {
        enabled: settings.enabled,
        token: "",
        checkIntervalS: settings.check_interval_s,
        throttleMinutes: settings.throttle_minutes,
        notifyRecovery: settings.notify_recovery,
      }
    )
  }, [settings])

  const dirty = useMemo(() => {
    if (!form || !settings) return false
    return (
      form.enabled !== settings.enabled ||
      form.checkIntervalS !== settings.check_interval_s ||
      form.throttleMinutes !== settings.throttle_minutes ||
      form.notifyRecovery !== settings.notify_recovery ||
      form.token.length > 0
    )
  }, [form, settings])

  const flash = (msg: string, ok = true) => {
    setStatusMsg(msg)
    setStatusOK(ok)
    setTimeout(() => setStatusMsg(""), 4000)
  }

  const handleSave = async () => {
    if (!form) return
    setBusy("save")
    try {
      const payload: Record<string, unknown> = {
        enabled: form.enabled,
        check_interval_s: form.checkIntervalS,
        throttle_minutes: form.throttleMinutes,
        notify_recovery: form.notifyRecovery,
      }
      if (form.token.length > 0) {
        payload.token = form.token
      }
      await api.updateBarkSettings(payload)
      setForm(prev => (prev ? { ...prev, token: "" } : prev))
      await refresh()
      flash("Settings saved")
    } catch (e) {
      flash(e instanceof Error ? e.message : String(e), false)
    } finally {
      setBusy("")
    }
  }

  const handleClearToken = async () => {
    setBusy("clearToken")
    try {
      await api.updateBarkSettings({ token: "" })
      setForm(prev => (prev ? { ...prev, token: "" } : prev))
      await refresh()
      flash("Token cleared")
    } catch (e) {
      flash(e instanceof Error ? e.message : String(e), false)
    } finally {
      setBusy("")
    }
  }

  const handleTest = async () => {
    setBusy("test")
    try {
      const res = await api.testBark(form?.token || undefined)
      if (res.ok) {
        flash("Test push sent")
      } else {
        flash(res.error || "Test failed", false)
      }
    } catch (e) {
      flash(e instanceof Error ? e.message : String(e), false)
    } finally {
      setBusy("")
    }
  }

  const handleMute = async (option: {
    label: string
    minutes?: number
    permanent?: boolean
  }) => {
    setBusy(`mute-${option.label}`)
    try {
      await api.setBarkMute({ minutes: option.minutes, permanent: option.permanent })
      await refresh()
      flash(option.permanent ? "Muted indefinitely" : `Muted for ${option.minutes}m`)
    } catch (e) {
      flash(e instanceof Error ? e.message : String(e), false)
    } finally {
      setBusy("")
    }
  }

  const handleUnmute = async () => {
    setBusy("unmute")
    try {
      await api.clearBarkMute()
      await refresh()
      flash("Mute cleared")
    } catch (e) {
      flash(e instanceof Error ? e.message : String(e), false)
    } finally {
      setBusy("")
    }
  }

  const muteState = describeMute(settings)
  const overallStatus = describeStatus(settings, muteState)

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Settings</h1>
          <p className="text-muted-foreground">Bark push alerts when miners drop offline</p>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant={overallStatus.variant}>{overallStatus.label}</Badge>
          {statusMsg && (
            <span
              className={cn(
                "text-xs",
                statusOK ? "text-primary" : "text-destructive"
              )}
            >
              {statusMsg}
            </span>
          )}
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <BellRing className="h-4 w-4 text-primary" />
            Alerter
          </CardTitle>
          <CardDescription>
            Sends a push notification when a miner has been offline for at least 5 minutes.
            Recovery and re-alert behavior is configurable below.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="flex items-center justify-between gap-4">
            <div>
              <p className="text-sm font-medium">Enable Bark alerts</p>
              <p className="text-xs text-muted-foreground">
                Alerter is a no-op until enabled and a token is set.
              </p>
            </div>
            <Switch
              checked={form?.enabled ?? false}
              onCheckedChange={value =>
                setForm(prev => (prev ? { ...prev, enabled: value } : prev))
              }
            />
          </div>

          <Separator />

          <div className="space-y-3">
            <div className="flex items-center gap-2 text-sm font-medium">
              <BellOff className="h-4 w-4 text-muted-foreground" />
              Mute
            </div>
            <p className="text-xs text-muted-foreground">{muteState.description}</p>
            <div className="flex flex-wrap gap-2">
              {muteQuickOptions.map(option => (
                <Button
                  key={option.label}
                  variant="secondary"
                  size="sm"
                  disabled={busy.startsWith("mute-")}
                  onClick={() =>
                    void handleMute(option)
                  }
                >
                  Mute {option.label}
                </Button>
              ))}
              <Button
                variant="ghost"
                size="sm"
                disabled={!muteState.active || busy === "unmute"}
                onClick={() => void handleUnmute()}
              >
                Unmute
              </Button>
            </div>
          </div>

          <Separator />

          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm font-medium">
              <KeyRound className="h-4 w-4 text-muted-foreground" />
              Bark device token
            </div>
            <input
              type="password"
              autoComplete="off"
              spellCheck={false}
              placeholder={
                settings?.token_set
                  ? `••••${settings.token_last4 ?? "****"} (leave blank to keep)`
                  : "Paste device key from Bark app"
              }
              className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              value={form?.token ?? ""}
              onChange={event =>
                setForm(prev => (prev ? { ...prev, token: event.target.value } : prev))
              }
            />
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span>
                Used to call <code className="text-foreground">https://bark.ws/&lt;token&gt;</code>
                .
              </span>
              {settings?.token_set && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  disabled={busy === "clearToken"}
                  onClick={() => void handleClearToken()}
                >
                  Clear stored token
                </Button>
              )}
            </div>
          </div>

          <div className="grid gap-6 md:grid-cols-2">
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Timer className="h-4 w-4 text-muted-foreground" />
                  Check interval
                </div>
                <span className="text-sm text-muted-foreground">
                  every {form?.checkIntervalS ?? 60}s
                </span>
              </div>
              <Slider
                value={[form?.checkIntervalS ?? 60]}
                min={10}
                max={300}
                step={10}
                onValueChange={([value]) =>
                  setForm(prev =>
                    prev ? { ...prev, checkIntervalS: value } : prev
                  )
                }
              />
              <p className="text-xs text-muted-foreground">
                How often the alerter scans miners. The miner status threshold (offline ≥5min)
                is independent of this.
              </p>
            </div>

            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <BellRing className="h-4 w-4 text-muted-foreground" />
                  Re-alert throttle
                </div>
                <span className="text-sm text-muted-foreground">
                  every {form?.throttleMinutes ?? 60}m
                </span>
              </div>
              <input
                type="number"
                min={1}
                max={1440}
                step={5}
                className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                value={form?.throttleMinutes ?? 60}
                onChange={event =>
                  setForm(prev =>
                    prev
                      ? {
                          ...prev,
                          throttleMinutes: clampInt(event.target.value, 1, 1440),
                        }
                      : prev
                  )
                }
              />
              <p className="text-xs text-muted-foreground">
                Minutes to wait before re-alerting on a miner that stays offline.
              </p>
            </div>
          </div>

          <Separator />

          <div className="flex items-center justify-between gap-4">
            <div>
              <p className="text-sm font-medium">Notify on recovery</p>
              <p className="text-xs text-muted-foreground">
                Send a push when a miner that was offline reports again.
              </p>
            </div>
            <Switch
              checked={form?.notifyRecovery ?? true}
              onCheckedChange={value =>
                setForm(prev => (prev ? { ...prev, notifyRecovery: value } : prev))
              }
            />
          </div>

          <Separator />

          <div className="flex flex-wrap items-center gap-2">
            <Button onClick={() => void handleSave()} disabled={!dirty || busy === "save"}>
              {busy === "save" ? "Saving..." : "Save changes"}
            </Button>
            <Button
              variant="secondary"
              disabled={busy === "test" || (!settings?.token_set && !form?.token)}
              onClick={() => void handleTest()}
            >
              <Send className="mr-2 h-4 w-4" />
              {busy === "test" ? "Sending..." : "Send test notification"}
            </Button>
            {settings && (
              <span className="ml-auto text-xs text-muted-foreground">
                Updated {formatTimeAgo(settings.updated_at)}
              </span>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Recent alerts</CardTitle>
          <CardDescription>
            Last 25 attempts (offline / recovery / test / muted-suppressed).
          </CardDescription>
        </CardHeader>
        <CardContent>
          {alerts && alerts.length > 0 ? (
            <ul className="space-y-2">
              {alerts.map(entry => (
                <li
                  key={entry.id}
                  className="flex items-start justify-between gap-3 rounded-lg border border-border/60 bg-background/50 p-3 text-sm"
                >
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge variant={kindVariant(entry.kind)}>{entry.kind}</Badge>
                      {entry.miner_id && (
                        <span className="font-mono text-xs text-muted-foreground">
                          {entry.miner_id}
                        </span>
                      )}
                      <span className="text-xs text-muted-foreground">
                        {formatTimeAgo(entry.created_at)}
                      </span>
                    </div>
                    <p className="truncate font-medium">{entry.title}</p>
                    <p className="truncate text-xs text-muted-foreground">{entry.body}</p>
                    {entry.error && (
                      <p className="text-xs text-destructive">{entry.error}</p>
                    )}
                  </div>
                  <Badge variant={entry.ok ? "success" : "destructive"} className="shrink-0">
                    {entry.ok ? "ok" : "fail"}
                  </Badge>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">No alerts recorded yet.</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function clampInt(raw: string, min: number, max: number): number {
  const v = Number.parseInt(raw, 10)
  if (Number.isNaN(v)) return min
  if (v < min) return min
  if (v > max) return max
  return v
}

type StatusBadge = {
  label: string
  variant: "default" | "secondary" | "success" | "warning" | "destructive" | "outline"
}

function describeStatus(
  settings: BarkSettingsView | null | undefined,
  mute: ReturnType<typeof describeMute>
): StatusBadge {
  if (!settings) return { label: "loading", variant: "secondary" }
  if (!settings.enabled) return { label: "disabled", variant: "secondary" }
  if (!settings.token_set) return { label: "no token", variant: "destructive" }
  if (mute.active) return { label: mute.shortLabel, variant: "warning" }
  return { label: "active", variant: "success" }
}

function describeMute(settings: BarkSettingsView | null | undefined) {
  if (!settings || !settings.mute_until) {
    return { active: false, description: "Alerts are not muted.", shortLabel: "active" }
  }
  if (settings.mute_forever) {
    return {
      active: true,
      description: "Alerts muted indefinitely. Click Unmute to resume.",
      shortLabel: "muted",
    }
  }
  const until = new Date(settings.mute_until)
  if (until.getTime() <= Date.now()) {
    return { active: false, description: "Mute window has expired.", shortLabel: "active" }
  }
  const minutes = Math.max(1, Math.round((until.getTime() - Date.now()) / 60000))
  const label = minutes >= 60 ? `${Math.round(minutes / 60)}h` : `${minutes}m`
  return {
    active: true,
    description: `Muted for ${label} more (until ${until.toLocaleString()}).`,
    shortLabel: `muted ${label}`,
  }
}

function kindVariant(kind: string): StatusBadge["variant"] {
  switch (kind) {
    case "recovery":
      return "success"
    case "test":
      return "secondary"
    case "offline":
      return "destructive"
    case "offline_suppressed":
      return "warning"
    default:
      return "outline"
  }
}
