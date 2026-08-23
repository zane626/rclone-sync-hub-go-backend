<template>
  <div class="page-shell operations-page">
    <header class="page-header">
      <div class="page-heading">
        <span class="eyebrow">SYSTEM OBSERVABILITY</span>
        <h1 class="page-title">运行中心</h1>
        <p class="page-description">服务健康、扫描批次、实时事件与安全审计的统一控制面。</p>
      </div>
      <div class="page-actions">
        <div class="stream-pill" :class="{ 'is-reconnecting': !eventConnected }"><i />{{ eventConnected ? 'EVENT STREAM LIVE' : 'STREAM RECONNECTING' }}</div>
        <button class="ui-button" type="button" :disabled="loading" @click="loadAll">
          <UiIcon name="refresh" :size="16" /> 刷新态势
        </button>
        <button v-if="isAdmin" class="ui-button is-primary" type="button" :disabled="scanTriggering" @click="handleTriggerScan">
          <span v-if="scanTriggering" class="mini-spinner is-dark" /><UiIcon v-else name="radar" :size="16" />
          {{ scanTriggering ? '正在下发' : '立即扫描' }}
        </button>
      </div>
    </header>

    <section class="ops-ribbon" :class="{ 'is-degraded': !operational && !systemChecking, 'is-checking': systemChecking }">
      <div class="ops-ribbon__primary">
        <span class="core-orb"><UiIcon :name="systemChecking ? 'radar' : operational ? 'shield' : 'alert'" :size="22" /></span>
        <div><strong>{{ systemChecking ? '正在建立服务拓扑' : operational ? '所有核心服务运行正常' : '节点存在异常，请立即检查' }}</strong><small>{{ systemChecking ? 'CONTROL PLANE / PROBING' : operational ? 'CONTROL PLANE / NOMINAL' : 'CONTROL PLANE / DEGRADED' }}</small></div>
      </div>
      <div><span>节点延迟</span><strong>{{ systemStatus.latencyMilliseconds }}<small> ms</small></strong></div>
      <div><span>运行时长</span><strong>{{ formatUptime(systemStatus.uptimeSeconds) }}</strong></div>
      <div><span>扫描批次</span><strong>{{ number(scanPagination.total) }}</strong></div>
      <div><span>存储端点</span><strong>{{ number(remotes.length) }}</strong></div>
      <span class="ops-ribbon__stamp">LAST SYNC {{ lastRefreshLabel }}</span>
    </section>

    <section class="ops-topology">
      <article class="ui-panel health-panel">
        <div class="panel-header">
          <div><h2 class="panel-title">服务拓扑</h2><p class="panel-subtitle">核心依赖探针与控制面连接状态</p></div>
          <span class="panel-code">HEALTH / PROBES</span>
        </div>
        <div class="probe-grid">
          <div v-for="probe in probes" :key="probe.key" class="probe-card" :class="`is-${probe.tone}`">
            <span class="probe-card__icon"><UiIcon :name="probe.icon" :size="19" /></span>
            <div><small>{{ probe.caption }}</small><strong>{{ probe.label }}</strong><p>{{ probe.detail }}</p></div>
            <i class="probe-card__signal" />
          </div>
        </div>
        <div class="health-footer">
          <span><UiIcon name="clock" :size="14" /> 探针每 15 秒自动刷新</span>
          <span class="mono">{{ systemStatus.lastCheckedAt ? formatDateTime(systemStatus.lastCheckedAt) : '等待首次探测' }}</span>
        </div>
      </article>

      <article class="ui-panel event-panel">
        <div class="panel-header">
          <div><h2 class="panel-title">实时事件流</h2><p class="panel-subtitle">任务状态与传输进度即时遥测</p></div>
          <button class="icon-button" type="button" title="清空事件" @click="liveEvents = []"><UiIcon name="trash" :size="14" /></button>
        </div>
        <div v-if="liveEvents.length" class="event-feed">
          <div v-for="(event, index) in liveEvents" :key="event.key" class="event-item" :class="`is-${event.tone}`">
            <span class="event-index">{{ String(index + 1).padStart(2, '0') }}</span>
            <i />
            <div><strong>{{ event.title }}</strong><p>{{ event.detail }}</p></div>
            <time>{{ event.time }}</time>
          </div>
        </div>
        <div v-else class="empty-state event-empty"><div><span class="empty-state__icon"><UiIcon name="activity" /></span><strong>等待实时事件</strong><p>任务状态变化会自动推送到这里。</p></div></div>
        <footer class="event-footer"><span><i :class="{ 'is-offline': !eventConnected }" /> {{ eventConnected ? 'SUBSCRIBED' : 'RETRYING' }}</span><span>{{ liveEvents.length }} / 20 EVENTS</span></footer>
      </article>
    </section>

    <section class="ui-panel scan-panel">
      <div class="panel-header scan-panel__header">
        <div><h2 class="panel-title">扫描批次</h2><p class="panel-subtitle">目录发现、稳定性判断与任务生成效率</p></div>
        <div class="scan-summary">
          <span><small>成功率</small><strong>{{ scanSuccessRate }}%</strong></span>
          <span><small>平均耗时</small><strong>{{ formatMilliseconds(scanAverageDuration) }}</strong></span>
          <span><small>发现文件</small><strong>{{ number(scanTotals.filesSeen) }}</strong></span>
          <span><small>创建任务</small><strong>{{ number(scanTotals.tasksCreated) }}</strong></span>
        </div>
      </div>
      <div class="scan-toolbar">
        <div class="folder-selector"><UiIcon name="folder" :size="15" /><select v-model="scanFolderId" class="ui-control" @change="changeScanFolder"><option value="">全部监控目录</option><option v-for="folder in watchFolders" :key="folder.id" :value="folder.id">{{ folder.name }}</option></select></div>
        <span>显示最近扫描记录，按启动时间倒序排列</span>
      </div>
      <div v-if="scanLoading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />LOADING SCAN TELEMETRY</div></div>
      <div v-else-if="scanRuns.length" class="data-table-shell">
        <table class="data-table scan-table">
          <thead><tr><th>批次 / 目录</th><th>状态</th><th>开始时间</th><th>扫描耗时</th><th>发现文件</th><th>未变化 / 稳定跳过</th><th>生成任务</th><th>错误</th></tr></thead>
          <tbody>
            <tr v-for="run in scanRuns" :key="run.id">
              <td><div class="scan-identity"><span><UiIcon name="radar" :size="16" /></span><div><strong>#{{ run.id }} · {{ run.watchFolderName || `目录 ${run.watchFolderId}` }}</strong><small>{{ formatBytes(run.totalBytes) }} DISCOVERED</small></div></div></td>
              <td><StatusBadge :status="run.status" /></td>
              <td><div class="date-cell"><strong>{{ formatDateTime(run.startedAt) }}</strong><small v-if="run.finishedAt">完成 {{ formatDateTime(run.finishedAt) }}</small></div></td>
              <td><strong class="mono">{{ run.status === 'running' ? 'RUNNING' : formatMilliseconds(run.durationMilliseconds) }}</strong></td>
              <td><strong class="mono text-strong">{{ number(run.filesSeen) }}</strong></td>
              <td><div class="split-metric"><span>{{ number(run.filesUnchanged) }}</span><i /> <span>{{ number(run.filesStableSkipped) }}</span></div></td>
              <td><strong class="mono cyan-text">{{ number(run.tasksCreated) }}</strong></td>
              <td><span v-if="run.scanErrors" class="scan-error" :title="run.errorMessage"><UiIcon name="alert" :size="13" /> {{ number(run.scanErrors) }}</span><span v-else class="muted-check"><UiIcon name="check" :size="13" /> 0</span></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="radar" /></span><strong>暂无扫描批次</strong><p>手动触发扫描或等待调度器执行。</p></div></div>
      <PaginationBar :page="scanPagination.page" :page-size="scanPagination.pageSize" :page-sizes="[10, 20, 50]" :total="scanPagination.total" @update:page="scanPagination.page = $event" @update:page-size="scanPagination.pageSize = $event" @change="loadScanRuns" />
    </section>

    <section class="ops-lower">
      <article class="ui-panel remote-panel">
        <div class="panel-header"><div><h2 class="panel-title">存储端点</h2><p class="panel-subtitle">Rclone 已配置 Remote，只展示非敏感信息</p></div><span class="panel-code">{{ remotes.length }} ENDPOINTS</span></div>
        <div v-if="remotes.length" class="remote-grid">
          <div v-for="(remote, index) in remotes" :key="remote.name" class="remote-card">
            <span class="remote-card__number">{{ String(index + 1).padStart(2, '0') }}</span>
            <span class="remote-card__icon"><UiIcon name="database" :size="18" /></span>
            <div><strong>{{ remote.name }}</strong><small>{{ remote.type || 'UNKNOWN DRIVER' }}</small></div>
            <i />
          </div>
        </div>
        <div v-else class="empty-state compact-empty"><div><span class="empty-state__icon"><UiIcon name="database" /></span><strong>未发现存储端点</strong><p>请检查 Rclone 配置。</p></div></div>
      </article>

      <article v-if="isAdmin" class="ui-panel audit-panel">
        <div class="panel-header"><div><h2 class="panel-title">安全审计</h2><p class="panel-subtitle">最近的管理操作与响应结果</p></div><span class="panel-code">ADMIN ONLY</span></div>
        <div v-if="auditLoading" class="loading-layer compact-loading"><div class="loading-indicator"><span class="spinner" />READING AUDIT TRAIL</div></div>
        <div v-else-if="auditLogs.length" class="audit-list">
          <div v-for="log in auditLogs" :key="log.id" class="audit-row">
            <span class="method-chip" :class="`is-${String(log.method).toLowerCase()}`">{{ log.method }}</span>
            <div><strong>{{ log.actor || 'SYSTEM' }} · {{ actionLabel(log.action) }}</strong><p class="mono" :title="log.path">{{ log.resource || log.path }}</p></div>
            <span class="response-code" :class="{ 'is-error': log.statusCode >= 400 }">{{ log.statusCode }}</span>
            <time>{{ formatDateTime(log.createdAt) }}</time>
          </div>
        </div>
        <div v-else class="empty-state compact-empty"><div><span class="empty-state__icon"><UiIcon name="shield" /></span><strong>暂无审计记录</strong></div></div>
        <PaginationBar :page="auditPagination.page" :page-size="auditPagination.pageSize" :page-sizes="[10, 20, 50]" :total="auditPagination.total" @update:page="auditPagination.page = $event" @update:page-size="auditPagination.pageSize = $event" @change="loadAuditLogs" />
      </article>

      <article v-else class="ui-panel access-panel">
        <span><UiIcon name="lock" :size="24" /></span><div><small>ROLE RESTRICTION</small><h2>审计轨迹仅管理员可见</h2><p>当前账户为只读观察员，可以查看运行状态、扫描批次和实时事件，但不能执行扫描或读取安全审计。</p></div>
      </article>
    </section>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import UiIcon from '../components/UiIcon.vue';
