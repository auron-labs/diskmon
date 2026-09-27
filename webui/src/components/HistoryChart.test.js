import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import HistoryChart from './HistoryChart.vue'

describe('HistoryChart', () => {
  it('keeps fractional averages and rollup peaks visible', () => {
    const wrapper = mount(HistoryChart, {
      props: {
        points: [
          { temperature: 40.25, temperature_max: 40.25 },
          { temperature: 41.5, temperature_max: 48.75 }
        ]
      }
    })

    expect(wrapper.text()).toContain('40.3°C — 48.8°C')
    expect(wrapper.findAll('polyline')).toHaveLength(2)
  })
})
