export function routeAwarePollInterval(
  activeRoute: string,
  currentPath: string,
  intervalMs: number,
  hidden = typeof document !== 'undefined' ? document.hidden : false,
): number | false {
  if (hidden) return false
  if (currentPath !== activeRoute) return false
  return intervalMs
}
