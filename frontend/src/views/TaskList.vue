<template>
  <div class="page-shell tasks-page">
    <header class="page-header">
      <div class="page-heading">
        <span class="eyebrow">TRANSFER QUEUE</span>
        <h1 class="page-title">任务中心</h1>
        <p class="page-description">观察任务流、传输进度和失败原因，并对队列执行精确控制。</p>
      </div>
      <div class="page-actions">
        <span class="live-feed" :class="{ 'is-reconnecting': !streamConnected }"><i /> {{ streamConnected ? 'EVENT STREAM CONNECTED' : 'EVENT STREAM RECONNECTING' }}</span>
        <button class="ui-button" type="button" :disabled="loading" @click="loadData"><UiIcon name="refresh" :size="16" /> 刷新队列</button>
      </div>
    </header>

    <section class="queue-ribbon">
      <div class="queue-ribbon__primary"><span class="queue-radar"><UiIcon name="radar" :size="21" /></span><div><strong>{{ pagination.total }}</strong><small>TOTAL QUEUED TASKS</small></div></div>
      <div><span>当前页运行</span><strong class="cyan-text">{{ pageCounts.running }}</strong></div>
      <div><span>当前页等待</span><strong class="amber-text">{{ pageCounts.pending }}</strong></div>
      <div><span>当前页完成</span><strong class="green-text">{{ pageCounts.success }}</strong></div>
      <div><span>当前页异常</span><strong class="red-text">{{ pageCounts.failed }}</strong></div>
      <div class="queue-ribbon__selection"><span>已选择</span><strong>{{ selectedIds.length }}</strong></div>
    </section>

    <section class="ui-panel queue-panel">
      <div class="queue-toolbar">
        <form class="queue-filters" @submit.prevent="handleSearch">
          <select v-model="filter.status" class="ui-control" aria-label="任务状态">
            <option value="">全部状态</option>
            <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <div class="search-field"><UiIcon name="search" :size="16" /><input v-model="filter.keyword" class="ui-control" placeholder="搜索文件名或路径" /></div>
          <button class="ui-button is-small" type="submit"><UiIcon name="filter" :size="14" /> 应用筛选</button>
          <button class="ui-button is-small is-ghost" type="button" @click="handleReset">重置</button>
        </form>
        <div v-if="isAdmin" class="batch-actions">
          <span v-if="selectedIds.length" class="batch-selection">{{ selectedIds.length }} SELECTED</span>
          <button class="ui-button is-small is-warning" type="button" :disabled="!selectedIds.length || actionLoading" @click="handleBatchPause"><UiIcon name="pause" :size="14" /> 暂停</button>
          <button class="ui-button is-small is-danger" type="button" :disabled="!selectedIds.length || actionLoading" @click="handleBatchCancel"><UiIcon name="close" :size="14" /> 取消</button>
          <button class="ui-button is-small is-violet" type="button" :disabled="!selectedIds.length || actionLoading" @click="handleBatchRetry"><UiIcon name="refresh" :size="14" /> 重试</button>
          <button class="icon-button danger-action" type="button" :disabled="!selectedIds.length || actionLoading" title="批量删除" @click="handleBatchDelete"><UiIcon name="trash" :size="15" /></button>
        </div>
      </div>

      <div v-if="loading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />READING DURABLE QUEUE</div></div>
      <div v-else-if="tableData.length" class="data-table-shell task-table-shell">
        <table class="data-table tasks-table">
          <thead>
            <tr>
              <th v-if="isAdmin"><input class="checkbox" type="checkbox" :checked="allSelected" aria-label="选择当前页所有任务" @change="toggleAll($event.target.checked)" /></th>
              <th>任务 / 文件</th><th>来源目录</th><th>状态与进度</th><th>路由</th><th>实时速率</th><th>大小</th><th>重试</th><th>更新时间</th><th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in tableData" :key="row.id" :class="{ 'is-selected': selectedIds.includes(row.id) }">
              <td v-if="isAdmin"><input v-model="selectedIds" class="checkbox" type="checkbox" :value="row.id" :aria-label="`选择任务 ${row.id}`" /></td>
              <td>
                <div class="task-identity">
                  <span :class="`is-${row.status}`"><UiIcon name="file" :size="17" /></span>
                  <div><strong :title="row.fileName">{{ row.fileName }}</strong><small class="mono">TASK #{{ String(row.id).padStart(6, '0') }}</small></div>
                </div>
              </td>
              <td><div class="source-cell"><strong>{{ row.watchFolderName || '未归属' }}</strong><small class="mono" :title="row.localPath">{{ row.localPath }}</small></div></td>
              <td>
                <div class="task-state">
                  <div><StatusBadge :status="row.status" /><span class="mono">{{ normalizedProgress(row.progress) }}%</span></div>
                  <span class="progress-track"><i :class="`is-${row.status}`" :style="{ width: `${normalizedProgress(row.progress)}%` }" /></span>
                  <p v-if="row.errorMsg" :title="row.errorMsg">{{ row.errorMsg }}</p>
                </div>
              </td>
              <td><div class="route-cell"><span>{{ row.remoteName || '-' }}</span><small class="mono" :title="row.remotePath">{{ row.remotePath }}</small></div></td>
              <td><div class="speed-cell"><strong class="mono">{{ formatSpeed(row.speed) }}</strong><small>{{ row.status === 'running' ? 'LIVE' : '—' }}</small></div></td>
              <td class="mono text-strong">{{ formatBytes(row.fileSize) }}</td>
              <td><span class="retry-count" :class="{ 'has-retry': row.retryCount > 0 }">{{ row.retryCount || 0 }}</span></td>
              <td><div class="date-cell"><strong>{{ formatDateTime(row.updatedAt) }}</strong><small v-if="row.durationSeconds">耗时 {{ formatDuration(row.durationSeconds) }}</small></div></td>
              <td>
                <div class="row-actions task-actions">
                  <button class="icon-button" type="button" title="查看日志" @click="openLogDrawer(row)"><UiIcon name="terminal" :size="15" /></button>
                  <template v-if="isAdmin">
                    <button class="icon-button" type="button" title="取消" :disabled="!['pending','running'].includes(row.status)" @click="handleSingleCancel(row)"><UiIcon name="close" :size="15" /></button>
                    <button class="icon-button" type="button" title="暂停" :disabled="row.status !== 'pending'" @click="handleSinglePause(row)"><UiIcon name="pause" :size="15" /></button>
                    <button class="icon-button" type="button" title="重试" :disabled="!['failed','paused','canceled'].includes(row.status)" @click="handleSingleRetry(row)"><UiIcon name="refresh" :size="15" /></button>
                    <button class="icon-button danger-action" type="button" title="删除" :disabled="row.status === 'running'" @click="handleSingleDelete(row)"><UiIcon name="trash" :size="15" /></button>
                  </template>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="tasks" /></span><strong>队列中没有匹配任务</strong><p>新文件进入监控目录后，任务会自动出现在这里。</p></div></div>

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
      <div v-if="logDrawerVisible" class="sheet-layer" @click.self="logDrawerVisible = false">
        <aside class="side-sheet is-wide log-sheet" role="dialog" aria-modal="true" aria-labelledby="log-sheet-title">
          <header class="sheet-header">
            <div><span class="eyebrow">TASK TELEMETRY</span><h2 id="log-sheet-title">任务日志 #{{ logDrawerTask?.id }}</h2><p>{{ logDrawerTask?.fileName }}</p></div>
            <button class="icon-button" type="button" aria-label="关闭日志" @click="logDrawerVisible = false"><UiIcon name="close" :size="18" /></button>
          </header>
          <div class="terminal-toolbar"><div><span class="terminal-dot is-red" /><span class="terminal-dot is-amber" /><span class="terminal-dot is-green" /></div><span>rclone-sync-hub / task-{{ logDrawerTask?.id }}.log</span><button class="ui-button is-small" type="button" :disabled="logLoading" @click="reloadLogs"><UiIcon name="refresh" :size="13" /> 刷新</button></div>
          <div class="terminal-body">
            <div v-if="logLoading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />STREAMING LOG BUFFER</div></div>
            <template v-else-if="logListDisplay.length">
              <div v-for="(item,index) in logListDisplay" :key="index" class="log-line">
                <span class="log-index">{{ String(index + 1).padStart(3, '0') }}</span>
                <time>{{ logTime(item) || '--:--:--' }}</time>
                <span class="log-level" :class="`is-${String(logLevel(item)).toLowerCase()}`">{{ logLevel(item) || 'INFO' }}</span>
                <span class="log-message">{{ formatLogMessage(item) }}</span>
              </div>
            </template>
            <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="terminal" /></span><strong>暂无任务日志</strong></div></div>
          </div>
          <footer class="terminal-status"><span><i /> BUFFER CONNECTED</span><span>{{ logListDisplay.length }} LINES</span></footer>
        </aside>
      </div>
    </Transition>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import UiIcon from '../components/UiIcon.vue';