import StatusBadge from '../components/StatusBadge.vue';
import PaginationBar from '../components/PaginationBar.vue';
import { fetchAuditLogs, fetchScanRuns } from '../api/operations';
import { fetchRcloneConfigs } from '../api/rclone';
import { fetchWatchFolders } from '../api/watchFolders';
import { streamTaskEvents } from '../api/events';
import { triggerScan } from '../api/scanner';
import { currentUser } from '../api/auth';
import { useSystemStatus } from '../composables/useSystemStatus';
import { errorText, toast } from '../composables/useUi';

const isAdmin = currentUser()?.role !== 'viewer';
const { status: systemStatus, operational, checking: systemChecking, refresh: refreshSystemStatus } = useSystemStatus({ interval: 15000 });
const loading = ref(false);
const scanLoading = ref(false);
const auditLoading = ref(false);
const scanTriggering = ref(false);
const eventConnected = ref(false);
const liveEvents = ref([]);
const scanRuns = ref([]);
const auditLogs = ref([]);
const remotes = ref([]);
const watchFolders = ref([]);
const scanFolderId = ref('');
const lastRefreshedAt = ref(null);
const scanPagination = reactive({ page: 1, pageSize: 10, total: 0 });
const auditPagination = reactive({ page: 1, pageSize: 10, total: 0 });
const eventAbortController = new AbortController();
let refreshTimer;

