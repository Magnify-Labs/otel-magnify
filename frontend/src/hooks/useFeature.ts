import { useQuery } from '@tanstack/react-query'
import { featuresAPI } from '../api/admin'
import { featuresKeys } from '../api/queryKeys'

export function useFeatures() {
  return useQuery({
    queryKey: featuresKeys.all,
    queryFn: featuresAPI.get,
    staleTime: Infinity,
  })
}

// Returning the loading state alongside `enabled` lets gated pages avoid
// redirecting on the initial render before /api/features resolves — a stale
// `enabled === false` would otherwise trigger a `<Navigate />` race.
export function useFeature(flag: string): { enabled: boolean; isLoading: boolean } {
  const { data, isLoading } = useFeatures()
  return { enabled: data?.[flag] === true, isLoading }
}
