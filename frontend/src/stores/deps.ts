import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

import { getDependencies, type DependenciesDTO } from '@/api/client'

export const useDepsStore = defineStore('deps', () => {
  const dependencies = ref<DependenciesDTO | null>(null)
  const loading = ref(false)
  const initialized = ref(false)

  const items = computed(() => {
    if (!dependencies.value) return []
    return [
      { key: 'yt-dlp', label: 'yt-dlp.exe', value: dependencies.value.ytDlp },
      { key: 'ffmpeg', label: 'ffmpeg.exe', value: dependencies.value.ffmpeg },
    ]
  })

  const allInstalled = computed(() => {
    if (!dependencies.value) return false
    return dependencies.value.ytDlp.exists && dependencies.value.ffmpeg.exists
  })

  async function check() {
    loading.value = true
    try {
      dependencies.value = await getDependencies()
      initialized.value = true
    } finally {
      loading.value = false
    }
  }

  return { dependencies, loading, initialized, items, allInstalled, check }
})
