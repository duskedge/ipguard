import { polyfillCountryFlagEmojis } from 'country-flag-emoji-polyfill'
import flagFontUrl from 'country-flag-emoji-polyfill/dist/TwemojiCountryFlags.woff2?url'
import 'leaflet/dist/leaflet.css'
import './styles.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'

import { setUnauthorizedHandler } from '@/api'
import App from '@/App.vue'
import { router } from '@/router'
import { useAuth } from '@/stores/auth'

polyfillCountryFlagEmojis('Twemoji Country Flags', flagFontUrl)

const app = createApp(App)
app.use(createPinia())
app.use(router)

setUnauthorizedHandler(() => {
  useAuth().logout()
  if (router.currentRoute.value.name !== 'login') {
    router.replace({ name: 'login', query: { next: router.currentRoute.value.fullPath } })
  }
})

app.mount('#app')
