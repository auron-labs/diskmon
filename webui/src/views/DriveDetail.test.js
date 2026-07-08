import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'

const driveMock = vi.fn()
const attributesMock = vi.fn()
const historyMock = vi.fn()
const testsMock = vi.fn()
const connectMock = vi.fn()
const disconnectMock = vi.fn()
const streamStatus = ref('connected')
const streamLastError = ref(null)
const streamRetryAttempt = ref(0)
const streamNeedsResync = ref(false)

vi.mock('vue-router', () => ({
  useRoute: () => ({
    params: {
      id: '42'
    }
  })
}))

vi.mock('../api/client', () => ({
  api: {
    drive: (...args) => driveMock(...args),
    attributes: (...args) => attributesMock(...args),
    history: (...args) => historyMock(...args),
    tests: (...args) => testsMock(...args)
  }
}))

vi.mock('../composables/useEventStream', () => ({
  useEventStream: () => ({
    connect: connectMock,
    disconnect: disconnectMock,
    status: streamStatus,
    lastError: streamLastError,
    retryAttempt: streamRetryAttempt,
    needsResync: streamNeedsResync
  })
}))

import DriveDetail from './DriveDetail.vue'

function mountDriveDetail() {
  return mount(DriveDetail, {
    global: {
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :href="typeof to === \'string\' ? to : to.path"><slot /></a>'
        },
        HistoryChart: {
          props: ['points'],
          template: '<div data-testid="history-chart">Temperature History {{ points.length }}</div>'
        },
        AttributeTable: {
          props: ['rows'],
          template: '<div data-testid="attribute-table">SMART Attributes {{ rows.length }}</div>'
        },
        HealthBadge: {
          props: ['status'],
          template: '<div>{{ status }}</div>'
        },
        TemperatureBadge: {
          props: ['value'],
          template: '<div>{{ value }}</div>'
        }
      }
    }
  })
}

