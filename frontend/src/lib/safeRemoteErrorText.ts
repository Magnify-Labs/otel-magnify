export function safeRemoteErrorText(value?: string): string {
  const text = value?.trim()
  if (!text) return ''
  if (remoteErrorLooksSensitive(text)) {
    return 'Remote config error details redacted'
  }
  const compact = text.split(/\s+/).join(' ')
  if (compact.length > 160) return `${compact.slice(0, 157)}…`
  return compact
}

function remoteErrorLooksSensitive(value: string): boolean {
  const lower = value.toLowerCase()
  if (value.includes('\n') || value.includes('\r')) return true
  if (lower.includes('://')) return true
  if (/\btenant[-_][a-z0-9][a-z0-9_-]*\b/i.test(value)) return true
  if (/\b(receivers|processors|exporters|extensions|service)\s*:/i.test(value)) return true
  if (
    lower.includes('authorization=') ||
    lower.includes('authorization:') ||
    lower.includes('bearer ') ||
    lower.includes('secret_token') ||
    lower.includes('secret-token') ||
    lower.includes('token=') ||
    lower.includes('token:') ||
    lower.includes('password=') ||
    lower.includes('password:') ||
    lower.includes('api_key') ||
    lower.includes('api-key')
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
