export const capabilityStates = ['enabled', 'disabled', 'read_only'] as const
export type CapabilityState = (typeof capabilityStates)[number]

export type Capability = {
  id: string
  state: CapabilityState
  reason_code?: string
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function parseCapability(value: unknown): Capability {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.state !== 'string') {
    throw new Error('Invalid capability entry')
  }
  if (!capabilityStates.includes(value.state as CapabilityState)) {
    throw new Error(`Invalid capability state for ${value.id}`)
  }
  const state = value.state as CapabilityState
  if (state === 'enabled' && value.reason_code !== undefined) {
    throw new Error(`Enabled capability ${value.id} must not include reason_code`)
  }
  if (state !== 'enabled' && (typeof value.reason_code !== 'string' || value.reason_code === '')) {
    throw new Error(`Disabled capability ${value.id} requires reason_code`)
  }
  return {
    id: value.id,
    state,
    ...(typeof value.reason_code === 'string' ? { reason_code: value.reason_code } : {}),
  }
}

export function parseCapabilitiesDocument(value: unknown): ReadonlyMap<string, Capability> {
  if (!isRecord(value) || value.api_version !== 'v1' || !Array.isArray(value.capabilities)) {
    throw new Error('Invalid capabilities document')
  }
  const capabilities = new Map<string, Capability>()
  for (const rawCapability of value.capabilities) {
    const capability = parseCapability(rawCapability)
    if (capabilities.has(capability.id)) {
      throw new Error(`Duplicate capability id ${capability.id}`)
    }
    capabilities.set(capability.id, capability)
  }
  return capabilities
}
