<script setup lang="ts" generic="T extends string">
defineProps<{ modelValue: T; items: [T, string][] }>()
defineEmits<{ 'update:modelValue': [v: T] }>()
</script>

<template>
  <div class="seg" role="group">
    <button
      v-for="[v, label] in items" :key="v" type="button" :aria-pressed="modelValue === v ? 'true' : 'false'"
      @click="$emit('update:modelValue', v)"
    >{{ label }}</button>
  </div>
</template>
