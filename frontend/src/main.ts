import { createApp } from 'vue'
import { createPinia } from 'pinia'
import Antd from 'ant-design-vue'
import { message } from 'ant-design-vue'
import 'ant-design-vue/dist/reset.css'

import App from './App.vue'
import router from './router'
import { setUnauthorizedHandler } from './api/client'
import { useSessionStore } from './stores/session'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)
app.use(Antd)

let unauthorizedNotified = false
setUnauthorizedHandler(() => {
  const session = useSessionStore(pinia)
  if (session.token) {
    session.logout()
  }
  if (!unauthorizedNotified) {
    unauthorizedNotified = true
    message.warning('登录已过期，请重新登录')
    window.setTimeout(() => {
      unauthorizedNotified = false
    }, 1000)
  }
  if (router.currentRoute.value.name !== 'login') {
    void router.push({ name: 'login' })
  }
})

app.mount('#app')
