import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { login as loginRequest, setToken } from '@/api/client'

const tokenKey = 'downgo-token'

export const useSessionStore = defineStore('session', () => {
  const token = ref(localStorage.getItem(tokenKey) ?? '')
  setToken(token.value)

  const authenticated = computed(() => token.value.length > 0)

  async function login(password: string) {
    const response = await loginRequest(password)
    token.value = response.token
    localStorage.setItem(tokenKey, response.token)
    setToken(response.token)
  }

  function logout() {
    token.value = ''
    localStorage.removeItem(tokenKey)
    setToken('')
  }

  return {
    token,
    authenticated,
    login,
    logout,
  }
})
