import { create } from 'zustand'
import type { RemoteConfigStatus, AutoRollbackEvent, MeResponse, UserPreferences } from '../types'

interface AppState {
  configStatus: Record<string, RemoteConfigStatus | undefined>
  lastRollback: Record<string, AutoRollbackEvent | undefined>
  connectedInstanceCounts: Record<string, number | undefined>
  driftedInstanceCounts: Record<string, number | undefined>

  me: MeResponse | null
  sessionChecked: boolean

  setConfigStatus: (workloadId: string, status: RemoteConfigStatus) => void
  setAutoRollback: (ev: AutoRollbackEvent) => void
  clearAutoRollback: (workloadId: string) => void

  setInstanceCounts: (workloadId: string, connected: number, drifted: number) => void

  setMe: (me: MeResponse | null) => void
  setSessionChecked: (checked: boolean) => void
  updateMyPreferences: (prefs: UserPreferences) => void
}

export const useStore = create<AppState>((set) => ({
  configStatus: {},
  lastRollback: {},
  connectedInstanceCounts: {},
  driftedInstanceCounts: {},

  me: null,
  sessionChecked: false,

  setConfigStatus: (workloadId, status) =>
    set((state) => ({ configStatus: { ...state.configStatus, [workloadId]: status } })),
  setAutoRollback: (ev) =>
    set((state) => ({ lastRollback: { ...state.lastRollback, [ev.workload_id]: ev } })),
  clearAutoRollback: (workloadId) =>
    set((state) => {
      const next = { ...state.lastRollback }
      delete next[workloadId]
      return { lastRollback: next }
    }),

  setInstanceCounts: (workloadId, connected, drifted) =>
    set((state) => ({
      connectedInstanceCounts: { ...state.connectedInstanceCounts, [workloadId]: connected },
      driftedInstanceCounts: { ...state.driftedInstanceCounts, [workloadId]: drifted },
    })),

  setMe: (me) => set({ me }),
  setSessionChecked: (checked) => set({ sessionChecked: checked }),
  updateMyPreferences: (prefs) =>
    set((state) => (state.me ? { me: { ...state.me, preferences: prefs } } : {})),
}))
