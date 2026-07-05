import { useStore } from '../store'
import { queryClient } from './queryClient'
import { endClientSession } from './session'
import type { QueryKey } from '@tanstack/react-query'
import type { Workload, Alert, RemoteConfigStatus, WorkloadEvent, EventsStats } from '../types'

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectAttempt = 0
let shouldReconnect = false

const RECONNECT_BASE_MS = 1_000
const RECONNECT_CAP_MS = 30_000
const RECONNECT_JITTER_RATIO = 0.2
const WS_CLOSE_POLICY_VIOLATION = 1008
const WS_CLOSE_TOKEN_EXPIRED_REASON = 'token expired'

interface TestWindowFlags {
  __OTEL_MAGNIFY_E2E_DISABLE_WS__?: boolean
}

interface WsMessage {
  type: string
  workload?: Workload
  connected_instance_count?: number
  drifted_instance_count?: number
  event?: WorkloadEvent
  workload_id?: string
  status?: RemoteConfigStatus
  alert?: Alert
  from_hash?: string
  to_hash?: string
  reason?: string
}

type WorkloadEventType = WorkloadEvent['event_type']

function isWorkloadEventType(value: string): value is WorkloadEventType {
  return value === 'connected' || value === 'disconnected' || value === 'version_changed'
}

function patchCachedQuery<T>(queryKey: QueryKey, updater: (old: T) => T) {
  queryClient.setQueryData<T | undefined>(queryKey, (current) => {
    if (current === undefined) return current
    return updater(current)
  })
}

function mergeWorkload(current: Workload | undefined, update: Workload): Workload {
  return current ? { ...current, ...update } : update
}

function upsertWorkloadList(current: Workload[] | undefined, update: Workload) {
  if (!current) return [update]
  const idx = current.findIndex((w) => w.id === update.id)
  if (idx === -1) return [update, ...current]

  const next = [...current]
  next[idx] = mergeWorkload(next[idx], update)
  return next
}

function patchWorkloadLists(workloadId: string, updater: (workload: Workload) => Workload) {
  patchCachedQuery<Workload[]>(['workloads'], (workloads) =>
    workloads.map((workload) => (workload.id === workloadId ? updater(workload) : workload)),
  )
}

function patchWorkloadCaches(workload: Workload) {
  queryClient.setQueryData<Workload[]>(['workloads'], (current) =>
    upsertWorkloadList(current, workload),
  )
  queryClient.setQueryData<Workload>(['workload', workload.id], (current) =>
    mergeWorkload(current, workload),
  )
  queryClient.invalidateQueries({ queryKey: ['workloads'], refetchType: 'none' })
  queryClient.invalidateQueries({ queryKey: ['workload', workload.id], refetchType: 'none' })
}

function patchWorkloadEvents(event: WorkloadEvent) {
  if (!isWorkloadEventType(event.event_type)) return

  patchCachedQuery<WorkloadEvent[]>(['workload-events', event.workload_id], (events) =>
    [event, ...events.filter((existing) => existing.id !== event.id)].slice(0, 100),
  )

  patchCachedQuery<EventsStats>(['workload-events-stats', event.workload_id], (stats) => {
    const next = { ...stats }
    switch (event.event_type) {
      case 'connected':
        next.connected += 1
        break
      case 'disconnected':
        next.disconnected += 1
        next.churn_rate_per_hour = next.disconnected / 24
        break
      case 'version_changed':
        next.version_changed += 1
        break
    }
    return next
  })
}

function patchAlertCaches(alert: Alert) {
  queryClient.setQueryData<Alert[]>(['alerts'], (current) => {
    if (!current) return alert.resolved_at ? current : [alert]
    if (alert.resolved_at) return current.filter((existing) => existing.id !== alert.id)
    return [alert, ...current.filter((existing) => existing.id !== alert.id)]
  })
  queryClient.invalidateQueries({ queryKey: ['alerts'], refetchType: 'none' })
}

