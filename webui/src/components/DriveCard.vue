<script setup>
import { computed } from 'vue'
import HealthBadge from './HealthBadge.vue'
import TemperatureBadge from './TemperatureBadge.vue'
import { driveType, formatPowerHours, healthAccent, healthBorderAccent, healthGlow } from '../stores/format'

const props = defineProps({ drive: { type: Object, required: true }, index: { type: Number, default: 0 } })

const borderClass = computed(() => healthBorderAccent(props.drive.health))
const glowClass = computed(() => healthGlow(props.drive.health))
const type = computed(() => driveType(props.drive.device))

const topAccent = computed(() => healthAccent(props.drive.health))
</script>

<template>
  <router-link
    :to="`/drives/${drive.id}`"
    class="rise group relative block overflow-hidden rounded-xl border bg-panel transition-all duration-300 hover:bg-panel-elevated hover:shadow-panel-hover hover:-translate-y-0.5"
    :class="[borderClass, glowClass]"
    :style="{ animationDelay: `${index * 40}ms` }"
  >
    <!-- Top accent line -->
    <div class="h-[2px] w-full" :class="topAccent" style="opacity: 0.6;"></div>

    <div class="p-4">
      <!-- Header row -->
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">{{ drive.device }}</span>
            <span
              class="mono rounded-sm px-1 py-0.5 text-2xs uppercase"
              :class="type === 'nvme' ? 'bg-accent/10 text-accent/60' : 'bg-white/5 text-[var(--text-tertiary)]'"
            >{{ type }}</span>
          </div>
          <h3 class="mt-1.5 truncate text-sm font-semibold leading-tight group-hover:text-white transition-colors">
            {{ drive.model || 'Unknown model' }}
          </h3>
        </div>
        <HealthBadge :status="drive.health" compact />
      </div>

      <!-- Metrics -->
      <div class="mt-4 space-y-2.5">
        <div>
          <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)] mb-1">Temp</p>
          <TemperatureBadge :value="drive.temperature" />
        </div>

        <div class="flex items-baseline justify-between gap-3">
          <div class="min-w-0">
            <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Power</p>
            <p class="mono mt-0.5 text-xs text-[var(--text-secondary)]">{{ formatPowerHours(drive.power_on_hours) }}</p>
          </div>
          <div class="text-right min-w-0">
            <p class="mono text-2xs uppercase tracking-wider text-[var(--text-tertiary)]">Serial</p>
            <p class="mono mt-0.5 truncate text-xs text-[var(--text-tertiary)]" style="max-width: 110px;">{{ drive.serial || 'n/a' }}</p>
          </div>
        </div>
      </div>
    </div>

    <!-- Hover arrow indicator -->
    <div class="absolute bottom-3 right-3 opacity-0 transition-opacity group-hover:opacity-40">
      <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
        <path d="M4 10L10 4M10 4H5M10 4V9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
    </div>
  </router-link>
</template>
