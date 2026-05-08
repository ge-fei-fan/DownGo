import { defineStore } from 'pinia'
import { ref } from 'vue'

import {
  listDownloads,
  openDownloadEvents,
  type DownloadEvent,
  type DownloadItem,
  type PagedDownloads,
} from '@/api/client'

export const useDownloadsStore = defineStore('downloads', () => {
  const activeList = ref<PagedDownloads>({ items: [], total: 0, page: 1, pageSize: 20 })
  const completedList = ref<PagedDownloads>({ items: [], total: 0, page: 1, pageSize: 20 })
  let source: EventSource | null = null
  let connected = false

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

  function connect() {
    if (connected) return
    connected = true
    source = openDownloadEvents((event) => applyEvent(event))
  }

  function disconnect() {
    source?.close()
    source = null
    connected = false
  }

  return { activeList, completedList, loadActive, loadCompleted, connect, disconnect }
})
