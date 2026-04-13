<script setup>
import { onMounted, ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api/client'
import HealthBadge from '../components/HealthBadge.vue'
import TemperatureBadge from '../components/TemperatureBadge.vue'
import AttributeTable from '../components/AttributeTable.vue'
import HistoryChart from '../components/HistoryChart.vue'
import { formatPowerHours, driveType, healthBorderAccent } from '../stores/format'
import { useEventStream } from '../composables/useEventStream'

const route = useRoute()
const loading = ref(true)
const error = ref('')
const detail = ref(null)
const attributes = ref([])
const history = ref([])
const tests = ref([])
const testsPage = ref(1)
const testsPerPage = 10
const testsTotal = ref(0)

const totalTestPages = computed(() => {
  const pages = Math.ceil(testsTotal.value / testsPerPage)
  return pages > 0 ? pages : 1
})

const type = computed(() => detail.value ? driveType(detail.value.device) : '')
const healthGuidance = computed(() => Array.isArray(detail.value?.health_guidance) ? detail.value.health_guidance : [])

function formatDate(d) {
  if (!d) return 'n/a'
  const date = new Date(d)
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
    + ', ' + date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

function testStatusClass(status) {
  const s = (status || '').toUpperCase()
  if (s === 'PASSED' || s === 'SUCCESS') return 'text-ok/80'
  if (s === 'STARTED' || s === 'IN_PROGRESS' || s === 'UNKNOWN') return 'text-warm/80'
  return 'text-danger/80'
}

async function loadTestsPage(page) {
  const id = route.params.id
  const resp = await api.tests(id, page, testsPerPage)
  tests.value = resp.items || []
  testsPage.value = resp.page || page
  testsTotal.value = resp.total || 0
}

async function changeTestsPage(page) {
  try {
    await loadTestsPage(page)
    error.value = ''
  } catch (err) {
    error.value = err.message
  }
}

async function loadDriveData(showLoading = false) {
  if (showLoading) loading.value = true
  try {
    const id = route.params.id
    const [d, a, h] = await Promise.all([api.drive(id), api.attributes(id), api.history(id)])
    detail.value = d
    attributes.value = a
    history.value = h
    await loadTestsPage(testsPage.value || 1)
    error.value = ''
  } catch (err) {
    error.value = err.message
  } finally {
    if (showLoading) loading.value = false
  }
}

const { connect } = useEventStream(
  ['sample.inserted', 'test.updated'],
  () => loadDriveData(false),
  { debounceMs: 400, filterDevice: () => detail.value?.device }
)

onMounted(async () => {
  await loadDriveData(true)
  connect()
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
          <p class="mono mt-2 text-sm text-[var(--text-primary)]">{{ formatDate(detail.last_seen) }}</p>
        </article>
      </div>

      <!-- Actionable health guidance if any -->
      <div
        v-if="healthGuidance.length || detail.health_reasons"
        class="rise rounded-xl border border-danger/20 bg-danger/5 p-4"
        style="animation-delay: 120ms;"
      >
        <p class="mono text-2xs uppercase tracking-wider text-danger/70 mb-2">
          {{ healthGuidance.length ? 'Recommended Actions' : 'Health Issues' }}
        </p>
        <ul v-if="healthGuidance.length" class="space-y-2">
          <li
            v-for="item in healthGuidance"
            :key="item"
            class="text-sm text-danger/90"
          >{{ item }}</li>
        </ul>
        <p v-else class="text-sm text-danger/90">{{ detail.health_reasons }}</p>
      </div>

      <HistoryChart :points="history" class="rise" style="animation-delay: 160ms;" />
      <div class="rise rounded-xl border border-edge bg-panel p-4" style="animation-delay: 190ms;">
        <div class="mb-3 flex items-center justify-between gap-3">
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">SMART Test Runs</p>
          <span class="mono text-2xs text-[var(--text-tertiary)]">{{ testsTotal }}</span>
        </div>
        <div v-if="tests.length" class="space-y-2">
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
              <p class="mono text-xs text-[var(--text-secondary)]">{{ formatDate(run.started_at) }}</p>
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
        <p v-else class="mono text-xs text-[var(--text-tertiary)]">No SMART test runs recorded.</p>
      </div>
      <AttributeTable :rows="attributes" class="rise" style="animation-delay: 200ms;" />
    </div>
  </section>
</template>
