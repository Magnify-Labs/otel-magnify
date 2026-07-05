import { queryClient } from './queryClient.ts'
import { useStore } from '../store/index.ts'

export function clearClientSessionState() {
  queryClient.clear()
  useStore.getState().setMe(null)
}

export function endClientSession() {
  void fetch('/api/auth/logout', {
    method: 'POST',
    credentials: 'same-origin',
    keepalive: true,
  }).catch(() => {})
  localStorage.removeItem('token')
  clearClientSessionState()
}

export function startClientSession() {
  clearClientSessionState()
  localStorage.removeItem('token')
}
