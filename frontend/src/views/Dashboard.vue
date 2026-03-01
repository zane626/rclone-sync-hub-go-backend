<template>
  <div class="dashboard-page">
    <div class="dashboard-toolbar">
      <h1 class="dashboard-title">数据分析</h1>
      <n-select
        v-model:value="days"
        :options="daysOptions"
        style="width: 120px"
        size="small"
        @update:value="loadDashboard"
      />
    </div>

    <n-spin :show="loading">
      <template v-if="data">
        <!-- 概览卡片 -->
        <section class="dashboard-section">
          <h2 class="app-section-title">概览</h2>
          <div class="overview-cards">
            <div class="stat-card">
              <div class="stat-card__accent stat-total" />
              <div class="stat-card__body">
                <span class="stat-card__label">任务总数</span>
                <span class="stat-card__value">{{ data.overview.task_total }}</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-pending" />
              <div class="stat-card__body">
                <span class="stat-card__label">待上传</span>
                <span class="stat-card__value">{{ data.overview.task_pending }}</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-running" />
              <div class="stat-card__body">
                <span class="stat-card__label">上传中</span>
                <span class="stat-card__value">{{ data.overview.task_running }}</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-success" />
              <div class="stat-card__body">
                <span class="stat-card__label">上传完成</span>
                <span class="stat-card__value">{{ data.overview.task_success }}</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-failed" />
              <div class="stat-card__body">
                <span class="stat-card__label">上传失败</span>
                <span class="stat-card__value">{{ data.overview.task_failed }}</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-paused" />
              <div class="stat-card__body">
                <span class="stat-card__label">已暂停</span>
                <span class="stat-card__value">{{ data.overview.task_paused }}</span>
              </div>
            </div>
            <div class="stat-card stat-wide">
              <div class="stat-card__accent stat-traffic" />
              <div class="stat-card__body">
                <span class="stat-card__label">累计上传</span>
                <span class="stat-card__value">{{ formatBytes(data.overview.uploaded_bytes_total) }}</span>
                <span class="stat-card__unit">共 {{ data.overview.uploaded_files_total }} 个文件</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-success" />
              <div class="stat-card__body">
                <span class="stat-card__label">监听文件夹</span>
                <span class="stat-card__value">{{ data.overview.watch_folder_count }}</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-success" />
              <div class="stat-card__body">
                <span class="stat-card__label">近 24h 完成</span>
                <span class="stat-card__value">{{ data.overview.recent_24h_completed }}</span>
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-card__accent stat-failed" />
              <div class="stat-card__body">
                <span class="stat-card__label">近 24h 失败</span>
                <span class="stat-card__value">{{ data.overview.recent_24h_failed }}</span>
              </div>
            </div>
          </div>
        </section>

        <div class="dashboard-grid">
          <!-- 按状态分布（柱状） -->
          <section class="dashboard-section app-card">
            <h2 class="app-section-title">按状态分布</h2>
            <div class="bar-chart">
              <div
                v-for="item in data.by_status"
                :key="item.status"
                class="bar-row"
              >
                <span class="bar-label">{{ item.label }}</span>
                <div class="bar-track">
                  <div
                    class="bar-fill"
                    :class="'bar-' + item.status"
                    :style="{ width: barWidth(item) }"
                  />
                </div>
                <span class="bar-value">{{ item.count }}</span>
              </div>
            </div>
          </section>

          <!-- 按时间趋势（折线/表） -->
          <section class="dashboard-section app-card">
            <h2 class="app-section-title">按日趋势（最近 {{ days }} 天）</h2>
            <div class="app-table-wrap">
              <n-data-table
                :columns="byTimeColumns"
                :data="data.by_time"
                size="small"
                :pagination="false"
                :bordered="false"
                striped
              />
            </div>
          </section>
        </div>

        <!-- 按监听文件夹 -->
        <section class="dashboard-section">
          <h2 class="app-section-title">按监听文件夹</h2>
          <div class="app-table-wrap">
            <n-data-table
              :columns="byWatchFolderColumns"
              :data="data.by_watch_folder"
              size="small"
              :pagination="false"
              :bordered="false"
              striped
            />
          </div>
        </section>

        <div class="dashboard-grid">
          <!-- 最近任务 -->
          <section class="dashboard-section">
            <h2 class="app-section-title">最近完成/失败任务（10 条）</h2>
            <div class="app-table-wrap">
              <n-data-table
                :columns="recentTaskColumns"
                :data="data.items.recent_tasks || []"
                size="small"
                :pagination="false"
                :bordered="false"
                striped
              />
            </div>
          </section>

          <!-- 失败任务 -->
          <section class="dashboard-section">
            <h2 class="app-section-title">失败任务（20 条）</h2>
            <div class="app-table-wrap">
              <n-data-table
                :columns="failedTaskColumns"
                :data="data.items.failed_tasks || []"
                size="small"
                :pagination="false"
                :bordered="false"
                striped
              />
            </div>
          </section>
        </div>
      </template>
      <template v-else-if="!loading">
        <div class="dashboard-empty">暂无数据</div>
      </template>
    </n-spin>
  </div>
