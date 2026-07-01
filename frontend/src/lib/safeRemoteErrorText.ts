export function safeRemoteErrorText(value?: string): string {
  return safeRemoteErrorTextWithOptions(value, {})
}

export function safeRollbackReasonText(value?: string): string {
  return safeRemoteErrorTextWithOptions(value, rollbackReasonTextOptions)
}

const rollbackReasonTextOptions: RemoteErrorTextOptions = {
  // Rollback reasons can arrive from legacy websocket replay before backend
  // normalization, so treat bare host:port strings as endpoints in this
  // display-only path without broadening every remote error label.
  includeBareHostEndpoints: true,
}

function safeRemoteErrorTextWithOptions(
  value: string | undefined,
  options: RemoteErrorTextOptions,
): string {
  const text = value?.trim()
  if (!text) return ''
  const sensitivity = classifyRemoteErrorSensitivity(text, options)
  if (sensitivity.size > 0) {
    return sensitiveRemoteErrorLabel(text, sensitivity, options)
  }
  const compact = text.split(/\s+/).join(' ')
  if (compact.length > 160) return `${compact.slice(0, 157)}…`
  return compact
}

function sensitiveRemoteErrorLabel(
  value: string,
  sensitivity: Set<RemoteErrorSensitivity>,
  options: RemoteErrorTextOptions,
): string {
  const safeCause = safeRemoteErrorCause(value, sensitivity, options)
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

interface RemoteErrorTextOptions {
  includeBareHostEndpoints?: boolean
}

function classifyRemoteErrorSensitivity(
  value: string,
  options: RemoteErrorTextOptions,
): Set<RemoteErrorSensitivity> {
  const sensitivity = new Set<RemoteErrorSensitivity>()

  if (credentialPattern.test(value)) sensitivity.add('credential')
  if (hasEndpointIdentifier(value, options)) sensitivity.add('endpoint')
  if (tenantPattern.test(value)) sensitivity.add('tenant')
  if (configSnippetPattern.test(value)) sensitivity.add('config')

  return sensitivity
}

function safeRemoteErrorCause(
  value: string,
  sensitivity: Set<RemoteErrorSensitivity>,
  options: RemoteErrorTextOptions,
): string {
  const compact = value.split(/\s+/).join(' ')
  const unknownComponent = compact.match(
    /\bunknown\s+(receiver|processor|exporter|extension)\s+['"]?([a-z0-9._/-]+)['"]?/i,
  )
  if (unknownComponent) {
    const componentName = unknownComponent[2]
    if (!componentNameLooksSafe(componentName, options)) return ''
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

function componentNameLooksSafe(value: string, options: RemoteErrorTextOptions): boolean {
  if (credentialPattern.test(value)) return false
  if (tenantPattern.test(value)) return false
  if (hasEndpointIdentifier(value, options)) return false
  return /^[a-z0-9][a-z0-9_-]*$/i.test(value)
}

const credentialPattern =
  /\b(?:authorization\s*[:=]\s*bearer|bearer\s+[a-z0-9._~+/-]+|secret[-_]?token(?:\s*[:=]|\b)|token\s*[:=]|password\s*[:=]|api[-_]?key\s*[:=])|\b[A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|API_KEY)[A-Z0-9_]*\b/i

function hasEndpointIdentifier(value: string, options: RemoteErrorTextOptions): boolean {
  const lower = value.toLowerCase()
  if (lower.includes('http://') || lower.includes('https://')) return true
  if (options.includeBareHostEndpoints && hasBareHostEndpoint(lower)) return true

  return lower
    .split(/[\s,;]+/)
    .some((part) => endpointSuffixes.some((suffix) => part.includes(suffix)))
}

function hasBareHostEndpoint(value: string): boolean {
  for (const part of value.split(/[\s,;]+/)) {
    if (part.includes('://')) continue

    const colonIndex = part.lastIndexOf(':')
    if (colonIndex <= 0) continue

    const host = trimHostBoundary(part.slice(0, colonIndex))
    if (!isValidBareHost(host)) continue

    if (isPortPrefix(part.slice(colonIndex + 1))) return true
  }

  return false
}

function trimHostBoundary(value: string): string {
  let start = 0
  let end = value.length

  while (start < end && !isAsciiAlphaNumeric(value[start])) start += 1
  while (end > start && !isAsciiAlphaNumeric(value[end - 1])) end -= 1

  return value.slice(start, end)
}

function isValidBareHost(value: string): boolean {
  if (!value) return false

  return value.split('.').every((label) => {
    if (!label || !isAsciiAlphaNumeric(label[0])) return false
    for (const char of label) {
      if (!isAsciiAlphaNumeric(char) && char !== '-') return false
    }
    return true
  })
}

function isPortPrefix(value: string): boolean {
  let digits = 0
  while (digits < value.length && isAsciiDigit(value[digits])) {
    digits += 1
  }

  if (digits < 2 || digits > 5) return false
  if (digits === value.length) return true

  return !isAsciiAlphaNumeric(value[digits])
}

function isAsciiAlphaNumeric(value: string): boolean {
  const code = value.charCodeAt(0)
  return (code >= 48 && code <= 57) || (code >= 65 && code <= 90) || (code >= 97 && code <= 122)
}

function isAsciiDigit(value: string): boolean {
  const code = value.charCodeAt(0)
  return code >= 48 && code <= 57
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
