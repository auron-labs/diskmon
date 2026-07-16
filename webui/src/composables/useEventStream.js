import { onUnmounted, ref } from 'vue'

export function useEventStream(events, onEvent, { debounceMs = 300, filterDevice = null } = {}) {
  let sse = null
  let timer = null
  let reconnectTimer = null
  let manualDisconnect = false
  let lastEventId = null
  let reloadInFlight = false
  let reloadPending = false
  let forceResyncPending = false
  const pendingEvents = new Set()

  const status = ref('disconnected')
  const lastEventAt = ref(null)
  const lastError = ref(null)
  const retryAttempt = ref(0)
  const needsResync = ref(false)

  const baseRetryMs = 1000
  const maxRetryMs = 15000

  const markEvent = (event = null) => {
    lastEventAt.value = new Date()
    if (event?.lastEventId) {
      lastEventId = event.lastEventId
    }
  }

  const recordError = (error, fallbackMessage = 'Event stream error') => {
    if (error instanceof Error) {
      lastError.value = error
      return
    }
    lastError.value = new Error(fallbackMessage)
  }

  const reload = async ({ forceResync = false, eventTypes = [] } = {}) => {
    try {
      await onEvent(eventTypes)
      if (forceResync) {
        needsResync.value = false
      }
    } catch (error) {
      recordError(error, 'Event stream reload failed')
    }
  }

  const flushReload = async ({ forceResync = false } = {}) => {
    if (reloadInFlight) {
      reloadPending = true
      forceResyncPending ||= forceResync
      return
    }

    reloadInFlight = true
    try {
      do {
        reloadPending = false
        forceResync ||= forceResyncPending
        forceResyncPending = false
        const eventTypes = [...pendingEvents]
        pendingEvents.clear()
        await reload({ forceResync, eventTypes })
        forceResync = false
      } while (reloadPending || pendingEvents.size)
    } finally {
      reloadInFlight = false
    }
  }

  const schedule = (eventType) => {
    pendingEvents.add(eventType)
    if (timer) return
    timer = setTimeout(async () => {
      timer = null
      await flushReload()
    }, debounceMs)
  }

  const makeHandler = (eventType) => {
    if (!filterDevice) {
      return (event) => {
        markEvent(event)
        schedule(eventType)
      }
    }

    return (event) => {
      markEvent(event)
      try {
        const payload = JSON.parse(event.data || '{}')
        const device = filterDevice()
        if (device && payload.device && payload.device !== device) return
      } catch {}
      schedule(eventType)
    }
  }

  const handleResync = async (event) => {
    markEvent(event)
    needsResync.value = true
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    pendingEvents.clear()
    await flushReload({ forceResync: true })
  }

  const closeStream = () => {
    if (!sse) return
    sse.close()
    sse = null
  }

  const clearReconnectTimer = () => {
    if (!reconnectTimer) return
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }

  const nextRetryDelay = (attempt) => {
    const capped = Math.min(maxRetryMs, baseRetryMs * (2 ** Math.max(attempt - 1, 0)))
    const jitter = Math.floor(Math.random() * Math.min(500, Math.floor(capped / 2)))
    return capped + jitter
  }

  const buildStreamUrl = () => {
    if (!lastEventId) return '/api/v1/events'
    const params = new URLSearchParams({ last_event_id: lastEventId })
    return `/api/v1/events?${params.toString()}`
  }

  function openStream() {
    clearReconnectTimer()
    closeStream()
    status.value = retryAttempt.value > 0 ? 'reconnecting' : 'connecting'
    sse = new EventSource(buildStreamUrl())
    for (const ev of events) {
      sse.addEventListener(ev, makeHandler(ev))
    }
    sse.addEventListener('stream.resync', handleResync)
    sse.addEventListener('stream.dropped', handleResync)
    sse.onopen = () => {
      status.value = 'connected'
      retryAttempt.value = 0
      lastError.value = null
    }
    sse.onerror = (event) => {
      if (manualDisconnect) return
      recordError(event, 'Event stream connection lost')
      closeStream()
      if (reconnectTimer) return
      const attempt = retryAttempt.value + 1
      retryAttempt.value = attempt
      status.value = 'reconnecting'
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null
        if (manualDisconnect) return
        openStream()
      }, nextRetryDelay(attempt))
    }
  }

  function connect() {
    manualDisconnect = false
    openStream()
  }

  function disconnect() {
    manualDisconnect = true
    clearReconnectTimer()
    closeStream()
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    pendingEvents.clear()
    status.value = 'disconnected'
    retryAttempt.value = 0
  }

  onUnmounted(disconnect)

  return { connect, disconnect, status, lastEventAt, lastError, retryAttempt, needsResync }
}
