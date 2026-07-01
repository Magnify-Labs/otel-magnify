export function safeRemoteErrorText(value?: string): string {
  const text = value?.trim()
  if (!text) return ''
  const sanitized = sanitizeRemoteErrorText(text)
  if (!sanitized || remoteErrorLooksSensitive(sanitized)) {
    return 'Remote config error details redacted'
  }
  const compact = sanitized.split(/\s+/).join(' ')
  if (compact.length > 160) return `${compact.slice(0, 157)}…`
  return compact
}

function sanitizeRemoteErrorText(value: string): string {
  return truncateConfigSnippet(value)
    .replace(/\bauthorization\s*[:=]\s*bearer\s+[^\s,;]+/gi, 'authorization=[redacted]')
    .replace(/\bsecret[-_]?token\s*[:=]\s*[^\s,;]+/gi, 'secret=[redacted]')
    .replace(/\bsecret[-_]?token\b/gi, 'secret')
    .replace(/\btoken\s*[:=]\s*[^\s,;]+/gi, 'token=[redacted]')
    .replace(/\bpassword\s*[:=]\s*[^\s,;]+/gi, 'password=[redacted]')
    .replace(/\bapi[-_]?key\s*[:=]\s*[^\s,;]+/gi, 'api_key=[redacted]')
    .replace(/https?:\/\/[^\s,;]+/gi, '[endpoint redacted]')
    .replace(
      /\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*\.(?:internal|local|corp|lan|svc|cluster)(?::\d+)?(?:\/[^\s,;]*)?/gi,
      '[endpoint redacted]',
    )
    .replace(/\btenant[-_][a-z0-9][a-z0-9_-]*\b/gi, '[tenant redacted]')
}

function truncateConfigSnippet(value: string): string {
  const configStart = value.search(/\b(receivers|processors|exporters|extensions|service)\s*:/i)
  if (configStart === -1) return value
  return value.slice(0, configStart).trim()
}

function remoteErrorLooksSensitive(value: string): boolean {
  const lower = value.toLowerCase()
  if (value.includes('\n') || value.includes('\r')) return true
  if (lower.includes('://')) return true
  if (/\btenant[-_][a-z0-9][a-z0-9_-]*\b/i.test(value)) return true
  if (/\b(receivers|processors|exporters|extensions|service)\s*:/i.test(value)) return true
  if (
    /authorization\s*[:=]\s*(?!\[redacted\])/i.test(value) ||
    lower.includes('bearer ') ||
    /secret[-_]?token\s*[:=]\s*(?!\[redacted\])/i.test(value) ||
    /\btoken\s*[:=]\s*(?!\[redacted\])/i.test(value) ||
    /password\s*[:=]\s*(?!\[redacted\])/i.test(value) ||
    /api[-_]?key\s*[:=]\s*(?!\[redacted\])/i.test(value)
  ) {
    return true
  }
  return lower
    .split(/\s+/)
    .some((part) =>
      ['.internal', '.local', '.corp', '.lan', '.svc', '.cluster'].some((suffix) =>
        part.includes(suffix),
      ),
    )
}
