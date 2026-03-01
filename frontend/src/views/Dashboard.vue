<template>
  <div class="dashboard">
    <section class="dashboard-stats">
      <div class="stat-card stat-running">
        <div class="stat-card__accent" />
        <div class="stat-card__body">
          <span class="stat-card__label">运行中</span>
          <span class="stat-card__value">{{ runningTasks }}</span>
          <span class="stat-card__unit">任务</span>
        </div>
      </div>
      <div class="stat-card stat-pending">
        <div class="stat-card__accent" />
        <div class="stat-card__body">
          <span class="stat-card__label">待处理</span>
          <span class="stat-card__value">{{ pendingTasks }}</span>
          <span class="stat-card__unit">任务</span>
        </div>
      </div>
      <div class="stat-card stat-success">
        <div class="stat-card__accent" />
        <div class="stat-card__body">
          <span class="stat-card__label">今日成功</span>
          <span class="stat-card__value">{{ todayUploadedFiles }}</span>
          <span class="stat-card__unit">文件</span>
        </div>
      </div>
      <div class="stat-card stat-traffic">
        <div class="stat-card__accent" />
        <div class="stat-card__body">
          <span class="stat-card__label">累计上传</span>
          <span class="stat-card__value">{{ totalUploadedGB }}</span>
          <span class="stat-card__unit">GB</span>
        </div>
      </div>
    </section>

    <section class="dashboard-section">
      <h2 class="app-section-title">监听文件夹概览</h2>
      <div class="app-table-wrap">
        <n-data-table
          :columns="watchFolderColumns"
          :data="watchFolders"
          size="small"
          :pagination="false"
          :bordered="false"
          striped
        />
      </div>
    </section>

    <section class="dashboard-section dashboard-side">
      <h2 class="app-section-title">任务状态分布</h2>
      <div class="app-card status-cards">
        <div
          v-for="item in taskStatusSummary"
          :key="item.status"
          class="status-row"
        >
          <span class="status-row__label">{{ item.label }}</span>
          <n-tag :type="item.type" size="small" round>{{ item.count }}</n-tag>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import {
  NDataTable,
  NTag
} from 'naive-ui';
import { fetchWatchFolders } from '../api/watchFolders';
import { fetchTasks } from '../api/tasks';

const runningTasks = ref(0);
const pendingTasks = ref(0);
const todayUploadedFiles = ref(0);
const totalUploadedGB = ref(0);
const watchFolders = ref([]);
const taskStatusSummary = ref([
  { status: 'running', label: '运行中', type: 'success', count: 0 },
  { status: 'pending', label: '待处理', type: 'warning', count: 0 },
  { status: 'failed', label: '失败', type: 'error', count: 0 },
  { status: 'success', label: '成功', type: 'info', count: 0 }
]);

const watchFolderColumns = [
  { title: '名称', key: 'name' },
  { title: '本地路径', key: 'localPath' },
  { title: '状态', key: 'status' },
  { title: '已上传', key: 'uploadedFileCount' },
  { title: '失败', key: 'failedFileCount' }
];

onMounted(async () => {
  try {
    const wf = await fetchWatchFolders({ page: 1, page_size: 5 });
    const raw = wf.items || [];
    watchFolders.value = raw.map((item) => ({
      name: item.Name ?? item.name,
      localPath: item.LocalPath ?? item.localPath,
      status: item.Status ?? item.status,
      uploadedFileCount: item.UploadedFileCount ?? item.uploadedFileCount,
      failedFileCount: item.FailedFileCount ?? item.failedFileCount
    }));
  } catch (e) {
    watchFolders.value = [];
  }

  try {
    const tasksRes = await fetchTasks({ page: 1, page_size: 200 });
    const items = tasksRes.items || tasksRes.data || [];

    const statusCount = {
      running: 0,
      pending: 0,
      success: 0,
      failed: 0
    };
    let todayCount = 0;
    let totalBytes = 0;
    const todayStr = new Date().toISOString().slice(0, 10);

    items.forEach((t) => {
      const status = t.Status ?? t.status;
      if (statusCount[status] !== undefined) {
        statusCount[status] += 1;
      }
      const finishedAt = t.FinishedAt ?? t.finishedAt;
      if (status === 'success' && finishedAt && String(finishedAt).startsWith(todayStr)) {
        todayCount += 1;
      }
      const fileSize = t.FileSize ?? t.fileSize;
      if (fileSize) {
        totalBytes += fileSize;
      }
    });

    runningTasks.value = statusCount.running;
    pendingTasks.value = statusCount.pending;
    todayUploadedFiles.value = todayCount;
    totalUploadedGB.value = +(totalBytes / (1024 * 1024 * 1024)).toFixed(2);

    taskStatusSummary.value = taskStatusSummary.value.map((item) => ({
      ...item,
      count: statusCount[item.status] || 0
    }));
  } catch (e) {
    runningTasks.value = 0;
    pendingTasks.value = 0;
    todayUploadedFiles.value = 0;
    totalUploadedGB.value = 0;
  }
});
</script>

<style scoped>
.dashboard {
  display: grid;
  gap: var(--space-page);
  grid-template-columns: 1fr 320px;
  grid-template-rows: auto 1fr;
  grid-template-areas:
    'stats stats'
    'table  side';
  min-height: calc(100vh - 56px - 2 * var(--space-page));
}

.dashboard-stats {
  grid-area: stats;
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
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

.stat-card__accent {
  position: absolute;
  top: 0;
  left: 0;
  width: 4px;
  height: 100%;
}

.stat-card.stat-running .stat-card__accent { background: var(--stat-running); }
.stat-card.stat-pending .stat-card__accent { background: var(--stat-pending); }
.stat-card.stat-success .stat-card__accent { background: var(--stat-success); }
.stat-card.stat-traffic .stat-card__accent { background: var(--stat-traffic); }

.stat-card__body {
  padding: 18px 20px 18px 24px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.stat-card__label {
  font-size: 12px;
  color: #64748b;
  font-weight: 500;
}

.stat-card__value {
  font-size: 26px;
  font-weight: 700;
  color: #1e293b;
  letter-spacing: -0.02em;
}

.stat-card__unit {
  font-size: 12px;
  color: #94a3b8;
}

.dashboard-section {
  grid-area: table;
  min-height: 0;
}

.dashboard-side {
  grid-area: side;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.dashboard-side .status-cards {
  flex: 1;
  min-height: 0;
  padding: var(--space-card);
}

.status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 0;
  border-bottom: 1px solid #f1f5f9;
}

.status-row:last-child {
  border-bottom: none;
}

.status-row__label {
  font-size: 13px;
  color: #475569;
}

@media (max-width: 1200px) {
  .dashboard {
    grid-template-columns: 1fr;
    grid-template-areas:
      'stats'
      'table'
      'side';
  }

  .dashboard-stats {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 600px) {
  .dashboard-stats {
    grid-template-columns: 1fr;
  }
}
</style>