const lastRefreshLabel = computed(() => lastRefreshedAt.value ? new Intl.DateTimeFormat('zh-CN', { hour:'2-digit', minute:'2-digit', second:'2-digit', hour12:false }).format(lastRefreshedAt.value) : '--:--:--');
const scanTotals = computed(() => scanRuns.value.reduce((total, run) => ({ filesSeen: total.filesSeen + run.filesSeen, tasksCreated: total.tasksCreated + run.tasksCreated }), { filesSeen: 0, tasksCreated: 0 }));
const scanSuccessRate = computed(() => scanRuns.value.length ? Math.round(scanRuns.value.filter((run) => run.status === 'success').length / scanRuns.value.length * 100) : 0);
const scanAverageDuration = computed(() => { const finished = scanRuns.value.filter((run) => run.durationMilliseconds > 0); return finished.length ? finished.reduce((sum, run) => sum + run.durationMilliseconds, 0) / finished.length : 0; });
const probes = computed(() => [
  { key:'api', label:systemStatus.liveness === 'checking' ? '正在探测 API 节点' : systemStatus.liveness === 'online' ? 'API 节点在线' : 'API 节点离线', caption:'APPLICATION NODE', detail:systemStatus.liveness === 'checking' ? '等待 Liveness 响应' : `往返延迟 ${systemStatus.latencyMilliseconds} ms`, icon:'server', tone:systemStatus.liveness === 'checking' ? 'amber' : systemStatus.liveness === 'online' ? 'green' : 'red' },
  { key:'database', label:systemStatus.readiness === 'checking' ? '正在检查数据库' : systemStatus.readiness === 'ready' ? '数据库已就绪' : '数据库未就绪', caption:'MYSQL READINESS', detail:systemStatus.readiness === 'checking' ? '等待 Readiness 响应' : systemStatus.readiness === 'ready' ? '读写探针通过' : '请检查 MySQL 健康状态', icon:'database', tone:systemStatus.readiness === 'checking' ? 'amber' : systemStatus.readiness === 'ready' ? 'green' : 'red' },
  { key:'events', label:eventConnected.value ? '事件通道已连接' : '事件通道重连中', caption:'SERVER-SENT EVENTS', detail:eventConnected.value ? '实时遥测订阅正常' : '自动重试间隔 3 秒', icon:'activity', tone:eventConnected.value ? 'cyan' : 'amber' },
  { key:'storage', label:`${remotes.value.length} 个存储端点`, caption:'RCLONE REMOTES', detail:remotes.value.length ? '配置读取成功' : '尚未发现可用 Remote', icon:'layers', tone:remotes.value.length ? 'violet' : 'amber' }
]);