import StatusBadge from '../components/StatusBadge.vue';
import PaginationBar from '../components/PaginationBar.vue';
import {
  fetchTasks, batchPauseTasks, batchCancelTasks, batchRetryTasks, batchDeleteTasks,
  deleteTask, pauseTask, cancelTask, fetchTaskLogs
} from '../api/tasks';
import { streamTaskEvents } from '../api/events';
import { currentUser } from '../api/auth';
import { confirmDialog, errorText, toast } from '../composables/useUi';

const isAdmin = currentUser()?.role !== 'viewer';
const tableData = ref([]);
const loading = ref(false);
const actionLoading = ref(false);
const streamConnected = ref(false);
const selectedIds = ref([]);
const logDrawerVisible = ref(false);
const logDrawerTask = ref(null);
const logList = ref([]);
const logLoading = ref(false);
const filter = reactive({ status: '', keyword: '' });
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
const statusOptions = [
  { label: '待处理', value: 'pending' }, { label: '传输中', value: 'running' }, { label: '已完成', value: 'success' },
  { label: '失败', value: 'failed' }, { label: '已暂停', value: 'paused' }, { label: '已取消', value: 'canceled' }
];
const eventAbortController = new AbortController();
let refreshTimer;

const allSelected = computed(() => tableData.value.length > 0 && tableData.value.every((row) => selectedIds.value.includes(row.id)));
const pageCounts = computed(() => ({
  running: tableData.value.filter((row) => row.status === 'running').length,
  pending: tableData.value.filter((row) => row.status === 'pending').length,
  success: tableData.value.filter((row) => row.status === 'success').length,
  failed: tableData.value.filter((row) => row.status === 'failed').length
}));
const logListDisplay = computed(() => Array.isArray(logList.value) ? [...logList.value].reverse() : []);

