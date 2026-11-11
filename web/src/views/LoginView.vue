<script setup lang="ts">
import { Moon, Shield, Sun } from 'lucide-vue-next'
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { errText } from '@/api'
import { useAuth } from '@/stores/auth'
import { useUi } from '@/stores/ui'

const auth = useAuth()
const ui = useUi()
const route = useRoute()
const router = useRouter()

const username = ref(auth.user || '')
const password = ref('')
const err = ref('')
const busy = ref(false)

async function submit() {
  if (!username.value.trim() || !password.value) { err.value = '请填写用户名和密码'; return }
  err.value = ''
  busy.value = true
  try {
    await auth.login(username.value.trim(), password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/dashboard'
    router.replace(next)
  } catch (e) {
    err.value = errText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section id="login">
    <button type="button" class="btn sm theme-float" @click="ui.toggleTheme()">
      <Sun v-if="ui.theme !== 'light'" class="i" :size="16" /><Moon v-else class="i" :size="16" />
      {{ ui.theme !== 'light' ? '浅色' : '深色' }}
    </button>
    <div class="login-card">
      <div class="brand-lockup"><span class="mark"><Shield :size="20" :stroke-width="1.75" /></span></div>
      <h1>ipguard</h1>
      <p class="sub">访问日志与 IP 封禁</p>
      <form autocomplete="on" @submit.prevent="submit">
        <label for="user">用户名</label>
        <input id="user" v-model="username" class="field" name="username" autocomplete="username" placeholder="用户名">
        <label for="pass">密码</label>
        <input id="pass" v-model="password" class="field" name="password" type="password" autocomplete="current-password" placeholder="密码">
        <p class="err" role="alert">{{ err }}</p>
        <button class="btn primary" type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</button>
      </form>
      <p class="login-foot"><span>仅限内网访问</span><span>ipguard</span></p>
    </div>
  </section>
</template>