</template>

<script setup>
import { ref, onMounted, h } from 'vue';
import { NSelect, NSpin, NDataTable, NTag } from 'naive-ui';
import { getDashboard } from '../api/analytics';

const days = ref(7);
const loading = ref(false);
const data = ref(null);

const daysOptions = [
  { label: '最近 7 天', value: 7 },
  { label: '最近 14 天', value: 14 },
  { label: '最近 30 天', value: 30 }
];

function formatBytes(bytes) {
  if (bytes == null || bytes === 0) return '0 B';
  const g = 1024 * 1024 * 1024;
  const m = 1024 * 1024;
  const k = 1024;
  if (bytes >= g) return (bytes / g).toFixed(2) + ' GB';
  if (bytes >= m) return (bytes / m).toFixed(2) + ' MB';
  if (bytes >= k) return (bytes / k).toFixed(2) + ' KB';
  return bytes + ' B';
}

function formatDate(v) {
  if (!v) return '-';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return String(v);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  const h = String(d.getHours()).padStart(2, '0');
  const min = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  return `${y}-${m}-${day} ${h}:${min}:${s}`;
}

const statusType = (status) => {
  const map = { pending: 'default', running: 'success', success: 'info', failed: 'error', paused: 'warning' };
  return map[status] || 'default';
};

const maxStatusCount = ref(1);
function barWidth(item) {
  const max = maxStatusCount.value || 1;
  const p = Math.min(100, (item.count / max) * 100);
  return p + '%';
}

const byTimeColumns = [
  { title: '日期', key: 'date', width: 110 },
  { title: '完成数', key: 'completed_count', width: 90 },
  { title: '失败数', key: 'failed_count', width: 90 },
  {
    title: '上传量',
    key: 'uploaded_bytes',
    width: 100,
    render: (row) => formatBytes(row.uploaded_bytes)
  }
];

const byWatchFolderColumns = [
  { title: '监听文件夹', key: 'watch_folder_name', width: 160 },
  { title: '任务数', key: 'task_count', width: 80 },
  { title: '成功', key: 'success_count', width: 70 },
  { title: '失败', key: 'failed_count', width: 70 },
  { title: '待上传', key: 'pending_count', width: 80 },
  { title: '上传中', key: 'running_count', width: 80 },
  { title: '已暂停', key: 'paused_count', width: 80 },
  {
    title: '上传量',
    key: 'uploaded_bytes',
    width: 90,
    render: (row) => formatBytes(row.uploaded_bytes)
  },
  { title: '文件数', key: 'uploaded_files', width: 80 }
];

const recentTaskColumns = [
  { title: 'ID', key: 'id', width: 60 },
  { title: '文件名', key: 'file_name', ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => {
      const s = row.status || row.Status;
      return h(NTag, { size: 'small', type: statusType(s) }, () => s || '-');
    }
  },
  {
    title: '完成时间',
    key: 'finished_at',
    width: 160,
    render: (row) => formatDate(row.finished_at || row.FinishedAt)
  }
];

const failedTaskColumns = [
  { title: 'ID', key: 'id', width: 60 },
  { title: '文件名', key: 'file_name', ellipsis: { tooltip: true } },
  { title: '错误信息', key: 'error_message', ellipsis: { tooltip: true }, render: (row) => row.error_message || row.ErrorMsg || '-' },
  {
    title: '时间',
    key: 'finished_at',
    width: 160,
    render: (row) => formatDate(row.finished_at || row.FinishedAt)
  }
];

