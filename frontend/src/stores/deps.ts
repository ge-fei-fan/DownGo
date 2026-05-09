import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { message } from 'ant-design-vue'

import {
  getDependencyInstallStatus,
  getDependencies,
  installMissingDependencies,
  openDependencyInstallEvents,
  type DependenciesDTO,
  type DependencyInstallEvent,
  type DependencyInstallSnapshot,
} from '@/api/client'

export const useDepsStore = defineStore('deps', () => {
  const dependencies = ref<DependenciesDTO | null>(null)
  const loading = ref(false)
  const initialized = ref(false)
  const installing = ref(false)
  const installEvents = ref<Record<string, DependencyInstallEvent>>({})
  const installError = ref('')

  let installSource: EventSource | null = null

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

  function summarizeInstallResult(result: DependenciesDTO) {
    const downloaded: string[] = []
    const failed: string[] = []
    const alreadyInstalled: string[] = []

    const resultItems = [
      ['yt-dlp.exe', result.ytDlp],
      ['ffmpeg.exe', result.ffmpeg],
    ] as const

    for (const [name, item] of resultItems) {
      if (item.downloaded) {
        downloaded.push(name)
        continue
      }
      if (item.error) {
        failed.push(`${name}：${item.error}`)
        continue
      }
      if (item.exists) {
        alreadyInstalled.push(name)
      }
    }

    if (failed.length > 0) {
      message.error(failed.join('；'))
      return
    }
    if (downloaded.length === 0) {
      message.success('依赖已安装，无需下载')
      return
    }

    const suffix = alreadyInstalled.length > 0 ? `；已跳过 ${alreadyInstalled.join('、')}` : ''
    message.success(`已下载 ${downloaded.join('、')}${suffix}`)
  }

  async function loadInstallStatus() {
    const snapshot = await getDependencyInstallStatus()
    applyInstallSnapshot(snapshot)
    if (snapshot.installing) {
      connectInstallEvents()
    }
  }

  async function installMissing() {
    if (installing.value) return

    disconnectInstall()
    installing.value = true
    installError.value = ''
    installEvents.value = {}

    try {
      const snapshot = await installMissingDependencies()
      applyInstallSnapshot(snapshot)
      if (snapshot.installing) {
        connectInstallEvents()
      } else {
        summarizeInstallResult(snapshot.status)
        await check()
      }
    } catch (error) {
      installing.value = false
      installError.value = error instanceof Error ? error.message : '启动依赖下载失败'
      message.error(installError.value)
    }
  }

  function connectInstallEvents() {
    if (installSource) return

    installSource = openDependencyInstallEvents({
      onMessage: async (event) => {
        if (event.name) {
          installEvents.value = { ...installEvents.value, [event.name]: event }
        }
        if (event.type === 'failed') {
          const error = event.error || `${event.name || '依赖'} 下载失败`
          installError.value = error
          message.error(error)
        }
        if (event.type === 'done') {
          disconnectInstall(false)
          installing.value = false
          if (event.status) {
            dependencies.value = event.status
            initialized.value = true
            summarizeInstallResult(event.status)
          }
          await check()
        }
      },
      onError: () => {
        disconnectInstall(false)
        void loadInstallStatus().catch(() => {
          installing.value = false
          installError.value = '下载依赖进度连接中断'
          message.error(installError.value)
        })
      },
    })
  }

  function applyInstallSnapshot(snapshot: DependencyInstallSnapshot) {
    installing.value = snapshot.installing
    installEvents.value = snapshot.events ?? {}
    installError.value = snapshot.error ?? ''
    dependencies.value = snapshot.status
    initialized.value = true
  }

  function disconnectInstall(resetInstalling = true) {
    installSource?.close()
    installSource = null
    if (resetInstalling) {
      installing.value = false
    }
  }

  return {
    dependencies,
    loading,
    initialized,
    installing,
    installEvents,
    installError,
    items,
    allInstalled,
    check,
    loadInstallStatus,
    installMissing,
    disconnectInstall,
  }
})
