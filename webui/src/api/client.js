const API_KEY_STORAGE = 'diskmon.apiKey'

function getAPIKey() {
  try {
    return localStorage.getItem(API_KEY_STORAGE) || ''
  } catch {
    return ''
  }
}

function authHeaders() {
  const key = getAPIKey()
  if (!key) return {}
  return { Authorization: `Bearer ${key}` }
}

async function parseError(res) {
  let message = `API error ${res.status}`
  try {
    const body = await res.json()
    if (body && typeof body.error === 'string' && body.error.trim() !== '') {
      message = body.error
    }
  } catch {}
  return new Error(message)
}

async function req(path) {
  const res = await fetch(`/api/v1${path}`, { headers: authHeaders() })
  if (!res.ok) {
    throw await parseError(res)
  }
  return res.json()
}

export const api = {
  drives: () => req('/drives'),
  drive: (id) => req(`/drives/${id}`),
  history: (id, limit) => {
    const params = limit ? `?limit=${limit}` : ''
    return req(`/drives/${id}/history${params}`)
  },
  attributes: (id) => req(`/drives/${id}/attributes`),
  tests: (id, page = 1, pageSize = 10) => req(`/drives/${id}/tests?page=${page}&page_size=${pageSize}`),

  setAPIKey(key) {
    try {
      if (key) localStorage.setItem(API_KEY_STORAGE, key)
      else localStorage.removeItem(API_KEY_STORAGE)
    } catch {}
  },

  hasAPIKey() {
    return getAPIKey() !== ''
  },

  getAPIKey() {
    return getAPIKey()
  }
}