function pick(item, ...keys) { for (const key of keys) if (item?.[key] !== undefined && item?.[key] !== null) return item[key]; return undefined; }
function normalizeScanRun(item) { return { id:pick(item,'ID','id'), watchFolderId:pick(item,'WatchFolderID','watch_folder_id'), watchFolderName:pick(item,'WatchFolderName','watch_folder_name') || '', status:pick(item,'Status','status') || 'running', startedAt:pick(item,'StartedAt','started_at'), finishedAt:pick(item,'FinishedAt','finished_at'), durationMilliseconds:Number(pick(item,'DurationMilliseconds','duration_milliseconds') || 0), filesSeen:Number(pick(item,'FilesSeen','files_seen') || 0), totalBytes:Number(pick(item,'TotalBytes','total_bytes') || 0), filesUnchanged:Number(pick(item,'FilesUnchanged','files_unchanged') || 0), filesStableSkipped:Number(pick(item,'FilesStableSkipped','files_stable_skipped') || 0), tasksCreated:Number(pick(item,'TasksCreated','tasks_created') || 0), scanErrors:Number(pick(item,'ScanErrors','scan_errors') || 0), errorMessage:pick(item,'ErrorMessage','error_message') || '' }; }
function normalizeAudit(item) { return { id:pick(item,'ID','id'), actor:pick(item,'Actor','actor'), action:pick(item,'Action','action'), resource:pick(item,'Resource','resource'), method:pick(item,'Method','method') || 'GET', path:pick(item,'Path','path'), statusCode:Number(pick(item,'StatusCode','status_code') || 0), createdAt:pick(item,'CreatedAt','created_at') }; }
function normalizeFolder(item) { return { id:pick(item,'ID','id'), name:pick(item,'Name','name') || `目录 ${pick(item,'ID','id')}` }; }
function number(value) { return new Intl.NumberFormat('zh-CN').format(Number(value || 0)); }
function formatBytes(bytes) { const value=Number(bytes||0); if(!value)return '0 B'; const units=['B','KB','MB','GB','TB']; const index=Math.min(Math.floor(Math.log(value)/Math.log(1024)),units.length-1); return `${(value/1024**index).toFixed(index>1?2:0)} ${units[index]}`; }
function formatMilliseconds(ms) { const value=Number(ms||0); if(!value)return '0 ms'; if(value>=60000)return `${(value/60000).toFixed(1)} min`; if(value>=1000)return `${(value/1000).toFixed(1)} s`; return `${Math.round(value)} ms`; }
function formatUptime(seconds) { const value=Number(seconds||0); if(value>=86400)return `${Math.floor(value/86400)}d ${Math.floor(value%86400/3600)}h`; if(value>=3600)return `${Math.floor(value/3600)}h ${Math.floor(value%3600/60)}m`; if(value>=60)return `${Math.floor(value/60)}m`; return `${value}s`; }
function formatDateTime(value) { if(!value)return '-'; const date=value instanceof Date?value:new Date(value); if(Number.isNaN(date.getTime()))return String(value); return new Intl.DateTimeFormat('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(date); }
function actionLabel(value) { return ({ create:'创建', update:'更新', delete:'删除', retry:'重试', pause:'暂停', cancel:'取消', scan:'扫描' })[String(value).toLowerCase()] || value || '操作'; }

async function loadScanRuns() { scanLoading.value=true; try { const result=await fetchScanRuns({ watch_folder_id:scanFolderId.value || undefined, page:scanPagination.page, page_size:scanPagination.pageSize }); scanRuns.value=(result.items||[]).map(normalizeScanRun); scanPagination.total=Number(result.total||0); } catch(error) { scanRuns.value=[]; toast(errorText(error,'扫描记录加载失败'),'error'); } finally { scanLoading.value=false; } }
async function loadAuditLogs() { if(!isAdmin)return; auditLoading.value=true; try { const result=await fetchAuditLogs({ page:auditPagination.page, page_size:auditPagination.pageSize }); auditLogs.value=(result.items||[]).map(normalizeAudit); auditPagination.total=Number(result.total||0); } catch(error) { auditLogs.value=[]; toast(errorText(error,'审计记录加载失败'),'error'); } finally { auditLoading.value=false; } }
async function loadReferenceData() { const [remoteResult, folderResult]=await Promise.allSettled([fetchRcloneConfigs(),fetchWatchFolders({page:1,page_size:200})]); if(remoteResult.status==='fulfilled')remotes.value=remoteResult.value.items||[]; if(folderResult.status==='fulfilled')watchFolders.value=(folderResult.value.items||[]).map(normalizeFolder); }
async function loadAll() { loading.value=true; const results=await Promise.allSettled([refreshSystemStatus(),loadScanRuns(),loadReferenceData(),loadAuditLogs()]); lastRefreshedAt.value=new Date(); loading.value=false; if(results.some((result)=>result.status==='rejected'))toast('部分运行数据暂时不可用','warning'); }
function changeScanFolder() { scanPagination.page=1; loadScanRuns(); }
async function handleTriggerScan() { scanTriggering.value=true; try { const result=await triggerScan(); toast(`扫描命令已下发${result?.enqueued !== undefined ? `，已加入 ${result.enqueued} 个目录` : ''}`,'success'); window.setTimeout(loadScanRuns,1000); } catch(error) { toast(errorText(error,'触发扫描失败'),'error'); } finally { scanTriggering.value=false; } }

function applyEvent(event) { if(!event || event.type==='heartbeat')return; const status=event.status||''; const tone=status==='failed'?'red':status==='success'?'green':event.type==='task_progress'?'cyan':'violet'; const taskId=event.task_id??event.taskId??'-'; const progress=event.percent!==undefined?` · ${Math.round(event.percent)}%`:''; liveEvents.value.unshift({ key:`${Date.now()}-${Math.random()}`, tone, title:event.type==='task_progress'?'任务传输进度':'任务状态变化', detail:`TASK #${taskId}${status?` · ${String(status).toUpperCase()}`:''}${progress}`, time:new Intl.DateTimeFormat('zh-CN',{hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(new Date()) }); liveEvents.value=liveEvents.value.slice(0,20); if(event.type==='task_status')window.setTimeout(()=>loadScanRuns(),500); }
async function startEventStream() { while(!eventAbortController.signal.aborted) { try { await streamTaskEvents(applyEvent,eventAbortController.signal,(connected)=>{eventConnected.value=connected;}); } catch { eventConnected.value=false; } if(eventAbortController.signal.aborted)return; await new Promise((resolve)=>window.setTimeout(resolve,3000)); } }

onMounted(()=>{ loadAll(); startEventStream(); refreshTimer=window.setInterval(()=>{ if(document.visibilityState==='visible'){ loadScanRuns(); if(isAdmin)loadAuditLogs(); } },30000); });
onBeforeUnmount(()=>{ eventAbortController.abort(); window.clearInterval(refreshTimer); });
</script>

<style scoped>
.operations-page{display:flex;flex-direction:column;gap:20px;max-width:none}.operations-page .page-header{margin-bottom:0}.stream-pill{display:flex;align-items:center;gap:8px;min-height:39px;padding:0 13px;color:#73aa93;border:1px solid rgba(61,225,162,.16);border-radius:10px;background:rgba(61,225,162,.045);font:600 8px var(--font-mono);letter-spacing:.09em}.stream-pill i{width:6px;height:6px;border-radius:50%;background:var(--green);box-shadow:0 0 10px var(--green)}.stream-pill.is-reconnecting{color:#a28558;border-color:rgba(255,180,74,.18);background:rgba(255,180,74,.05)}.stream-pill.is-reconnecting i{background:var(--amber);box-shadow:0 0 10px var(--amber);animation:pulse 1.2s infinite}
.mini-spinner{width:15px;height:15px;border:2px solid rgba(88,224,255,.17);border-top-color:var(--cyan);border-radius:50%;animation:spin .7s linear infinite}.mini-spinner.is-dark{border-color:rgba(4,16,20,.2);border-top-color:#041014}
.ops-ribbon{position:relative;z-index:1;min-height:92px;display:grid;grid-template-columns:minmax(280px,1.5fr) repeat(4,minmax(115px,.7fr));align-items:center;overflow:hidden;border:1px solid rgba(61,225,162,.16);border-radius:17px;background:linear-gradient(105deg,rgba(16,36,37,.8),rgba(10,17,26,.94));box-shadow:0 18px 48px rgba(0,0,0,.17)}.ops-ribbon>div{min-height:58px;display:flex;flex-direction:column;justify-content:center;padding:0 23px;border-left:1px solid var(--line)}.ops-ribbon__primary{flex-direction:row!important;align-items:center;gap:14px;border-left:0!important}.core-orb{width:45px;height:45px;display:grid;place-items:center;color:var(--green);border:1px solid rgba(61,225,162,.2);border-radius:50%;background:rgba(61,225,162,.08);box-shadow:0 0 25px rgba(61,225,162,.07)}.ops-ribbon strong,.ops-ribbon small,.ops-ribbon span{display:block}.ops-ribbon__primary strong{color:#e5fcf3;font-size:14px}.ops-ribbon__primary small{margin-top:4px;color:#4b7867;font:600 8px var(--font-mono);letter-spacing:.1em}.ops-ribbon>div>span{color:var(--text-muted);font-size:9px}.ops-ribbon>div>strong{margin-top:4px;color:var(--text-strong);font:600 18px var(--font-mono)}.ops-ribbon>div>strong small{display:inline;color:#586579;font-size:9px}.ops-ribbon__stamp{position:absolute;right:14px;bottom:7px;color:#354455;font:500 7px var(--font-mono);letter-spacing:.08em}.ops-ribbon.is-degraded{border-color:rgba(255,98,125,.18);background:linear-gradient(105deg,rgba(52,24,30,.64),rgba(10,17,26,.94))}.ops-ribbon.is-degraded .core-orb{color:var(--red);border-color:rgba(255,98,125,.2);background:rgba(255,98,125,.08)}.ops-ribbon.is-checking{border-color:rgba(255,180,74,.16);background:linear-gradient(105deg,rgba(51,38,20,.58),rgba(10,17,26,.94))}.ops-ribbon.is-checking .core-orb{color:var(--amber);border-color:rgba(255,180,74,.2);background:rgba(255,180,74,.07)}.ops-ribbon.is-checking .core-orb .ui-icon{animation:spin 4s linear infinite}
.ops-topology{position:relative;z-index:1;display:grid;grid-template-columns:minmax(0,1.45fr) minmax(380px,.75fr);gap:20px}.probe-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:11px;padding:18px}.probe-card{position:relative;display:grid;grid-template-columns:40px 1fr;align-items:center;gap:12px;min-height:101px;padding:15px;border:1px solid var(--line);border-radius:13px;background:rgba(255,255,255,.021)}.probe-card__icon{width:40px;height:40px;display:grid;place-items:center;border-radius:11px;color:var(--text-muted);background:rgba(255,255,255,.035)}.probe-card small,.probe-card strong,.probe-card p{display:block}.probe-card small{color:#536176;font:600 7px var(--font-mono);letter-spacing:.11em}.probe-card strong{margin-top:3px;color:var(--text-strong);font-size:12px}.probe-card p{margin:5px 0 0;color:var(--text-muted);font-size:9px}.probe-card__signal{position:absolute;right:12px;top:12px;width:6px;height:6px;border-radius:50%;background:currentColor;box-shadow:0 0 9px currentColor}.probe-card.is-green{color:var(--green)}.probe-card.is-cyan{color:var(--cyan)}.probe-card.is-violet{color:var(--violet)}.probe-card.is-amber{color:var(--amber)}.probe-card.is-red{color:var(--red)}.probe-card.is-green .probe-card__icon{color:var(--green);background:rgba(61,225,162,.07)}.probe-card.is-cyan .probe-card__icon{color:var(--cyan);background:rgba(88,224,255,.07)}.probe-card.is-violet .probe-card__icon{color:#a997ff;background:rgba(140,118,255,.08)}.probe-card.is-amber .probe-card__icon{color:var(--amber);background:rgba(255,180,74,.07)}.probe-card.is-red .probe-card__icon{color:var(--red);background:rgba(255,98,125,.07)}.health-footer{display:flex;justify-content:space-between;gap:16px;padding:12px 20px;color:#4e5c70;border-top:1px solid var(--line);font-size:8px}.health-footer span{display:flex;align-items:center;gap:7px}
.event-panel{display:flex;flex-direction:column;min-height:329px}.event-feed{position:relative;z-index:1;max-height:250px;overflow:auto;padding:8px 0}.event-item{display:grid;grid-template-columns:25px 7px 1fr auto;align-items:center;gap:9px;padding:8px 15px;border-left:2px solid transparent}.event-item:hover{border-left-color:currentColor;background:rgba(255,255,255,.018)}.event-index{color:#374354;font:500 8px var(--font-mono)}.event-item>i{width:6px;height:6px;border-radius:50%;background:currentColor;box-shadow:0 0 8px currentColor}.event-item strong{display:block;color:#aeb9c8;font-size:10px}.event-item p{margin:2px 0 0;color:#536074;font:500 8px var(--font-mono)}.event-item time{color:#4b5869;font:500 8px var(--font-mono)}.event-item.is-cyan{color:var(--cyan)}.event-item.is-green{color:var(--green)}.event-item.is-red{color:var(--red)}.event-item.is-violet{color:#a997ff}.event-empty{min-height:210px}.event-footer{display:flex;justify-content:space-between;margin-top:auto;padding:9px 15px;color:#405064;border-top:1px solid var(--line);font:500 7px var(--font-mono);letter-spacing:.08em}.event-footer span{display:flex;align-items:center;gap:6px}.event-footer i{width:5px;height:5px;border-radius:50%;background:var(--green);box-shadow:0 0 7px var(--green)}.event-footer i.is-offline{background:var(--amber);box-shadow:0 0 7px var(--amber)}
.scan-panel{position:relative;z-index:1}.scan-panel__header{align-items:flex-start}.scan-summary{display:flex;align-items:center}.scan-summary>span{min-width:105px;padding:0 17px;border-left:1px solid var(--line)}.scan-summary small,.scan-summary strong{display:block}.scan-summary small{color:var(--text-muted);font-size:8px}.scan-summary strong{margin-top:4px;color:var(--text-strong);font:600 14px var(--font-mono)}.scan-toolbar{position:relative;z-index:1;display:flex;align-items:center;justify-content:space-between;gap:20px;padding:12px 18px;border-bottom:1px solid var(--line);background:rgba(4,8,14,.2)}.scan-toolbar>span{color:#4b586b;font-size:9px}.folder-selector{position:relative;width:260px}.folder-selector>.ui-icon{position:absolute;z-index:2;left:12px;top:50%;color:#657387;transform:translateY(-50%)}.folder-selector .ui-control{height:35px;padding-left:36px;font-size:10px}.scan-table{min-width:1120px}.scan-identity{display:flex;align-items:center;gap:10px;min-width:205px}.scan-identity>span{width:34px;height:34px;display:grid;place-items:center;color:var(--cyan);border:1px solid rgba(88,224,255,.13);border-radius:10px;background:rgba(88,224,255,.05)}.scan-identity strong,.scan-identity small{display:block}.scan-identity strong{max-width:250px;overflow:hidden;color:var(--text-strong);font-size:10px;text-overflow:ellipsis;white-space:nowrap}.scan-identity small{margin-top:3px;color:#49586b;font:500 7px var(--font-mono);letter-spacing:.06em}.date-cell strong,.date-cell small{display:block}.date-cell strong{color:#95a3b5;font-size:9px;font-weight:500}.date-cell small{margin-top:4px;color:#4c586a;font-size:8px}.split-metric{display:flex;align-items:center;gap:8px;font:600 10px var(--font-mono)}.split-metric i{width:1px;height:14px;background:var(--line-strong)}.cyan-text{color:var(--cyan)}.scan-error,.muted-check{display:inline-flex;align-items:center;gap:5px;font:600 9px var(--font-mono)}.scan-error{color:var(--red)}.muted-check{color:#4a695e}
.ops-lower{position:relative;z-index:1;display:grid;grid-template-columns:minmax(320px,.62fr) minmax(0,1.38fr);gap:20px}.remote-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:9px;padding:16px}.remote-card{position:relative;display:grid;grid-template-columns:26px 37px 1fr 6px;align-items:center;gap:9px;min-height:70px;padding:10px;border:1px solid var(--line);border-radius:12px;background:rgba(255,255,255,.02)}.remote-card__number{color:#394658;font:500 8px var(--font-mono)}.remote-card__icon{width:37px;height:37px;display:grid;place-items:center;color:#a997ff;border-radius:10px;background:rgba(140,118,255,.08)}.remote-card strong,.remote-card small{display:block}.remote-card strong{max-width:130px;overflow:hidden;color:var(--text-strong);font-size:10px;text-overflow:ellipsis;white-space:nowrap}.remote-card small{margin-top:4px;color:#57657a;font:600 7px var(--font-mono);text-transform:uppercase}.remote-card>i{width:5px;height:5px;border-radius:50%;background:var(--green);box-shadow:0 0 7px var(--green)}.audit-list{max-height:343px;overflow:auto}.audit-row{display:grid;grid-template-columns:54px minmax(0,1fr) 42px 118px;align-items:center;gap:10px;min-height:57px;padding:9px 17px;border-bottom:1px solid rgba(150,176,210,.07)}.audit-row:last-child{border-bottom:0}.audit-row strong{display:block;color:#aeb9c8;font-size:9px}.audit-row p{max-width:380px;margin:3px 0 0;overflow:hidden;color:#4d5b6f;font-size:8px;text-overflow:ellipsis;white-space:nowrap}.audit-row time{color:#546175;font:500 8px var(--font-mono);text-align:right}.method-chip{min-height:23px;display:grid;place-items:center;color:var(--cyan);border:1px solid rgba(88,224,255,.15);border-radius:6px;background:rgba(88,224,255,.055);font:700 7px var(--font-mono)}.method-chip.is-delete{color:var(--red);border-color:rgba(255,98,125,.16);background:rgba(255,98,125,.06)}.method-chip.is-post,.method-chip.is-put{color:var(--amber);border-color:rgba(255,180,74,.16);background:rgba(255,180,74,.06)}.response-code{color:var(--green);font:600 9px var(--font-mono)}.response-code.is-error{color:var(--red)}.compact-empty{min-height:190px}.compact-loading{min-height:220px}.access-panel{display:flex;align-items:center;gap:20px;min-height:245px;padding:32px}.access-panel>span{width:60px;height:60px;display:grid;place-items:center;color:#657388;border:1px solid var(--line);border-radius:18px;background:rgba(255,255,255,.025)}.access-panel small{color:#505d70;font:600 8px var(--font-mono);letter-spacing:.12em}.access-panel h2{margin:7px 0 8px;color:var(--text-strong);font-size:17px}.access-panel p{max-width:520px;margin:0;color:var(--text-muted);font-size:11px;line-height:1.7}
@media(max-width:1300px){.ops-ribbon{grid-template-columns:minmax(250px,1.4fr) repeat(3,1fr)}.ops-ribbon>div:nth-child(5){display:none}.ops-topology{grid-template-columns:1fr}.event-panel{min-height:310px}.ops-lower{grid-template-columns:1fr}.remote-grid{grid-template-columns:repeat(4,minmax(0,1fr))}}
@media(max-width:860px){.ops-ribbon{grid-template-columns:1.4fr 1fr 1fr}.ops-ribbon>div:nth-child(4){display:none}.probe-grid{grid-template-columns:1fr}.scan-panel__header{flex-direction:column}.scan-summary{width:100%;overflow:auto}.scan-summary>span:first-child{padding-left:0;border-left:0}.scan-toolbar{align-items:flex-start;flex-direction:column}.folder-selector{width:100%}.remote-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.audit-row{grid-template-columns:50px minmax(0,1fr) 38px}.audit-row time{display:none}}
@media(max-width:560px){.stream-pill{display:none}.ops-ribbon{grid-template-columns:1fr 1fr}.ops-ribbon>div:nth-child(3){display:none}.ops-ribbon__primary strong{font-size:12px}.core-orb{width:38px;height:38px}.remote-grid{grid-template-columns:1fr}.scan-summary>span{min-width:90px;padding:0 12px}.health-footer{align-items:flex-start;flex-direction:column}.event-item{grid-template-columns:20px 6px 1fr}.event-item time{display:none}}

/* Theme-aware contrast normalization. */
.stream-pill { color: var(--green); }
.stream-pill.is-reconnecting { color: var(--amber); }
.mini-spinner.is-dark { border-color: rgba(var(--cyan-rgb),.22); border-top-color: var(--text-on-accent); }
.ops-ribbon { background: var(--success-summary-background); box-shadow: var(--shadow-card); }
.ops-ribbon.is-degraded { background: var(--danger-summary-background); }
.ops-ribbon.is-checking { background: var(--warning-summary-background); }
.ops-ribbon__primary strong { color: var(--text-strong); }
.ops-ribbon__primary small { color: var(--green); }
.ops-ribbon > div > strong small,
.ops-ribbon__stamp,
.probe-card small,
.health-footer,
.event-index,
.event-item p,
.event-item time,
.event-footer,
.scan-toolbar > span,
.folder-selector > .ui-icon,
.scan-identity small,
.date-cell small,
.remote-card__number,
.remote-card small,
.audit-row p,
.audit-row time,
.access-panel > span,
.access-panel small { color: var(--text-muted); }
.probe-card,
.remote-card { background: var(--surface-soft); }
.probe-card.is-violet .probe-card__icon,
.event-item.is-violet,
.remote-card__icon { color: var(--violet-text); }
.event-item strong,
.date-cell strong,
.audit-row strong { color: var(--text); }
.scan-toolbar { background: var(--surface-inset); }
.muted-check { color: var(--green); }
.audit-row { border-bottom-color: var(--row-line); }
.access-panel > span { background: var(--surface-soft); }
</style>
