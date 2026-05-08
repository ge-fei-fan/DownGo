import { createRouter, createWebHistory } from 'vue-router'

import { useSessionStore } from '@/stores/session'
import DownloadsView from '@/views/DownloadsView.vue'
import LoginView from '@/views/LoginView.vue'
import SettingsView from '@/views/SettingsView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/downloads' },
    { path: '/login', name: 'login', component: LoginView },
    { path: '/downloads', name: 'downloads', component: DownloadsView },
    { path: '/settings', name: 'settings', component: SettingsView },
  ],
})

router.beforeEach((to) => {
  const session = useSessionStore()
  if (to.name === 'login') {
    if (session.token) {
      return { name: 'downloads' }
    }
    return true
  }
  if (!session.token) {
    return { name: 'login' }
  }
  return true
})

export default router
