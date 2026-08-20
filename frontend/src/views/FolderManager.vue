<template>
  <div class="page-shell folders-page">
    <header class="page-header">
      <div class="page-heading">
        <span class="eyebrow">WATCH TOPOLOGY</span>
        <h1 class="page-title">监控目录</h1>
        <p class="page-description">配置本地扫描边界与远端目标，控制每一条文件传输通道。</p>
      </div>
      <div v-if="isAdmin" class="page-actions">
        <button class="ui-button" type="button" :disabled="scanTriggering" @click="handleTriggerScan">
          <span v-if="scanTriggering" class="mini-spinner" /><UiIcon v-else name="radar" :size="16" />
          {{ scanTriggering ? '正在下发' : '立即扫描' }}
        </button>
        <button class="ui-button is-primary" type="button" @click="openCreate"><UiIcon name="plus" :size="17" /> 新建目录</button>
      </div>
    </header>

    <section class="folder-summary" aria-label="目录状态摘要">
      <div><span class="summary-icon is-cyan"><UiIcon name="folder" :size="18" /></span><p><small>目录总数</small><strong>{{ pagination.total }}</strong></p></div>
      <div><span class="summary-icon is-green"><UiIcon name="activity" :size="18" /></span><p><small>当前页监控中</small><strong>{{ watchingCount }}</strong></p></div>
      <div><span class="summary-icon is-amber"><UiIcon name="radar" :size="18" /></span><p><small>当前页扫描中</small><strong>{{ detectingCount }}</strong></p></div>
      <div><span class="summary-icon is-red"><UiIcon name="alert" :size="18" /></span><p><small>当前页异常</small><strong>{{ errorCount }}</strong></p></div>
      <span class="summary-sequence">NODE TOPOLOGY / {{ String(pagination.page).padStart(2, '0') }}</span>
    </section>

    <section class="ui-panel folder-list-panel">
      <div class="folder-toolbar">
        <div class="folder-toolbar__title"><span><UiIcon name="server" :size="18" /></span><div><h2>目录节点列表</h2><p>WATCH FOLDERS / {{ pagination.total }} NODES</p></div></div>
        <form class="folder-filters" @submit.prevent="handleSearch">
          <select v-model="filter.status" class="ui-control compact-select" aria-label="状态筛选">
            <option value="">全部状态</option>
            <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <div class="search-field"><UiIcon name="search" :size="16" /><input v-model="filter.keyword" class="ui-control" placeholder="搜索名称或路径" /></div>
          <button class="ui-button is-small" type="submit">筛选</button>
          <button class="ui-button is-small is-ghost" type="button" @click="handleReset">重置</button>
        </form>
      </div>

      <div v-if="loading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />LOADING WATCH NODES</div></div>
      <div v-else-if="tableData.length" class="data-table-shell">
        <table class="data-table folders-table">
          <thead><tr><th>目录节点</th><th>本地路径</th><th>远端目标</th><th>状态</th><th>扫描周期</th><th>最近扫描</th><th>文件 / 容量</th><th v-if="isAdmin">操作</th></tr></thead>
          <tbody>
            <tr v-for="row in tableData" :key="row.id">
              <td>
                <div class="folder-node">
                  <span class="folder-node__icon"><UiIcon name="folder" :size="18" /></span>
                  <div><strong>{{ row.name }}</strong><small>NODE #{{ String(row.id).padStart(4, '0') }}</small></div>
                </div>
              </td>
              <td><span class="path-cell mono" :title="row.localPath">{{ row.localPath }}</span></td>
              <td><div class="remote-cell"><span>{{ row.remoteName }}</span><small class="mono" :title="row.remotePath">{{ row.remotePath }}</small></div></td>
              <td><StatusBadge :status="row.status" /><p v-if="row.lastError" class="row-error" :title="row.lastError">{{ row.lastError }}</p></td>
              <td><div class="cycle-cell"><strong class="mono">{{ formatDuration(row.scanIntervalSeconds) }}</strong><small>DEPTH {{ row.maxDepth || '∞' }}</small></div></td>
              <td><div class="date-cell"><strong>{{ formatDateTime(row.lastScanAt) }}</strong><small v-if="row.lastScanDurationMs">耗时 {{ formatMilliseconds(row.lastScanDurationMs) }}</small></div></td>
              <td><div class="capacity-cell"><strong class="mono">{{ number(row.totalFileCount) }} files</strong><small>{{ formatBytes(row.totalFileSize) }}</small></div></td>
              <td v-if="isAdmin">
                <div class="row-actions">
                  <button class="icon-button" type="button" title="编辑" @click="openEdit(row)"><UiIcon name="edit" :size="15" /></button>
                  <button class="icon-button" type="button" :title="row.status === 'paused' ? '启动' : '暂停'" @click="handleToggleStatus(row)"><UiIcon :name="row.status === 'paused' ? 'play' : 'pause'" :size="15" /></button>
                  <button class="icon-button danger-action" type="button" title="删除" @click="handleDelete(row)"><UiIcon name="trash" :size="15" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="folder" /></span><strong>没有匹配的监控目录</strong><p>调整筛选条件，或创建第一条传输通道。</p></div></div>

      <PaginationBar
        :page="pagination.page"
        :page-size="pagination.pageSize"
        :total="pagination.total"
        @update:page="pagination.page = $event"
        @update:page-size="pagination.pageSize = $event"
        @change="loadData"
      />
    </section>

    <Transition name="sheet-slide">
      <div v-if="drawerVisible" class="sheet-layer" @click.self="closeDrawer">
        <aside class="side-sheet" role="dialog" aria-modal="true" aria-labelledby="folder-sheet-title">
          <header class="sheet-header">
            <div><span class="eyebrow">{{ drawerMode === 'create' ? 'NEW WATCH NODE' : 'EDIT WATCH NODE' }}</span><h2 id="folder-sheet-title">{{ drawerMode === 'create' ? '创建监控目录' : '编辑监控目录' }}</h2></div>
            <button class="icon-button" type="button" aria-label="关闭" @click="closeDrawer"><UiIcon name="close" :size="18" /></button>
          </header>
          <form class="sheet-body folder-form" @submit.prevent="handleSubmit">
            <section class="form-section">
              <div class="form-section__heading"><span>01</span><div><strong>通道身份</strong><small>CHANNEL IDENTITY</small></div></div>
              <div class="field"><label for="folder-name">显示名称 *</label><input id="folder-name" v-model="form.name" class="ui-control" placeholder="例如：视频素材库" /></div>
              <div class="field"><label for="local-path">本地扫描路径 *</label><div class="control-group"><input id="local-path" v-model="form.local_path" class="ui-control mono" placeholder="D:/data/videos" /><button class="ui-button" type="button" @click="openPathSelector"><UiIcon name="folder" :size="15" /> 浏览</button></div><p class="field-help">目录不能与现有监控路径重叠。</p></div>
            </section>

            <section class="form-section">
              <div class="form-section__heading"><span>02</span><div><strong>远端路由</strong><small>REMOTE ROUTING</small></div></div>
              <div class="form-grid">
                <div class="field"><label for="remote-name">Rclone Remote *</label><select id="remote-name" v-model="form.remote_name" class="ui-control"><option value="" disabled>{{ remoteLoading ? '加载中...' : '选择 Remote' }}</option><option v-for="option in remoteOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></div>
                <div class="field"><label for="sync-type">同步方向</label><select id="sync-type" v-model="form.sync_type" class="ui-control"><option value="local_to_remote">本地 → 远端</option></select></div>
              </div>
              <div class="field"><label for="remote-path">远端存储路径 *</label><input id="remote-path" v-model="form.remote_path" class="ui-control mono" placeholder="backup/videos" /></div>
            </section>

            <section class="form-section">
              <div class="form-section__heading"><span>03</span><div><strong>扫描策略</strong><small>SCAN POLICY</small></div></div>
              <div class="form-grid">
                <div class="field"><label for="max-depth">最大扫描深度</label><input id="max-depth" v-model.number="form.max_depth" class="ui-control mono" type="number" min="0" /><p class="field-help">0 表示不限制</p></div>
                <div class="field"><label for="scan-interval">扫描周期（秒）</label><input id="scan-interval" v-model.number="form.scan_interval_seconds" class="ui-control mono" type="number" min="60" /><p class="field-help">最短 60 秒</p></div>
              </div>
              <div class="field"><label for="filter-keywords">排除关键字</label><textarea id="filter-keywords" v-model="form.filter_keywords" class="ui-control mono" rows="5" placeholder="每行一个关键字&#10;例如：.tmp&#10;cache/"></textarea><p class="field-help">文件名或路径包含任意关键字时将被排除。</p></div>
            </section>
          </form>
          <footer class="sheet-footer">
            <button class="ui-button is-ghost" type="button" @click="closeDrawer">取消</button>
            <button class="ui-button is-primary" type="button" :disabled="saving" @click="handleSubmit"><span v-if="saving" class="mini-spinner is-dark" /><UiIcon v-else name="check" :size="16" />{{ saving ? '正在保存' : '保存通道' }}</button>
          </footer>
        </aside>
      </div>
    </Transition>

    <Teleport to="body">
      <Transition name="dialog-fade">
        <div v-if="pathSelectorVisible" class="path-modal-layer" @click.self="pathSelectorVisible = false">
          <section class="path-modal" role="dialog" aria-modal="true" aria-labelledby="path-modal-title">
            <header><div><span class="eyebrow">LOCAL FILESYSTEM</span><h2 id="path-modal-title">选择本地目录</h2></div><button class="icon-button" type="button" aria-label="关闭" @click="pathSelectorVisible = false"><UiIcon name="close" :size="18" /></button></header>
            <div class="path-modal__current"><UiIcon name="folder" :size="16" /><input v-model="browsePath" class="ui-control mono" aria-label="当前路径" @keyup.enter="loadBrowsePath(browsePath)" /><button class="ui-button is-small" type="button" @click="loadBrowsePath(browsePath)">打开</button></div>
            <div class="path-browser">
              <div v-if="pathTreeLoading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />READING FILESYSTEM</div></div>
              <template v-else-if="browseItems.length">
                <div v-for="item in browseItems" :key="item.path" class="path-entry" role="group" @dblclick="item.has_sub_dirs && loadBrowsePath(item.path)">
                  <span><UiIcon name="folder" :size="18" /></span><div><strong>{{ item.name }}</strong><small class="mono">{{ item.path }}</small></div>
                  <button v-if="item.has_sub_dirs" class="icon-button" type="button" title="进入目录" @click.stop="loadBrowsePath(item.path)"><UiIcon name="chevronRight" :size="15" /></button>
                  <button class="ui-button is-small" type="button" @click.stop="selectPath(item.path)">选择</button>
                </div>
              </template>
              <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="folder" /></span><strong>该目录没有子目录</strong><p>可以直接选择当前路径。</p></div></div>
            </div>
            <footer><span class="mono">{{ browsePath }}</span><div><button class="ui-button is-ghost" type="button" @click="pathSelectorVisible = false">取消</button><button class="ui-button is-primary" type="button" @click="selectPath(browsePath)">选择当前目录</button></div></footer>
          </section>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import UiIcon from '../components/UiIcon.vue';