// 兼容后端 snake_case / PascalCase
function normalizeTask(t) {
  if (!t) return t;
  return {
    ...t,
    file_name: t.file_name ?? t.FileName,
    status: t.status ?? t.Status,
    finished_at: t.finished_at ?? t.FinishedAt,
    error_message: t.error_message ?? t.ErrorMsg
  };
}

async function loadDashboard() {
  loading.value = true;
  data.value = null;
  try {
    const res = await getDashboard(days.value);
    data.value = res;
    const list = res.by_status || [];
    const max = list.length ? Math.max(...list.map((x) => x.count), 1) : 1;
    maxStatusCount.value = max;
    if (data.value.items?.recent_tasks) {
      data.value.items.recent_tasks = data.value.items.recent_tasks.map(normalizeTask);
    }
    if (data.value.items?.failed_tasks) {
      data.value.items.failed_tasks = data.value.items.failed_tasks.map(normalizeTask);
    }
  } catch (e) {
    data.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  loadDashboard();
});
</script>

<style scoped>
.dashboard-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-page);
  min-height: calc(100vh - 56px - 2 * var(--space-page));
}

.dashboard-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}

.dashboard-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: #1e293b;
}

.dashboard-section {
  min-height: 0;
}

.overview-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 12px;
}

.stat-card {
  position: relative;
  background: var(--card-bg);
  border-radius: var(--radius-md);
  box-shadow: var(--card-shadow);
  border: 1px solid rgba(0, 0, 0, 0.04);
  overflow: hidden;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}

.stat-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--card-shadow-hover);
}

.stat-card.stat-wide {
  grid-column: span 2;
}

.stat-card__accent {
  position: absolute;
  top: 0;
  left: 0;
  width: 4px;
  height: 100%;
}

.stat-card__body {
  padding: 14px 16px 14px 20px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.stat-card__label {
  font-size: 12px;
  color: #64748b;
  font-weight: 500;
}

.stat-card__value {
  font-size: 22px;
  font-weight: 700;
  color: #1e293b;
  letter-spacing: -0.02em;
}

.stat-card__unit {
  font-size: 11px;
  color: #94a3b8;
}

.stat-total { background: linear-gradient(135deg, #64748b 0%, #475569 100%); }
.stat-pending { background: var(--stat-pending); }
.stat-running { background: var(--stat-running); }
.stat-success { background: var(--stat-success); }
.stat-traffic { background: var(--stat-traffic); }
.stat-failed { background: linear-gradient(135deg, #ef4444 0%, #dc2626 100%); }
.stat-paused { background: linear-gradient(135deg, #94a3b8 0%, #64748b 100%); }

.dashboard-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-page);
}

.dashboard-grid .dashboard-section.app-card {
  padding: var(--space-card);
}

.bar-chart {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.bar-row {
  display: grid;
  grid-template-columns: 90px 1fr 50px;
  align-items: center;
  gap: 10px;
}

.bar-label {
  font-size: 13px;
  color: #475569;
}

.bar-track {
  height: 20px;
  background: #f1f5f9;
  border-radius: 4px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  border-radius: 4px;
  min-width: 2px;
  transition: width 0.3s ease;
}

.bar-pending { background: var(--stat-pending); }
.bar-running { background: var(--stat-running); }
.bar-success { background: var(--stat-success); }
.bar-failed { background: linear-gradient(90deg, #ef4444, #dc2626); }
.bar-paused { background: #94a3b8; }

.bar-value {
  font-size: 13px;
  font-weight: 600;
  color: #334155;
  text-align: right;
}

.dashboard-empty {
  text-align: center;
  color: #94a3b8;
  padding: 48px;
}

@media (max-width: 1024px) {
  .dashboard-grid {
    grid-template-columns: 1fr;
  }

  .overview-cards {
    grid-template-columns: repeat(2, 1fr);
  }

  .stat-card.stat-wide {
    grid-column: span 2;
  }
}

@media (max-width: 600px) {
  .overview-cards {
    grid-template-columns: 1fr;
  }

  .stat-card.stat-wide {
    grid-column: span 1;
  }

  .bar-row {
    grid-template-columns: 70px 1fr 40px;
  }
}
</style>
