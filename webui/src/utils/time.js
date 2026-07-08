const NA = 'n/a'
const SECOND_MS = 1000
const MINUTE_MS = 60 * SECOND_MS
const HOUR_MS = 60 * MINUTE_MS
const DAY_MS = 24 * HOUR_MS

function parseDate(value) {
  if (!value) return null
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatTimestamp(value, options = {}) {
  const date = parseDate(value)
  if (!date) return NA

  const { locale = 'en-US', timeZone = 'UTC' } = options
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    timeZone,
    timeZoneName: 'short'
  }).format(date)
}

export function formatLastUpdated(value, options = {}) {
  const date = parseDate(value)
  if (!date) return NA

  const now = parseDate(options.now) ?? new Date()
  const diffMs = Math.max(0, now.getTime() - date.getTime())

  if (diffMs < MINUTE_MS) return 'just now'
  if (diffMs < HOUR_MS) return `${Math.floor(diffMs / MINUTE_MS)}m ago`
  if (diffMs < DAY_MS) return `${Math.floor(diffMs / HOUR_MS)}h ago`
  return `${Math.floor(diffMs / DAY_MS)}d ago`
}

export function isStale(value, options = {}) {
  const date = parseDate(value)
  if (!date) return true

  const now = parseDate(options.now) ?? new Date()
  const thresholdMs = options.thresholdMs ?? 5 * MINUTE_MS
  return now.getTime() - date.getTime() > thresholdMs
}
