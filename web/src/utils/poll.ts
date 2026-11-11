import { onBeforeUnmount, onMounted, ref, watch, type WatchSource } from 'vue'

import { errText } from '@/api'
import { useUi } from '@/stores/ui'

/**
 * 页面数据加载：挂载时、依赖变化时、封禁/解封之后、以及每 30 秒静默刷新一次。
 * 只采用最后一次请求的结果，避免快速切换筛选时旧响应覆盖新数据。
 */
export function useLoader<T>(fetcher: () => Promise<T>, deps: WatchSource[] = [], intervalMs = 30_000) {
  const data = ref<T | null>(null)
  const loading = ref(false)
  const error = ref('')
  const ui = useUi()
  let seq = 0
  let timer: ReturnType<typeof setInterval> | undefined

  async function load(silent = false) {
    const my = ++seq
    if (!silent) loading.value = true
    try {
      const v = await fetcher()
      if (my === seq) { data.value = v as never; error.value = '' }
    } catch (e) {
      if (my === seq) error.value = errText(e)
    } finally {
      if (my === seq) loading.value = false
    }
  }

  onMounted(() => {
    load()
    timer = setInterval(() => { if (!document.hidden) load(true) }, intervalMs)
  })
  onBeforeUnmount(() => clearInterval(timer))
  if (deps.length) watch(deps, () => load(), { deep: true })
  watch(() => ui.dataVersion, () => load(true))

  return { data, loading, error, reload: load }
}

/** 输入框防抖：给 watch 用的延迟值。 */
export function useDebounced<T>(src: () => T, ms = 250) {
  const out = ref(src()) as { value: T }
  let t: ReturnType<typeof setTimeout> | undefined
  watch(src, (v) => {
    clearTimeout(t)
    t = setTimeout(() => { out.value = v }, ms)
  })
  return out
}
