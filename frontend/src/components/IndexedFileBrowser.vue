<template>
  <Teleport to="body">
    <Transition name="sheet-slide">
      <div v-if="visible" class="sheet-layer indexed-browser-layer" @click.self="emit('close')">
        <aside class="side-sheet is-wide indexed-browser" role="dialog" aria-modal="true" aria-labelledby="indexed-browser-title">
          <header class="sheet-header">
            <div>
              <span class="eyebrow">PERSISTED FILE INDEX</span>
              <h2 id="indexed-browser-title">{{ title }}</h2>
              <p v-if="subtitle">{{ subtitle }}</p>
            </div>
            <button class="icon-button" type="button" aria-label="关闭" @click="emit('close')"><UiIcon name="close" :size="18" /></button>
          </header>

          <div class="index-toolbar">
            <button class="icon-button" type="button" title="返回上一层" :disabled="!currentPath || loading" @click="openPath(parentPath)"><UiIcon name="arrowLeft" :size="17" /></button>
            <div class="index-location"><UiIcon name="folder" :size="16" /><span class="mono">{{ currentPath || '/' }}</span></div>
            <button class="ui-button is-small" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" :size="15" /> 刷新</button>
          </div>

          <div class="indexed-browser__body">
            <div v-if="loading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />LOADING FILE INDEX</div></div>
            <div v-else-if="items.length" class="data-table-shell">
              <table class="data-table index-table">
                <thead><tr><th>名称</th><th>类型</th><th>状态</th><th>大小</th><th>修改时间</th><th>最近发现</th></tr></thead>
                <tbody>
                  <tr v-for="item in items" :key="`${item.type}:${item.path}:${item.id || ''}`" :class="{ 'is-directory': item.type === 'directory' }" @dblclick="item.type === 'directory' && openPath(item.path)">
                    <td>
                      <button v-if="item.type === 'directory'" class="index-name is-link" type="button" @click="openPath(item.path)"><span><UiIcon name="folder" :size="17" /></span><div><strong>{{ item.name }}</strong><small class="mono">{{ item.path }}</small></div><UiIcon name="chevronRight" :size="15" /></button>
                      <div v-else class="index-name"><span><UiIcon name="file" :size="17" /></span><div><strong>{{ item.name }}</strong><small class="mono">{{ item.path }}</small></div></div>
                    </td>
                    <td>{{ item.type === 'directory' ? `文件夹 · ${number(item.file_count)} 文件` : '文件' }}</td>
                    <td><StatusBadge :status="item.type === 'directory' && item.status !== 'missing' ? 'directory' : item.status" /></td>
                    <td class="mono">{{ item.type === 'directory' || item.size ? formatBytes(item.size) : '—' }}</td>
                    <td>{{ formatDate(item.mod_time) }}</td>
                    <td>{{ formatDate(item.last_seen_at) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="folder" /></span><strong>该层没有已记录项目</strong><p>完成一次扫描后，文件与目录会显示在这里。</p></div></div>
          </div>

          <PaginationBar
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="total"
            :page-sizes="[50, 100, 200, 500]"
            @change="load"
          />
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import UiIcon from './UiIcon.vue';
import StatusBadge from './StatusBadge.vue';
import PaginationBar from './PaginationBar.vue';
import { errorText, toast } from '../composables/useUi';

const props = defineProps({
  visible: { type: Boolean, default: false },
  title: { type: String, default: '文件索引' },
  subtitle: { type: String, default: '' },
  loader: { type: Function, required: true }
});
const emit = defineEmits(['close']);
const loading = ref(false);
const currentPath = ref('');
const parentPath = ref('');
const items = ref([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(100);

function number(value) { return new Intl.NumberFormat('zh-CN').format(Number(value || 0)); }
function formatBytes(bytes) { const value = Number(bytes || 0); if (!value) return '0 B'; const units = ['B','KB','MB','GB','TB','PB']; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index > 1 ? 2 : 0)} ${units[index]}`; }
function formatDate(value) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? String(value) : new Intl.DateTimeFormat('zh-CN', { year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', hour12:false }).format(date); }

async function load() {
  if (!props.visible) return;
  loading.value = true;
  try {
    const result = await props.loader(currentPath.value, page.value, pageSize.value);
    currentPath.value = result.current_path || '';
    parentPath.value = result.parent_path || '';
    items.value = result.items || [];
    total.value = Number(result.total || 0);
  } catch (error) {
    items.value = [];
    total.value = 0;
    toast(errorText(error, '文件索引加载失败'), 'error');
  } finally { loading.value = false; }
}

function openPath(path) {
  currentPath.value = path || '';
  page.value = 1;
  load();
}

function handleKeydown(event) { if (event.key === 'Escape' && props.visible) emit('close'); }
watch(() => props.visible, (visible) => { if (visible) { currentPath.value = ''; parentPath.value = ''; page.value = 1; load(); } });
onMounted(() => window.addEventListener('keydown', handleKeydown));
onBeforeUnmount(() => window.removeEventListener('keydown', handleKeydown));
</script>

<style scoped>
.indexed-browser-layer { z-index: 180; }
.indexed-browser .sheet-header p { max-width: 650px; margin: 6px 0 0; color: var(--text-muted); font-size: 10px; }
.index-toolbar { display: grid; grid-template-columns: 34px minmax(0,1fr) auto; align-items: center; gap: 10px; padding: 13px 18px; border-bottom: 1px solid var(--line); background: var(--surface-soft); }
.index-location { min-width: 0; display: flex; align-items: center; gap: 9px; color: var(--cyan); }
.index-location span { overflow: hidden; color: var(--text); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.indexed-browser__body { min-height: 0; flex: 1; overflow: auto; }
.index-table { min-width: 900px; }
.index-table tbody tr.is-directory { cursor: pointer; }
.index-name { width: 100%; min-width: 280px; display: grid; grid-template-columns: 34px minmax(0,1fr) auto; align-items: center; gap: 9px; padding: 0; color: inherit; border: 0; background: transparent; text-align: left; }
.index-name > span { width: 32px; height: 32px; display: grid; place-items: center; color: var(--text-muted); border: 1px solid var(--line); border-radius: 9px; background: var(--surface-soft); }
.index-name.is-link { cursor: pointer; }
.index-name.is-link > span,.index-name.is-link > .ui-icon { color: var(--cyan); }
.index-name strong,.index-name small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.index-name strong { color: var(--text-strong); font-size: 11px; }
.index-name small { max-width: 390px; margin-top: 3px; color: var(--text-muted); font-size: 8px; }
</style>
