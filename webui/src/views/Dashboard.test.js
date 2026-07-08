import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const drivesMock = vi.fn()
const connectMock = vi.fn()
const statusRef = ref('connected')
const lastEventAtRef = ref(null)
const lastErrorRef = ref(null)
const retryAttemptRef = ref(0)
const needsResyncRef = ref(false)

vi.mock('../api/client', () => ({
  api: {
    drives: (...args) => drivesMock(...args)
  }
}))

vi.mock('../composables/useEventStream', () => ({
  useEventStream: () => ({
    connect: connectMock,
    disconnect: vi.fn(),
    status: statusRef,
    lastEventAt: lastEventAtRef,
    lastError: lastErrorRef,
    retryAttempt: retryAttemptRef,
    needsResync: needsResyncRef
  })
}))

import Dashboard from './Dashboard.vue'

function mountDashboard() {
  return mount(Dashboard, {
    global: {
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :href="to"><slot /></a>'
        }
      }
    }
  })
}

describe('Dashboard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-08T12:00:00Z'))
    vi.clearAllMocks()
    statusRef.value = 'connected'
    lastEventAtRef.value = new Date('2026-07-08T11:59:00Z')
    lastErrorRef.value = null
    retryAttemptRef.value = 0
    needsResyncRef.value = false
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders operational controls and filters drives locally', async () => {
    drivesMock.mockResolvedValue([
      {
        id: 1,
        device: '/dev/sda',
        model: 'Archive HDD',
        serial: 'HDD-001',
        health: 'RED',
        temperature: 51,
        power_on_hours: 42000
      },
      {
        id: 2,
        device: '/dev/nvme0n1',
        model: 'Fast NVMe',
        serial: 'NVME-001',
        health: 'GREEN',
        temperature: 39,
        power_on_hours: 1200
      },
      {
        id: 3,
        device: '/dev/sdb',
        model: 'Backup HDD',
        serial: 'HDD-002',
        health: 'YELLOW',
        temperature: 46,
        power_on_hours: 23000
      }
    ])

    const wrapper = mountDashboard()
    await flushPromises()

    expect(drivesMock).toHaveBeenCalledTimes(1)
    expect(connectMock).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Fleet')
    expect(wrapper.text()).toContain('3 drives')
    expect(wrapper.text()).toContain('Live')
    expect(wrapper.text()).toContain('Last successful update')
    expect(wrapper.text()).toContain('just now')
    expect(wrapper.text()).toContain('Last event 1m ago')
    expect(wrapper.text()).toContain('Refresh')
    expect(wrapper.text()).toContain('Fast NVMe')
    expect(wrapper.text()).toContain('Archive HDD')
    expect(wrapper.text()).toContain('Backup HDD')

    const sectionHeadings = wrapper.findAll('h2').map((node) => node.text())
    expect(sectionHeadings).toEqual(['NVMe Drives', 'Hard Drives'])

    await wrapper.find('input[type="search"]').setValue('nvme-001')
    expect(wrapper.text()).toContain('1 of 3 drives')
    expect(wrapper.text()).toContain('Fast NVMe')
    expect(wrapper.text()).not.toContain('Archive HDD')
    expect(wrapper.text()).not.toContain('Backup HDD')

    await wrapper.find('select').setValue('red')
    expect(wrapper.text()).toContain('No drives match the current filters.')

    await wrapper.unmount()
  })

  it('keeps previous drives visible when a refresh fails and surfaces a retry banner', async () => {
    drivesMock
      .mockResolvedValueOnce([
        {
          id: 2,
          device: '/dev/nvme0n1',
          model: 'Fast NVMe',
          serial: 'NVME-001',
          health: 'GREEN',
          temperature: 39,
          power_on_hours: 1200
        }
      ])
      .mockRejectedValueOnce(new Error('API down'))

    statusRef.value = 'reconnecting'
    retryAttemptRef.value = 2
    lastErrorRef.value = new Error('Event stream connection lost')
    needsResyncRef.value = true

    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.text()).toContain('Fast NVMe')
    expect(wrapper.text()).toContain('Reconnecting (2)')
    expect(wrapper.text()).toContain('Resync pending')
    expect(wrapper.text()).toContain('Event stream connection lost')

    const buttons = wrapper.findAll('button')
    const refreshButton = buttons.find((button) => button.text().includes('Refresh'))
    expect(refreshButton).toBeTruthy()

    await refreshButton.trigger('click')
    await flushPromises()

    expect(drivesMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Refresh failed')
    expect(wrapper.text()).toContain('API down')
    expect(wrapper.text()).toContain('Retry')
    expect(wrapper.text()).toContain('Fast NVMe')

    await wrapper.unmount()
  })
})
