<template>
  <div class="page-shell dashboard-page">
    <header class="page-header">
      <div class="page-heading">
        <span class="eyebrow">LIVE OPERATIONS</span>
        <h1 class="page-title">运行总览</h1>
        <p class="page-description">扫描节点、任务队列与传输结果的统一态势视图。</p>
      </div>
      <div class="page-actions">
        <div class="segmented" aria-label="统计周期">
          <button v-for="option in dayOptions" :key="option" type="button" :class="{ 'is-active': days === option }" @click="selectDays(option)">
            {{ option }}D
          </button>
        </div>
        <button class="ui-button" type="button" :disabled="loading" @click="loadDashboard">
          <UiIcon name="refresh" :size="16" /> 刷新数据
        </button>
      </div>
    </header>

    <div class="signal-ribbon">
      <div class="signal-ribbon__title">
        <span class="signal-dot" />
        <div><strong>系统链路正常</strong><small>ALL SERVICES OPERATIONAL</small></div>
      </div>
      <div class="signal-ribbon__item"><span>监控目录</span><strong>{{ overview.watch_folder_count || 0 }}</strong></div>
      <div class="signal-ribbon__item"><span>24H 完成</span><strong>{{ overview.recent_24h_completed || 0 }}</strong></div>
      <div class="signal-ribbon__item is-alert"><span>24H 异常</span><strong>{{ overview.recent_24h_failed || 0 }}</strong></div>
      <div class="signal-ribbon__scan"><UiIcon name="radar" :size="20" /> LIVE SYNC</div>
    </div>

    <div v-if="loading && !data" class="ui-panel loading-layer">
      <div class="loading-indicator"><span class="spinner" />SYNCING TELEMETRY</div>
    </div>

    <template v-else-if="data">
      <section class="metric-grid" aria-label="关键指标">
        <article v-for="metric in metrics" :key="metric.key" class="metric-card" :class="`is-${metric.tone}`">
          <div class="metric-card__top">
            <span class="metric-card__icon"><UiIcon :name="metric.icon" :size="19" /></span>
            <span class="metric-card__code">{{ metric.code }}</span>
          </div>
          <strong>{{ metric.value }}</strong>
          <div class="metric-card__bottom">
            <span>{{ metric.label }}</span>
            <small>{{ metric.caption }}</small>
          </div>
          <span class="metric-card__beam" />
        </article>
      </section>

      <section class="dashboard-primary">
        <article class="ui-panel trend-panel">
          <div class="panel-header">
            <div><h2 class="panel-title">传输趋势</h2><p class="panel-subtitle">最近 {{ days }} 天完成与失败任务走势</p></div>
            <div class="chart-legend"><span class="is-success">完成</span><span class="is-failed">失败</span></div>
          </div>
          <div v-if="trend.length" class="trend-chart">
            <div class="trend-y-labels"><span>{{ trendMax }}</span><span>{{ Math.round(trendMax / 2) }}</span><span>0</span></div>
            <svg viewBox="0 0 720 238" preserveAspectRatio="none" role="img" aria-label="任务完成和失败趋势图">
              <defs>
                <linearGradient id="successArea" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0" stop-color="#58e0ff" stop-opacity=".22" />
                  <stop offset="1" stop-color="#58e0ff" stop-opacity="0" />
                </linearGradient>
              </defs>
              <g class="chart-grid"><line v-for="y in [20, 79, 138, 197]" :key="y" x1="0" :y1="y" x2="720" :y2="y" /></g>
              <polygon :points="successAreaPoints" fill="url(#successArea)" />
              <polyline :points="successPoints" class="line-success" />
              <polyline :points="failedPoints" class="line-failed" />
              <g class="point-group is-success">
                <circle v-for="(point, index) in successPointList" :key="index" :cx="point.x" :cy="point.y" r="3.4" />
              </g>
              <g class="point-group is-failed">
                <circle v-for="(point, index) in failedPointList" :key="index" :cx="point.x" :cy="point.y" r="3" />
              </g>
            </svg>
            <div class="trend-dates">
              <span v-for="(item, index) in trend" :key="item.date" :class="{ 'is-hidden': !showTrendLabel(index) }">{{ shortDate(item.date) }}</span>
            </div>
          </div>
          <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="activity" /></span><strong>暂无趋势数据</strong><p>完成传输后将在这里形成趋势。</p></div></div>
          <div class="trend-summary">
            <div><span>周期传输量</span><strong>{{ formatBytes(periodBytes) }}</strong></div>
            <div><span>周期完成</span><strong>{{ periodCompleted }}</strong></div>
            <div><span>周期失败</span><strong class="danger-text">{{ periodFailed }}</strong></div>
          </div>
        </article>

        <article class="ui-panel status-panel">
          <div class="panel-header">
            <div><h2 class="panel-title">队列构成</h2><p class="panel-subtitle">全部任务实时状态分布</p></div>
            <span class="panel-code">QUEUE / MATRIX</span>
          </div>
          <div class="status-orbit">
            <div class="completion-ring" :style="ringStyle">
              <div><strong>{{ completionRate }}%</strong><span>完成率</span></div>
            </div>
            <span class="status-orbit__ring" />
          </div>
          <div class="status-bars">
            <div v-for="item in statusItems" :key="item.status" class="status-row">
              <div><StatusBadge :status="item.status" :label="item.label" /><strong>{{ item.count }}</strong></div>
              <span class="status-track"><i :class="`is-${item.status}`" :style="{ width: statusWidth(item.count) }" /></span>
            </div>
          </div>
        </article>
      </section>

      <section class="ui-panel folder-panel">
        <div class="panel-header">
          <div><h2 class="panel-title">目录传输矩阵</h2><p class="panel-subtitle">各监听目录的任务分布与累计流量</p></div>
          <router-link class="ui-button is-small" to="/folders">管理目录 <UiIcon name="chevronRight" :size="14" /></router-link>
        </div>
        <div v-if="folderItems.length" class="data-table-shell">
          <table class="data-table folder-table">
            <thead><tr><th>监听目录</th><th>任务</th><th>完成</th><th>运行</th><th>待处理</th><th>失败</th><th>传输文件</th><th>上传量</th></tr></thead>
            <tbody>
              <tr v-for="folder in folderItems" :key="folder.watch_folder_id">
                <td><div class="folder-identity"><span><UiIcon name="folder" :size="16" /></span><div><strong>{{ folder.watch_folder_name || '未命名目录' }}</strong><small>#{{ folder.watch_folder_id }}</small></div></div></td>
                <td class="mono text-strong">{{ folder.task_count }}</td>
                <td class="success-text mono">{{ folder.success_count }}</td>
                <td class="cyan-text mono">{{ folder.running_count }}</td>
                <td class="amber-text mono">{{ folder.pending_count }}</td>
                <td class="danger-text mono">{{ folder.failed_count }}</td>
                <td class="mono">{{ folder.uploaded_files }}</td>
                <td class="mono text-strong">{{ formatBytes(folder.uploaded_bytes) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="folder" /></span><strong>暂无目录数据</strong><p>创建监听目录后将在这里展示传输统计。</p></div></div>
      </section>

      <section class="activity-grid">
        <article class="ui-panel activity-panel">
          <div class="panel-header"><div><h2 class="panel-title">最近活动</h2><p class="panel-subtitle">最新完成或失败的传输任务</p></div><span class="panel-code">LAST / 10</span></div>
          <div v-if="recentTasks.length" class="activity-list">
            <div v-for="task in recentTasks" :key="taskId(task)" class="activity-item">
              <span class="activity-item__icon" :class="`is-${taskStatus(task)}`"><UiIcon :name="taskStatus(task) === 'success' ? 'check' : 'alert'" :size="16" /></span>
              <div class="activity-item__main"><strong :title="taskFileName(task)">{{ taskFileName(task) }}</strong><span>{{ taskFolderName(task) || '未归属目录' }}</span></div>
              <StatusBadge :status="taskStatus(task)" />
              <time>{{ relativeDate(taskFinishedAt(task)) }}</time>
            </div>
          </div>
          <div v-else class="empty-state"><div><strong>暂无最近任务</strong></div></div>
        </article>

        <article class="ui-panel failures-panel">
          <div class="panel-header"><div><h2 class="panel-title">异常追踪</h2><p class="panel-subtitle">需要关注的最近失败任务</p></div><span class="failure-count">{{ failedTasks.length }}</span></div>
          <div v-if="failedTasks.length" class="failure-list">
            <div v-for="task in failedTasks.slice(0, 6)" :key="taskId(task)" class="failure-item">
              <span class="failure-item__line" />
              <div><strong>{{ taskFileName(task) }}</strong><p :title="taskError(task)">{{ taskError(task) || '未记录错误详情' }}</p></div>
              <time>{{ relativeDate(taskFinishedAt(task)) }}</time>
            </div>
          </div>
          <div v-else class="all-clear"><span><UiIcon name="shield" :size="24" /></span><strong>当前没有失败任务</strong><p>传输链路运行稳定。</p></div>
        </article>
      </section>
    </template>

    <div v-else class="ui-panel empty-state dashboard-error">
      <div><span class="empty-state__icon"><UiIcon name="alert" /></span><strong>无法获取运行数据</strong><p>请检查 API 服务或稍后重试。</p><button class="ui-button is-primary" type="button" @click="loadDashboard">重新连接</button></div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue';
import UiIcon from '../components/UiIcon.vue';
import StatusBadge from '../components/StatusBadge.vue';
import { getDashboard } from '../api/analytics';
import { errorText, toast } from '../composables/useUi';

const dayOptions = [7, 14, 30];
const days = ref(7);
const loading = ref(false);
const data = ref(null);

const overview = computed(() => data.value?.overview || {});
const trend = computed(() => data.value?.by_time || []);
const folderItems = computed(() => data.value?.by_watch_folder || []);
const recentTasks = computed(() => data.value?.items?.recent_tasks || []);
const failedTasks = computed(() => data.value?.items?.failed_tasks || []);
const statusItems = computed(() => {
  if (data.value?.by_status?.length) return data.value.by_status;
  const o = overview.value;
  return [
    ['pending', o.task_pending], ['running', o.task_running], ['success', o.task_success],
    ['failed', o.task_failed], ['paused', o.task_paused], ['canceled', o.task_canceled]
  ].map(([status, count]) => ({ status, count: count || 0 }));
});

const metrics = computed(() => [
  { key: 'total', label: '任务总量', caption: 'ALL TASKS', value: number(overview.value.task_total), icon: 'layers', tone: 'cyan', code: 'M-01' },
  { key: 'running', label: '正在传输', caption: 'ACTIVE NOW', value: number(overview.value.task_running), icon: 'upload', tone: 'green', code: 'M-02' },
  { key: 'pending', label: '等待队列', caption: 'PENDING', value: number(overview.value.task_pending), icon: 'clock', tone: 'amber', code: 'M-03' },
  { key: 'failed', label: '异常任务', caption: 'REQUIRES ACTION', value: number(overview.value.task_failed), icon: 'alert', tone: 'red', code: 'M-04' },
  { key: 'traffic', label: '累计上传', caption: `${number(overview.value.uploaded_files_total)} FILES`, value: formatBytes(overview.value.uploaded_bytes_total), icon: 'database', tone: 'violet', code: 'M-05' }
]);

const periodCompleted = computed(() => trend.value.reduce((sum, item) => sum + Number(item.completed_count || 0), 0));
const periodFailed = computed(() => trend.value.reduce((sum, item) => sum + Number(item.failed_count || 0), 0));
const periodBytes = computed(() => trend.value.reduce((sum, item) => sum + Number(item.uploaded_bytes || 0), 0));
const trendMax = computed(() => Math.max(1, ...trend.value.flatMap((item) => [Number(item.completed_count || 0), Number(item.failed_count || 0)])));
const statusMax = computed(() => Math.max(1, ...statusItems.value.map((item) => Number(item.count || 0))));
const completionRate = computed(() => {
  const total = Number(overview.value.task_total || 0);
  return total ? Math.round((Number(overview.value.task_success || 0) / total) * 100) : 0;
});
const ringStyle = computed(() => ({ background: `conic-gradient(var(--cyan) 0 ${completionRate.value}%, rgba(88,224,255,.08) ${completionRate.value}% 100%)` }));

function pointList(key) {
  const count = trend.value.length;
  return trend.value.map((item, index) => ({
    x: count === 1 ? 360 : 18 + (index / (count - 1)) * 684,
    y: 207 - (Number(item[key] || 0) / trendMax.value) * 177
  }));
}
const successPointList = computed(() => pointList('completed_count'));
const failedPointList = computed(() => pointList('failed_count'));
const successPoints = computed(() => successPointList.value.map((point) => `${point.x},${point.y}`).join(' '));
const failedPoints = computed(() => failedPointList.value.map((point) => `${point.x},${point.y}`).join(' '));
const successAreaPoints = computed(() => successPointList.value.length ? `18,207 ${successPoints.value} 702,207` : '');

function selectDays(value) { days.value = value; loadDashboard(); }
function number(value) { return new Intl.NumberFormat('zh-CN').format(Number(value || 0)); }
function statusWidth(value) { return `${Math.max(2, (Number(value || 0) / statusMax.value) * 100)}%`; }
function showTrendLabel(index) { const count = trend.value.length; return count <= 10 || index === 0 || index === count - 1 || index % Math.ceil(count / 7) === 0; }
function shortDate(value) { const date = new Date(value); return Number.isNaN(date.getTime()) ? String(value).slice(5) : `${date.getMonth() + 1}/${date.getDate()}`; }
function formatBytes(bytes) {
  const value = Number(bytes || 0);
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index > 1 ? 2 : 0)} ${units[index]}`;
}
function taskId(task) { return task.ID ?? task.id; }
function taskFileName(task) { return task.FileName ?? task.file_name ?? '未命名文件'; }
function taskFolderName(task) { return task.WatchFolderName ?? task.watch_folder_name; }
function taskStatus(task) { return task.Status ?? task.status ?? 'pending'; }
function taskFinishedAt(task) { return task.FinishedAt ?? task.finished_at; }
function taskError(task) { return task.ErrorMsg ?? task.error_message ?? ''; }
function relativeDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const delta = Date.now() - date.getTime();
  if (delta < 60_000) return '刚刚';
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)} 分钟前`;
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)} 小时前`;
  return `${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

async function loadDashboard() {
  loading.value = true;
  try {
    data.value = await getDashboard(days.value);
  } catch (error) {
    if (!data.value) data.value = null;
    toast(errorText(error, '运行数据加载失败'), 'error');
  } finally {
    loading.value = false;
  }
}

onMounted(loadDashboard);
</script>

<style scoped>
.dashboard-page { display: flex; flex-direction: column; gap: 22px; }
.dashboard-page .page-header { margin-bottom: 0; }
.signal-ribbon { position: relative; z-index: 1; min-height: 72px; display: grid; grid-template-columns: minmax(230px,1.5fr) repeat(3,minmax(100px,.6fr)) auto; align-items: center; gap: 0; padding: 0 20px; overflow: hidden; border: 1px solid rgba(61,225,162,.14); border-radius: 16px; background: linear-gradient(90deg, rgba(61,225,162,.065), rgba(14,23,34,.84) 40%, rgba(88,224,255,.035)); box-shadow: 0 18px 45px rgba(0,0,0,.18); }
.signal-ribbon::after { content: ''; position: absolute; inset: 0; pointer-events: none; background: repeating-linear-gradient(90deg, transparent 0 35px, rgba(61,225,162,.018) 36px); }
.signal-ribbon__title { display: flex; align-items: center; gap: 13px; }
.signal-dot { width: 10px; height: 10px; border-radius: 50%; background: var(--green); box-shadow: 0 0 0 6px rgba(61,225,162,.08),0 0 16px var(--green); }
.signal-ribbon__title strong, .signal-ribbon__title small { display: block; }.signal-ribbon__title strong { color: #dffcf2; font-size: 13px; }.signal-ribbon__title small { margin-top: 4px; color: #466b5e; font: 500 8px var(--font-mono); letter-spacing: .1em; }
.signal-ribbon__item { padding: 4px 22px; border-left: 1px solid var(--line); }.signal-ribbon__item span, .signal-ribbon__item strong { display: block; }.signal-ribbon__item span { color: var(--text-muted); font-size: 9px; }.signal-ribbon__item strong { margin-top: 4px; color: var(--text-strong); font: 600 16px var(--font-mono); }.signal-ribbon__item.is-alert strong { color: var(--red); }
.signal-ribbon__scan { display: flex; align-items: center; gap: 8px; padding-left: 22px; color: var(--cyan); font: 600 9px var(--font-mono); letter-spacing: .12em; }
.signal-ribbon__scan .ui-icon { animation: spin 5s linear infinite; }
.metric-grid { position: relative; z-index: 1; display: grid; grid-template-columns: repeat(5,minmax(160px,1fr)); gap: 14px; }
.metric-card { position: relative; min-height: 164px; display: flex; flex-direction: column; overflow: hidden; padding: 18px; border: 1px solid var(--line); border-radius: 17px; background: linear-gradient(145deg, rgba(20,29,43,.9), rgba(10,16,25,.9)); box-shadow: 0 18px 45px rgba(0,0,0,.17); transition: transform .18s ease,border-color .18s ease; }
.metric-card:hover { transform: translateY(-3px); border-color: rgba(88,224,255,.23); }.metric-card__top { display: flex; align-items: center; justify-content: space-between; }.metric-card__icon { width: 36px; height: 36px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.17); border-radius: 11px; background: rgba(88,224,255,.07); }.metric-card__code { color: #435064; font: 500 8px var(--font-mono); letter-spacing: .1em; }.metric-card > strong { margin-top: 22px; color: var(--text-strong); font: 600 clamp(22px,2.5vw,31px)/1 var(--font-mono); letter-spacing: -.04em; }.metric-card__bottom { display: flex; align-items: flex-end; justify-content: space-between; gap: 8px; margin-top: auto; }.metric-card__bottom span { color: #9eabba; font-size: 11px; }.metric-card__bottom small { color: #455165; font: 500 8px var(--font-mono); }.metric-card__beam { position: absolute; inset: auto 18px 0; height: 1px; background: linear-gradient(90deg, transparent,var(--cyan),transparent); opacity: .55; }
.metric-card.is-green .metric-card__icon { color: var(--green); border-color: rgba(61,225,162,.18); background: rgba(61,225,162,.07); }.metric-card.is-green .metric-card__beam { background: linear-gradient(90deg,transparent,var(--green),transparent); }.metric-card.is-amber .metric-card__icon { color: var(--amber); border-color: rgba(255,180,74,.18); background: rgba(255,180,74,.07); }.metric-card.is-amber .metric-card__beam { background: linear-gradient(90deg,transparent,var(--amber),transparent); }.metric-card.is-red .metric-card__icon { color: var(--red); border-color: rgba(255,98,125,.18); background: rgba(255,98,125,.07); }.metric-card.is-red .metric-card__beam { background: linear-gradient(90deg,transparent,var(--red),transparent); }.metric-card.is-violet .metric-card__icon { color: #a997ff; border-color: rgba(140,118,255,.2); background: rgba(140,118,255,.08); }.metric-card.is-violet .metric-card__beam { background: linear-gradient(90deg,transparent,var(--violet),transparent); }
.dashboard-primary { position: relative; z-index: 1; display: grid; grid-template-columns: minmax(0,1.7fr) minmax(330px,.7fr); gap: 18px; }.trend-panel,.status-panel { min-height: 450px; }.chart-legend { display: flex; gap: 18px; font-size: 10px; }.chart-legend span { display: flex; align-items: center; gap: 7px; color: var(--text-muted); }.chart-legend span::before { content: ''; width: 16px; height: 2px; background: currentColor; box-shadow: 0 0 7px currentColor; }.chart-legend .is-success { color: var(--cyan); }.chart-legend .is-failed { color: var(--red); }
.trend-chart { position: relative; z-index: 1; height: 270px; padding: 18px 24px 0 54px; }.trend-chart svg { width: 100%; height: 218px; overflow: visible; }.chart-grid line { stroke: rgba(150,176,210,.1); stroke-width: 1; stroke-dasharray: 4 7; }.line-success,.line-failed { fill: none; stroke-width: 2.2; vector-effect: non-scaling-stroke; }.line-success { stroke: var(--cyan); filter: drop-shadow(0 0 5px rgba(88,224,255,.4)); }.line-failed { stroke: var(--red); stroke-width: 1.8; }.point-group circle { stroke-width: 2; vector-effect: non-scaling-stroke; }.point-group.is-success circle { fill: #0d1721; stroke: var(--cyan); }.point-group.is-failed circle { fill: #0d1721; stroke: var(--red); }.trend-y-labels { position: absolute; top: 26px; bottom: 34px; left: 18px; display: flex; flex-direction: column; justify-content: space-between; color: #4b586b; font: 500 8px var(--font-mono); }.trend-dates { display: flex; justify-content: space-between; color: #4b586b; font: 500 8px var(--font-mono); }.trend-dates span.is-hidden { visibility: hidden; }.trend-summary { position: relative; z-index: 1; display: grid; grid-template-columns: repeat(3,1fr); margin: 0 22px 22px; border: 1px solid var(--line); border-radius: 12px; background: rgba(4,8,14,.35); }.trend-summary div { padding: 12px 15px; border-right: 1px solid var(--line); }.trend-summary div:last-child { border-right: 0; }.trend-summary span,.trend-summary strong { display: block; }.trend-summary span { color: var(--text-muted); font-size: 9px; }.trend-summary strong { margin-top: 5px; color: var(--text-strong); font: 600 13px var(--font-mono); }
.status-orbit { position: relative; height: 178px; display: grid; place-items: center; }.completion-ring { position: relative; z-index: 1; width: 128px; height: 128px; display: grid; place-items: center; border-radius: 50%; box-shadow: 0 0 40px rgba(88,224,255,.07); }.completion-ring::after { content: ''; position: absolute; inset: 9px; border-radius: 50%; background: #101722; box-shadow: inset 0 0 24px rgba(0,0,0,.3); }.completion-ring div { position: relative; z-index: 1; text-align: center; }.completion-ring strong,.completion-ring span { display: block; }.completion-ring strong { color: var(--text-strong); font: 600 25px var(--font-mono); }.completion-ring span { margin-top: 4px; color: var(--text-muted); font-size: 9px; }.status-orbit__ring { position: absolute; width: 158px; height: 158px; border: 1px dashed rgba(88,224,255,.13); border-radius: 50%; animation: spin 18s linear infinite; }.status-bars { position: relative; z-index: 1; display: grid; gap: 11px; padding: 2px 22px 22px; }.status-row > div { display: flex; align-items: center; justify-content: space-between; }.status-row strong { color: var(--text); font: 600 11px var(--font-mono); }.status-track { height: 3px; display: block; margin-top: 7px; overflow: hidden; border-radius: 3px; background: rgba(255,255,255,.04); }.status-track i { height: 100%; display: block; border-radius: inherit; background: var(--text-muted); }.status-track i.is-running { background: var(--cyan); box-shadow: 0 0 8px var(--cyan); }.status-track i.is-success { background: var(--green); }.status-track i.is-failed { background: var(--red); }.status-track i.is-pending { background: var(--amber); }.status-track i.is-paused { background: var(--violet); }
.folder-panel,.activity-grid { position: relative; z-index: 1; }.folder-panel .ui-button { text-decoration: none; }.folder-identity { display: flex; align-items: center; gap: 10px; min-width: 175px; }.folder-identity > span { width: 32px; height: 32px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.14); border-radius: 9px; background: rgba(88,224,255,.06); }.folder-identity strong,.folder-identity small { display: block; }.folder-identity strong { max-width: 180px; overflow: hidden; color: var(--text-strong); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.folder-identity small { margin-top: 3px; color: #4a5668; font: 500 8px var(--font-mono); }.success-text { color: var(--green)!important; }.cyan-text { color: var(--cyan)!important; }.amber-text { color: var(--amber)!important; }.danger-text { color: var(--red)!important; }
.activity-grid { display: grid; grid-template-columns: 1.15fr .85fr; gap: 18px; }.activity-list,.failure-list { position: relative; z-index: 1; }.activity-item { display: grid; grid-template-columns: 34px minmax(0,1fr) auto 75px; align-items: center; gap: 11px; padding: 13px 20px; border-bottom: 1px solid rgba(150,176,210,.07); }.activity-item:last-child { border-bottom: 0; }.activity-item__icon { width: 32px; height: 32px; display: grid; place-items: center; color: var(--green); border: 1px solid rgba(61,225,162,.16); border-radius: 9px; background: rgba(61,225,162,.06); }.activity-item__icon.is-failed { color: var(--red); border-color: rgba(255,98,125,.16); background: rgba(255,98,125,.06); }.activity-item__main { min-width: 0; }.activity-item__main strong,.activity-item__main span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.activity-item__main strong { color: var(--text); font-size: 11px; }.activity-item__main span { margin-top: 3px; color: var(--text-muted); font-size: 9px; }.activity-item time,.failure-item time { color: #4c5869; font: 500 8px var(--font-mono); text-align: right; }.failure-item { position: relative; display: grid; grid-template-columns: 2px minmax(0,1fr) 74px; gap: 12px; padding: 15px 20px; border-bottom: 1px solid rgba(150,176,210,.07); }.failure-item:last-child { border-bottom: 0; }.failure-item__line { height: 100%; min-height: 34px; border-radius: 2px; background: var(--red); box-shadow: 0 0 8px rgba(255,98,125,.4); }.failure-item strong { display: block; overflow: hidden; color: #e1c8ce; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.failure-item p { margin: 4px 0 0; overflow: hidden; color: #835b65; font: 500 9px var(--font-mono); text-overflow: ellipsis; white-space: nowrap; }.failure-count { min-width: 29px; height: 25px; display: grid; place-items: center; color: var(--red); border: 1px solid rgba(255,98,125,.2); border-radius: 8px; background: rgba(255,98,125,.07); font: 600 10px var(--font-mono); }.all-clear { min-height: 205px; display: grid; place-items: center; align-content: center; text-align: center; }.all-clear > span { width: 50px; height: 50px; display: grid; place-items: center; color: var(--green); border: 1px solid rgba(61,225,162,.18); border-radius: 16px; background: rgba(61,225,162,.06); }.all-clear strong { margin-top: 13px; color: #acd9c8; font-size: 12px; }.all-clear p { margin: 5px 0 0; color: #4d6c61; font-size: 10px; }.dashboard-error .ui-button { margin-top: 18px; }
@media (max-width: 1320px) { .metric-grid { grid-template-columns: repeat(3,1fr); }.dashboard-primary { grid-template-columns: 1fr; }.status-panel { display: grid; grid-template-columns: 1fr 220px 1.2fr; min-height: auto; }.status-panel .panel-header { grid-row: 1; grid-column: 1/-1; }.activity-grid { grid-template-columns: 1fr; } }
@media (max-width: 850px) { .signal-ribbon { grid-template-columns: 1fr auto; }.signal-ribbon__item { display: none; }.metric-grid { grid-template-columns: repeat(2,1fr); }.status-panel { display: block; }.trend-summary { margin: 0 12px 14px; }.activity-item { grid-template-columns: 34px minmax(0,1fr) auto; }.activity-item time { display: none; } }
@media (max-width: 560px) { .metric-grid { grid-template-columns: 1fr; }.metric-card { min-height: 145px; }.signal-ribbon__scan { display: none; }.trend-chart { padding-left: 38px; padding-right: 10px; }.trend-y-labels { left: 10px; }.trend-summary { grid-template-columns: 1fr; }.trend-summary div { border-right: 0; border-bottom: 1px solid var(--line); }.activity-item { grid-template-columns: 32px minmax(0,1fr); }.activity-item .status-badge { display: none; } }
</style>
