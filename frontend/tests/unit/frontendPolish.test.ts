import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import { shouldUseMergeEditor } from '../../src/lib/diffPerformance.ts'
import { routeAwarePollInterval } from '../../src/lib/queryPolling.ts'

const lazyPagesSource = readFileSync(new URL('../../src/routes/lazyPages.tsx', import.meta.url), 'utf8')
const activityTabSource = readFileSync(
  new URL('../../src/components/workloads/ActivityTab.tsx', import.meta.url),
  'utf8',
)
const loginSource = readFileSync(new URL('../../src/pages/Login.tsx', import.meta.url), 'utf8')

test('large config diffs use the lightweight diff fallback instead of mounting CodeMirror MergeView', () => {
  const largeYaml = Array.from({ length: 1_601 }, (_, index) => `receivers:\n  otlp_${index}: {}`).join('\n')

  assert.equal(shouldUseMergeEditor('receivers: {}', 'exporters: {}'), true)
  assert.equal(shouldUseMergeEditor(largeYaml, 'exporters: {}'), false)
})

test('polling intervals stop when the tab is hidden or the user leaves the active route', () => {
  assert.equal(routeAwarePollInterval('/workloads/abc', '/workloads/abc', 10_000), 10_000)
  assert.equal(routeAwarePollInterval('/workloads/abc', '/inventory', 10_000), false)
  assert.equal(routeAwarePollInterval('/workloads/abc', '/workloads/abc', 10_000, true), false)
})

test('route modules are lazy-loaded to keep the initial bundle small', () => {
  assert.match(lazyPagesSource, /React\.lazy\(\(\) => import\('\.\.\/pages\/Dashboard'\)\)/)
  assert.doesNotMatch(lazyPagesSource, /import Dashboard from '\.\.\/pages\/Dashboard'/)
  assert.doesNotMatch(lazyPagesSource, /import WorkloadDetail from '\.\.\/pages\/WorkloadDetail'/)
})

test('activity polling is route and visibility aware', () => {
  assert.match(activityTabSource, /routeAwarePollInterval/)
  assert.match(activityTabSource, /useLocation\(\)/)
  assert.doesNotMatch(activityTabSource, /refetchInterval:\s*10_000/)
})

test('login page gives a visible recovery path for expired sessions', () => {
  assert.match(loginSource, /useSearchParams\(\)/)
  assert.match(loginSource, /expired/)
  assert.match(loginSource, /Your session expired/)
})
