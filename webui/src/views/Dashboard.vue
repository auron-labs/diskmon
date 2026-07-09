<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api/client'
import DriveCard from '../components/DriveCard.vue'
import { useEventStream } from '../composables/useEventStream'
import { driveType } from '../stores/format'
import { formatLastUpdated, formatTimestamp, isStale } from '../utils/time'

const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
const drives = ref([])
const search = ref('')
const healthFilter = ref('all')
const lastSuccessfulUpdateAt = ref(null)
const now = ref(new Date())

let nowTimer = null

const healthFilters = [
  { value: 'all', label: 'All health' },
  { value: 'green', label: 'Healthy' },
  { value: 'yellow', label: 'Warning' },
  { value: 'red', label: 'Critical' },
  { value: 'unknown', label: 'Unknown' }
]

function driveHealthValue(health) {
  const normalized = (health || '').toLowerCase()
  if (normalized === 'green' || normalized === 'yellow' || normalized === 'red') return normalized
  return 'unknown'
}

const filteredDrives = computed(() => {
  const query = search.value.trim().toLowerCase()

  return drives.value.filter((drive) => {
    const matchesHealth = healthFilter.value === 'all' || driveHealthValue(drive.health) === healthFilter.value
    if (!matchesHealth) return false

    if (!query) return true

    const haystack = [drive.device, drive.model, drive.serial, driveType(drive.device)]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()

    return haystack.includes(query)
  })
})

const grouped = computed(() => {
  const groups = {}
  filteredDrives.value.forEach((drive) => {
    const type = driveType(drive.device)
    if (!groups[type]) groups[type] = []
    groups[type].push(drive)
  })

  const order = ['nvme', 'hdd', 'unknown']
  return order
    .filter((key) => groups[key]?.length)
    .map((key) => ({ type: key, drives: groups[key] }))
})

const stats = computed(() => {
  const all = filteredDrives.value
  return {
    total: all.length,
    healthy: all.filter((drive) => drive.health === 'GREEN').length,
    warning: all.filter((drive) => drive.health === 'YELLOW').length,
    critical: all.filter((drive) => drive.health === 'RED').length
  }
})

const labels = {
  nvme: 'NVMe Drives',
  hdd: 'Hard Drives',
  unknown: 'Other Devices'
}

const filtersActive = computed(() => search.value.trim() !== '' || healthFilter.value !== 'all')
const hasVisibleDrives = computed(() => filteredDrives.value.length > 0)
const hasAnyDrives = computed(() => drives.value.length > 0)
const stale = computed(() => isStale(lastSuccessfulUpdateAt.value, { now: now.value }))

async function reload({ showLoading = false } = {}) {
  if (showLoading) {
    loading.value = true
  } else {
    refreshing.value = true
  }

  try {
    drives.value = await api.drives()
    error.value = ''
    lastSuccessfulUpdateAt.value = new Date()
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load drives'
  } finally {
    if (showLoading) {
      loading.value = false
    } else {
      refreshing.value = false
    }
  }
}

async function refreshNow() {
  await reload()
}

const { connect, status, lastEventAt, lastError, retryAttempt, needsResync } = useEventStream(
  ['sample.inserted', 'test.updated'],
  () => reload(),
  { debounceMs: 300 }
)

const streamLabel = computed(() => {
  if (status.value === 'connected') return 'Live'
  if (status.value === 'reconnecting') return `Reconnecting${retryAttempt.value ? ` (${retryAttempt.value})` : ''}`
  if (status.value === 'connecting') return 'Connecting'
  return 'Disconnected'
})

const streamClass = computed(() => {
  if (status.value === 'connected' && !needsResync.value) return 'border-ok/30 bg-ok/10 text-ok'
  if (status.value === 'reconnecting' || needsResync.value) return 'border-warm/30 bg-warm/10 text-warm'
  if (status.value === 'connecting') return 'border-accent/30 bg-accent/10 text-accent/80'
  return 'border-danger/30 bg-danger/10 text-danger'
})

