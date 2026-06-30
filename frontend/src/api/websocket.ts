import { useStore } from '../store'
import { queryClient } from './queryClient'
import type { Workload, Alert, RemoteConfigStatus, WorkloadEvent } from '../types'

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null

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

function dispatch(data: WsMessage) {
  const store = useStore.getState()

  switch (data.type) {
    case 'workload_update': {
      if (!data.workload) break
      store.updateWorkload(data.workload)
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
      queryClient.invalidateQueries({ queryKey: ['workloads'] })
      queryClient.invalidateQueries({ queryKey: ['workload', data.workload.id] })
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
      if (data.alert) store.addAlert(data.alert)
      queryClient.invalidateQueries({ queryKey: ['alerts'] })
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

export function connectWS() {
  if (
    (window as unknown as { __OTEL_MAGNIFY_E2E_DISABLE_WS__?: boolean })
      .__OTEL_MAGNIFY_E2E_DISABLE_WS__
  ) {
    return
  }
  if (ws?.readyState === WebSocket.OPEN) return

  const token = localStorage.getItem('token')
  if (!token) return

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${protocol}//${window.location.host}/ws?token=${token}`)

  ws.onmessage = (event) => {
    dispatch(JSON.parse(event.data))
  }

  ws.onclose = () => {
    reconnectTimer = setTimeout(connectWS, 3000)
  }

  ws.onerror = () => {
    ws?.close()
  }
}

export function disconnectWS() {
  if (reconnectTimer) clearTimeout(reconnectTimer)
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
