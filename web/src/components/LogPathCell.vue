<script setup lang="ts">
import { computed, ref } from 'vue'
import { ExternalLink, Image, X } from 'lucide-vue-next'
import type { LogRow } from '@/api/types'
import { useUi } from '@/stores/ui'
import { imagePreviewUrl } from '@/utils/preview'

const props = defineProps<{
  row: Pick<LogRow, 'host' | 'path' | 'status'> & { method?: string }
  clickable?: boolean
}>()
const emit = defineEmits<{ select: [] }>()
const ui = useUi()
const url = computed(() =>
  imagePreviewUrl(
    { ...props.row, method: props.row.method ?? 'GET' },
    ui.meta?.logs?.preview ?? { scheme: 'https', port: 443 },
  ),
)
const dialog = ref<HTMLDialogElement | null>(null)
const opened = ref(false)
const loading = ref(true)
const failed = ref(false)
function show() {
  loading.value = true
  failed.value = false
  opened.value = true
  dialog.value?.showModal()
}
function close() { dialog.value?.close(); opened.value = false }
function onBackdrop(event: MouseEvent) {
  if (event.target !== dialog.value) return
  const r = dialog.value?.getBoundingClientRect()
  if (r && (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom)) close()
}
</script>

<template>
  <div class="log-path">
    <button
      v-if="clickable"
      type="button"
      class="link clip mono"
      :title="row.path"
      @click="emit('select')"
    >
      {{ row.path }}
    </button>
    <span v-else class="clip mono" :title="row.path">{{ row.path }}</span>
    <button
      v-if="url"
      type="button"
      class="image-preview-trigger"
      :aria-label="`预览图片 ${row.path}`"
      @click.stop="show"
    >
      <Image :size="13" />预览
    </button>
  </div>
  <Teleport to="body">
    <dialog ref="dialog" class="log-image-dialog" aria-label="图片预览" @cancel.prevent="close" @click="onBackdrop" @close="opened = false">
      <header><strong>图片预览</strong><a :href="url" target="_blank" rel="noopener noreferrer" title="在新窗口打开原图"><ExternalLink :size="14" />打开原图</a><button type="button" class="x" aria-label="关闭图片预览" autofocus @click="close"><X :size="18" /></button></header>
      <p class="image-source mono">{{ url }}</p>
      <div class="image-stage">
        <p v-if="loading && !failed" class="preview-message" role="status">正在加载图片…</p>
        <p v-if="failed" class="preview-message" role="alert">图片加载失败，请检查访问权限、证书和域名对外端口，也可尝试打开原图。</p>
        <img v-if="opened && !failed" :src="url" alt="访问日志中的 PNG 图片" referrerpolicy="no-referrer" @load="loading = false" @error="failed = true; loading = false">
      </div>
    </dialog>
  </Teleport>
</template>

<style scoped>
.log-path { display: flex; align-items: center; gap: 9px; min-width: 0; max-width: 100%; }
.log-path .clip { flex: 0 1 auto; max-width: 100%; min-width: 0; }
.image-preview-trigger { display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0; padding: 3px 5px; border: 0; border-radius: 4px; background: var(--accent-soft); color: var(--accent-text); font: inherit; font-size: 11px; white-space: nowrap; }
.image-preview-trigger:hover { background: var(--hover); }
.log-image-dialog { width: min(960px, calc(100vw - 32px)); max-height: calc(100dvh - 48px); padding: 0; border: 1px solid var(--line-strong); border-radius: 10px; color: var(--text); background: var(--elev); box-shadow: var(--pop-shadow); overflow: auto; }
.log-image-dialog::backdrop { background: var(--overlay); }
.log-image-dialog header { display: flex; align-items: center; gap: 16px; padding: 14px 18px; border-bottom: 1px solid var(--line); }
.log-image-dialog header strong { margin-right: auto; font-size: 14px; }
.log-image-dialog header a { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; }
.image-source { margin: 0; padding: 10px 18px; color: var(--dim); white-space: normal; overflow-wrap: anywhere; }
.image-stage { display: grid; place-items: center; padding: 18px; min-height: 160px; background: var(--sunken); }
.image-stage img { display: block; max-width: 100%; max-height: 68dvh; object-fit: contain; }
.preview-message { margin: 12px 0; font-size: 13px; color: var(--dim); max-width: 60ch; text-align: center; line-height: 1.7; }
</style>
