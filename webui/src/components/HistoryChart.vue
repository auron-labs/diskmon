<script setup>
import { computed } from 'vue'

const props = defineProps({ points: { type: Array, default: () => [] } })

function formatTemperature(value) {
  return Number(value.toFixed(1))
}

const chartData = computed(() => {
  const pts = [...props.points].reverse().filter((p) => p.temperature != null)
  if (pts.length < 2) return null

  const temps = pts.flatMap((p) => [p.temperature, p.temperature_max ?? p.temperature])
  const min = Math.min(...temps)
  const max = Math.max(...temps)
  const span = max - min
  const isFlat = span <= 1

  // For flat data, show a reasonable range around the value
  const yMin = isFlat ? min - 5 : min - span * 0.15
  const yMax = isFlat ? max + 5 : max + span * 0.15
  const ySpan = yMax - yMin

  const coords = pts.map((p, i) => ({
    x: (i / (pts.length - 1)) * 100,
    y: 100 - ((p.temperature - yMin) / ySpan) * 100
  }))
  const peakCoords = pts.map((p, i) => ({
    x: (i / (pts.length - 1)) * 100,
    y: 100 - (((p.temperature_max ?? p.temperature) - yMin) / ySpan) * 100
  }))

  const path = coords.map((c) => `${c.x},${c.y}`).join(' ')
  const area = `0,100 ${path} 100,100`

  // Y-axis labels: generate 3-5 evenly spaced labels
  const yLabels = []
  const labelMin = Math.floor(yMin)
  const labelMax = Math.ceil(yMax)
  const labelSpan = labelMax - labelMin
  const step = labelSpan <= 4 ? 1 : labelSpan <= 10 ? 2 : Math.ceil(labelSpan / 5)
  for (let v = labelMin; v <= labelMax; v += step) {
    const y = 100 - ((v - yMin) / ySpan) * 100
    if (y >= -5 && y <= 105) {
      yLabels.push({ value: v, y })
    }
  }

  const peakPath = peakCoords.map((c) => `${c.x},${c.y}`).join(' ')
  return { path, peakPath, area, yLabels, min, max, isFlat }
})
</script>

<template>
  <div class="rounded-xl border border-edge bg-panel p-5">
    <div class="flex items-center justify-between mb-4">
      <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Temperature History</p>
      <p v-if="chartData" class="mono text-xs text-[var(--text-secondary)]">
        <template v-if="chartData.isFlat">steady at {{ formatTemperature(chartData.min) }}°C</template>
        <template v-else>{{ formatTemperature(chartData.min) }}°C — {{ formatTemperature(chartData.max) }}°C</template>
      </p>
    </div>

    <div v-if="chartData" class="relative pl-8">
      <!-- Y-axis labels rendered as HTML for crisp text -->
      <div class="absolute left-0 top-0 bottom-0 w-7">
        <div
          v-for="label in chartData.yLabels"
          :key="label.value"
          class="mono absolute right-0 text-2xs text-[var(--text-tertiary)] leading-none"
          :style="{ top: label.y + '%', transform: 'translateY(-50%)' }"
        >{{ label.value }}°</div>
      </div>

      <svg viewBox="0 0 100 100" class="h-44 w-full" preserveAspectRatio="none">
        <!-- Horizontal grid lines -->
        <line
          v-for="label in chartData.yLabels" :key="'g' + label.value"
          x1="0" x2="100" :y1="label.y" :y2="label.y"
          stroke="var(--edge-subtle)" stroke-width="0.5"
        />

        <!-- Area fill -->
        <polygon :points="chartData.area" fill="url(#areaGrad)" />

        <!-- Line -->
        <polyline
          :points="chartData.path"
          fill="none"
          stroke="#c8ff3e"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          vector-effect="non-scaling-stroke"
        />
        <polyline
          v-if="chartData.peakPath !== chartData.path"
          :points="chartData.peakPath"
          fill="none"
          stroke="#ff9f43"
          stroke-width="1"
          stroke-dasharray="2 2"
          stroke-linecap="round"
          stroke-linejoin="round"
          vector-effect="non-scaling-stroke"
        />

        <defs>
          <linearGradient id="areaGrad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="#c8ff3e" stop-opacity="0.08" />
            <stop offset="100%" stop-color="#c8ff3e" stop-opacity="0" />
          </linearGradient>
        </defs>
      </svg>
    </div>

    <div v-else class="flex h-44 items-center justify-center">
      <p class="mono text-xs text-[var(--text-tertiary)]">No temperature data available</p>
    </div>
  </div>
</template>
