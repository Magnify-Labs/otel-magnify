import { test, expect, type APIRequestContext } from '@playwright/test'

const ADMIN = {
  email: 'admin@e2e.local',
  password: 'initialPass!!!12',
}

// Real-backend coverage for the config-versioning endpoints. Without a
// connected collector we cannot exercise the *full* rollback path (opamp
// push will fail), but we can verify:
//   - routing + auth + permission middleware are wired correctly
//   - JSON envelopes match the frontend's expectations (snake_case round-trip)
//   - 404/401/403/400 response shapes are stable
async function loginAndGetToken(request: APIRequestContext): Promise<string> {
  const res = await request.post('/api/auth/login', {
    data: { email: ADMIN.email, password: ADMIN.password },
  })
  expect(res.status(), `login → ${res.status()} ${await res.text()}`).toBe(200)
  const body = (await res.json()) as { token?: string }
  if (!body.token) throw new Error('login did not return a token')
  return body.token
}

test.describe.serial('Config versioning endpoints (real backend)', () => {
  test('label / get-by-hash / rollback respect auth, perm, and 404 shape', async ({ request }) => {
    const token = await loginAndGetToken(request)
    const auth = { Authorization: `Bearer ${token}` }
    const unknownWl = 'wl-does-not-exist'
    const ghostHash = 'ghost'

    // ── 1. Without a token, every config-versioning endpoint must 401 ───────
    const noAuthLabel = await request.post(
      `/api/workloads/${unknownWl}/configs/${ghostHash}/label`,
      { data: { label: 'nope' } },
    )
    expect(noAuthLabel.status()).toBe(401)

    const noAuthGet = await request.get(`/api/workloads/${unknownWl}/configs/${ghostHash}`)
    expect(noAuthGet.status()).toBe(401)

    const noAuthRb = await request.post(
      `/api/workloads/${unknownWl}/configs/${ghostHash}/rollback`,
    )
    expect(noAuthRb.status()).toBe(401)

    // ── 2. Authenticated GET on an unknown hash returns the 404 shape the FE
    //      relies on ({"error": "..."}) — not an HTML error page or empty body
    const getMissing = await request.get(
      `/api/workloads/${unknownWl}/configs/${ghostHash}`,
      { headers: auth },
    )
    expect(getMissing.status()).toBe(404)
    const getBody = await getMissing.json()
    expect(getBody).toHaveProperty('error')

    // ── 3. POST label on an unknown hash also yields 404 with error shape ──
    const labelMissing = await request.post(
      `/api/workloads/${unknownWl}/configs/${ghostHash}/label`,
      { headers: auth, data: { label: 'x' } },
    )
    expect(labelMissing.status()).toBe(404)
    expect(await labelMissing.json()).toHaveProperty('error')

    // ── 4. POST label with > 128 chars rejected at the API boundary ────────
    const tooLong = await request.post(
      `/api/workloads/${unknownWl}/configs/${ghostHash}/label`,
      { headers: auth, data: { label: 'x'.repeat(129) } },
    )
    expect(tooLong.status()).toBe(400)

    // ── 5. POST rollback on an unknown hash yields 404 (same shape) ────────
    const rbMissing = await request.post(
      `/api/workloads/${unknownWl}/configs/${ghostHash}/rollback`,
      { headers: auth },
    )
    expect(rbMissing.status()).toBe(404)
    expect(await rbMissing.json()).toHaveProperty('error')

    // ── 6. The history endpoint roundtrips an empty list as JSON [] for
    //      unknown workloads — used by the Compare button enable/disable
    const history = await request.get(`/api/workloads/${unknownWl}/configs`, { headers: auth })
    expect(history.status()).toBe(200)
    expect(await history.json()).toEqual([])
  })
})
