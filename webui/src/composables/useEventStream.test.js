import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

import { useEventStream } from './useEventStream'

class MockEventSource {
  static instances = []

  constructor(url) {
    this.url = url
    this.listeners = new Map()
    this.close = vi.fn()
    this.onerror = null
    MockEventSource.instances.push(this)
  }

  addEventListener(event, handler) {
    this.listeners.set(event, handler)
  }

  open(payload = {}) {
    if (this.onopen) this.onopen(payload)
  }

  emit(event, payload) {
    const handler = this.listeners.get(event)
    if (handler) handler(payload)
  }
}

function mountHarness(options = {}) {
  let controls

  const Harness = defineComponent({
    setup() {
      controls = useEventStream(['sample.inserted', 'test.updated'], options.onEvent, options.config)
      return () => null
    }
  })

  return { wrapper: mount(Harness), controls }
}

describe('useEventStream', () => {
  beforeEach(() => {
    MockEventSource.instances = []
    vi.useFakeTimers()
    global.EventSource = MockEventSource
    vi.spyOn(Math, 'random').mockReturnValue(0)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('reconnects with the last seen event id', async () => {
    const onEvent = vi.fn().mockResolvedValue(undefined)
    const { wrapper, controls } = mountHarness({ onEvent })

    controls.connect()

    expect(MockEventSource.instances).toHaveLength(1)
    const firstStream = MockEventSource.instances[0]
    expect(firstStream.url).toBe('/api/v1/events')

    firstStream.open()
    firstStream.emit('sample.inserted', {
      data: JSON.stringify({ device: '/dev/nvme0n1' }),
      lastEventId: 'event-42'
    })

    firstStream.onerror({})
    expect(firstStream.close).toHaveBeenCalledTimes(1)
    expect(MockEventSource.instances).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(1000)

    expect(MockEventSource.instances).toHaveLength(2)
    expect(MockEventSource.instances[1].url).toBe('/api/v1/events?last_event_id=event-42')

    await wrapper.unmount()
  })

  it('debounces matching events and ignores non-matching devices', async () => {
    const onEvent = vi.fn().mockResolvedValue(undefined)
    const { wrapper, controls } = mountHarness({
      onEvent,
      config: {
        debounceMs: 25,
        filterDevice: () => '/dev/nvme0n1'
      }
    })

    controls.connect()

    expect(MockEventSource.instances).toHaveLength(1)
    const sse = MockEventSource.instances[0]
    expect(sse.url).toBe('/api/v1/events')
    expect(sse.onerror).toEqual(expect.any(Function))

    sse.emit('sample.inserted', { data: JSON.stringify({ device: '/dev/sda' }) })
    await vi.advanceTimersByTimeAsync(25)
    expect(onEvent).not.toHaveBeenCalled()

    sse.emit('sample.inserted', { data: JSON.stringify({ device: '/dev/nvme0n1' }) })
    sse.emit('test.updated', { data: JSON.stringify({ device: '/dev/nvme0n1' }) })
    await vi.advanceTimersByTimeAsync(24)
    expect(onEvent).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(onEvent).toHaveBeenCalledTimes(1)
    expect(onEvent).toHaveBeenCalledWith(['sample.inserted', 'test.updated'])

    await wrapper.unmount()
  })

  it('coalesces events that arrive while a reload is in flight', async () => {
    let finishReload
    const onEvent = vi
      .fn()
      .mockImplementationOnce(() => new Promise((resolve) => { finishReload = resolve }))
      .mockResolvedValue(undefined)
    const { wrapper, controls } = mountHarness({ onEvent, config: { debounceMs: 25 } })

    controls.connect()
    const sse = MockEventSource.instances[0]
    sse.emit('sample.inserted', { data: '{}' })
    await vi.advanceTimersByTimeAsync(25)

    sse.emit('test.updated', { data: '{}' })
    await vi.advanceTimersByTimeAsync(25)
    expect(onEvent).toHaveBeenCalledTimes(1)

    finishReload()
    await vi.runAllTimersAsync()
    expect(onEvent).toHaveBeenCalledTimes(2)
    expect(onEvent).toHaveBeenLastCalledWith(['test.updated'])

    await wrapper.unmount()
  })

  it('falls back to scheduling reloads for malformed payloads', async () => {
    const onEvent = vi.fn().mockResolvedValue(undefined)
    const { wrapper, controls } = mountHarness({
      onEvent,
      config: {
        debounceMs: 25,
        filterDevice: () => '/dev/nvme0n1'
      }
    })

    controls.connect()

    const sse = MockEventSource.instances[0]
    sse.emit('sample.inserted', { data: '{bad json', lastEventId: 'event-9' })
    await vi.advanceTimersByTimeAsync(25)
    expect(onEvent).toHaveBeenCalledTimes(1)

    sse.onerror({})
    await vi.advanceTimersByTimeAsync(1000)
    expect(MockEventSource.instances[1].url).toBe('/api/v1/events?last_event_id=event-9')

    await wrapper.unmount()
  })

  it('manual disconnect does not schedule reconnect', async () => {
    const onEvent = vi.fn().mockResolvedValue(undefined)
    const { wrapper, controls } = mountHarness({
      onEvent,
      config: {
        debounceMs: 25,
        filterDevice: () => '/dev/nvme0n1'
      }
    })

    controls.connect()

    const sse = MockEventSource.instances[0]
    
    controls.disconnect()
    expect(sse.close).toHaveBeenCalledTimes(1)

    sse.onerror({})

    await vi.advanceTimersByTimeAsync(5000)
    expect(MockEventSource.instances).toHaveLength(1)
    expect(onEvent).not.toHaveBeenCalled()

    await wrapper.unmount()
  })
})