function dispatch(data: WsMessage) {
  const store = useStore.getState()

  switch (data.type) {
    case 'workload_update': {
      if (!data.workload) break
      patchWorkloadCaches(data.workload)
      if (
        typeof data.connected_instance_count === 'number' &&
        typeof data.drifted_instance_count === 'number'
      ) {
        store.setInstanceCounts(
          data.workload.id,
          data.connected_instance_count,
          data.drifted_instance_count,
        )
      }
      break
    }
    case 'workload_event': {
      if (!data.event) break
      patchWorkloadEvents(data.event)
      break
    }
    case 'workload_config_status': {
      if (!data.workload_id || !data.status) break
      store.setConfigStatus(data.workload_id, data.status)
      patchWorkloadLists(data.workload_id, (workload) => ({
        ...workload,
        remote_config_status: data.status,
      }))
      patchCachedQuery<Workload>(['workload', data.workload_id], (workload) => ({
        ...workload,
        remote_config_status: data.status,
      }))
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', data.workload_id] })
      break
    }
    case 'alert_update':
      if (data.alert) {
        patchAlertCaches(data.alert)
      }
      break
    case 'auto_rollback_applied':
      if (!data.workload_id || !data.from_hash || !data.to_hash) break
      store.setAutoRollback({
        workload_id: data.workload_id,
        from_hash: data.from_hash,
        to_hash: data.to_hash,
        reason: data.reason ?? '',
      })
      patchWorkloadLists(data.workload_id, (workload) => ({
        ...workload,
        active_config_hash: data.to_hash,
      }))
      patchCachedQuery<Workload>(['workload', data.workload_id], (workload) => ({
        ...workload,
        active_config_hash: data.to_hash,
      }))
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', data.workload_id] })
      break
  }
}

function warnMalformedFrame(reason: string, frame: unknown) {
  const length = typeof frame === 'string' ? frame.length : undefined
  console.warn('[otel-magnify] Ignoring malformed websocket frame', { reason, length })
}

function parseWsMessage(frame: unknown): WsMessage | null {
  if (typeof frame !== 'string') {
    warnMalformedFrame('non-string frame', frame)
    return null
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(frame)
  } catch {
    warnMalformedFrame('invalid JSON', frame)
    return null
  }

  if (
    !parsed ||
    typeof parsed !== 'object' ||
    typeof (parsed as { type?: unknown }).type !== 'string'
  ) {
    warnMalformedFrame('missing message type', frame)
    return null
  }

  return parsed as WsMessage
}

function nextReconnectDelay() {
  const exponential = Math.min(RECONNECT_BASE_MS * 2 ** reconnectAttempt, RECONNECT_CAP_MS)
  const jitter = exponential * RECONNECT_JITTER_RATIO * Math.random()
  reconnectAttempt += 1
  return Math.round(Math.min(exponential + jitter, RECONNECT_CAP_MS))
}

function scheduleReconnect() {
  if (!shouldReconnect || reconnectTimer) return
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    connectWS()
  }, nextReconnectDelay())
}

function isAuthClose(event: CloseEvent) {
  return event.code === WS_CLOSE_POLICY_VIOLATION && event.reason === WS_CLOSE_TOKEN_EXPIRED_REASON
}

export function connectWS() {
  if ((window as unknown as TestWindowFlags).__OTEL_MAGNIFY_E2E_DISABLE_WS__) return
  if (ws?.readyState === WebSocket.OPEN || ws?.readyState === WebSocket.CONNECTING) return

  shouldReconnect = true
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${protocol}//${window.location.host}/ws`)

  ws.onopen = () => {
    reconnectAttempt = 0
  }

  ws.onmessage = (event) => {
    const message = parseWsMessage(event.data)
    if (message) dispatch(message)
  }

  ws.onclose = (event) => {
    ws = null
    if (isAuthClose(event)) {
      shouldReconnect = false
      reconnectAttempt = 0
      endClientSession()
      window.location.href = '/login'
      return
    }
    scheduleReconnect()
  }

  ws.onerror = () => {
    ws?.close()
  }
}

export function disconnectWS() {
  shouldReconnect = false
  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = null
  reconnectAttempt = 0
  if (ws) ws.onclose = null
  ws?.close()
  ws = null
}

// Test-only hook exposed in dev builds to let Playwright simulate WS events
// without a live backend. Production bundles do not expose this helper.
if (typeof window !== 'undefined' && import.meta.env.DEV) {
  interface TestWindow {
    __testWsInject?: (ev: unknown) => void
  }
  ;(window as unknown as TestWindow).__testWsInject = (ev) => {
    dispatch(ev as WsMessage)
  }
}
