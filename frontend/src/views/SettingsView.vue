<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'

import {
  bilibiliAvatarURL,
  checkBilibiliSession,
  clearBilibiliSession,
  createBilibiliQRCode,
  getFavoriteFolders,
  getFavoriteSubscription,
  getBilibiliSession,
  getSettings,
  pollBilibiliQRCode,
  runFavoriteSubscription,
  selectDownloadDir,
  selectYtDlpCookieFile,
  updateFavoriteSubscription,
  updateSettings,
  type BilibiliSessionDTO,
  type DependencyInstallEvent,
  type FavoriteFolderDTO,
  type FavoriteSubscriptionDTO,
  type SettingsDTO,
} from '@/api/client'
import { useDepsStore } from '@/stores/deps'

const deps = useDepsStore()

const loading = ref(false)
const saving = ref(false)
const selectingDir = ref(false)
const selectingCookie = ref(false)
const biliLoading = ref(false)
const biliChecking = ref(false)
const biliDialogOpen = ref(false)
const biliQrUrl = ref('')
const biliQrKey = ref('')
const biliQrStatusCode = ref(1)
const biliSession = ref<BilibiliSessionDTO | null>(null)
const favoriteFolders = ref<FavoriteFolderDTO[]>([])
const favoriteSubscription = ref<FavoriteSubscriptionDTO | null>(null)
const favoriteLoading = ref(false)
const favoriteSaving = ref(false)
const favoriteRunning = ref(false)
let biliPollTimer: number | undefined
let favoriteSavePromise: Promise<boolean> | null = null
const form = reactive<SettingsDTO>({
  bindHost: '0.0.0.0',
  port: 12225,
  downloadDir: '',
  concurrentDownloads: 2,
  ytDlpPath: '',
  ytDlpCookiePath: '',
  ytDlpCookieEnabled: false,
  ffmpegPath: '',
  bilibiliMid: 0,
  bilibiliUname: '',
  bilibiliFace: '',
  bilibiliLoginAt: '',
  accessPassword: '',
})

const biliLoggedIn = computed(() => Boolean(biliSession.value?.loggedIn))
const favoriteForm = reactive({ mediaId: 0, title: '', enabled: false })
const favoriteFolderOptions = computed(() =>
  favoriteFolders.value.map((folder) => ({ label: folder.title, value: folder.id })),
)

