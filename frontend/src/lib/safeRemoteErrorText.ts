export function safeRemoteErrorText(value?: string): string {
  const text = value?.trim()
  if (!text) return ''
  const sensitivity = classifyRemoteErrorSensitivity(text)
  if (sensitivity.size > 0) {
    return sensitiveRemoteErrorLabel(text, sensitivity)
  }
  const compact = text.split(/\s+/).join(' ')
  if (compact.length > 160) return `${compact.slice(0, 157)}…`
  return compact
}

function sensitiveRemoteErrorLabel(
  value: string,
  sensitivity: Set<RemoteErrorSensitivity>,
): string {
  const safeCause = safeRemoteErrorCause(value, sensitivity)
  const labels = [
    sensitivity.has('credential') ? 'redacted credential' : undefined,
    sensitivity.has('endpoint') ? 'redacted endpoint' : undefined,
    sensitivity.has('tenant') ? 'redacted tenant' : undefined,
    sensitivity.has('config') ? 'configuration error' : undefined,
  ].filter(Boolean)
  const detail = labels.length > 0 ? labels.join('; ') : 'details redacted'

  return safeCause ? `${safeCause} — ${detail}` : detail
}

type RemoteErrorSensitivity = 'credential' | 'endpoint' | 'tenant' | 'config'

function classifyRemoteErrorSensitivity(value: string): Set<RemoteErrorSensitivity> {
  const sensitivity = new Set<RemoteErrorSensitivity>()

  if (credentialPattern.test(value)) sensitivity.add('credential')
  if (hasEndpointIdentifier(value)) sensitivity.add('endpoint')
  if (tenantPattern.test(value)) sensitivity.add('tenant')
  if (configSnippetPattern.test(value)) sensitivity.add('config')

  return sensitivity
}

function safeRemoteErrorCause(value: string, sensitivity: Set<RemoteErrorSensitivity>): string {
  const compact = value.split(/\s+/).join(' ')
  const unknownComponent = compact.match(
    /\bunknown\s+(receiver|processor|exporter|extension)\s+['"]?([a-z0-9._/-]+)['"]?/i,
  )
  if (unknownComponent) {
    const componentName = unknownComponent[2]
    if (!componentNameLooksSafe(componentName)) return ''
    return `unknown ${unknownComponent[1].toLowerCase()} '${componentName}'`
  }

  if (/\btimeout|timed out\b/i.test(value)) return 'remote config timeout'
  if (/\bconnection (?:refused|reset|lost)|\bconnect\b/i.test(value)) {
    return 'remote config connection error'
  }
  if (sensitivity.has('config')) return ''
  if (/\bvalidat(?:e|ion|or)|\brejected\b/i.test(value)) return 'collector validation error'

  return ''
}

function componentNameLooksSafe(value: string): boolean {
  if (credentialPattern.test(value)) return false
  if (tenantPattern.test(value)) return false
  if (hasEndpointIdentifier(value)) return false
  return /^[a-z0-9][a-z0-9_-]*$/i.test(value)
}

const credentialPattern =
  /\b(?:authorization\s*[:=]\s*bearer|bearer\s+[a-z0-9._~+/-]+|secret[-_]?token(?:\s*[:=]|\b)|token\s*[:=]|password\s*[:=]|api[-_]?key\s*[:=])|\b[A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|API_KEY)[A-Z0-9_]*\b/i

function hasEndpointIdentifier(value: string): boolean {
  const lower = value.toLowerCase()
  if (lower.includes('http://') || lower.includes('https://')) return true

  return lower
    .split(/[\s,;]+/)
    .some((part) => endpointSuffixes.some((suffix) => part.includes(suffix)))
}

const endpointSuffixes = [
  '.internal',
  '.local',
  '.corp',
  '.lan',
  '.svc',
  '.cluster',
  '.com',
  '.net',
  '.org',
  '.io',
  '.dev',
  '.cloud',
]

const tenantPattern = /\btenant[-_][a-z0-9][a-z0-9_-]*\b/i

const configSnippetPattern =
  /(?:^|[\s{,])(?:receivers|processors|exporters|extensions|service|endpoint|headers)\s*:/i
