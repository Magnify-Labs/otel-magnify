import { useStore } from '../store'
import { queryClient } from './queryClient'
import type { Workload, Alert, RemoteConfigStatus, WorkloadEvent } from '../types'

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectAttempt = 0
let shouldReconnect = false

const RECONNECT_BASE_MS = 1_000
const RECONNECT_CAP_MS = 30_000
const RECONNECT_JITTER_RATIO = 0.2

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

function upsertAlertList(current: Alert[] | undefined, update: Alert) {
  if (!current) return update.resolved_at ? current : [update]
  if (update.resolved_at) return current.filter((a) => a.id !== update.id)

  const idx = current.findIndex((a) => a.id === update.id)
  if (idx === -1) return [update, ...current]

  const next = [...current]
  next[idx] = { ...next[idx], ...update }
  return next
}

function dispatch(data: WsMessage) {
  const store = useStore.getState()

  switch (data.type) {
    case 'workload_update': {
      if (!data.workload) break
      queryClient.setQueryData<Workload[]>(['workloads'], (current) =>
        upsertWorkloadList(current, data.workload!),
      )
      queryClient.setQueryData<Workload>(['workload', data.workload.id], (current) =>
        mergeWorkload(current, data.workload!),
      )
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
      queryClient.invalidateQueries({ queryKey: ['workloads'], refetchType: 'none' })
      queryClient.invalidateQueries({
        queryKey: ['workload', data.workload.id],
        refetchType: 'none',
      })
      break
    }
    case 'workload_event': {
      if (!data.event) break
      const wid = data.event.workload_id
      queryClient.invalidateQueries({ queryKey: ['workload-events', wid] })
      queryClient.invalidateQueries({ queryKey: ['workload-events-stats', wid] })
      break
    }
    case 'workload_config_status': {
      if (!data.workload_id || !data.status) break
      store.setConfigStatus(data.workload_id, data.status)
      queryClient.invalidateQueries({ queryKey: ['workload', data.workload_id] })
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', data.workload_id] })
      break
    }
    case 'alert_update':
      if (data.alert) {
        queryClient.setQueryData<Alert[]>(['alerts'], (current) =>
          upsertAlertList(current, data.alert!),
        )
      }
      queryClient.invalidateQueries({ queryKey: ['alerts'], refetchType: 'none' })
      break
    case 'auto_rollback_applied':
      if (!data.workload_id || !data.from_hash || !data.to_hash) break
      store.setAutoRollback({
        workload_id: data.workload_id,
        from_hash: data.from_hash,
        to_hash: data.to_hash,
        reason: data.reason ?? '',
      })
      queryClient.invalidateQueries({ queryKey: ['workload', data.workload_id] })
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

  ws.onclose = () => {
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

// Test-only hook exposed on window to let Playwright simulate WS events
// without a live backend. No-op in production when nothing calls it.
if (typeof window !== 'undefined') {
  interface TestWindow {
    __testWsInject?: (ev: unknown) => void
  }
  ;(window as unknown as TestWindow).__testWsInject = (ev) => {
    dispatch(ev as WsMessage)
  }
}
