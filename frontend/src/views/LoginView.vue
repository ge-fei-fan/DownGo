<script setup lang="ts">
import { reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useRouter } from 'vue-router'

import { useSessionStore } from '@/stores/session'

const router = useRouter()
const session = useSessionStore()
const loading = ref(false)
const form = reactive({ password: '' })

const submit = async () => {
  if (loading.value) {
    return
  }
  if (!form.password.trim()) {
    message.warning('请输入访问密码')
    return
  }

  loading.value = true
  try {
    await session.login(form.password.trim())
    router.push({ name: 'downloads' })
  } catch (error) {
    message.error(error instanceof Error ? error.message : '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <div class="eyebrow">共享访问登录</div>
      <h1>DownGo</h1>
      <p>请输入日志文件中记录的初始密码，或设置页中配置的共享密码。</p>

      <a-form :model="form" layout="vertical" @finish="submit">
        <a-form-item label="访问密码" name="password">
          <a-input-password
            v-model:value="form.password"
            placeholder="请输入访问密码"
            @pressEnter="submit"
          />
        </a-form-item>
        <a-button
          type="primary"
          block
          html-type="submit"
          size="large"
          :loading="loading"
          @click="submit"
        >
          登录
        </a-button>
      </a-form>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  background:
    radial-gradient(circle at top, rgba(255, 214, 163, 0.35), transparent 30%),
    linear-gradient(135deg, #edf4ff, #f8fdf2);
}

.login-card {
  width: min(420px, calc(100vw - 32px));
  padding: 32px;
  border-radius: 24px;
  background: rgba(255, 255, 255, 0.92);
  box-shadow: 0 20px 60px rgba(18, 34, 64, 0.16);
}

.eyebrow {
  color: #6682a3;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  font-size: 12px;
  font-weight: 700;
}

h1 {
  margin: 10px 0 12px;
}

p {
  color: #5f6f86;
  margin-bottom: 24px;
}

@media (max-width: 640px) {
  .login-card {
    width: calc(100vw - 24px);
    padding: 24px 18px;
    border-radius: 20px;
  }
}
</style>
