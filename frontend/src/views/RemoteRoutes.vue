<template>
  <div class="page-shell routes-page">
    <header class="page-header">
      <div class="page-heading"><span class="eyebrow">REMOTE INDEX TOPOLOGY</span><h1 class="page-title">远端路由</h1><p class="page-description">集中维护 rclone 目标、后台扫描周期与远端文件索引，监控目录只需选择对应路由。</p></div>
      <div v-if="isAdmin" class="page-actions">
        <button class="ui-button" type="button" :disabled="scanAllLoading" @click="handleScanAll"><span v-if="scanAllLoading" class="mini-spinner" /><UiIcon v-else name="radar" :size="16" />扫描全部远端</button>
        <button class="ui-button is-primary" type="button" @click="openCreate"><UiIcon name="plus" :size="16" />新建路由</button>
      </div>
    </header>

    <section class="route-summary">
      <div><small>路由总数</small><strong>{{ pagination.total }}</strong></div>
      <div><small>当前页就绪</small><strong>{{ readyCount }}</strong></div>
      <div><small>当前页扫描中</small><strong>{{ scanningCount }}</strong></div>
      <div><small>当前页远端文件</small><strong>{{ number(indexedFiles) }}</strong></div>
    </section>

    <section class="ui-panel route-panel">
      <div class="route-toolbar">
        <div><span><UiIcon name="database" :size="18" /></span><section><h2>远端路由列表</h2><p>REMOTE ROUTES / PERSISTED FILE INDEX</p></section></div>
        <form @submit.prevent="handleSearch"><select v-model="filter.status" class="ui-control"><option value="">全部状态</option><option value="ready">索引就绪</option><option value="scanning">扫描中</option><option value="pending">待扫描</option><option value="error">异常</option><option value="disabled">已禁用</option></select><div class="search-field"><UiIcon name="search" :size="16" /><input v-model="filter.keyword" class="ui-control" placeholder="搜索名称或远端路径" /></div><button class="ui-button is-small" type="submit">筛选</button></form>
      </div>
      <div v-if="loading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />LOADING REMOTE ROUTES</div></div>
      <div v-else-if="rows.length" class="data-table-shell">
        <table class="data-table routes-table">
          <thead><tr><th>路由</th><th>Rclone 目标</th><th>索引状态</th><th>扫描周期</th><th>最近扫描</th><th>文件 / 容量</th><th>监控目录</th><th>操作</th></tr></thead>
          <tbody><tr v-for="row in rows" :key="row.id">
            <td><div class="route-identity"><span><UiIcon name="database" :size="17" /></span><div><strong>{{ row.name }}</strong><small>ROUTE #{{ String(row.id).padStart(4, '0') }}</small></div></div></td>
            <td><div class="route-target"><strong class="mono">{{ row.remoteName }}:</strong><small class="mono" :title="row.remotePath">{{ row.remotePath }}</small></div></td>
            <td><StatusBadge :status="row.status" /><p v-if="row.lastError" class="route-error" :title="row.lastError">{{ row.lastError }}</p></td>
            <td class="mono">{{ formatDuration(row.scanIntervalSeconds) }}</td>
            <td><div class="date-cell"><strong>{{ formatDate(row.lastScanFinishedAt) }}</strong><small v-if="row.lastScanDurationMs">耗时 {{ formatMilliseconds(row.lastScanDurationMs) }}</small></div></td>
            <td><div class="capacity-cell"><strong class="mono">{{ number(row.totalFileCount) }} files</strong><small>{{ formatBytes(row.totalFileSize) }}</small></div></td>
            <td><span class="route-link-count">{{ row.watchFolderCount }}</span></td>
            <td><div class="row-actions">
              <button class="icon-button" type="button" title="查看远端文件" @click="openFiles(row)"><UiIcon name="eye" :size="15" /></button>
              <template v-if="isAdmin"><button class="icon-button" type="button" title="立即扫描" :disabled="row.status === 'scanning'" @click="handleScan(row)"><UiIcon name="radar" :size="15" /></button><button class="icon-button" type="button" title="编辑" @click="openEdit(row)"><UiIcon name="edit" :size="15" /></button><button class="icon-button danger-action" type="button" title="删除" @click="handleDelete(row)"><UiIcon name="trash" :size="15" /></button></template>
            </div></td>
          </tr></tbody>
        </table>
      </div>
      <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="database" /></span><strong>还没有远端路由</strong><p>创建路由后，后台会自动读取并保存远端文件路径。</p></div></div>
      <PaginationBar v-model:page="pagination.page" v-model:page-size="pagination.pageSize" :total="pagination.total" @change="loadData" />
    </section>

    <Transition name="sheet-slide"><div v-if="drawerVisible" class="sheet-layer" @click.self="closeDrawer"><aside class="side-sheet" role="dialog" aria-modal="true" aria-labelledby="route-form-title">
      <header class="sheet-header"><div><span class="eyebrow">{{ drawerMode === 'create' ? 'NEW REMOTE ROUTE' : 'EDIT REMOTE ROUTE' }}</span><h2 id="route-form-title">{{ drawerMode === 'create' ? '创建远端路由' : '编辑远端路由' }}</h2></div><button class="icon-button" type="button" @click="closeDrawer"><UiIcon name="close" :size="18" /></button></header>
      <form class="sheet-body route-form" @submit.prevent="handleSubmit">
        <div class="field"><label for="route-name">路由名称 *</label><input id="route-name" v-model="form.name" class="ui-control" placeholder="例如：生产归档盘" /></div>
        <div class="field"><label for="route-remote">Rclone Remote *</label><select id="route-remote" v-model="form.remote_name" class="ui-control"><option value="" disabled>{{ remoteLoading ? '加载中...' : '选择 Remote' }}</option><option v-for="option in remoteOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></div>
        <div class="field"><label for="route-path">远端根路径 *</label><input id="route-path" v-model="form.remote_path" class="ui-control mono" placeholder="backup/videos" /><p class="field-help">监控目录中的相对文件路径会追加到该根路径。</p></div>
        <div class="field"><label for="route-interval">远端扫描周期（秒）</label><input id="route-interval" v-model.number="form.scan_interval_seconds" class="ui-control mono" type="number" min="60" max="604800" /></div>
        <label class="toggle-field"><input v-model="form.enabled" type="checkbox" /><span><strong>启用后台扫描</strong><small>关闭后保留已索引记录，但不再自动读取远端。</small></span></label>
      </form>
      <footer class="sheet-footer"><button class="ui-button is-ghost" type="button" @click="closeDrawer">取消</button><button class="ui-button is-primary" type="button" :disabled="saving" @click="handleSubmit"><span v-if="saving" class="mini-spinner is-dark" /><UiIcon v-else name="check" :size="16" />{{ saving ? '正在保存' : '保存路由' }}</button></footer>
    </aside></div></Transition>

    <IndexedFileBrowser
      :visible="fileBrowserVisible"
      :title="`${selectedRoute?.name || ''} · 远端文件`"
      :subtitle="selectedRoute ? `${selectedRoute.remoteName}:${selectedRoute.remotePath}` : ''"
      :root-label="selectedRoute ? `${selectedRoute.remoteName}:${selectedRoute.remotePath}` : ''"
      :loader="loadSelectedFiles"
      :can-manage="isAdmin"
      :create-folder="createSelectedRemoteFolder"
      :rename-folder="renameSelectedRemoteFolder"
      :move-files="moveSelectedRemoteFiles"
      tree
      @close="fileBrowserVisible = false"
    />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import UiIcon from '../components/UiIcon.vue';
