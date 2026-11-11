import { defineStore } from 'pinia'
import { ref } from 'vue'

import { api, getToken, setToken } from '@/api'

export const useAuth = defineStore('auth', () => {
  const token = ref(getToken())
  const user = ref(localStorageGet('ipguard-user'))

  async function login(username: string, password: string) {
    const r = await api.login(username, password)
    token.value = r.token
    user.value = r.username
    setToken(r.token)
    localStorageSet('ipguard-user', r.username)
  }
  function logout() {
    token.value = ''
    setToken('')
  }
  return { token, user, login, logout }
})

function localStorageGet(k: string): string {
  try { return localStorage.getItem(k) ?? '' } catch { return '' }
}
function localStorageSet(k: string, v: string) {
  try { localStorage.setItem(k, v) } catch { /* ignore */ }
}
