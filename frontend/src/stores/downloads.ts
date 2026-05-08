import { defineStore } from 'pinia'
import { ref } from 'vue'

import {
  listDownloads,
  openDownloadEvents,
  type DownloadEvent,
  type DownloadItem,
  type PagedDownloads,
} from '@/api/client'

export type DownloadsConnectionState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'error'

const reconnectDelayMs = 2000

export const useDownloadsStore = defineStore('downloads', () => {
  const activeList = ref<PagedDownloads>({ items: [], total: 0, page: 1, pageSize: 20 })
  const completedList = ref<PagedDownloads>({ items: [], total: 0, page: 1, pageSize: 20 })
  const connectionState = ref<DownloadsConnectionState>('idle')
  const connectionMessage = ref('')

  let source: EventSource | null = null
  let reconnectTimer: number | null = null
  let reconnectAttempts = 0
  let shouldReconnect = false

  function upsertItem(list: PagedDownloads, item: DownloadItem) {
    const index = list.items.findIndex((entry) => entry.id === item.id)
    if (index >= 0) {
      list.items[index] = item
    } else {
      list.items.unshift(item)
      list.total += 1
    }
  }

  function removeItem(list: PagedDownloads, id: number) {
    const next = list.items.filter((entry) => entry.id !== id)
    if (next.length !== list.items.length) {
      list.items = next
      list.total = Math.max(0, list.total - 1)
    }
  }

  function removeLocal(id: number) {
    removeItem(activeList.value, id)
    removeItem(completedList.value, id)
  }

  function upsertLocal(item: DownloadItem) {
    if (item.status === 'completed') {
      removeItem(activeList.value, item.id)
      upsertItem(completedList.value, item)
      return
    }

    removeItem(completedList.value, item.id)
    upsertItem(activeList.value, item)
  }

  function applyEvent(event: DownloadEvent) {
    if (event.type === 'removed') {
      removeItem(activeList.value, event.item.id)
      removeItem(completedList.value, event.item.id)
      return
    }

    if (event.item.status === 'completed') {
      removeItem(activeList.value, event.item.id)
      upsertItem(completedList.value, event.item)
      return
    }

    removeItem(completedList.value, event.item.id)
    upsertItem(activeList.value, event.item)
  }

  async function loadActive(page = activeList.value.page, pageSize = activeList.value.pageSize) {
    activeList.value = await listDownloads('active', page, pageSize)
  }

  async function loadCompleted(page = completedList.value.page, pageSize = completedList.value.pageSize) {
    completedList.value = await listDownloads('completed', page, pageSize)
  }

  function clearReconnectTimer() {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  function cleanupSource() {
    if (source) {
      source.onopen = null
      source.onmessage = null
      source.onerror = null
      source.close()
      source = null
    }
  }

  function scheduleReconnect() {
    if (!shouldReconnect || reconnectTimer !== null) {
      return
    }
    connectionState.value = reconnectAttempts > 0 ? 'reconnecting' : 'error'
    connectionMessage.value = '实时连接已断开，正在重连...'
    reconnectAttempts += 1
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null
      openConnection()
    }, reconnectDelayMs)
  }

  function openConnection() {
    if (!shouldReconnect || source) {
      return
    }

    connectionState.value = reconnectAttempts > 0 ? 'reconnecting' : 'connecting'
    connectionMessage.value = reconnectAttempts > 0 ? '正在重新连接实时更新...' : '正在连接实时更新...'

    source = openDownloadEvents({
      onMessage: (event) => {
        connectionState.value = 'connected'
        connectionMessage.value = ''
        reconnectAttempts = 0
        applyEvent(event)
      },
      onOpen: () => {
        connectionState.value = 'connected'
        connectionMessage.value = ''
        reconnectAttempts = 0
      },
      onError: () => {
        cleanupSource()
        if (!shouldReconnect) {
          connectionState.value = 'idle'
          connectionMessage.value = ''
          return
        }
        scheduleReconnect()
      },
    })
  }

  function connect() {
    shouldReconnect = true
    clearReconnectTimer()
    if (source) {
      return
    }
    openConnection()
  }

  function disconnect() {
    shouldReconnect = false
    reconnectAttempts = 0
    clearReconnectTimer()
    cleanupSource()
    connectionState.value = 'idle'
    connectionMessage.value = ''
  }

  return {
    activeList,
    completedList,
    connectionState,
    connectionMessage,
    loadActive,
    loadCompleted,
    connect,
    disconnect,
    removeLocal,
    upsertLocal,
  }
})
