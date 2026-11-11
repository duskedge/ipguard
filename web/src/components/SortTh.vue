<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ k: string; label: string; sort: string; dir: 'asc' | 'desc' }>()
const emit = defineEmits<{ sort: [key: string, dir: 'asc' | 'desc'] }>()
const on = computed(() => props.sort === props.k)

function click() {
  if (on.value) emit('sort', props.k, props.dir === 'asc' ? 'desc' : 'asc')
  else emit('sort', props.k, 'desc')
}
</script>

<template>
  <th :aria-sort="on ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'" @click="click">
    {{ label }}<span class="sort">{{ on ? (dir === 'asc' ? '↑' : '↓') : '' }}</span>
  </th>
</template>
