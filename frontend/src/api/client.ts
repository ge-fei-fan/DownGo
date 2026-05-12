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
  platform: string
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

function normalizePagedDownloads(payload: PagedDownloads): PagedDownloads {
  return {
    items: Array.isArray(payload?.items) ? payload.items : [],
    total: payload?.total ?? 0,
    page: payload?.page ?? 1,
    pageSize: payload?.pageSize ?? 20,
  }
}

export type SettingsDTO = {
  bindHost: string
  port: number
  downloadDir: string
  concurrentDownloads: number
  ytDlpPath: string
  ffmpegPath: string
  bilibiliMid: number
  bilibiliUname: string
  bilibiliFace: string
  bilibiliLoginAt: string
  accessPassword?: string
}

export type BilibiliSessionDTO = {
  loggedIn: boolean
  mid: number
  uname: string
  face: string
  faceLocal: string
  loginAt: string
  status: 'missing' | 'unchecked' | 'valid' | 'invalid' | 'error'
  checkedAt: string
  message: string
  level: number
  sex: string
  sign: string
  vipStatus: number
  vipType: number
  vipLabel: string
  vipDueDate: number
  seniorMember: boolean
}

export type BilibiliQRCodeDTO = {
  url: string
  qrcodeKey: string
}

export type FavoriteFolderDTO = {
  id: number
  title: string
}

export type FavoriteSubscriptionDTO = {
  mediaId: number
  title: string
  enabled: boolean
  lastCheckedAt?: string
  lastError: string
  updatedAt: string
}

export type BilibiliPollDTO = {
  code: number
  message: string
  session?: BilibiliSessionDTO
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
  smartctl: DependencyFileStatus
}

export type DirectorySelectionDTO = {
  path: string
}

export type DependencyInstallEvent = {
  type: 'started' | 'progress' | 'skipped' | 'completed' | 'failed' | 'done'
  name?: string
  path?: string
  bytes?: number
  totalBytes?: number
  percent?: number
  error?: string
  status?: DependenciesDTO
}

export type DependencyInstallSnapshot = {
  installing: boolean
  events: Record<string, DependencyInstallEvent>
  status: DependenciesDTO
  error?: string
}

export type DownloadEvent = {
  type: 'created' | 'updated' | 'removed'
  item: DownloadItem
}

type OpenDownloadEventsOptions = {
  onMessage: (event: DownloadEvent) => void
  onOpen?: () => void
  onError?: () => void
}

type OpenDependencyInstallEventsOptions = {
  onMessage: (event: DependencyInstallEvent) => void
  onOpen?: () => void
  onError?: () => void
}

let token = ''
let unauthorizedHandler: (() => void) | null = null

export function setToken(next: string) {
  token = next
}

export function setUnauthorizedHandler(handler: (() => void) | null) {
  unauthorizedHandler = handler
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
    if (response.status === 401 && input !== '/api/auth/login') {
      unauthorizedHandler?.()
    }
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

export async function selectDownloadDir(currentDir: string) {
  return request<DirectorySelectionDTO | undefined>('/api/settings/download-dir/select', {
    method: 'POST',
    body: JSON.stringify({ currentDir }),
  })
}

export async function createBilibiliQRCode() {
  return request<BilibiliQRCodeDTO>('/api/bilibili/qrcode', { method: 'POST' })
}

export async function pollBilibiliQRCode(key: string) {
  return request<BilibiliPollDTO>(`/api/bilibili/qrcode/poll?key=${encodeURIComponent(key)}`)
}

export async function getBilibiliSession() {
  return request<BilibiliSessionDTO>('/api/bilibili/session')
}

export async function checkBilibiliSession() {
  return request<BilibiliSessionDTO>('/api/bilibili/session/check', { method: 'POST' })
}

export async function clearBilibiliSession() {
  return request<void>('/api/bilibili/session', { method: 'DELETE' })
}

export async function getFavoriteFolders() {
  return request<FavoriteFolderDTO[]>('/api/bilibili/favorites/folders')
}

export async function getFavoriteSubscription() {
  return request<FavoriteSubscriptionDTO>('/api/bilibili/favorites/subscription')
}

export async function updateFavoriteSubscription(payload: Pick<FavoriteSubscriptionDTO, 'mediaId' | 'title' | 'enabled'>) {
  return request<FavoriteSubscriptionDTO>('/api/bilibili/favorites/subscription', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function runFavoriteSubscription() {
  return request<FavoriteSubscriptionDTO>('/api/bilibili/favorites/subscription/run', { method: 'POST' })
}

export function bilibiliAvatarURL(session?: BilibiliSessionDTO | null) {
  if (!session?.faceLocal) return ''
  const query = new URLSearchParams()
  if (token) query.set('token', token)
  query.set('t', session.checkedAt || session.loginAt || String(Date.now()))
  return `${session.faceLocal}?${query.toString()}`
}

export async function getDependencies() {
  return request<DependenciesDTO>('/api/tools/dependencies')
}

export async function installMissingDependencies() {
  return request<DependencyInstallSnapshot>('/api/tools/dependencies/install', {
    method: 'POST',
  })
}

export async function getDependencyInstallStatus() {
  return request<DependencyInstallSnapshot>('/api/tools/dependencies/install/status')
}

export function openDependencyInstallEvents(options: OpenDependencyInstallEventsOptions) {
  const query = token ? `?token=${encodeURIComponent(token)}` : ''
  const source = new EventSource(`/api/tools/dependencies/install/events${query}`)
  source.onopen = () => {
    options.onOpen?.()
  }
  source.onmessage = (event) => {
    const payload = JSON.parse(event.data) as DependencyInstallEvent
    options.onMessage(payload)
  }
  source.onerror = () => {
    options.onError?.()
  }
  return source
}

export async function inspectDownload(url: string) {
  return request<InspectResult[]>('/api/downloads/inspect', {
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
  const payload = await request<PagedDownloads>(`/api/downloads?view=${view}&page=${page}&pageSize=${pageSize}`)
  return normalizePagedDownloads(payload)
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

export function downloadThumbnailURL(id: number) {
  const query = token ? `?token=${encodeURIComponent(token)}` : ''
  return `/api/downloads/${id}/thumbnail${query}`
}

export function openDownloadEvents(options: OpenDownloadEventsOptions) {
  const query = token ? `?token=${encodeURIComponent(token)}` : ''
  const source = new EventSource(`/api/downloads/events${query}`)
  source.onopen = () => {
    options.onOpen?.()
  }
  source.onmessage = (event) => {
    const payload = JSON.parse(event.data) as DownloadEvent
    options.onMessage(payload)
  }
  source.onerror = () => {
    options.onError?.()
  }
  return source
}
