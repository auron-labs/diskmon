<script setup>
import { onMounted, onUnmounted, ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api/client'
import HealthBadge from '../components/HealthBadge.vue'
import TemperatureBadge from '../components/TemperatureBadge.vue'
import AttributeTable from '../components/AttributeTable.vue'
import HistoryChart from '../components/HistoryChart.vue'
import { formatPowerHours, driveType, healthBorderAccent } from '../stores/format'
import { useEventStream } from '../composables/useEventStream'
import { formatTimestamp, formatLastUpdated, isStale } from '../utils/time'

const route = useRoute()
const loading = ref(true)
const error = ref('')
const refreshError = ref('')
const detail = ref(null)
const attributes = ref([])
const attributesError = ref('')
const attributesLoading = ref(false)
const attributesUpdatedAt = ref(null)
const history = ref([])
const historyError = ref('')
const historyLoading = ref(false)
const historyUpdatedAt = ref(null)
const tests = ref([])
const testsError = ref('')
const testsLoading = ref(false)
const testsUpdatedAt = ref(null)
const testsPage = ref(1)
const testsPerPage = 10
const testsTotal = ref(0)
const primaryUpdatedAt = ref(null)
const refreshing = ref(false)
const now = ref(new Date())

let refreshTicker = null

const totalTestPages = computed(() => {
  const pages = Math.ceil(testsTotal.value / testsPerPage)
  return pages > 0 ? pages : 1
})

const type = computed(() => detail.value ? driveType(detail.value.device) : '')
const healthGuidance = computed(() => Array.isArray(detail.value?.health_guidance) ? detail.value.health_guidance.filter(Boolean) : [])
const dataStale = computed(() => isStale(primaryUpdatedAt.value, { now: now.value }))
const primaryUpdatedLabel = computed(() => formatTimestamp(primaryUpdatedAt.value))
const primaryUpdatedRelative = computed(() => formatLastUpdated(primaryUpdatedAt.value, { now: now.value }))
const attributesUpdatedLabel = computed(() => formatTimestamp(attributesUpdatedAt.value))
const historyUpdatedLabel = computed(() => formatTimestamp(historyUpdatedAt.value))
const testsUpdatedLabel = computed(() => formatTimestamp(testsUpdatedAt.value))

const streamStatusMeta = computed(() => {
  if (refreshError.value) {
    return {
      label: 'Errored',
      detail: refreshError.value,
      dotClass: 'bg-danger',
      panelClass: 'border-danger/30 bg-danger/5 text-danger'
    }
  }

  if (status.value === 'reconnecting') {
    const suffix = retryAttempt.value > 0 ? ` (attempt ${retryAttempt.value})` : ''
    return {
      label: 'Reconnecting',
      detail: `Live updates are reconnecting${suffix}`,
      dotClass: 'bg-warm',
      panelClass: 'border-warm/30 bg-warm/5 text-warm'
    }
  }

  if (status.value === 'disconnected' && lastError.value) {
    return {
      label: 'Errored',
      detail: lastError.value.message,
      dotClass: 'bg-danger',
      panelClass: 'border-danger/30 bg-danger/5 text-danger'
    }
  }

  if (needsResync.value || dataStale.value) {
    return {
      label: 'Stale',
      detail: needsResync.value ? 'A full resync is pending.' : 'Drive data may be out of date.',
      dotClass: 'bg-warm',
      panelClass: 'border-warm/30 bg-warm/5 text-warm'
    }
  }

  if (status.value === 'connected') {
    return {
      label: 'Fresh',
      detail: 'Live updates connected.',
      dotClass: 'bg-ok',
      panelClass: 'border-ok/30 bg-ok/5 text-ok'
    }
  }

  return {
    label: 'Connecting',
    detail: 'Connecting live updates...',
    dotClass: 'bg-accent',
    panelClass: 'border-accent/30 bg-accent/5 text-accent'
  }
})

function testStatusClass(status) {
  const s = (status || '').toUpperCase()
  if (s === 'PASSED' || s === 'SUCCESS') return 'text-ok/80'
  if (s === 'STARTED' || s === 'IN_PROGRESS' || s === 'UNKNOWN') return 'text-warm/80'
  return 'text-danger/80'
}

function markSectionUpdated(target) {
  target.value = new Date()
}

function sectionMeta(label, updatedLabel) {
  return `${label} • Updated ${updatedLabel}`
}

async function loadTestsPage(page) {
  testsLoading.value = true
  const id = route.params.id
  try {
    const resp = await api.tests(id, page, testsPerPage)
    tests.value = resp.items || []
    testsPage.value = resp.page || page
    testsTotal.value = resp.total || 0
    testsError.value = ''
    markSectionUpdated(testsUpdatedAt)
  } catch (err) {
    testsError.value = err.message
    throw err
  } finally {
    testsLoading.value = false
  }
}

async function changeTestsPage(page) {
  try {
    await loadTestsPage(page)
  } catch {}
}

async function retryTests() {
  try {
    await loadTestsPage(testsPage.value || 1)
  } catch {}
}

async function retryAttributes() {
  await loadAttributes()
}

async function retryHistory() {
  await loadHistory()
}

async function loadAttributes() {
  attributesLoading.value = true
  try {
    attributes.value = await api.attributes(route.params.id)
    attributesError.value = ''
    markSectionUpdated(attributesUpdatedAt)
  } catch (err) {
    attributesError.value = err.message
  } finally {
    attributesLoading.value = false
  }
}

async function loadHistory() {
  historyLoading.value = true
  try {
    history.value = await api.history(route.params.id)
    historyError.value = ''
    markSectionUpdated(historyUpdatedAt)
  } catch (err) {
    historyError.value = err.message
  } finally {
    historyLoading.value = false
  }
}

async function loadDriveData(showLoading = false) {
  if (showLoading) loading.value = true
  if (!showLoading && detail.value) refreshing.value = true
  try {
    const id = route.params.id
    const nextDetail = await api.drive(id)
    detail.value = nextDetail
    markSectionUpdated(primaryUpdatedAt)
    error.value = ''
    refreshError.value = ''

    await Promise.allSettled([
      loadAttributes(),
      loadHistory(),
      loadTestsPage(testsPage.value || 1),
    ])
  } catch (err) {
    if (detail.value) {
      refreshError.value = err.message
    } else {
      error.value = err.message
    }
  } finally {
    if (showLoading) loading.value = false
    refreshing.value = false
  }
}

async function manualRefresh() {
  await loadDriveData(false)
}

const { connect, status, lastError, retryAttempt, needsResync } = useEventStream(
  ['sample.inserted', 'test.updated'],
  () => loadDriveData(false),
  { debounceMs: 400, filterDevice: () => detail.value?.device }
)

onMounted(async () => {
  refreshTicker = window.setInterval(() => {
    now.value = new Date()
  }, 30000)
  await loadDriveData(true)
  connect()
})

onUnmounted(() => {
  if (!refreshTicker) return
  window.clearInterval(refreshTicker)
  refreshTicker = null
})
</script>

<template>
  <section>
    <router-link
      to="/"
      class="rise mono inline-flex items-center gap-2 text-xs uppercase tracking-[0.15em] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
    >
      <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
        <path d="M9 3L5 7L9 11" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
      Dashboard
    </router-link>

    <div v-if="loading" class="mt-6 rounded-xl border border-edge bg-panel p-6">
      <div class="flex items-center gap-3">
        <div class="h-4 w-4 rounded-full border-2 border-accent/40 border-t-accent animate-spin"></div>
        <span class="mono text-sm text-[var(--text-secondary)]">Loading drive data...</span>
      </div>
    </div>

    <div v-else-if="error" class="mt-6 rounded-xl border border-danger/40 bg-danger/5 p-6">
      <p class="mono text-sm text-danger">{{ error }}</p>
    </div>

    <div v-else class="mt-6 space-y-5">
      <div class="rise rounded-xl border border-edge bg-panel p-4">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="space-y-2">
            <div class="flex flex-wrap items-center gap-2">
              <span
                class="inline-flex items-center gap-2 rounded-full border px-2.5 py-1 mono text-2xs uppercase tracking-[0.18em]"
                :class="streamStatusMeta.panelClass"
              >
                <span class="h-1.5 w-1.5 rounded-full" :class="streamStatusMeta.dotClass"></span>
                {{ streamStatusMeta.label }}
              </span>
              <span v-if="refreshing" class="mono text-2xs uppercase tracking-[0.18em] text-[var(--text-tertiary)]">Refreshing…</span>
            </div>
            <p class="mono text-xs text-[var(--text-secondary)]">{{ streamStatusMeta.detail }}</p>
            <p class="mono text-xs text-[var(--text-tertiary)]">
              Last primary update {{ primaryUpdatedRelative }} · {{ primaryUpdatedLabel }}
            </p>
          </div>

          <button
            type="button"
            class="mono rounded-lg border border-edge px-3 py-2 text-2xs uppercase tracking-[0.18em] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)] disabled:cursor-not-allowed disabled:opacity-40"
            :disabled="loading || refreshing"
            @click="manualRefresh"
          >
            {{ refreshing ? 'Refreshing…' : 'Refresh drive' }}
          </button>
        </div>
      </div>

      <!-- Drive header -->
      <div class="rise rounded-xl border bg-panel p-6" :class="healthBorderAccent(detail.health)">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div>
            <div class="flex items-center gap-2 mb-2">
              <span class="mono text-xs uppercase tracking-wider text-[var(--text-secondary)]">{{ detail.device }}</span>
              <span
                class="mono rounded px-1.5 py-0.5 text-2xs uppercase font-medium"
                :class="type === 'nvme' ? 'bg-accent/10 text-accent/70' : 'bg-white/5 text-[var(--text-secondary)]'"
              >{{ type }}</span>
            </div>
            <h2 class="text-2xl font-bold tracking-tight">{{ detail.model || detail.device }}</h2>
            <p class="mono mt-1 text-sm text-[var(--text-secondary)]">{{ detail.serial || 'n/a' }}</p>
          </div>
          <HealthBadge :status="detail.health" />
        </div>
      </div>

      <!-- Metric cards -->
      <div class="rise grid gap-3 sm:grid-cols-2 lg:grid-cols-4" style="animation-delay: 80ms;">
        <article class="rounded-xl border border-edge bg-panel p-4">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Health Score</p>
          <p class="mt-2 text-2xl font-bold tabular-nums">{{ detail.health_score ?? '--' }}<span class="text-sm font-normal text-[var(--text-tertiary)]">/100</span></p>
        </article>

        <article class="rounded-xl border border-edge bg-panel p-4">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)] mb-2">Temperature</p>
          <TemperatureBadge :value="detail.temperature" />
        </article>

        <article class="rounded-xl border border-edge bg-panel p-4">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Power On</p>
          <p class="mono mt-2 text-sm font-medium text-[var(--text-primary)]">{{ formatPowerHours(detail.power_on_hours) }}</p>
        </article>

        <article class="rounded-xl border border-edge bg-panel p-4">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Last Seen</p>
          <p class="mono mt-2 text-sm text-[var(--text-primary)]">{{ formatTimestamp(detail.last_seen) }}</p>
        </article>
      </div>

      <!-- Health reasons if any -->
      <div
        v-if="detail.health_reasons"
        class="rise rounded-xl border border-danger/20 bg-danger/5 p-4"
        style="animation-delay: 120ms;"
      >
        <p class="mono text-2xs uppercase tracking-wider text-danger/70 mb-2">Health Issues</p>
        <p class="text-sm text-danger/90">{{ detail.health_reasons }}</p>
      </div>

      <div
        v-if="healthGuidance.length"
        class="rise rounded-xl border border-edge bg-panel p-4"
        style="animation-delay: 140ms;"
      >
        <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)] mb-3">Guidance</p>
        <div class="space-y-2">
          <div
            v-for="(guidance, index) in healthGuidance"
            :key="`${detail.device}-guidance-${index}`"
            class="rounded-lg border border-edge/70 bg-white/[0.02] px-3 py-2 text-sm text-[var(--text-secondary)]"
          >
            {{ guidance }}
          </div>
        </div>
      </div>

      <div class="rise space-y-3" style="animation-delay: 160ms;">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Temperature History</p>
          <span class="mono text-2xs text-[var(--text-tertiary)]">{{ sectionMeta('History', historyUpdatedLabel) }}</span>
        </div>
        <div v-if="historyError" class="rounded-xl border border-danger/40 bg-danger/5 p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="mono text-xs text-danger">{{ historyError }}</p>
            <button
              type="button"
              class="mono rounded border border-danger/40 px-2 py-1 text-2xs uppercase tracking-wider text-danger"
              :disabled="historyLoading"
              @click="retryHistory"
            >
              {{ historyLoading ? 'Retrying...' : 'Retry history' }}
            </button>
          </div>
        </div>
        <div v-if="historyLoading && !history.length" class="rounded-xl border border-edge bg-panel p-4 mono text-xs text-[var(--text-tertiary)]">
          Loading history...
        </div>
        <HistoryChart v-else-if="history.length" :points="history" />
        <div v-else-if="!historyError" class="rounded-xl border border-edge bg-panel p-4 mono text-xs text-[var(--text-tertiary)]">
          No history recorded yet.
        </div>
      </div>

      <div class="rise rounded-xl border border-edge bg-panel p-4" style="animation-delay: 190ms;">
        <div class="mb-3 flex items-center justify-between gap-3">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">SMART Test Runs</p>
          <span class="mono text-2xs text-[var(--text-tertiary)]">{{ sectionMeta(`${testsTotal} tests`, testsUpdatedLabel) }}</span>
        </div>
        <div v-if="testsError" class="rounded-lg border border-danger/40 bg-danger/5 p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="mono text-xs text-danger">{{ testsError }}</p>
            <button
              type="button"
              class="mono rounded border border-danger/40 px-2 py-1 text-2xs uppercase tracking-wider text-danger"
              :disabled="testsLoading"
              @click="retryTests"
            >
              {{ testsLoading ? 'Retrying...' : 'Retry tests' }}
            </button>
          </div>
        </div>
        <div v-if="testsLoading && !tests.length" class="mono text-xs text-[var(--text-tertiary)]">Loading SMART test runs...</div>
        <div v-else-if="tests.length" class="space-y-2">
          <div
            v-for="run in tests"
            :key="run.id"
            class="grid grid-cols-[72px_110px_1fr] items-start gap-3 rounded-lg border border-edge/70 px-3 py-2"
          >
            <span class="mono text-2xs uppercase tracking-wider text-[var(--text-secondary)]">{{ run.test_type }}</span>
            <span
              class="mono text-2xs uppercase tracking-wider"
              :class="testStatusClass(run.status)"
            >{{ run.status }}</span>
            <div>
              <p class="mono text-xs text-[var(--text-secondary)]">{{ formatTimestamp(run.started_at) }}</p>
              <p class="mt-1 break-words text-xs text-[var(--text-tertiary)]">{{ run.message || 'n/a' }}</p>
            </div>
          </div>
          <div class="mt-3 flex items-center justify-between">
            <span class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">
              Page {{ testsPage }} of {{ totalTestPages }}
            </span>
            <div class="flex items-center gap-2">
              <button
                type="button"
                class="mono rounded border border-edge px-2 py-1 text-2xs uppercase tracking-wider text-[var(--text-secondary)] disabled:opacity-40"
                :disabled="testsPage <= 1"
                @click="changeTestsPage(Math.max(1, testsPage - 1))"
              >
                Prev
              </button>
              <button
                type="button"
                class="mono rounded border border-edge px-2 py-1 text-2xs uppercase tracking-wider text-[var(--text-secondary)] disabled:opacity-40"
                :disabled="testsPage >= totalTestPages"
                @click="changeTestsPage(Math.min(totalTestPages, testsPage + 1))"
              >
                Next
              </button>
            </div>
          </div>
        </div>
        <p v-else-if="!testsError" class="mono text-xs text-[var(--text-tertiary)]">No SMART test runs recorded.</p>
      </div>

      <div class="rise space-y-3" style="animation-delay: 200ms;">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">SMART Attributes</p>
          <span class="mono text-2xs text-[var(--text-tertiary)]">{{ sectionMeta('Attributes', attributesUpdatedLabel) }}</span>
        </div>
        <div v-if="attributesError" class="rounded-xl border border-danger/40 bg-danger/5 p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="mono text-xs text-danger">{{ attributesError }}</p>
            <button
              type="button"
              class="mono rounded border border-danger/40 px-2 py-1 text-2xs uppercase tracking-wider text-danger"
              :disabled="attributesLoading"
              @click="retryAttributes"
            >
              {{ attributesLoading ? 'Retrying...' : 'Retry attributes' }}
            </button>
          </div>
        </div>
        <div v-if="attributesLoading && !attributes.length" class="rounded-xl border border-edge bg-panel p-4 mono text-xs text-[var(--text-tertiary)]">
          Loading attributes...
        </div>
        <AttributeTable v-else-if="attributes.length" :rows="attributes" />
        <div v-else-if="!attributesError" class="rounded-xl border border-edge bg-panel p-4 mono text-xs text-[var(--text-tertiary)]">
          No SMART attributes recorded.
        </div>
      </div>
    </div>
  </section>
</template>
