<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'

import {
  getSettings,
  installMissingDependencies,
  updateSettings,
  type DependenciesDTO,
  type SettingsDTO,
} from '@/api/client'
import { useDepsStore } from '@/stores/deps'

const deps = useDepsStore()

const loading = ref(false)
const saving = ref(false)
const installingDeps = ref(false)
const form = reactive<SettingsDTO>({
  bindHost: '0.0.0.0',
  port: 12225,
  downloadDir: '',
  concurrentDownloads: 2,
  ytDlpPath: '',
  ffmpegPath: '',
  accessPassword: '',
})

async function load() {
  loading.value = true
  try {
    const settings = await getSettings()
    Object.assign(form, settings, { accessPassword: '' })
  } catch (error) {
    message.error(error instanceof Error ? error.message : '加载设置失败')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    await updateSettings(form)
    message.success('设置已保存')
    form.accessPassword = ''
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存设置失败')
  } finally {
    saving.value = false
  }
}

function summarizeInstallResult(result: DependenciesDTO) {
  const downloaded: string[] = []
  const failed: string[] = []
  const alreadyInstalled: string[] = []

  const items = [
    ['yt-dlp.exe', result.ytDlp],
    ['ffmpeg.exe', result.ffmpeg],
  ] as const

  for (const [name, item] of items) {
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

async function installDependencies() {
  installingDeps.value = true
  try {
    const result = await installMissingDependencies()
    summarizeInstallResult(result)
    await deps.check()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '下载依赖失败')
  } finally {
    installingDeps.value = false
  }
}

onMounted(load)
</script>

<template>
  <a-card :loading="loading" :bordered="false" class="settings-card">
    <div class="header">
      <div>
        <div class="header-kicker">服务配置</div>
        <h1>系统设置</h1>
      </div>
      <a-button type="primary" :loading="saving" @click="save">保存设置</a-button>
    </div>

    <a-form layout="vertical">
      <a-row :gutter="16">
        <a-col :xs="24" :md="12">
          <a-form-item label="监听地址">
            <a-input v-model:value="form.bindHost" placeholder="0.0.0.0" />
            <div class="field-hint">填 `0.0.0.0` 可允许局域网设备访问页面。</div>
          </a-form-item>
        </a-col>
        <a-col :xs="24" :md="12">
          <a-form-item label="端口">
            <a-input-number v-model:value="form.port" style="width: 100%" :min="1" :max="65535" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item label="下载目录">
        <a-input v-model:value="form.downloadDir" />
      </a-form-item>

      <a-row :gutter="16">
        <a-col :xs="24" :md="12">
          <a-form-item label="并发下载数">
            <a-input-number v-model:value="form.concurrentDownloads" style="width: 100%" :min="1" :max="16" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item label="yt-dlp.exe 路径">
        <a-input v-model:value="form.ytDlpPath" />
      </a-form-item>

      <a-form-item label="ffmpeg.exe 路径">
        <a-input v-model:value="form.ffmpegPath" />
      </a-form-item>

      <a-form-item label="新的共享密码">
        <a-input-password v-model:value="form.accessPassword" placeholder="留空则保持当前密码不变" />
      </a-form-item>
    </a-form>

    <a-divider />

    <div class="deps-section">
      <div class="deps-header">
        <div>
          <h2>依赖工具</h2>
          <div class="field-hint">仅会下载到默认目录，不会修改上方自定义路径配置。</div>
        </div>
        <a-space>
          <a-button :loading="deps.loading" @click="deps.check()">刷新状态</a-button>
          <a-button type="primary" :loading="installingDeps" @click="installDependencies">
            下载缺失依赖
          </a-button>
        </a-space>
      </div>

      <div v-if="deps.dependencies" class="deps-summary">
        默认目录：{{ deps.dependencies.binDir }}
      </div>

      <div class="deps-list">
        <div v-for="item in deps.items" :key="item.key" class="deps-item">
          <div class="deps-main">
            <div class="deps-name">{{ item.label }}</div>
            <a-tag :color="item.value.exists ? 'success' : 'default'">
              {{ item.value.exists ? '已安装' : '未安装' }}
            </a-tag>
          </div>
          <div class="deps-path">{{ item.value.path }}</div>
          <div v-if="item.value.error" class="deps-error">{{ item.value.error }}</div>
        </div>
      </div>
    </div>
  </a-card>
</template>

<style scoped>
.settings-card {
  background: rgba(255, 255, 255, 0.82);
  border-radius: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
}

.header-kicker {
  color: #6f89a4;
  text-transform: uppercase;
  letter-spacing: 0.12em;
  font-size: 12px;
  font-weight: 700;
}

h1 {
  margin: 10px 0 0;
}

.deps-section {
  display: grid;
  gap: 16px;
}

.deps-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.deps-header h2 {
  margin: 0 0 6px;
}

.deps-summary,
.field-hint {
  color: #6f89a4;
  font-size: 12px;
}

.deps-list {
  display: grid;
  gap: 12px;
}

.deps-item {
  padding: 14px 16px;
  border: 1px solid rgba(22, 32, 51, 0.08);
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.7);
}

.deps-main {
  display: flex;
  align-items: center;
  gap: 12px;
}

.deps-name {
  font-weight: 700;
}

.deps-path {
  margin-top: 6px;
  color: #58708f;
  word-break: break-all;
}

.deps-error {
  margin-top: 8px;
  color: #d4380d;
}

@media (max-width: 960px) {
  .header {
    flex-direction: column;
    align-items: stretch;
  }

  .deps-header {
    flex-direction: column;
  }

  .deps-header :deep(.ant-space) {
    width: 100%;
  }

  .deps-header :deep(.ant-space-item) {
    flex: 1;
  }

  .deps-header :deep(.ant-btn) {
    width: 100%;
  }
}
</style>