async function load() {
  loading.value = true
  try {
    const settings = await getSettings()
    Object.assign(form, settings, { accessPassword: '' })
    await loadBilibiliSession()
    await loadFavoriteSubscription()
    if (biliSession.value?.loggedIn) {
      await loadFavoriteFolders()
    }
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

async function loadBilibiliSession() {
  biliSession.value = await getBilibiliSession()
}

async function loadFavoriteSubscription() {
  favoriteSubscription.value = await getFavoriteSubscription()
  favoriteForm.mediaId = favoriteSubscription.value.mediaId || 0
  favoriteForm.title = favoriteSubscription.value.title || ''
  favoriteForm.enabled = Boolean(favoriteSubscription.value.enabled)
  ensureFavoriteFolderOption(favoriteSubscription.value)
}

async function chooseDownloadDir() {
  selectingDir.value = true
  try {
    const selected = await selectDownloadDir(form.downloadDir)
    if (selected?.path) {
      form.downloadDir = selected.path
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '选择目录失败')
  } finally {
    selectingDir.value = false
  }
}

async function chooseYtDlpCookie() {
  selectingCookie.value = true
  try {
    const selected = await selectYtDlpCookieFile(form.ytDlpCookiePath)
    if (selected?.path) {
      form.ytDlpCookiePath = selected.path
      form.ytDlpCookieEnabled = true
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '选择 Cookie 文件失败')
  } finally {
    selectingCookie.value = false
  }
}

function clearYtDlpCookie() {
  form.ytDlpCookiePath = ''
  form.ytDlpCookieEnabled = false
}

function installDependencies() {
  deps.installMissing()
}

async function startBilibiliLogin() {
  stopBilibiliPolling()
  biliLoading.value = true
  biliQrStatusCode.value = 1
  try {
    const qr = await createBilibiliQRCode()
    biliQrUrl.value = qr.url
    biliQrKey.value = qr.qrcodeKey
    biliDialogOpen.value = true
    biliQrStatusCode.value = 86101
    biliPollTimer = window.setInterval(pollBilibiliLogin, 1000)
  } catch (error) {
    message.error(error instanceof Error ? error.message : '获取 Bilibili 登录二维码失败')
  } finally {
    biliLoading.value = false
  }
}

async function pollBilibiliLogin() {
  if (!biliQrKey.value) return
  try {
    const result = await pollBilibiliQRCode(biliQrKey.value)
    biliQrStatusCode.value = result.code
    if (result.code === 0) {
      stopBilibiliPolling()
      if (result.session) {
        biliSession.value = result.session
      } else {
        await loadBilibiliSession()
      }
      message.success('Bilibili 登录成功，已保存凭据')
      biliDialogOpen.value = false
      return
    }
    if (result.code === 86038) {
      stopBilibiliPolling()
    }
  } catch (error) {
    stopBilibiliPolling()
    message.error(error instanceof Error ? error.message : '轮询 Bilibili 登录状态失败')
  }
}

async function clearBilibiliLogin() {
  try {
    await clearBilibiliSession()
    biliSession.value = { loggedIn: false, mid: 0, uname: '', face: '', faceLocal: '', loginAt: '', status: 'missing', checkedAt: '', message: '', level: 0, sex: '', sign: '', vipStatus: 0, vipType: 0, vipLabel: '', vipDueDate: 0, seniorMember: false }
    message.success('已清除 Bilibili 登录凭据')
  } catch (error) {
    message.error(error instanceof Error ? error.message : '清除 Bilibili 登录失败')
  }
}

async function checkBilibiliLogin() {
  biliChecking.value = true
  try {
    biliSession.value = await checkBilibiliSession()
    if (biliSession.value.status === 'valid') {
      message.success(biliSession.value.message || 'Bilibili 登录有效')
    } else if (biliSession.value.status === 'invalid') {
      message.warning(biliSession.value.message || 'Bilibili 登录已失效，请重新登录')
    } else if (biliSession.value.status === 'error') {
      message.warning(`${biliSession.value.message || '检测失败，请稍后重试'}。不会自动清除已保存凭据，可稍后重试。`)
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '检查 Bilibili 登录状态失败')
  } finally {
    biliChecking.value = false
  }
}

async function loadFavoriteFolders() {
  favoriteLoading.value = true
  try {
    favoriteFolders.value = await getFavoriteFolders()
    ensureFavoriteFolderOption(favoriteSubscription.value)
    if (favoriteFolders.value.length === 0) {
      message.warning('未找到可订阅的 Bilibili 收藏夹')
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '获取 Bilibili 收藏夹失败')
  } finally {
    favoriteLoading.value = false
  }
}

function ensureFavoriteFolderOption(subscription = favoriteSubscription.value) {
  if (!subscription?.mediaId || !subscription.title) {
    return
  }
  if (favoriteFolders.value.some((folder) => folder.id === subscription.mediaId)) {
    return
  }
  favoriteFolders.value = [{ id: subscription.mediaId, title: subscription.title }, ...favoriteFolders.value]
}

function favoriteFormSnapshot() {
  return {
    mediaId: favoriteForm.mediaId,
    title: favoriteForm.title,
    enabled: favoriteForm.enabled,
  }
}

function restoreFavoriteForm(snapshot = favoriteSubscription.value) {
  favoriteForm.mediaId = snapshot?.mediaId || 0
  favoriteForm.title = snapshot?.title || ''
  favoriteForm.enabled = Boolean(snapshot?.enabled)
  ensureFavoriteFolderOption(snapshot)
}

function favoriteFormMatchesSubscription() {
  return (
    favoriteSubscription.value !== null &&
    favoriteForm.mediaId === (favoriteSubscription.value.mediaId || 0) &&
    favoriteForm.title === (favoriteSubscription.value.title || '') &&
    favoriteForm.enabled === Boolean(favoriteSubscription.value.enabled)
  )
}

async function persistFavoriteSubscription(previous = favoriteFormSnapshot(), successMessage = 'Bilibili 收藏夹订阅已保存') {
  const next = favoriteFormSnapshot()
  if (favoriteSavePromise) {
    await favoriteSavePromise
  }
  if (next.enabled && !next.mediaId) {
    favoriteForm.enabled = false
    message.warning('请先选择 Bilibili 收藏夹')
    return false
  }

  favoriteSaving.value = true
  const saveTask = updateFavoriteSubscription({
    mediaId: next.mediaId,
    title: next.title,
    enabled: next.enabled,
  })
    .then((subscription) => {
      favoriteSubscription.value = subscription
      restoreFavoriteForm(subscription)
      message.success(successMessage)
      return true
    })
    .catch((error) => {
      restoreFavoriteForm(previous)
      message.error(error instanceof Error ? error.message : '保存 Bilibili 收藏夹订阅失败')
      return false
    })
    .finally(() => {
      favoriteSaving.value = false
      favoriteSavePromise = null
    })
  favoriteSavePromise = saveTask
  return saveTask
}

async function onFavoriteFolderAutoChange(value: number) {
  const previous = favoriteFormSnapshot()
  const folder = favoriteFolders.value.find((item) => item.id === value)
  favoriteForm.title = folder?.title ?? ''
  await persistFavoriteSubscription(previous, 'Bilibili 收藏夹已保存')
}

async function onFavoriteEnabledChange() {
  const previous = favoriteSubscription.value
    ? {
        mediaId: favoriteSubscription.value.mediaId || 0,
        title: favoriteSubscription.value.title || '',
        enabled: Boolean(favoriteSubscription.value.enabled),
      }
    : favoriteFormSnapshot()
  await persistFavoriteSubscription(previous, favoriteForm.enabled ? 'Bilibili 收藏夹订阅已启用' : 'Bilibili 收藏夹订阅已关闭')
}

async function runFavoriteNow() {
  favoriteRunning.value = true
  try {
    if (favoriteSavePromise) {
      const saved = await favoriteSavePromise
      if (!saved) {
        return
      }
    }
    if (!favoriteFormMatchesSubscription()) {
      const saved = await persistFavoriteSubscription(favoriteSubscription.value ?? favoriteFormSnapshot(), 'Bilibili 收藏夹已保存')
      if (!saved) {
        return
      }
    }
    favoriteSubscription.value = await runFavoriteSubscription()
    restoreFavoriteForm(favoriteSubscription.value)
    message.success('已检查 Bilibili 收藏夹')
  } catch (error) {
    message.error(error instanceof Error ? error.message : '检查 Bilibili 收藏夹失败')
  } finally {
    favoriteRunning.value = false
  }
}

function closeBilibiliDialog() {
  biliDialogOpen.value = false
  stopBilibiliPolling()
}

function stopBilibiliPolling() {
  if (biliPollTimer !== undefined) {
    window.clearInterval(biliPollTimer)
    biliPollTimer = undefined
  }
}

function biliQrStatus() {
  switch (biliQrStatusCode.value) {
    case 86090:
      return 'scanned'
    case 86038:
      return 'expired'
    case 0:
      return 'success'
    case 1:
      return 'loading'
    default:
      return undefined
  }
}

function biliQrStatusText() {
  switch (biliQrStatusCode.value) {
    case 86101:
      return '等待扫码'
    case 86090:
      return '已扫码，请在手机端确认'
    case 86038:
      return '二维码已过期，请重新获取'
    case 0:
      return '登录成功'
    default:
      return '正在获取登录状态'
  }
}

function formatBiliLoginAt(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function formatBiliVipDueDate(value?: number) {
  if (!value || value <= 0) return '-'
  return new Date(value).toLocaleString()
}

function biliVipText() {
  if (!biliSession.value || biliSession.value.vipStatus !== 1) return '未开通大会员'
  return biliSession.value.vipLabel || '大会员'
}

function localBiliAvatarURL() {
  return bilibiliAvatarURL(biliSession.value)
}

function biliStatusText() {
  switch (biliSession.value?.status) {
    case 'valid':
      return '登录有效'
    case 'invalid':
      return '登录已失效'
    case 'error':
      return '检查失败'
    case 'unchecked':
      return '未检查'
    default:
      return '未登录'
  }
}

function biliStatusColor() {
  switch (biliSession.value?.status) {
    case 'valid':
      return 'success'
    case 'invalid':
      return 'error'
    case 'error':
      return 'warning'
    case 'unchecked':
      return 'processing'
    default:
      return 'default'
  }
}

function dependencyEvent(name: string) {
  return deps.installEvents[name]
}

function dependencyProgress(event?: DependencyInstallEvent) {
  if (!event?.percent || event.percent < 0) return 0
  return Math.min(100, Math.round(event.percent))
}

function dependencyStatus(item: { label: string; value: { exists: boolean } }) {
  const event = dependencyEvent(item.label)
  if (!event) return item.value.exists ? '已安装' : '未安装'
  switch (event.type) {
    case 'started':
      return '准备下载'
    case 'progress':
      return event.totalBytes && event.totalBytes > 0 ? `下载中 ${dependencyProgress(event)}%` : '下载中'
    case 'skipped':
      return '已跳过'
    case 'completed':
      return '已下载'
    case 'failed':
      return '下载失败'
    default:
      return item.value.exists ? '已安装' : '未安装'
  }
}

function dependencyTagColor(item: { label: string; value: { exists: boolean } }) {
  const event = dependencyEvent(item.label)
  if (event?.type === 'failed') return 'error'
  if (event?.type === 'progress' || event?.type === 'started') return 'processing'
  if (event?.type === 'completed' || item.value.exists) return 'success'
  return 'default'
}

function formatBytes(value?: number) {
  if (!value || value <= 0) return '-'
  const mib = value / 1024 / 1024
  return `${mib.toFixed(1)} MiB`
}

onMounted(load)
onBeforeUnmount(stopBilibiliPolling)
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
        <a-input-group compact class="download-dir-control">
          <a-input v-model:value="form.downloadDir" placeholder="选择或输入下载目录" />
          <a-button :loading="selectingDir" @click="chooseDownloadDir">选择目录</a-button>
        </a-input-group>
      </a-form-item>

      <a-form-item label="yt-dlp Cookie">
        <div class="cookie-control">
          <a-switch v-model:checked="form.ytDlpCookieEnabled" checked-children="启用" un-checked-children="关闭" />
          <a-input-group compact class="cookie-path-control">
            <a-input v-model:value="form.ytDlpCookiePath" placeholder="选择或输入 cookie txt 文件路径" />
            <a-button :loading="selectingCookie" @click="chooseYtDlpCookie">选择文件</a-button>
            <a-button @click="clearYtDlpCookie">清空</a-button>
          </a-input-group>
        </div>
        <div class="field-hint">默认关闭；启用后，YouTube 解析和下载会通过 yt-dlp 使用该 cookies.txt 文件。</div>
      </a-form-item>

      <a-row :gutter="16">
        <a-col :xs="24" :md="12">
          <a-form-item label="并发下载数">
            <a-input-number v-model:value="form.concurrentDownloads" style="width: 100%" :min="1" :max="16" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-form-item label="新的共享密码">
        <a-input-password v-model:value="form.accessPassword" placeholder="留空则保持当前密码不变" />
      </a-form-item>
    </a-form>

    <a-divider />

    <div class="bili-section">
      <div class="deps-header">
        <div>
          <h2>Bilibili 登录</h2>
          <div class="field-hint">扫码登录后会保存 Bilibili 登录凭据，当前仅用于保存登录状态展示。</div>
        </div>
        <a-space>
          <a-button v-if="biliLoggedIn" :loading="biliChecking" @click="checkBilibiliLogin">检查登录状态</a-button>
          <a-button :loading="biliLoading" @click="startBilibiliLogin">
            {{ biliLoggedIn ? '重新登录' : '扫码登录' }}
          </a-button>
          <a-button v-if="biliLoggedIn" danger @click="clearBilibiliLogin">清除登录</a-button>
        </a-space>
      </div>

      <div class="bili-card">
        <a-avatar v-if="biliLoggedIn && localBiliAvatarURL()" :size="56" :src="localBiliAvatarURL()" />
        <a-avatar v-else :size="56">B</a-avatar>
        <div class="bili-info">
          <div class="bili-title">
            <span>{{ biliLoggedIn ? '已登录' : '未登录' }}</span>
            <a-tag :color="biliLoggedIn ? 'success' : 'default'">
              {{ biliLoggedIn ? '已保存凭据' : '未保存凭据' }}
            </a-tag>
            <a-tag :color="biliStatusColor()">{{ biliStatusText() }}</a-tag>
          </div>
          <div v-if="biliLoggedIn" class="bili-meta">
            昵称：{{ biliSession?.uname || '-' }} · MID：{{ biliSession?.mid || '-' }} · LV{{ biliSession?.level || 0 }}
          </div>
          <div v-if="biliLoggedIn" class="bili-meta">
            会员：{{ biliVipText() }} · 到期：{{ formatBiliVipDueDate(biliSession?.vipDueDate) }} · 硬核会员：{{ biliSession?.seniorMember ? '是' : '否' }}
          </div>
          <div v-if="biliLoggedIn" class="bili-meta">
            性别：{{ biliSession?.sex || '-' }} · 登录时间：{{ formatBiliLoginAt(biliSession?.loginAt) }} · 上次检查：{{ formatBiliLoginAt(biliSession?.checkedAt) }}
          </div>
          <div v-if="biliLoggedIn && biliSession?.sign" class="bili-sign">{{ biliSession.sign }}</div>
          <div v-if="!biliLoggedIn" class="field-hint">点击“扫码登录”后使用 Bilibili 手机客户端扫码确认。</div>
        </div>
      </div>

      <div class="bili-card favorite-card">
        <div class="favorite-main">
          <div class="bili-title">
            <span>Bilibili 收藏夹订阅</span>
            <a-tag :color="favoriteForm.enabled ? 'processing' : 'default'">
              {{ favoriteForm.enabled ? '已启用' : '未启用' }}
            </a-tag>
          </div>
          <div class="field-hint">每 10 分钟检查一次收藏夹；视频全部下载完成后自动移出收藏夹。</div>
          <div class="favorite-controls">
            <a-select
              v-model:value="favoriteForm.mediaId"
              :options="favoriteFolderOptions"
              :loading="favoriteLoading"
              :disabled="favoriteSaving"
              placeholder="请选择收藏夹"
              style="min-width: 240px"
              @dropdownVisibleChange="(open: boolean) => open && favoriteFolders.length === 0 && loadFavoriteFolders()"
              @change="onFavoriteFolderAutoChange"
            />
            <a-switch v-model:checked="favoriteForm.enabled" :loading="favoriteSaving" :disabled="favoriteSaving" @change="onFavoriteEnabledChange" />
            <a-button :loading="favoriteLoading" @click="loadFavoriteFolders">刷新收藏夹</a-button>
            <a-button :disabled="!favoriteForm.enabled || favoriteSaving" :loading="favoriteRunning" @click="runFavoriteNow">立即检查</a-button>
          </div>
          <div class="bili-meta">
            当前收藏夹：{{ favoriteForm.title || '-' }} 路 上次检查：{{ formatBiliLoginAt(favoriteSubscription?.lastCheckedAt) }}
          </div>
          <div v-if="favoriteSubscription?.lastError" class="deps-error">{{ favoriteSubscription.lastError }}</div>
        </div>
      </div>
    </div>

    <a-divider />

    <div class="deps-section">
      <div class="deps-header">
        <div>
          <h2>依赖工具</h2>
          <div class="field-hint">路径固定为 exe 同级 data 目录，无需手动配置。</div>
        </div>
        <a-space>
          <a-button :loading="deps.loading" @click="deps.check()">刷新状态</a-button>
          <a-button type="primary" :loading="deps.installing" @click="installDependencies">
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
            <a-tag :color="dependencyTagColor(item)">
              {{ dependencyStatus(item) }}
            </a-tag>
          </div>
          <div class="deps-path">{{ item.value.path }}</div>
          <div v-if="dependencyEvent(item.label)?.type === 'progress'" class="deps-progress">
            <a-progress
              :percent="dependencyProgress(dependencyEvent(item.label))"
              :status="dependencyEvent(item.label)?.error ? 'exception' : 'active'"
              :show-info="Boolean(dependencyEvent(item.label)?.totalBytes)"
            />
            <div class="field-hint">
              已下载 {{ formatBytes(dependencyEvent(item.label)?.bytes) }} / {{ formatBytes(dependencyEvent(item.label)?.totalBytes) }}
            </div>
          </div>
          <div v-if="item.value.error" class="deps-error">{{ item.value.error }}</div>
          <div v-if="dependencyEvent(item.label)?.error" class="deps-error">{{ dependencyEvent(item.label)?.error }}</div>
        </div>
      </div>
    </div>
  </a-card>

  <a-modal v-model:open="biliDialogOpen" title="Bilibili 扫码登录" :footer="null" @cancel="closeBilibiliDialog">
    <div class="bili-qrcode-box">
      <a-qrcode :value="biliQrUrl || 'loading'" :status="biliQrStatus()" />
      <div class="bili-qrcode-status">{{ biliQrStatusText() }}</div>
      <a-button v-if="biliQrStatusCode === 86038" type="primary" @click="startBilibiliLogin">重新获取二维码</a-button>
    </div>
  </a-modal>
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

.bili-section {
  display: grid;
  gap: 16px;
}

.bili-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  border: 1px solid rgba(22, 32, 51, 0.08);
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.72);
}

.bili-info {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.bili-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
}

.bili-meta {
  color: #58708f;
  word-break: break-all;
}

.bili-sign {
  color: #6f89a4;
  font-size: 12px;
  line-height: 1.6;
  word-break: break-all;
}

.favorite-card {
  align-items: stretch;
}

.favorite-main {
  display: grid;
  gap: 10px;
  width: 100%;
}

.favorite-controls {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}

.bili-qrcode-box {
  display: grid;
  place-items: center;
  gap: 14px;
  padding: 12px 0;
}

.bili-qrcode-status {
  color: #58708f;
}

.download-dir-control {
  display: flex;
}

.download-dir-control :deep(.ant-input) {
  flex: 1;
  min-width: 0;
}

.cookie-control {
  display: flex;
  align-items: center;
  gap: 12px;
}

.cookie-path-control {
  display: flex;
  flex: 1;
  min-width: 0;
}

.cookie-path-control :deep(.ant-input) {
  flex: 1;
  min-width: 0;
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
.field-hint,
.field-readonly {
  color: #6f89a4;
  font-size: 12px;
}

.field-readonly {
  font-family: monospace;
  word-break: break-all;
  padding: 4px 0;
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

.deps-progress {
  display: grid;
  gap: 4px;
  margin-top: 8px;
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

  .cookie-control {
    display: grid;
  }
}
</style>
