<script setup>
import { computed } from 'vue'
import { healthBackground, healthColor, healthDot, healthIcon, healthLabel, normalizeHealthStatus } from '../stores/format'
const props = defineProps({ status: { type: String, default: 'UNKNOWN' }, compact: Boolean })
const klass = computed(() => healthColor(props.status))
const label = computed(() => healthLabel(props.status))
const icon = computed(() => healthIcon(props.status))
const bgClass = computed(() => healthBackground(props.status))
const dotClass = computed(() => healthDot(props.status))
const isCritical = computed(() => normalizeHealthStatus(props.status) === 'RED')
</script>

<template>
  <span
    class="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium"
    :class="[klass, bgClass]"
  >
    <span class="h-1.5 w-1.5 rounded-full" :class="dotClass"
      :style="isCritical ? 'animation: pulse-dot 1.5s ease-in-out infinite' : ''"
    ></span>
    <span v-if="!compact">{{ label }}</span>
    <span v-else class="mono">{{ icon }}</span>
  </span>
</template>
