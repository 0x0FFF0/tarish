import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatHashrate(h: number): string {
  if (h >= 1000) return `${(h / 1000).toFixed(2)} KH/s`
  return `${h.toFixed(1)} H/s`
}

export function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const mins = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h ${mins}m`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

export function formatTimeAgo(date: string | Date): string {
  const d = typeof date === "string" ? new Date(date) : date
  const seconds = Math.floor((Date.now() - d.getTime()) / 1000)
  if (seconds < 60) return `${seconds}s ago`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86400)}d ago`
}

export function displayName(miner: { hostname: string; cpu_family: string; miner_id: string }): string {
  if (miner.hostname) {
    const short = miner.hostname.replace(/\.local$/, "")
    return `${short} (${friendlyCPU(miner.cpu_family)})`
  }
  return miner.miner_id || "unknown"
}

export function friendlyCPU(family: string): string {
  return family
    .replace("apple_", "")
    .replace(/_/g, " ")
    .replace(/\b\w/g, c => c.toUpperCase())
}