describe('DriveDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-08T12:05:00Z'))
    streamStatus.value = 'connected'
    streamLastError.value = null
    streamRetryAttempt.value = 0
    streamNeedsResync.value = false
  })

  it('keeps the main drive detail visible when SMART test loading fails', async () => {
    driveMock.mockResolvedValue({
      id: 42,
      device: '/dev/nvme0n1',
      model: 'Fast NVMe',
      serial: 'NVME-042',
      health: 'YELLOW',
      health_score: 78,
      health_reasons: 'Temperature elevated',
      health_guidance: ['Schedule closer monitoring'],
      temperature: 48,
      power_on_hours: 1234,
      last_seen: '2026-07-08T12:00:00Z'
    })
    attributesMock.mockResolvedValue([
      { attribute_id: 194, name: 'Temperature', value: 48, raw: 48, threshold: 0, status: 'YELLOW' }
    ])
    historyMock.mockResolvedValue([
      { temperature: 45 },
      { temperature: 48 }
    ])
    testsMock.mockRejectedValue(new Error('SMART tests unavailable'))

    const wrapper = mountDriveDetail()

    await flushPromises()

    expect(driveMock).toHaveBeenCalledWith('42')
    expect(attributesMock).toHaveBeenCalledWith('42')
    expect(historyMock).toHaveBeenCalledWith('42')
    expect(testsMock).toHaveBeenCalledWith('42', 1, 10)
    expect(connectMock).toHaveBeenCalledTimes(1)

    const text = wrapper.text()
    expect(text).toContain('Fresh')
    expect(text).toContain('Last primary update just now')
    expect(text).toContain('Fast NVMe')
    expect(text).toContain('NVME-042')
    expect(text).toContain('Health Score')
    expect(text).toContain('78')
    expect(text).toContain('Temperature elevated')
    expect(text).toContain('Schedule closer monitoring')
    expect(text).toContain('SMART Test Runs')
    expect(text).toContain('SMART tests unavailable')
    expect(text).toContain('Retry tests')
    expect(text).toContain('SMART Attributes')
    expect(text).toContain('Temperature History')
    expect(text).not.toContain('No SMART test runs recorded.')

    wrapper.unmount()
  })

  it('preserves section data and timestamps when refresh failures happen while surfacing stale and reconnecting states', async () => {
    driveMock
      .mockResolvedValueOnce({
        id: 42,
        device: '/dev/nvme0n1',
        model: 'Fast NVMe',
        serial: 'NVME-042',
        health: 'GREEN',
        health_score: 91,
        temperature: 40,
        power_on_hours: 1234,
        last_seen: '2026-07-08T12:00:00Z'
      })
      .mockResolvedValueOnce({
        id: 42,
        device: '/dev/nvme0n1',
        model: 'Fast NVMe',
        serial: 'NVME-042',
        health: 'GREEN',
        health_score: 92,
        temperature: 41,
        power_on_hours: 1234,
        last_seen: '2026-07-08T12:06:00Z'
      })

    attributesMock
      .mockResolvedValueOnce([{ attribute_id: 194, name: 'Temperature' }])
      .mockRejectedValueOnce(new Error('Attributes refresh failed'))

    historyMock
      .mockResolvedValueOnce([{ temperature: 40 }])
      .mockRejectedValueOnce(new Error('History refresh failed'))

    testsMock
      .mockResolvedValueOnce({
        items: [{ id: 9, test_type: 'short', status: 'PASSED', started_at: '2026-07-08T12:00:00Z', message: 'ok' }],
        page: 1,
        total: 1
      })
      .mockRejectedValueOnce(new Error('Tests refresh failed'))

    const wrapper = mountDriveDetail()
    await flushPromises()

    expect(wrapper.text()).toContain('Fresh')
    expect(wrapper.get('[data-testid="history-chart"]').text()).toContain('1')
    expect(wrapper.get('[data-testid="attribute-table"]').text()).toContain('1')
    expect(wrapper.text()).toContain('History • Updated Jul 8, 2026, 12:05 PM UTC')
    expect(wrapper.text()).toContain('Attributes • Updated Jul 8, 2026, 12:05 PM UTC')
    expect(wrapper.text()).toContain('1 tests • Updated Jul 8, 2026, 12:05 PM UTC')

    vi.setSystemTime(new Date('2026-07-08T12:12:00Z'))
    streamStatus.value = 'reconnecting'
    streamRetryAttempt.value = 2
    await nextTick()

    expect(wrapper.text()).toContain('Reconnecting')
    expect(wrapper.text()).toContain('attempt 2')

    streamStatus.value = 'connected'
    streamRetryAttempt.value = 0
    streamNeedsResync.value = true
    await nextTick()

    expect(wrapper.text()).toContain('Stale')
    expect(wrapper.text()).toContain('full resync is pending')

    streamNeedsResync.value = false
    await wrapper.get('button[type="button"]').trigger('click')
    await flushPromises()

    expect(driveMock).toHaveBeenCalledTimes(2)
    expect(attributesMock).toHaveBeenCalledTimes(2)
    expect(historyMock).toHaveBeenCalledTimes(2)
    expect(testsMock).toHaveBeenCalledTimes(2)

    const text = wrapper.text()
    expect(text).toContain('92')
    expect(text).toContain('History refresh failed')
    expect(text).toContain('Attributes refresh failed')
    expect(text).toContain('Tests refresh failed')
    expect(text).toContain('History • Updated Jul 8, 2026, 12:05 PM UTC')
    expect(text).toContain('Attributes • Updated Jul 8, 2026, 12:05 PM UTC')
    expect(text).toContain('1 tests • Updated Jul 8, 2026, 12:05 PM UTC')
    expect(text).toContain('Last primary update just now · Jul 8, 2026, 12:12 PM UTC')
    expect(wrapper.get('[data-testid="history-chart"]').text()).toContain('1')
    expect(wrapper.get('[data-testid="attribute-table"]').text()).toContain('1')
    expect(text).toContain('PASSED')

    wrapper.unmount()
  })
})
