const healthMeta = {
  RED: {
    color: 'text-danger border-danger/40',
    glow: 'glow-danger pulse-danger',
    border: 'border-danger/25',
    label: 'Critical',
    icon: '!',
    background: 'bg-danger/10',
    dot: 'bg-danger',
    accent: 'bg-danger'
  },
  YELLOW: {
    color: 'text-warm border-warm/40',
    glow: 'glow-warn',
    border: 'border-warm/20',
    label: 'Warning',
    icon: '~',
    background: 'bg-warm/10',
    dot: 'bg-warm',
    accent: 'bg-warm'
  },
  GREEN: {
    color: 'text-ok border-ok/40',
    glow: 'glow-ok',
    border: 'border-ok/15',
    label: 'Healthy',
    icon: '+',
    background: 'bg-ok/8',
    dot: 'bg-ok',
    accent: 'bg-ok'
  },
  UNKNOWN: {
    color: 'text-[var(--text-secondary)] border-[var(--edge)]',
    glow: '',
    border: 'border-edge',
    label: 'Unknown',
    icon: '?',
    background: 'bg-white/5',
    dot: 'bg-[var(--text-tertiary)]',
    accent: 'bg-[var(--edge)]'
  }
}

export function normalizeHealthStatus(status) {
  const normalized = typeof status === 'string' ? status.toUpperCase() : ''
  return Object.hasOwn(healthMeta, normalized) ? normalized : 'UNKNOWN'
}

function getHealthMeta(status) {
  return healthMeta[normalizeHealthStatus(status)]
}

export const healthColor = (status) => getHealthMeta(status).color
export const healthGlow = (status) => getHealthMeta(status).glow
export const healthBorderAccent = (status) => getHealthMeta(status).border
export const healthLabel = (status) => getHealthMeta(status).label
export const healthIcon = (status) => getHealthMeta(status).icon
export const healthBackground = (status) => getHealthMeta(status).background
export const healthDot = (status) => getHealthMeta(status).dot
export const healthAccent = (status) => getHealthMeta(status).accent

export function tempText(v) {
  return v === null || v === undefined ? 'n/a' : `${v}°C`
}

export function formatPowerHours(hours) {
  if (hours == null) return 'n/a'
  if (hours < 1000) return `${hours}h`
  const years = (hours / 8760).toFixed(1)
  return `${hours.toLocaleString()}h (${years}y)`
}

export function driveType(device) {
  if (!device) return 'unknown'
  if (device.includes('nvme')) return 'nvme'
  return 'hdd'
}
