export const adminSSOKeys = {
  all: ['admin', 'sso'] as const,
  providers: () => [...adminSSOKeys.all, 'providers'] as const,
  provider: (id: string) => [...adminSSOKeys.all, 'provider', id] as const,
  mappings: (id: string) => [...adminSSOKeys.all, 'provider', id, 'mappings'] as const,
}

export const capabilitiesKeys = {
  all: ['capabilities'] as const,
}
