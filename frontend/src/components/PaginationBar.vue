<template>
  <div class="pagination-bar">
    <span class="pagination-total">共 <strong>{{ total }}</strong> 条记录</span>
    <div class="pagination-controls">
      <label class="pagination-size">
        <span>每页</span>
        <select :value="pageSize" @change="changeSize(Number($event.target.value))">
          <option v-for="size in pageSizes" :key="size" :value="size">{{ size }}</option>
        </select>
      </label>
      <button class="icon-button" type="button" :disabled="page <= 1" aria-label="上一页" @click="go(page - 1)">
        <UiIcon name="chevronLeft" :size="16" />
      </button>
      <span class="pagination-page"><strong>{{ safePage }}</strong> / {{ pageCount }}</span>
      <button class="icon-button" type="button" :disabled="page >= pageCount" aria-label="下一页" @click="go(page + 1)">
        <UiIcon name="chevronRight" :size="16" />
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import UiIcon from './UiIcon.vue';

const props = defineProps({
  page: { type: Number, default: 1 },
  pageSize: { type: Number, default: 20 },
  total: { type: Number, default: 0 },
  pageSizes: { type: Array, default: () => [20, 50, 100, 200] }
});
const emit = defineEmits(['update:page', 'update:pageSize', 'change']);
const pageCount = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const safePage = computed(() => Math.min(Math.max(1, props.page), pageCount.value));

function go(page) {
  const next = Math.min(Math.max(1, page), pageCount.value);
  if (next === props.page) return;
  emit('update:page', next);
  emit('change');
}

function changeSize(size) {
  emit('update:pageSize', size);
  emit('update:page', 1);
  emit('change');
}
</script>
