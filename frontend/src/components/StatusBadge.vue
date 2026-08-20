<template>
  <span class="status-badge" :class="`is-${tone}`">
    <span class="status-badge__dot" />
    {{ displayLabel }}
  </span>
</template>

<script setup>
import { computed } from 'vue';

const props = defineProps({
  status: { type: String, default: '' },
  label: { type: String, default: '' }
});

const labels = {
  pending: '待处理', running: '传输中', success: '已完成', failed: '失败', paused: '已暂停', canceled: '已取消',
  detecting: '扫描中', watching: '监控中', stopped: '已停止', error: '异常', online: '在线'
};
const tones = {
  pending: 'amber', running: 'cyan', success: 'green', failed: 'red', paused: 'violet', canceled: 'muted',
  detecting: 'cyan', watching: 'green', stopped: 'muted', error: 'red', online: 'green'
};

const displayLabel = computed(() => props.label || labels[props.status] || props.status || '未知');
const tone = computed(() => tones[props.status] || 'muted');
</script>
