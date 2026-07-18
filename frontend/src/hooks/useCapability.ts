import { useQuery } from '@tanstack/react-query'
import { capabilitiesAPI } from '../api/capabilities'
import type { CapabilityState } from '../api/capabilitiesContract'
import { capabilitiesKeys } from '../api/queryKeys'

export function useCapabilities() {
  return useQuery({
    queryKey: capabilitiesKeys.all,
    queryFn: capabilitiesAPI.get,
    staleTime: Infinity,
  })
}

export function useCapability(id: string): {
  state: CapabilityState | undefined
  enabled: boolean
  readOnly: boolean
  isLoading: boolean
  isError: boolean
  error: Error | null
} {
  const query = useCapabilities()
  const state = query.data?.get(id)?.state
  return {
    state,
    enabled: state === 'enabled',
    readOnly: state === 'read_only',
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
  }
}