function pick(item, ...keys) { for (const key of keys) if (item?.[key] !== undefined && item?.[key] !== null) return item[key]; return undefined; }
function normalizeTask(item) {
  return {
    ...item, id: pick(item,'ID','id'), watchFolderName: pick(item,'WatchFolderName','watch_folder_name') || '', fileName: pick(item,'FileName','file_name') || '未命名文件',
    localPath: pick(item,'LocalPath','local_path') || '-', remoteName: pick(item,'RemoteName','remote_name') || '', remotePath: pick(item,'RemotePath','remote_path') || '-',
    status: pick(item,'Status','status') || 'pending', progress: Number(pick(item,'Progress','progress') || 0), speed: Number(pick(item,'Speed','speed') || 0),
    retryCount: Number(pick(item,'RetryCount','retry_count') || 0), errorMsg: pick(item,'ErrorMsg','error_message') || '', fileSize: Number(pick(item,'FileSize','file_size') || 0),
    durationSeconds: Number(pick(item,'DurationSeconds','duration_seconds') || 0), updatedAt: pick(item,'UpdatedAt','updated_at','LastStatusAt','last_status_at')
  };
}
function normalizedProgress(value) { return Math.min(100, Math.max(0, Math.round(Number(value || 0)))); }
function formatBytes(bytes) { const value = Number(bytes || 0); if (!value) return '0 B'; const units=['B','KB','MB','GB','TB']; const index=Math.min(Math.floor(Math.log(value)/Math.log(1024)),units.length-1); return `${(value/1024**index).toFixed(index>1?2:0)} ${units[index]}`; }
function formatSpeed(speed) { return Number(speed || 0) > 0 ? `${(Number(speed) / 1024 / 1024).toFixed(2)} MB/s` : '—'; }
function formatDuration(seconds) { const value=Number(seconds||0); if(value>=3600)return `${Math.floor(value/3600)}h ${Math.floor(value%3600/60)}m`; if(value>=60)return `${Math.floor(value/60)}m ${value%60}s`; return `${value}s`; }
function formatDateTime(value) { if(!value)return '-'; const date=new Date(value); if(Number.isNaN(date.getTime()))return String(value); return new Intl.DateTimeFormat('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(date); }
function toggleAll(checked) { selectedIds.value = checked ? tableData.value.map((row) => row.id) : []; }

async function loadData({ silent = false } = {}) {
  if (!silent) { loading.value = true; selectedIds.value = []; }
  try {
    const result = await fetchTasks({ status: filter.status || undefined, keyword: filter.keyword.trim() || undefined, page: pagination.page, page_size: pagination.pageSize });
    pagination.total = Number.isFinite(Number(result.total)) ? Number(result.total) : 0;
    tableData.value = (result.items || result.data || []).map(normalizeTask);
    selectedIds.value = selectedIds.value.filter((id) => tableData.value.some((row) => row.id === id));
    const maxPage = Math.max(1, Math.ceil(pagination.total / pagination.pageSize));
    if (pagination.page > maxPage) { pagination.page = maxPage; await loadData({ silent }); }
  } catch (error) {
    if (!silent) { tableData.value = []; pagination.total = 0; toast(errorText(error, '任务队列加载失败'), 'error'); }
  } finally { if (!silent) loading.value = false; }
}
function handleSearch() { pagination.page = 1; loadData(); }
function handleReset() { filter.status = ''; filter.keyword = ''; pagination.page = 1; loadData(); }

function batchResultMessage(result, verb) {
  const okCount = result?.ok_ids?.length || 0;
  const errors = Object.values(result?.failed || {});
  if (errors.length) toast(`${verb} ${okCount} 个，${errors.length} 个失败：${errors.join('；')}`, 'warning', 5200);
  else toast(`已${verb} ${okCount} 个任务`, 'success');
}
async function runBatch(request, verb) {
  if (!selectedIds.value.length) { toast('请先选择任务', 'warning'); return; }
  actionLoading.value = true;
  try { const result = await request({ ids: [...selectedIds.value] }); batchResultMessage(result, verb); await loadData(); }
  catch (error) { toast(errorText(error, `${verb}失败`), 'error'); }
  finally { actionLoading.value = false; }
}
function handleBatchPause() { return runBatch(batchPauseTasks, '暂停'); }
function handleBatchCancel() { return runBatch(batchCancelTasks, '取消'); }
function handleBatchRetry() { return runBatch(batchRetryTasks, '提交重试'); }
async function handleBatchDelete() {
  if (!selectedIds.value.length) { toast('请先选择任务', 'warning'); return; }
  const confirmed = await confirmDialog({ title:'批量删除任务', message:`确定删除选中的 ${selectedIds.value.length} 个任务？运行中的任务将被拒绝删除。`, confirmText:'删除任务' });
  if (confirmed) await runBatch(batchDeleteTasks, '删除');
}
async function handleSinglePause(row) { if(row.status!=='pending')return; try{await pauseTask(row.id);toast('任务已暂停','success');await loadData();}catch(error){toast(errorText(error,'暂停失败'),'error');} }
async function handleSingleCancel(row) { if(!['pending','running'].includes(row.status))return; try{await cancelTask(row.id);toast('取消请求已提交','success');await loadData({silent:true});}catch(error){toast(errorText(error,'取消失败'),'error');} }
async function handleSingleRetry(row) { if(!['failed','paused','canceled'].includes(row.status))return; actionLoading.value=true; try{const result=await batchRetryTasks({ids:[row.id]});batchResultMessage(result,'提交重试');await loadData();}catch(error){toast(errorText(error,'重试失败'),'error');}finally{actionLoading.value=false;} }
async function handleSingleDelete(row) {
  if(row.status==='running')return;
  const confirmed=await confirmDialog({title:'删除任务',message:`确定删除任务「${row.fileName}」？该操作不会删除本地文件。`,confirmText:'确认删除'});
  if(!confirmed)return;
  try{await deleteTask(row.id);toast('任务已删除','success');await loadData();}catch(error){toast(errorText(error,'删除失败'),'error');}
}

async function openLogDrawer(row) { logDrawerTask.value=row; logDrawerVisible.value=true; await reloadLogs(); }
async function reloadLogs() { if(!logDrawerTask.value)return; logList.value=[]; logLoading.value=true; try{const result=await fetchTaskLogs(logDrawerTask.value.id);logList.value=Array.isArray(result)?result:[];}catch(error){toast(errorText(error,'获取日志失败'),'error');}finally{logLoading.value=false;} }
function logTime(item) { const raw=pick(item,'created_at','CreatedAt','createdAt'); return raw?formatDateTime(raw):''; }
function logLevel(item) { return pick(item,'level','Level') || ''; }
function formatLogMessage(item) { return pick(item,'message','Message','msg') ?? (typeof item==='string'?item:JSON.stringify(item)); }

function applyTaskEvent(event) {
  const row=tableData.value.find((item)=>item.id===event.task_id);
  if(!row)return;
  if(event.type==='task_progress'){row.progress=event.percent??row.progress;row.speed=event.speed??row.speed;}
  if(event.type==='task_status'){row.status=event.status||row.status;if(event.status==='success')row.progress=100;}
}
async function startEventStream() {
  while(!eventAbortController.signal.aborted){try{await streamTaskEvents(applyTaskEvent,eventAbortController.signal,(connected)=>{streamConnected.value=connected;});}catch{streamConnected.value=false;if(eventAbortController.signal.aborted)return;}await new Promise((resolve)=>window.setTimeout(resolve,3000));}
}
function handleKeydown(event){if(event.key==='Escape')logDrawerVisible.value=false;}
onMounted(()=>{loadData();startEventStream();refreshTimer=window.setInterval(()=>{if(document.visibilityState==='visible')loadData({silent:true});},30000);window.addEventListener('keydown',handleKeydown);});
onBeforeUnmount(()=>{eventAbortController.abort();window.clearInterval(refreshTimer);window.removeEventListener('keydown',handleKeydown);});
</script>

<style scoped>
.tasks-page { display:flex;flex-direction:column;gap:20px;max-width:none; }.tasks-page .page-header{margin-bottom:0}.live-feed{display:flex;align-items:center;gap:8px;padding:9px 11px;color:#527467;border:1px solid rgba(61,225,162,.13);border-radius:999px;background:rgba(61,225,162,.04);font:600 8px var(--font-mono);letter-spacing:.1em}.live-feed i{width:6px;height:6px;border-radius:50%;background:var(--green);box-shadow:0 0 10px var(--green)}
.live-feed.is-reconnecting{color:#766a50;border-color:rgba(255,180,74,.14);background:rgba(255,180,74,.04)}.live-feed.is-reconnecting i{background:var(--amber);box-shadow:0 0 10px var(--amber);animation:pulse 1.2s ease-in-out infinite}
.queue-ribbon{position:relative;z-index:1;min-height:88px;display:grid;grid-template-columns:minmax(230px,1.4fr) repeat(4,minmax(105px,.6fr)) minmax(100px,.5fr);align-items:center;overflow:hidden;border:1px solid var(--line);border-radius:16px;background:linear-gradient(100deg,rgba(19,29,43,.92),rgba(9,15,24,.92));box-shadow:0 18px 45px rgba(0,0,0,.16)}.queue-ribbon>div{min-height:54px;display:flex;flex-direction:column;justify-content:center;padding:0 22px;border-left:1px solid var(--line)}.queue-ribbon__primary{flex-direction:row!important;align-items:center;gap:13px;border-left:0!important}.queue-radar{width:42px;height:42px;display:grid;place-items:center;color:var(--cyan);border:1px solid rgba(88,224,255,.17);border-radius:13px;background:rgba(88,224,255,.07)}.queue-radar .ui-icon{animation:spin 5s linear infinite}.queue-ribbon span,.queue-ribbon strong{display:block}.queue-ribbon>div>span,.queue-ribbon small{color:var(--text-muted);font-size:9px}.queue-ribbon strong{margin-top:3px;color:var(--text-strong);font:600 18px var(--font-mono)}.queue-ribbon__primary strong{margin:0;font-size:24px}.queue-ribbon__primary small{display:block;margin-top:4px;font:500 8px var(--font-mono);letter-spacing:.09em}.queue-ribbon__selection{background:rgba(140,118,255,.035)}.queue-ribbon__selection strong{color:#a997ff}.cyan-text{color:var(--cyan)!important}.amber-text{color:var(--amber)!important}.green-text{color:var(--green)!important}.red-text{color:var(--red)!important}
.queue-panel{position:relative;z-index:1}.queue-toolbar{position:relative;z-index:1;display:flex;align-items:center;justify-content:space-between;gap:15px;padding:16px 18px;border-bottom:1px solid var(--line)}.queue-filters,.batch-actions{display:flex;align-items:center;gap:8px}.queue-filters>select{width:138px}.queue-filters .ui-control{height:36px;font-size:11px}.queue-filters .search-field{min-width:260px}.batch-selection{margin-right:4px;color:#786ba6;font:600 8px var(--font-mono);letter-spacing:.08em}.danger-action:hover:not(:disabled){color:var(--red)!important;border-color:rgba(255,98,125,.25)!important;background:rgba(255,98,125,.07)!important}
.task-table-shell{max-height:calc(100vh - 360px);min-height:250px}.tasks-table{min-width:1520px}.tasks-table tbody tr.is-selected{background:rgba(88,224,255,.035)}.task-identity{display:flex;align-items:center;gap:10px;min-width:200px}.task-identity>span{width:36px;height:36px;display:grid;place-items:center;color:var(--text-muted);border:1px solid var(--line);border-radius:10px;background:rgba(255,255,255,.025)}.task-identity>span.is-running{color:var(--cyan);border-color:rgba(88,224,255,.16);background:rgba(88,224,255,.06)}.task-identity>span.is-success{color:var(--green);border-color:rgba(61,225,162,.16);background:rgba(61,225,162,.06)}.task-identity>span.is-failed{color:var(--red);border-color:rgba(255,98,125,.16);background:rgba(255,98,125,.06)}.task-identity strong,.task-identity small,.source-cell strong,.source-cell small,.route-cell span,.route-cell small,.speed-cell strong,.speed-cell small,.date-cell strong,.date-cell small{display:block}.task-identity strong{max-width:210px;overflow:hidden;color:var(--text-strong);font-size:11px;text-overflow:ellipsis;white-space:nowrap}.task-identity small{margin-top:4px;color:#465265;font-size:8px}.source-cell{min-width:150px}.source-cell strong{max-width:170px;overflow:hidden;color:#aeb9c8;font-size:10px;text-overflow:ellipsis;white-space:nowrap}.source-cell small,.route-cell small{max-width:200px;margin-top:5px;overflow:hidden;color:#576478;font-size:8px;text-overflow:ellipsis;white-space:nowrap}.route-cell{min-width:150px}.route-cell span{color:#a997ff;font:600 10px var(--font-mono)}.task-state{min-width:190px}.task-state>div{display:flex;align-items:center;justify-content:space-between;gap:10px}.task-state>div>span:last-child{color:#8190a3;font-size:9px}.progress-track{height:4px;display:block;margin-top:8px;overflow:hidden;border-radius:4px;background:rgba(255,255,255,.045)}.progress-track i{height:100%;display:block;border-radius:inherit;background:#536075}.progress-track i.is-running{background:linear-gradient(90deg,#31c3e4,var(--cyan));box-shadow:0 0 8px var(--cyan)}.progress-track i.is-success{background:var(--green)}.progress-track i.is-failed{background:var(--red)}.progress-track i.is-pending{background:var(--amber)}.progress-track i.is-paused{background:var(--violet)}.task-state p{max-width:190px;margin:5px 0 0;overflow:hidden;color:#845a63;font-size:8px;text-overflow:ellipsis;white-space:nowrap}.speed-cell strong{color:var(--text-strong);font-size:10px}.speed-cell small{margin-top:4px;color:#43665a;font:600 7px var(--font-mono);letter-spacing:.12em}.retry-count{min-width:27px;height:24px;display:inline-grid;place-items:center;color:#687589;border:1px solid var(--line);border-radius:7px;background:rgba(255,255,255,.025);font:600 9px var(--font-mono)}.retry-count.has-retry{color:var(--amber);border-color:rgba(255,180,74,.17);background:rgba(255,180,74,.06)}.date-cell strong{color:#8d9bad;font-size:9px;font-weight:500}.date-cell small{margin-top:4px;color:#4d596b;font-size:8px}.task-actions{min-width:190px}
.tasks-table th:last-child,.tasks-table td:last-child{position:sticky;right:0;z-index:2;background:#0e1520;box-shadow:-12px 0 24px rgba(4,7,12,.72)}.tasks-table th:last-child{z-index:3;background:#0b111a}.tasks-table tr:hover td:last-child,.tasks-table tr.is-selected td:last-child{background:#111b27}
.log-sheet .sheet-header p{max-width:620px;margin:6px 0 0;overflow:hidden;color:var(--text-muted);font-size:10px;text-overflow:ellipsis;white-space:nowrap}.terminal-toolbar{display:grid;grid-template-columns:90px 1fr auto;align-items:center;gap:12px;padding:11px 17px;border-bottom:1px solid var(--line);background:#090e16}.terminal-toolbar>div{display:flex;gap:6px}.terminal-dot{width:8px;height:8px;border-radius:50%}.terminal-dot.is-red{background:var(--red)}.terminal-dot.is-amber{background:var(--amber)}.terminal-dot.is-green{background:var(--green)}.terminal-toolbar>span{color:#4f5d70;font:500 9px var(--font-mono)}.terminal-body{flex:1;min-height:0;overflow:auto;padding:12px 0;background:#070b11;font-family:var(--font-mono)}.log-line{display:grid;grid-template-columns:42px 132px 54px minmax(0,1fr);gap:10px;padding:6px 17px;border-left:2px solid transparent;font-size:9px;line-height:1.6}.log-line:hover{border-left-color:var(--cyan);background:rgba(88,224,255,.025)}.log-index{color:#303a49;text-align:right}.log-line time{color:#596678}.log-level{color:var(--cyan);font-weight:700}.log-level.is-error,.log-level.is-fatal{color:var(--red)}.log-level.is-warn,.log-level.is-warning{color:var(--amber)}.log-message{color:#9eabba;word-break:break-all}.terminal-status{display:flex;justify-content:space-between;padding:9px 17px;color:#425064;border-top:1px solid var(--line);background:#090e16;font:500 8px var(--font-mono);letter-spacing:.08em}.terminal-status span{display:flex;align-items:center;gap:7px}.terminal-status i{width:5px;height:5px;border-radius:50%;background:var(--green);box-shadow:0 0 7px var(--green)}
@media(max-width:1350px){.queue-toolbar{align-items:flex-start;flex-direction:column}.queue-filters,.batch-actions{width:100%}.batch-actions{justify-content:flex-end}.queue-ribbon{grid-template-columns:minmax(190px,1.2fr) repeat(4,1fr)}.queue-ribbon__selection{display:none!important}}
@media(max-width:850px){.queue-ribbon{grid-template-columns:1fr 1fr 1fr}.queue-ribbon>div:nth-child(4),.queue-ribbon>div:nth-child(5){display:none}.queue-filters{align-items:stretch;flex-wrap:wrap}.queue-filters>select,.queue-filters .search-field{width:100%;min-width:100%}.batch-actions{justify-content:flex-start;flex-wrap:wrap}.task-table-shell{max-height:none}.log-line{grid-template-columns:32px 100px 44px minmax(0,1fr);gap:6px}.terminal-toolbar{grid-template-columns:65px 1fr auto}}
@media(max-width:540px){.queue-ribbon{grid-template-columns:1.4fr 1fr}.queue-ribbon>div:nth-child(3){display:none}.log-line{grid-template-columns:28px 44px minmax(0,1fr)}.log-line time{display:none}.terminal-toolbar>span{display:none}.terminal-toolbar{grid-template-columns:1fr auto}}

/* Theme-aware contrast normalization. */
.live-feed { color: var(--green); }
.live-feed.is-reconnecting { color: var(--amber); }
.queue-ribbon { background: var(--summary-background); box-shadow: var(--shadow-card); }
.queue-ribbon__selection { background: rgba(var(--violet-rgb),.06); }
.queue-ribbon__selection strong { color: var(--violet-text); }
.batch-selection { color: var(--violet-text); }
.tasks-table tbody tr.is-selected { background: var(--surface-selected); }
.task-identity small,
.source-cell small,
.route-cell small,
.task-state > div > span:last-child,
.date-cell small { color: var(--text-muted); }
.source-cell strong,
.date-cell strong { color: var(--text); }
.route-cell span { color: var(--violet-text); }
.task-state p { color: var(--red); }
.speed-cell small { color: var(--green); }
.retry-count { color: var(--text-muted); background: var(--surface-soft); }
.tasks-table th:last-child { background: var(--sticky-header-background); }
.tasks-table td:last-child { background: var(--sticky-cell-background); }
.tasks-table tr:hover td:last-child,
.tasks-table tr.is-selected td:last-child { background: var(--sticky-hover-background); }
.terminal-toolbar,
.terminal-status { background: var(--terminal-toolbar-background); }
.terminal-body { background: var(--terminal-background); }
.terminal-toolbar > span,
.terminal-status,
.log-index,
.log-line time { color: var(--text-muted); }
.log-message { color: var(--terminal-text); }
</style>
