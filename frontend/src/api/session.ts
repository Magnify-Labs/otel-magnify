import { queryClient } from './queryClient'
import { useStore } from '../store'

export function clearClientSessionState() {
  queryClient.clear()
  useStore.getState().setMe(null)
}

export function endClientSession() {
  localStorage.removeItem('token')
  clearClientSessionState()
}

export function startClientSession(token: string) {
  clearClientSessionState()
  localStorage.setItem('token', token)
}