import StatusBadge from '../components/StatusBadge.vue';
import PaginationBar from '../components/PaginationBar.vue';
import { fetchWatchFolders, createWatchFolder, updateWatchFolder, deleteWatchFolder } from '../api/watchFolders';
import { fetchSubdirs } from '../api/fs';
import { fetchRcloneConfigs } from '../api/rclone';
import { currentUser } from '../api/auth';
import { triggerScan } from '../api/scanner';
import { confirmDialog, errorText, toast } from '../composables/useUi';

const isAdmin = currentUser()?.role !== 'viewer';
const tableData = ref([]);
const loading = ref(false);
const saving = ref(false);
const scanTriggering = ref(false);
const drawerVisible = ref(false);
const drawerMode = ref('create');
const editingId = ref(null);
const remoteOptions = ref([]);
const remoteLoading = ref(false);
const pathSelectorVisible = ref(false);
const pathTreeLoading = ref(false);
const browsePath = ref('/volumes');
const browseItems = ref([]);

const statusOptions = [
  { label: '扫描中', value: 'detecting' }, { label: '监控中', value: 'watching' },
  { label: '已停止', value: 'stopped' }, { label: '已暂停', value: 'paused' }, { label: '异常', value: 'error' }
];
const filter = reactive({ status: '', keyword: '' });
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
const form = reactive(defaultForm());