const lastUpdatedText = computed(() => formatLastUpdated(lastSuccessfulUpdateAt.value, { now: now.value }))
const lastUpdatedTimestamp = computed(() => formatTimestamp(lastSuccessfulUpdateAt.value))
const lastEventText = computed(() => formatLastUpdated(lastEventAt.value, { now: now.value }))
const streamErrorText = computed(() => lastError.value?.message || '')
const primaryActionLabel = computed(() => (error.value ? 'Retry' : 'Refresh'))

onMounted(async () => {
  nowTimer = window.setInterval(() => {
    now.value = new Date()
  }, 30000)

  await reload({ showLoading: true })
  connect()
})

onUnmounted(() => {
  if (nowTimer) {
    clearInterval(nowTimer)
    nowTimer = null
  }
})
</script>

<template>
  <section>
    <div v-if="loading" class="rounded-xl border border-edge bg-panel p-6">
      <div class="flex items-center gap-3">
        <div class="h-4 w-4 animate-spin rounded-full border-2 border-accent/40 border-t-accent"></div>
        <span class="mono text-sm text-[var(--text-secondary)]">Scanning drives...</span>
      </div>
    </div>

    <div v-else-if="error && !hasAnyDrives" class="rounded-xl border border-danger/40 bg-danger/5 p-6">
      <p class="mono text-sm text-danger">{{ error }}</p>
      <button
        class="mono mt-4 inline-flex items-center rounded-lg border border-danger/40 px-3 py-2 text-xs uppercase tracking-wider text-danger transition hover:bg-danger/10"
        type="button"
        @click="refreshNow"
      >
        Retry
      </button>
    </div>

    <div v-else>
      <div v-if="error" class="mb-4 rounded-xl border border-danger/40 bg-danger/5 px-4 py-3">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <p class="mono text-xs uppercase tracking-[0.2em] text-danger/80">Refresh failed</p>
            <p class="mono mt-1 text-sm text-danger">{{ error }}</p>
          </div>
          <button
            class="mono inline-flex items-center justify-center rounded-lg border border-danger/40 px-3 py-2 text-xs uppercase tracking-wider text-danger transition hover:bg-danger/10"
            type="button"
            :disabled="refreshing"
            @click="refreshNow"
          >
            {{ refreshing ? 'Retrying…' : 'Retry' }}
          </button>
        </div>
      </div>

      <div class="rise mb-8 rounded-xl border border-edge bg-panel px-5 py-4">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div class="flex flex-wrap items-center gap-6">
            <div class="flex items-center gap-2">
              <span class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Fleet</span>
              <span class="mono text-sm font-medium">
                {{ stats.total }}<template v-if="filtersActive"> of {{ drives.length }}</template> drives
              </span>
            </div>
            <div class="h-4 w-px bg-edge"></div>
            <div class="flex items-center gap-4">
              <div v-if="stats.healthy" class="flex items-center gap-1.5">
                <span class="h-1.5 w-1.5 rounded-full bg-ok"></span>
                <span class="mono text-xs text-ok/80">{{ stats.healthy }}</span>
              </div>
              <div v-if="stats.warning" class="flex items-center gap-1.5">
                <span class="h-1.5 w-1.5 rounded-full bg-warm"></span>
                <span class="mono text-xs text-warm/80">{{ stats.warning }}</span>
              </div>
              <div v-if="stats.critical" class="flex items-center gap-1.5">
                <span class="h-1.5 w-1.5 rounded-full bg-danger" style="animation: pulse-dot 1.5s ease-in-out infinite"></span>
                <span class="mono text-xs text-danger/80">{{ stats.critical }}</span>
              </div>
            </div>
          </div>

          <div class="flex flex-wrap items-center gap-2.5">
            <div class="rounded-lg border px-3 py-2" :class="streamClass">
              <div class="flex items-center gap-2">
                <span class="h-2 w-2 rounded-full bg-current"></span>
                <span class="mono text-xs uppercase tracking-wider">{{ streamLabel }}</span>
                <span v-if="needsResync" class="mono text-2xs uppercase tracking-wider text-warm">Resync pending</span>
              </div>
              <p class="mono mt-1 text-2xs text-[var(--text-secondary)]">Last event {{ lastEventText }}</p>
            </div>

            <div class="rounded-lg border border-edge bg-[var(--bg)]/30 px-3 py-2">
              <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Last successful update</p>
              <p class="mono mt-1 text-xs text-[var(--text-primary)]">{{ lastUpdatedText }}</p>
              <p class="mono mt-1 text-2xs text-[var(--text-secondary)]">{{ lastUpdatedTimestamp }}</p>
            </div>

            <button
              class="mono inline-flex items-center justify-center rounded-lg border border-edge px-3 py-2 text-xs uppercase tracking-wider text-[var(--text-primary)] transition hover:border-accent/40 hover:text-accent disabled:cursor-not-allowed disabled:opacity-60"
              type="button"
              :disabled="refreshing"
              @click="refreshNow"
            >
              {{ refreshing ? 'Refreshing…' : primaryActionLabel }}
            </button>
          </div>
        </div>

        <div class="mt-4 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div class="flex flex-wrap items-center gap-2">
            <span
              v-if="stale"
              class="mono rounded-full border border-warm/30 bg-warm/10 px-2.5 py-1 text-2xs uppercase tracking-wider text-warm"
            >
              Data may be stale
            </span>
            <span
              v-if="status !== 'connected'"
              class="mono rounded-full border border-danger/30 bg-danger/10 px-2.5 py-1 text-2xs uppercase tracking-wider text-danger"
            >
              Live updates unavailable
            </span>
            <span
              v-if="streamErrorText"
              class="mono rounded-full border border-danger/20 bg-danger/5 px-2.5 py-1 text-2xs text-danger/80"
            >
              {{ streamErrorText }}
            </span>
          </div>

          <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_180px] lg:min-w-[480px]">
            <label class="block">
              <span class="mono mb-1.5 block text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Search</span>
              <input
                v-model="search"
                class="mono w-full rounded-lg border border-edge bg-[var(--bg)]/40 px-3 py-2 text-sm text-[var(--text-primary)] outline-hidden transition focus:border-accent/50"
                type="search"
                placeholder="Device, model, serial, type"
              >
            </label>

            <label class="block">
              <span class="mono mb-1.5 block text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Health</span>
              <select
                v-model="healthFilter"
                class="mono w-full rounded-lg border border-edge bg-[var(--bg)]/40 px-3 py-2 text-sm text-[var(--text-primary)] outline-hidden transition focus:border-accent/50"
              >
                <option v-for="option in healthFilters" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
            </label>
          </div>
        </div>
      </div>

      <div v-if="!hasVisibleDrives" class="rounded-xl border border-edge bg-panel p-6">
        <p class="mono text-sm text-[var(--text-primary)]">No drives match the current filters.</p>
      </div>

      <div v-for="(group, gi) in grouped" :key="group.type" :class="gi > 0 ? 'mt-10' : ''">
        <div class="rise mb-4 flex items-center gap-3" :style="{ animationDelay: `${gi * 100}ms` }">
          <h2 class="mono text-xs font-medium uppercase tracking-[0.2em] text-[var(--text-tertiary)]">
            {{ labels[group.type] || group.type }}
          </h2>
          <div class="h-px flex-1 bg-edge/50"></div>
          <span class="mono text-2xs text-[var(--text-tertiary)]">{{ group.drives.length }}</span>
        </div>

        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          <DriveCard
            v-for="(drive, di) in group.drives"
            :key="drive.id"
            :drive="drive"
            :index="gi * 10 + di"
          />
        </div>
      </div>
    </div>
  </section>
</template>
