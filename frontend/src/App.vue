<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { BellOutlined, ClockCircleOutlined, DownloadOutlined, LogoutOutlined, MenuOutlined, SettingOutlined } from '@ant-design/icons-vue'
import zhCN from 'ant-design-vue/es/locale/zh_CN'

import { useDepsStore } from '@/stores/deps'
import { useDownloadsStore } from '@/stores/downloads'
import { useSessionStore } from '@/stores/session'

const downloads = useDownloadsStore()
const deps = useDepsStore()

const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const navOpen = ref(false)
const isMobile = ref(false)

const isLogin = computed(() => route.name === 'login')

const selectedKeys = computed(() => [String(route.name ?? 'downloads')])

const updateViewport = () => {
  isMobile.value = window.innerWidth <= 960
  if (!isMobile.value) {
    navOpen.value = false
  }
}

const logout = () => {
  navOpen.value = false
  downloads.disconnect()
  deps.disconnectInstall()
  session.logout()
  router.push({ name: 'login' })
}

const goTo = (name: 'downloads' | 'notifications' | 'scheduled-tasks' | 'settings') => {
  navOpen.value = false
  router.push({ name })
}

onMounted(() => {
  updateViewport()
  window.addEventListener('resize', updateViewport)
  if (session.authenticated) {
    downloads.connect()
    if (!deps.initialized) deps.check()
    deps.loadInstallStatus()
  }
})

watch(() => session.authenticated, (val) => {
  if (val) {
    downloads.connect()
    if (!deps.initialized) deps.check()
    deps.loadInstallStatus()
  } else {
    downloads.disconnect()
    deps.disconnectInstall()
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', updateViewport)
})
</script>

<template>
  <a-config-provider :locale="zhCN">
    <router-view v-if="isLogin" />

    <a-layout v-else class="app-shell">
      <a-layout-sider v-if="!isMobile" width="240" theme="light" class="sider">
        <div class="brand">
          <div class="brand-kicker">DownGo</div>
          <div class="brand-copy">局域网视频下载服务</div>
        </div>

        <a-menu :selected-keys="selectedKeys" mode="inline">
          <a-menu-item key="downloads" @click="goTo('downloads')">
            <template #icon><DownloadOutlined /></template>
            下载列表
          </a-menu-item>
          <a-menu-item key="notifications" @click="goTo('notifications')">
            <template #icon><BellOutlined /></template>
            通知
          </a-menu-item>
          <a-menu-item key="scheduled-tasks" @click="goTo('scheduled-tasks')">
            <template #icon><ClockCircleOutlined /></template>
            定时任务
          </a-menu-item>
          <a-menu-item key="settings" @click="goTo('settings')">
            <template #icon><SettingOutlined /></template>
            设置
          </a-menu-item>
        </a-menu>

        <div class="sider-footer">
          <a-button block @click="logout">
            <template #icon><LogoutOutlined /></template>
            退出登录
          </a-button>
        </div>
      </a-layout-sider>

      <a-layout-content class="content">
        <div v-if="isMobile" class="mobile-header">
          <div>
            <div class="brand-kicker">DownGo</div>
            <div class="brand-copy">局域网视频下载服务</div>
          </div>
          <a-button type="text" size="large" @click="navOpen = true">
            <template #icon><MenuOutlined /></template>
          </a-button>
        </div>
        <router-view />
      </a-layout-content>
    </a-layout>

    <a-drawer v-model:open="navOpen" placement="left" width="280" title="导航" :body-style="{ padding: '16px' }">
      <a-menu :selected-keys="selectedKeys" mode="inline">
        <a-menu-item key="downloads" @click="goTo('downloads')">
          <template #icon><DownloadOutlined /></template>
          下载列表
        </a-menu-item>
        <a-menu-item key="notifications" @click="goTo('notifications')">
          <template #icon><BellOutlined /></template>
          通知
        </a-menu-item>
        <a-menu-item key="scheduled-tasks" @click="goTo('scheduled-tasks')">
          <template #icon><ClockCircleOutlined /></template>
          定时任务
        </a-menu-item>
        <a-menu-item key="settings" @click="goTo('settings')">
          <template #icon><SettingOutlined /></template>
          设置
        </a-menu-item>
      </a-menu>
      <a-button block class="drawer-logout" @click="logout">
        <template #icon><LogoutOutlined /></template>
        退出登录
      </a-button>
    </a-drawer>
  </a-config-provider>
</template>

<style scoped>
.app-shell {
  min-height: 100vh;
  background:
    radial-gradient(circle at top left, rgba(157, 214, 255, 0.28), transparent 30%),
    linear-gradient(145deg, #edf4ff 0%, #f9fcf6 100%);
}

.sider {
  border-right: 1px solid rgba(22, 32, 51, 0.08);
  display: flex;
  flex-direction: column;
}

.brand {
  padding: 28px 20px 20px;
}

.brand-kicker {
  font-size: 28px;
  font-weight: 700;
  letter-spacing: 0.03em;
}

.brand-copy {
  margin-top: 8px;
  color: #58708f;
}

.sider-footer {
  margin-top: auto;
  padding: 20px;
}

.content {
  padding: 24px;
}

.mobile-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  padding: 14px 16px;
  border: 1px solid rgba(22, 32, 51, 0.08);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.8);
  box-shadow: 0 12px 30px rgba(18, 34, 64, 0.08);
}

.drawer-logout {
  margin-top: 16px;
}

@media (max-width: 960px) {
  .content {
    padding: 16px;
  }

  .brand-kicker {
    font-size: 24px;
  }

  .brand-copy {
    margin-top: 4px;
  }
}
</style>
