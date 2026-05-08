export type DownloadItem = {
  id: number
  sourceUrl: string
  normalizedUrl: string
  platform: string
  videoId: string
  title: string
  thumbnailUrl: string
  qualityLabel: string
  container: string
  outputFilename: string
  outputPath: string
  status: string
  progressPercent: number
  speedBps: number
  etaSeconds: number
  errorMessage: string
  processPid: number
  createdAt: string
  startedAt?: string
  completedAt?: string
  updatedAt: string
}

export type InspectResult = {
  normalizedUrl: string
  videoId: string
  title: string
  thumbnailUrl: string
  qualityLabel: string
  container: string
  durationSeconds: number
  estimatedSizeBytes: number
  suggestedFilename: string
  duplicateOf?: DownloadItem
}

export type PagedDownloads = {
  items: DownloadItem[]
  total: number
  page: number
  pageSize: number
}

export type SettingsDTO = {
  bindHost: string
  port: number
  downloadDir: string
  concurrentDownloads: number
  ytDlpPath: string
  ffmpegPath: string
  accessPassword?: string
}

export type DependencyFileStatus = {
  path: string
  exists: boolean
  downloaded: boolean
  error?: string
}

export type DependenciesDTO = {
  binDir: string
  ytDlp: DependencyFileStatus
  ffmpeg: DependencyFileStatus
}

export type DownloadEvent = {
  type: 'created' | 'updated' | 'removed'
  item: DownloadItem
}

let token = ''

export function setToken(next: string) {
  token = next
}

async function request<T>(input: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers ?? {})
  headers.set('Content-Type', 'application/json')
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const response = await fetch(input, { ...init, headers })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    const error = new Error(body.error ?? body.message ?? '请求失败') as Error & {
      code?: string
      item?: DownloadItem
    }
    error.code = body.code
    error.item = body.item
    throw error
  }

  if (response.status === 204) {
    return undefined as T
  }

  return response.json() as Promise<T>
}

export async function login(password: string) {
  return request<{ token: string }>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

export async function getSettings() {
  return request<SettingsDTO>('/api/settings')
}

export async function updateSettings(payload: SettingsDTO) {
  return request<SettingsDTO>('/api/settings', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function getDependencies() {
  return request<DependenciesDTO>('/api/tools/dependencies')
}

export async function installMissingDependencies() {
  return request<DependenciesDTO>('/api/tools/dependencies/install', {
    method: 'POST',
  })
}

export async function inspectDownload(url: string) {
  return request<InspectResult>('/api/downloads/inspect', {
    method: 'POST',
    body: JSON.stringify({ url }),
  })
}

export async function createDownload(url: string) {
  return request<DownloadItem>('/api/downloads', {
    method: 'POST',
    body: JSON.stringify({ url }),
  })
}

export async function listDownloads(view: 'active' | 'completed', page = 1, pageSize = 20) {
  return request<PagedDownloads>(`/api/downloads?view=${view}&page=${page}&pageSize=${pageSize}`)
}

export async function cancelDownload(id: number) {
  return request<DownloadItem>(`/api/downloads/${id}/cancel`, { method: 'POST' })
}

export async function retryDownload(id: number) {
  return request<DownloadItem>(`/api/downloads/${id}/retry`, { method: 'POST' })
}

export async function deleteDownload(id: number) {
  return request<void>(`/api/downloads/${id}`, { method: 'DELETE' })
}

export async function openDownloadPath(id: number) {
  return request<void>(`/api/downloads/${id}/open-path`, { method: 'POST' })
}

export function downloadFileURL(id: number) {
  const query = token ? `?token=${encodeURIComponent(token)}` : ''
  return `/api/downloads/${id}/file${query}`
}

export function openDownloadEvents(onMessage: (event: DownloadEvent) => void) {
  const query = token ? `?token=${encodeURIComponent(token)}` : ''
  const source = new EventSource(`/api/downloads/events${query}`)
  source.onmessage = (event) => {
    const payload = JSON.parse(event.data) as DownloadEvent
    onMessage(payload)
  }
  return source
}