import StatusBadge from '../components/StatusBadge.vue';
import PaginationBar from '../components/PaginationBar.vue';
import IndexedFileBrowser from '../components/IndexedFileBrowser.vue';
import { currentUser } from '../api/auth';
import { fetchRcloneConfigs } from '../api/rclone';
import { createRemoteFolder, createRemoteRoute, deleteRemoteRoute, fetchRemoteRouteFiles, fetchRemoteRoutes, moveRemoteFiles, renameRemoteFolder, scanAllRemoteRoutes, scanRemoteRoute, updateRemoteRoute } from '../api/remoteRoutes';
import { confirmDialog, errorText, toast } from '../composables/useUi';

const isAdmin = currentUser()?.role !== 'viewer';
const rows = ref([]); const loading = ref(false); const saving = ref(false); const scanAllLoading = ref(false);
const drawerVisible = ref(false); const drawerMode = ref('create'); const editingId = ref(null);
const remoteOptions = ref([]); const remoteLoading = ref(false); const fileBrowserVisible = ref(false); const selectedRoute = ref(null);
const filter = reactive({ status: '', keyword: '' }); const pagination = reactive({ page: 1, pageSize: 20, total: 0 }); const form = reactive(defaultForm());
let refreshTimer;

const readyCount = computed(() => rows.value.filter((row) => row.status === 'ready').length);
const scanningCount = computed(() => rows.value.filter((row) => row.status === 'scanning').length);
const indexedFiles = computed(() => rows.value.reduce((sum, row) => sum + row.totalFileCount, 0));
function defaultForm() { return { name: '', remote_name: '', remote_path: '', scan_interval_seconds: 3600, enabled: true }; }
function pick(item, ...keys) { for (const key of keys) if (item?.[key] !== undefined && item?.[key] !== null) return item[key]; }
function normalize(item) { return { id: pick(item,'ID','id'), name: pick(item,'Name','name') || '未命名路由', remoteName: pick(item,'RemoteName','remote_name') || '-', remotePath: pick(item,'RemotePath','remote_path') || '-', enabled: Boolean(pick(item,'Enabled','enabled')), status: pick(item,'Status','status') || 'pending', scanIntervalSeconds: Number(pick(item,'ScanIntervalSeconds','scan_interval_seconds') || 3600), lastError: pick(item,'LastError','last_error') || '', lastScanFinishedAt: pick(item,'LastScanFinishedAt','last_scan_finished_at'), lastScanDurationMs: Number(pick(item,'LastScanDurationMs','last_scan_duration_ms') || 0), totalFileCount: Number(pick(item,'TotalFileCount','total_file_count') || 0), totalFileSize: Number(pick(item,'TotalFileSize','total_file_size') || 0), watchFolderCount: Number(pick(item,'WatchFolderCount','watch_folder_count') || 0) }; }
function number(value) { return new Intl.NumberFormat('zh-CN').format(Number(value || 0)); }
function formatBytes(bytes) { const value=Number(bytes||0); if(!value)return '0 B'; const units=['B','KB','MB','GB','TB','PB']; const i=Math.min(Math.floor(Math.log(value)/Math.log(1024)),units.length-1); return `${(value/1024**i).toFixed(i>1?2:0)} ${units[i]}`; }
function formatDuration(seconds) { const value=Number(seconds||0); return value>=86400?`${Math.round(value/86400)}d`:value>=3600?`${Math.round(value/3600)}h`:value>=60?`${Math.round(value/60)}m`:`${value}s`; }
function formatMilliseconds(ms) { return ms>=1000?`${(ms/1000).toFixed(1)}s`:`${ms}ms`; }
function formatDate(value) { if(!value)return '从未扫描'; const date=new Date(value); return Number.isNaN(date.getTime())?String(value):new Intl.DateTimeFormat('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}).format(date); }

async function loadData(silent=false) { if(!silent) loading.value=true; try { const result=await fetchRemoteRoutes({ status:filter.status||undefined, keyword:filter.keyword.trim()||undefined, page:pagination.page, page_size:pagination.pageSize }); pagination.total=Number(result.total||0); rows.value=(result.items||[]).map(normalize); } catch(error) { if(!silent) toast(errorText(error,'远端路由加载失败'),'error'); } finally { if(!silent) loading.value=false; } }
function handleSearch(){pagination.page=1;loadData();}
function openCreate(){drawerMode.value='create';editingId.value=null;Object.assign(form,defaultForm());drawerVisible.value=true;}
function openEdit(row){drawerMode.value='edit';editingId.value=row.id;Object.assign(form,{name:row.name,remote_name:row.remoteName,remote_path:row.remotePath,scan_interval_seconds:row.scanIntervalSeconds,enabled:row.enabled});drawerVisible.value=true;}
function closeDrawer(){if(!saving.value)drawerVisible.value=false;}
async function handleSubmit(){if(!form.name.trim()||!form.remote_name||!form.remote_path.trim()){toast('请填写路由名称、Remote 和远端根路径','warning');return;} if(Number(form.scan_interval_seconds)<60){toast('远端扫描周期不能小于 60 秒','warning');return;} saving.value=true; const payload={name:form.name.trim(),remote_name:form.remote_name,remote_path:form.remote_path.trim(),scan_interval_seconds:Number(form.scan_interval_seconds)||3600,enabled:Boolean(form.enabled)}; try{if(drawerMode.value==='create')await createRemoteRoute(payload);else await updateRemoteRoute(editingId.value,payload);toast('远端路由已保存','success');drawerVisible.value=false;await loadData();}catch(error){toast(errorText(error,'路由保存失败'),'error');}finally{saving.value=false;}}
async function handleDelete(row){const confirmed=await confirmDialog({title:'删除远端路由',message:`确定删除「${row.name}」及其远端文件索引？仍被监控目录使用的路由不能删除。`,confirmText:'确认删除'});if(!confirmed)return;try{await deleteRemoteRoute(row.id);toast('远端路由已删除','success');await loadData();}catch(error){toast(errorText(error,'删除失败'),'error');}}
async function handleScan(row){try{await scanRemoteRoute(row.id);toast(`已安排「${row.name}」后台扫描`,'success');setTimeout(()=>loadData(true),500);}catch(error){toast(errorText(error,'扫描下发失败'),'error');}}
async function handleScanAll(){scanAllLoading.value=true;try{const result=await scanAllRemoteRoutes();toast(`已安排 ${Number(result.scheduled||0)} 条远端路由扫描`,'success');setTimeout(()=>loadData(true),500);}catch(error){toast(errorText(error,'扫描下发失败'),'error');}finally{scanAllLoading.value=false;}}
function openFiles(row){selectedRoute.value=row;fileBrowserVisible.value=true;}
function loadSelectedFiles(path,page,pageSize){return fetchRemoteRouteFiles(selectedRoute.value.id,{path,page,page_size:pageSize});}
async function createSelectedRemoteFolder(parentPath,name){const result=await createRemoteFolder(selectedRoute.value.id,{parent_path:parentPath,name});setTimeout(()=>loadData(true),500);return result;}
async function renameSelectedRemoteFolder(path,newName){const result=await renameRemoteFolder(selectedRoute.value.id,{path,new_name:newName});setTimeout(()=>loadData(true),500);return result;}
async function moveSelectedRemoteFiles(sourcePaths,targetFolder,onProgress){const result=await moveRemoteFiles(selectedRoute.value.id,{source_paths:sourcePaths,target_folder:targetFolder},onProgress);setTimeout(()=>loadData(true),500);return result;}
async function loadRemotes(){remoteLoading.value=true;try{const result=await fetchRcloneConfigs();remoteOptions.value=(result.items||[]).map((item)=>({label:`${item.name}${item.type?` · ${item.type}`:''}`,value:item.name}));}catch(error){toast(errorText(error,'Remote 列表加载失败'),'warning');}finally{remoteLoading.value=false;}}
function handleKeydown(event){if(event.key==='Escape'&&drawerVisible.value)closeDrawer();}
onMounted(()=>{loadData();loadRemotes();refreshTimer=setInterval(()=>loadData(true),15000);window.addEventListener('keydown',handleKeydown);});
onBeforeUnmount(()=>{clearInterval(refreshTimer);window.removeEventListener('keydown',handleKeydown);});
</script>

<style scoped>
.routes-page{display:flex;flex-direction:column;gap:20px;max-width:none}.routes-page .page-header{margin-bottom:0}.mini-spinner{width:15px;height:15px;border:2px solid rgba(var(--cyan-rgb),.2);border-top-color:var(--cyan);border-radius:50%;animation:spin .7s linear infinite}.mini-spinner.is-dark{border-color:rgba(4,16,20,.2);border-top-color:#041014}
.route-summary{position:relative;z-index:1;display:grid;grid-template-columns:repeat(4,1fr);overflow:hidden;border:1px solid var(--line);border-radius:16px;background:var(--panel-background);box-shadow:var(--shadow-card)}.route-summary>div{min-height:82px;display:flex;flex-direction:column;justify-content:center;padding:0 22px;border-right:1px solid var(--line)}.route-summary>div:last-child{border-right:0}.route-summary small{color:var(--text-muted);font-size:9px}.route-summary strong{margin-top:5px;color:var(--text-strong);font:600 21px var(--font-mono)}
.route-panel{z-index:1}.route-toolbar{position:relative;z-index:1;display:flex;align-items:center;justify-content:space-between;gap:20px;padding:17px 20px;border-bottom:1px solid var(--line)}.route-toolbar>div{display:flex;align-items:center;gap:10px}.route-toolbar>div>span{width:36px;height:36px;display:grid;place-items:center;color:var(--violet);border-radius:10px;background:rgba(140,118,255,.09)}.route-toolbar h2,.route-toolbar p{margin:0}.route-toolbar h2{color:var(--text-strong);font-size:13px}.route-toolbar p{margin-top:3px;color:var(--text-muted);font:500 8px var(--font-mono)}.route-toolbar form{display:flex;gap:8px}.route-toolbar form>select{width:135px}.route-toolbar .ui-control{height:36px;font-size:11px}.route-toolbar .search-field{min-width:240px}.routes-table{min-width:1180px}.route-identity{display:flex;align-items:center;gap:10px;min-width:170px}.route-identity>span{width:36px;height:36px;display:grid;place-items:center;color:var(--violet);border-radius:10px;background:rgba(140,118,255,.09)}.route-identity strong,.route-identity small,.route-target strong,.route-target small,.date-cell strong,.date-cell small,.capacity-cell strong,.capacity-cell small{display:block}.route-identity strong{color:var(--text-strong);font-size:11px}.route-identity small,.route-target small,.date-cell small,.capacity-cell small{margin-top:4px;color:var(--text-muted);font-size:8px}.route-target strong{color:var(--violet);font-size:10px}.route-target small{max-width:210px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.date-cell strong{color:var(--text);font-size:10px;font-weight:500}.route-error{max-width:150px;margin:4px 0 0;overflow:hidden;color:var(--red);font-size:8px;text-overflow:ellipsis;white-space:nowrap}.route-link-count{min-width:29px;height:26px;display:inline-grid;place-items:center;color:var(--cyan);border:1px solid var(--line);border-radius:8px;background:var(--surface-soft);font:600 10px var(--font-mono)}.danger-action:hover{color:var(--red)!important}.route-form{display:flex;flex-direction:column;gap:20px}.toggle-field{display:flex;gap:11px;padding:16px;border:1px solid var(--line);border-radius:12px;background:var(--surface-soft);cursor:pointer}.toggle-field input{accent-color:var(--cyan)}.toggle-field strong,.toggle-field small{display:block}.toggle-field strong{color:var(--text-strong);font-size:11px}.toggle-field small{margin-top:4px;color:var(--text-muted);font-size:9px}
@media(max-width:900px){.route-toolbar{align-items:flex-start;flex-direction:column}.route-toolbar form{width:100%;flex-wrap:wrap}.route-toolbar .search-field{min-width:200px;flex:1}}@media(max-width:650px){.route-summary{grid-template-columns:repeat(2,1fr)}.route-summary>div:nth-child(2){border-right:0}.route-summary>div:nth-child(-n+2){border-bottom:1px solid var(--line)}.route-toolbar form>select,.route-toolbar .search-field{width:100%;min-width:100%}}

.mini-spinner.is-dark { border-color: rgba(var(--cyan-rgb),.22); border-top-color: var(--text-on-accent); }
.route-toolbar > div > span,
.route-identity > span,
.route-target strong { color: var(--violet-text); }
</style>