const watchingCount = computed(() => tableData.value.filter((row) => row.status === 'watching').length);
const detectingCount = computed(() => tableData.value.filter((row) => row.status === 'detecting').length);
const errorCount = computed(() => tableData.value.filter((row) => row.status === 'error').length);

function defaultForm() {
  return { name: '', local_path: '', remote_name: '', remote_path: '', max_depth: 5, filter_keywords: '', scan_interval_seconds: 300, sync_type: 'local_to_remote' };
}
function pick(item, ...keys) { for (const key of keys) if (item?.[key] !== undefined && item?.[key] !== null) return item[key]; return undefined; }
function normalizeRow(item) {
  return {
    ...item,
    id: pick(item, 'ID', 'id'), name: pick(item, 'Name', 'name') || '未命名目录',
    localPath: pick(item, 'LocalPath', 'local_path') || '-', remoteName: pick(item, 'RemoteName', 'remote_name') || '-', remotePath: pick(item, 'RemotePath', 'remote_path') || '-',
    status: pick(item, 'Status', 'status') || 'stopped', maxDepth: Number(pick(item, 'MaxDepth', 'max_depth') || 0), filterKeywords: pick(item, 'FilterKeywords', 'filter_keywords') || '',
    scanIntervalSeconds: Number(pick(item, 'ScanIntervalSeconds', 'scan_interval_seconds') || 300), syncType: pick(item, 'SyncType', 'sync_type') || 'local_to_remote',
    lastScanAt: pick(item, 'LastScanFinishedAt', 'last_scan_finished_at', 'LastScanAt', 'last_scan_at'), lastScanDurationMs: Number(pick(item, 'LastScanDurationMs', 'last_scan_duration_ms') || 0),
    totalFileCount: Number(pick(item, 'TotalFileCount', 'total_file_count') || 0), totalFileSize: Number(pick(item, 'TotalFileSize', 'total_file_size') || 0), lastError: pick(item, 'LastError', 'last_error') || ''
  };
}
function number(value) { return new Intl.NumberFormat('zh-CN').format(Number(value || 0)); }
function formatBytes(bytes) { const value = Number(bytes || 0); if (!value) return '0 B'; const units = ['B','KB','MB','GB','TB']; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index > 1 ? 2 : 0)} ${units[index]}`; }
function formatDuration(seconds) { const value = Number(seconds || 0); return value >= 3600 ? `${Math.round(value / 3600)}h` : value >= 60 ? `${Math.round(value / 60)}m` : `${value}s`; }
function formatMilliseconds(ms) { return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`; }
function formatDateTime(value) { if (!value) return '从未扫描'; const date = new Date(value); if (Number.isNaN(date.getTime())) return String(value); return new Intl.DateTimeFormat('zh-CN', { month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', hour12:false }).format(date); }

async function loadData() {
  loading.value = true;
  try {
    const result = await fetchWatchFolders({ status: filter.status || undefined, keyword: filter.keyword.trim() || undefined, page: pagination.page, page_size: pagination.pageSize });
    pagination.total = Number.isFinite(Number(result.total)) ? Number(result.total) : 0;
    tableData.value = (result.items || []).map(normalizeRow);
    const maxPage = Math.max(1, Math.ceil(pagination.total / pagination.pageSize));
    if (pagination.page > maxPage) { pagination.page = maxPage; await loadData(); }
  } catch (error) {
    tableData.value = [];
    pagination.total = 0;
    toast(errorText(error, '加载监控目录失败'), 'error');
  } finally { loading.value = false; }
}
function handleSearch() { pagination.page = 1; loadData(); }
function handleReset() { filter.status = ''; filter.keyword = ''; pagination.page = 1; loadData(); }

async function handleTriggerScan() {
  scanTriggering.value = true;
  try { const result = await triggerScan(); toast(`已下发 ${Number(result?.enqueued) || 0} 个目录扫描任务`, 'success'); window.setTimeout(loadData, 600); }
  catch (error) { toast(errorText(error, '提交扫描任务失败'), 'error'); }
  finally { scanTriggering.value = false; }
}
async function handleToggleStatus(row) {
  const status = row.status === 'paused' ? 'watching' : 'paused';
  try { await updateWatchFolder(row.id, { status }); toast(status === 'paused' ? '目录监控已暂停' : '目录监控已恢复', 'success'); await loadData(); }
  catch (error) { toast(errorText(error, '状态更新失败'), 'error'); }
}
async function handleDelete(row) {
  const confirmed = await confirmDialog({ title: '删除监控目录', message: `确定删除「${row.name}」？未运行的关联任务会被终止，历史快照将与目录解绑。`, confirmText: '确认删除' });
  if (!confirmed) return;
  try { await deleteWatchFolder(row.id); toast('监控目录已删除', 'success'); await loadData(); }
  catch (error) { toast(errorText(error, '删除失败'), 'error'); }
}
function openCreate() { drawerMode.value = 'create'; editingId.value = null; Object.assign(form, defaultForm()); drawerVisible.value = true; }
function openEdit(row) { drawerMode.value = 'edit'; editingId.value = row.id; Object.assign(form, { name: row.name, local_path: row.localPath, remote_name: row.remoteName, remote_path: row.remotePath, max_depth: row.maxDepth, filter_keywords: row.filterKeywords, scan_interval_seconds: row.scanIntervalSeconds, sync_type: row.syncType }); drawerVisible.value = true; }
function closeDrawer() { if (!saving.value) drawerVisible.value = false; }
function normalizeKeywords(value) { return String(value || '').split(/\r?\n/).map((line) => line.trim()).filter(Boolean).join('\n'); }
async function handleSubmit() {
  if (!form.name.trim() || !form.local_path.trim() || !form.remote_name || !form.remote_path.trim()) { toast('请填写名称、本地路径、Remote 和远端路径', 'warning'); return; }
  if (Number(form.scan_interval_seconds) < 60) { toast('扫描周期不能小于 60 秒', 'warning'); return; }
  const payload = { name: form.name.trim(), local_path: form.local_path.trim(), remote_name: form.remote_name, remote_path: form.remote_path.trim(), max_depth: Number(form.max_depth) || 0, filter_keywords: normalizeKeywords(form.filter_keywords), scan_interval_seconds: Number(form.scan_interval_seconds) || 300, sync_type: form.sync_type };
  saving.value = true;
  try { if (drawerMode.value === 'create') await createWatchFolder(payload); else await updateWatchFolder(editingId.value, payload); toast('监控目录已保存', 'success'); drawerVisible.value = false; await loadData(); }
  catch (error) { toast(errorText(error, '保存失败'), 'error'); }
  finally { saving.value = false; }
}
async function loadRemoteOptions() {
  remoteLoading.value = true;
  try { const result = await fetchRcloneConfigs(); remoteOptions.value = (result.items || []).map((item) => ({ label: item.name, value: item.name })); }
  catch (error) { remoteOptions.value = []; toast(errorText(error, 'Remote 列表加载失败'), 'warning'); }
  finally { remoteLoading.value = false; }
}
async function openPathSelector() { browsePath.value = form.local_path || '/volumes'; pathSelectorVisible.value = true; await loadBrowsePath(browsePath.value); }
async function loadBrowsePath(path) {
  pathTreeLoading.value = true;
  try { const result = await fetchSubdirs(path); browsePath.value = path; browseItems.value = result.items || []; }
  catch (error) { browseItems.value = []; toast(errorText(error, '目录读取失败'), 'error'); }
  finally { pathTreeLoading.value = false; }
}
function selectPath(path) { form.local_path = path; pathSelectorVisible.value = false; }
function handleKeydown(event) { if (event.key !== 'Escape') return; if (pathSelectorVisible.value) pathSelectorVisible.value = false; else closeDrawer(); }

onMounted(() => { loadData(); loadRemoteOptions(); window.addEventListener('keydown', handleKeydown); });
onBeforeUnmount(() => window.removeEventListener('keydown', handleKeydown));
</script>

<style scoped>
.folders-page { display: flex; flex-direction: column; gap: 20px; }.folders-page .page-header { margin-bottom: 0; }
.mini-spinner { width: 15px; height: 15px; border: 2px solid rgba(88,224,255,.17); border-top-color: var(--cyan); border-radius: 50%; animation: spin .7s linear infinite; }.mini-spinner.is-dark { border-color: rgba(4,16,20,.2); border-top-color: #041014; }
.folder-summary { position: relative; z-index: 1; min-height: 92px; display: grid; grid-template-columns: repeat(4,minmax(140px,1fr)) auto; align-items: center; gap: 0; overflow: hidden; border: 1px solid var(--line); border-radius: 16px; background: linear-gradient(100deg, rgba(18,27,40,.9), rgba(10,16,25,.9)); box-shadow: 0 18px 45px rgba(0,0,0,.16); }.folder-summary > div { display: flex; align-items: center; gap: 12px; min-height: 54px; padding: 0 20px; border-right: 1px solid var(--line); }.summary-icon { width: 38px; height: 38px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.15); border-radius: 11px; background: rgba(88,224,255,.06); }.summary-icon.is-green { color: var(--green); border-color: rgba(61,225,162,.15); background: rgba(61,225,162,.06); }.summary-icon.is-amber { color: var(--amber); border-color: rgba(255,180,74,.15); background: rgba(255,180,74,.06); }.summary-icon.is-red { color: var(--red); border-color: rgba(255,98,125,.15); background: rgba(255,98,125,.06); }.folder-summary p { margin: 0; }.folder-summary small,.folder-summary strong { display: block; }.folder-summary small { color: var(--text-muted); font-size: 9px; }.folder-summary strong { margin-top: 4px; color: var(--text-strong); font: 600 20px var(--font-mono); }.summary-sequence { padding: 0 20px; color: #455266; font: 500 8px var(--font-mono); letter-spacing: .1em; }
.folder-list-panel { position: relative; z-index: 1; }.folder-toolbar { position: relative; z-index: 1; display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 18px 20px; border-bottom: 1px solid var(--line); }.folder-toolbar__title { display: flex; align-items: center; gap: 11px; }.folder-toolbar__title > span { width: 36px; height: 36px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.15); border-radius: 10px; background: rgba(88,224,255,.06); }.folder-toolbar h2,.folder-toolbar p { margin: 0; }.folder-toolbar h2 { color: var(--text-strong); font-size: 13px; }.folder-toolbar p { margin-top: 3px; color: #4c596c; font: 500 8px var(--font-mono); letter-spacing: .08em; }.folder-filters { display: flex; align-items: center; gap: 8px; }.folder-filters .ui-control { height: 36px; font-size: 11px; }.compact-select { width: 135px; }.folder-filters .search-field { min-width: 230px; }
.folders-table { min-width: 1220px; }.folder-node { display: flex; align-items: center; gap: 10px; min-width: 150px; }.folder-node__icon { width: 36px; height: 36px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.14); border-radius: 11px; background: rgba(88,224,255,.06); }.folder-node strong,.folder-node small { display: block; }.folder-node strong { max-width: 150px; overflow: hidden; color: var(--text-strong); text-overflow: ellipsis; white-space: nowrap; }.folder-node small { margin-top: 4px; color: #455265; font: 500 8px var(--font-mono); }.path-cell { max-width: 230px; display: block; overflow: hidden; color: #8391a4; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.remote-cell span,.remote-cell small,.cycle-cell strong,.cycle-cell small,.date-cell strong,.date-cell small,.capacity-cell strong,.capacity-cell small { display: block; }.remote-cell span { color: var(--violet); font: 600 10px var(--font-mono); }.remote-cell small { max-width: 190px; margin-top: 4px; overflow: hidden; color: var(--text-muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }.cycle-cell small,.date-cell small,.capacity-cell small { margin-top: 4px; color: #4e5a6d; font-size: 8px; }.date-cell strong { color: #9ba8b8; font-size: 10px; font-weight: 500; }.row-error { max-width: 130px; margin: 5px 0 0; overflow: hidden; color: #8b5d66; font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }.danger-action:hover { color: var(--red)!important; border-color: rgba(255,98,125,.25)!important; background: rgba(255,98,125,.07)!important; }
.folders-table th:last-child,.folders-table td:last-child{position:sticky;right:0;z-index:2;background:#0e1520;box-shadow:-12px 0 24px rgba(4,7,12,.72)}.folders-table th:last-child{z-index:3;background:#0b111a}.folders-table tr:hover td:last-child{background:#111b27}
.folder-form { display: flex; flex-direction: column; gap: 20px; }.form-section { padding: 20px; border: 1px solid var(--line); border-radius: 14px; background: rgba(255,255,255,.018); }.form-section__heading { display: flex; align-items: center; gap: 10px; margin-bottom: 19px; padding-bottom: 14px; border-bottom: 1px solid var(--line); }.form-section__heading > span { width: 30px; height: 30px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.17); border-radius: 9px; background: rgba(88,224,255,.06); font: 600 9px var(--font-mono); }.form-section__heading strong,.form-section__heading small { display: block; }.form-section__heading strong { color: var(--text-strong); font-size: 12px; }.form-section__heading small { margin-top: 3px; color: #4c586a; font: 500 8px var(--font-mono); letter-spacing: .08em; }.form-section .field + .field { margin-top: 16px; }.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }.form-grid + .field { margin-top: 16px; }
.path-modal-layer { position: fixed; inset: 0; z-index: 220; display: grid; place-items: center; padding: 20px; background: rgba(1,4,8,.78); backdrop-filter: blur(8px); }.path-modal { width: min(760px,100%); max-height: min(760px,90vh); display: flex; flex-direction: column; overflow: hidden; border: 1px solid var(--line-strong); border-radius: 19px; background: #0e1520; box-shadow: 0 35px 100px rgba(0,0,0,.58); }.path-modal > header { display: flex; align-items: center; justify-content: space-between; padding: 21px 23px; border-bottom: 1px solid var(--line); }.path-modal h2 { margin: 6px 0 0; color: var(--text-strong); font-size: 18px; }.path-modal__current { display: grid; grid-template-columns: 20px 1fr auto; align-items: center; gap: 10px; padding: 14px 20px; border-bottom: 1px solid var(--line); background: rgba(4,8,14,.4); }.path-modal__current > .ui-icon { color: var(--cyan); }.path-modal__current .ui-control { height: 37px; }.path-browser { min-height: 310px; flex: 1; overflow: auto; padding: 10px; }.path-entry { width: 100%; display: grid; grid-template-columns: 38px minmax(0,1fr) 34px auto; align-items: center; gap: 10px; padding: 10px; color: inherit; border: 1px solid transparent; border-radius: 11px; background: transparent; text-align: left; cursor: pointer; }.path-entry:hover { border-color: var(--line); background: rgba(88,224,255,.035); }.path-entry > span { width: 36px; height: 36px; display: grid; place-items: center; color: var(--cyan); border-radius: 10px; background: rgba(88,224,255,.06); }.path-entry strong,.path-entry small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.path-entry strong { color: var(--text); font-size: 11px; }.path-entry small { margin-top: 3px; color: #536075; font-size: 8px; }.path-modal > footer { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 15px 20px; border-top: 1px solid var(--line); }.path-modal > footer > span { max-width: 45%; overflow: hidden; color: #657287; font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }.path-modal > footer > div { display: flex; gap: 8px; }
@media (max-width: 1120px) { .folder-summary { grid-template-columns: repeat(4,1fr); }.summary-sequence { display: none; }.folder-toolbar { align-items: flex-start; flex-direction: column; }.folder-filters { width: 100%; }.folder-filters .search-field { flex: 1; } }
@media (max-width: 700px) { .folder-summary { grid-template-columns: repeat(2,1fr); }.folder-summary > div:nth-child(2) { border-right: 0; }.folder-summary > div:nth-child(-n+2) { border-bottom: 1px solid var(--line); }.folder-filters { align-items: stretch; flex-wrap: wrap; }.folder-filters .compact-select,.folder-filters .search-field { width: 100%; min-width: 100%; }.form-grid { grid-template-columns: 1fr; }.path-entry { grid-template-columns: 36px minmax(0,1fr) auto; }.path-entry > .icon-button { display: none; }.path-modal > footer { align-items: stretch; flex-direction: column; }.path-modal > footer > span { max-width: 100%; }.path-modal > footer > div { justify-content: flex-end; } }
</style>
