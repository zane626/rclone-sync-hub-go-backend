<template>
  <div class="list-page">
    <div class="app-card list-toolbar">
      <div class="app-toolbar">
        <div class="app-toolbar-filters">
          <span class="filter-label">状态</span>
          <n-select
            v-model:value="filter.status"
            :options="statusOptions"
            placeholder="全部"
            style="width: 160px"
            size="small"
          />
          <n-input
            v-model:value="filter.keyword"
            placeholder="文件名或路径"
            clearable
            size="small"
            style="width: 220px"
          />
          <n-button size="small" type="primary" @click="handleSearch">查询</n-button>
          <n-button size="small" quaternary @click="handleReset">重置</n-button>
        </div>
        <div class="app-toolbar-actions">
          <n-button size="small" type="warning" secondary @click="handleBatchPause">批量暂停</n-button>
          <n-button size="small" type="info" secondary @click="handleBatchRetry">批量重试</n-button>
          <n-button size="small" type="error" secondary @click="handleBatchDelete">批量删除</n-button>
        </div>
      </div>
    </div>

    <div class="app-table-wrap">
      <n-data-table
        :columns="columns"
        :data="tableData"
        :bordered="false"
        :pagination="pagination"
        :row-key="rowKey"
        :scroll-x="1400"
        v-model:checked-row-keys="checkedRowKeys"
        striped
      />
    </div>

    <n-drawer v-model:show="logDrawerVisible" :width="1000" placement="right" class="log-drawer">
      <n-drawer-content :title="logDrawerTitle" closable>
        <n-spin :show="logLoading" class="log-spin">
          <div class="log-content">
            <template v-if="logListDisplay.length">
              <div
                v-for="(item, index) in logListDisplay"
                :key="index"
                class="log-line"
              >
                <span v-if="logTime(item)" class="log-time">{{ logTime(item) }}</span>
                <span v-if="logLevel(item)" class="log-level">{{ logLevel(item) }}</span>
                <span class="log-message">{{ formatLogMessage(item) }}</span>
              </div>
            </template>
            <div v-else-if="!logLoading" class="log-empty">暂无日志</div>
          </div>
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, h } from 'vue';
import {
  NButton,
  NDataTable,
  NSelect,
  NInput,
  NSpace,
  NTag,
  NProgress,
  NDrawer,
  NDrawerContent,
  NSpin,
  useMessage,
  useDialog
} from 'naive-ui';
import {
  fetchTasks,
  batchPauseTasks,
  batchRetryTasks,
  batchDeleteTasks,
  deleteTask,
  pauseTask,
  fetchTaskLogs
} from '../api/tasks';

const tableData = ref([]);

const statusOptions = [
  { label: '全部', value: null },
  { label: '待处理', value: 'pending' },
  { label: '运行中', value: 'running' },
  { label: '成功', value: 'success' },
  { label: '失败', value: 'failed' }
];

const filter = ref({
  status: null,
  keyword: ''
});

const pagination = ref({
  page: 1,
  pageSize: 20,
  itemCount: 0,
  showSizePicker: false,
  onChange: (page) => {
    pagination.value.page = page;
    loadData();
  }
});

const message = useMessage();
const dialog = useDialog();

const checkedRowKeys = ref([]);

const logDrawerVisible = ref(false);
const logDrawerTask = ref(null);
const logList = ref([]);
const logLoading = ref(false);

const logDrawerTitle = computed(() => {
  const t = logDrawerTask.value;
  return t ? `任务 #${t.id} 日志${t.fileName ? '：' + t.fileName : ''}` : '任务日志';
});

const logListDisplay = computed(() => {
  const list = logList.value;
  return Array.isArray(list) ? [...list].reverse() : [];
});

function rowKey(row) {
  return row.id;
}

function logTime(item) {
  const raw = item.created_at ?? item.CreatedAt ?? item.createdAt ?? '';
  if (!raw) return '';
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return raw;
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  const h = String(d.getHours()).padStart(2, '0');
  const min = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  return `${y}-${m}-${day} ${h}:${min}:${s}`;
}

function logLevel(item) {
  return item.level ?? item.Level ?? '';
}

function formatLogMessage(item) {
  if (item.message != null) return item.message;
  if (item.Message != null) return item.Message;
  if (item.msg != null) return item.msg;
  if (typeof item === 'string') return item;
  return JSON.stringify(item);
}

const columns = [
  { type: 'selection' },
  { title: '任务 ID', key: 'id', width: 80 },
  { title: '监听文件夹', key: 'watchFolderName', width: 140 },
  { title: '文件名', key: 'fileName', width: 200 },
  { title: '本地路径', key: 'localPath' },
  { title: 'remote', key: 'remoteName', width: 100 },
  { title: '远端路径', key: 'remotePath' },
  {
    title: '状态 / 进度',
    key: 'status',
    width: 220,
    render(row) {
      const map = {
        pending: { label: '待处理', type: 'default' },
        running: { label: '运行中', type: 'success' },
        success: { label: '成功', type: 'info' },
        failed: { label: '失败', type: 'error' }
      };
      const info = map[row.status] || { label: row.status, type: 'default' };
      return h(
        'div',
        { style: 'display: flex; flex-direction: column; gap: 4px; white-space: nowrap;' },
        [
          h(
            NTag,
            { size: 'small', type: info.type },
            { default: () => info.label }
          ),
          h(NProgress, { percentage: row.progress || 0, height: 8 })
        ]
      );
    }
  },
  {
    title: '速度 / 重试',
    key: 'speed',
    width: 140,
    render(row) {
      const mbps = row.speed ? (row.speed / (1024 * 1024)).toFixed(1) : '-';
      return h(
        'div',
        { style: 'display: flex; flex-direction: column; gap: 4px;' },
        [
          h('span', null, `${mbps} MB/s`),
          h('span', null, `重试：${row.retryCount || 0}`)
        ]
      );
    }
  },
  {
    title: '日志',
    key: 'logs',
    width: 80,
    render(row) {
      return h(
        NButton,
        {
          size: 'small',
          tertiary: true,
          type: 'primary',
          onClick: () => openLogDrawer(row)
        },
        { default: () => '日志' }
      );
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    fixed: 'right',
    render(row) {
      return h(
        NSpace,
        { size: 'small' },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                tertiary: true,
                type: 'warning',
                disabled: row.status !== 'pending',
                onClick: row.status === 'pending' ? () => handleSinglePause(row) : undefined
              },
              { default: () => '暂停' }
            ),
            h(
              NButton,
              {
                size: 'small',
                tertiary: true,
                type: 'info',
                disabled: row.status !== 'failed',
                onClick: row.status === 'failed' ? () => handleSingleRetry(row) : undefined
              },
              { default: () => '重试' }
            ),
            h(
              NButton,
              {
                size: 'small',
                tertiary: true,
                type: 'error',
                disabled: row.status === 'running',
                onClick: row.status !== 'running' ? () => handleSingleDelete(row) : undefined
              },
              { default: () => '删除' }
            )
          ]
        }
      );
    }
  }
];

async function loadData() {
  checkedRowKeys.value = [];
  try {
    const res = await fetchTasks({
      status: filter.value.status || undefined,
      keyword: filter.value.keyword || undefined,
      page: pagination.value.page,
      page_size: pagination.value.pageSize
    });
    const items = res.items || res.data || [];
    tableData.value = items.map((t) => ({
      id: t.ID,
      watchFolderName: t.WatchFolderName,
      fileName: t.FileName,
      localPath: t.LocalPath,
      remoteName: t.RemoteName,
      remotePath: t.RemotePath,
      status: t.Status,
      progress: t.Progress,
      speed: t.Speed,
      retryCount: t.RetryCount,
      errorMsg: t.ErrorMsg,
      fileSize: t.FileSize
    }));
    if (typeof res.total === 'number') {
      pagination.value.itemCount = res.total;
    }
  } catch (e) {
    message.error('加载任务列表失败');
    tableData.value = [];
  }
}

function handleSearch() {
  pagination.value.page = 1;
  loadData();
}

function handleReset() {
  filter.value.status = null;
  filter.value.keyword = '';
  pagination.value.page = 1;
  loadData();
}

function getSelectedIds() {
  return [...checkedRowKeys.value];
}

async function openLogDrawer(row) {
  logDrawerTask.value = row;
  logDrawerVisible.value = true;
  logList.value = [];
  logLoading.value = true;
  try {
    const data = await fetchTaskLogs(row.id);
    logList.value = Array.isArray(data) ? data : [];
  } catch (e) {
    message.error(e?.response?.data?.error || e?.message || '获取日志失败');
    logList.value = [];
  } finally {
    logLoading.value = false;
  }
}

async function handleBatchPause() {
  const ids = getSelectedIds();
  if (!ids.length) {
    message.warning('请先勾选要暂停的任务');
    return;
  }
  try {
    const res = await batchPauseTasks({ ids });
    const failed = res?.failed || {};
    const okCount = res?.ok_ids?.length ?? 0;
    const failCount = Object.keys(failed).length;
    if (failCount > 0) {
      message.warning(`已暂停 ${okCount} 个，${failCount} 个失败：${Object.values(failed).join('；')}`);
    } else {
      message.success(`已暂停 ${okCount} 个任务`);
    }
    loadData();
  } catch (e) {
    message.error(e?.response?.data?.error || e?.message || '批量暂停失败');
  }
}

async function handleSinglePause(row) {
  if (row.status !== 'pending') return;
  try {
    await pauseTask(row.id);
    message.success('已暂停');
    loadData();
  } catch (e) {
    message.error(e?.response?.data?.error || e?.message || '暂停失败');
  }
}

async function handleBatchRetry() {
  const ids = getSelectedIds();
  if (!ids.length) {
    message.warning('请先勾选要重试的任务');
    return;
  }
  try {
    const res = await batchRetryTasks({ ids });
    const failed = res?.failed || {};
    const okCount = res?.ok_ids?.length ?? 0;
    const failCount = Object.keys(failed).length;
    if (failCount > 0) {
      message.warning(`已提交重试 ${okCount} 个，${failCount} 个失败：${Object.values(failed).join('；')}`);
    } else {
      message.success(`已提交重试 ${okCount} 个任务`);
    }
    loadData();
  } catch (e) {
    message.error(e?.response?.data?.error || e?.message || '批量重试失败');
  }
}

async function handleSingleRetry(row) {
  if (row.status !== 'failed') return;
  try {
    const res = await batchRetryTasks({ ids: [row.id] });
    const failed = res?.failed || {};
    if (Object.keys(failed).length > 0) {
      message.warning(Object.values(failed).join('；'));
    } else {
      message.success('已提交重试');
    }
    loadData();
  } catch (e) {
    message.error(e?.response?.data?.error || e?.message || '重试失败');
  }
}

function handleSingleDelete(row) {
  if (row.status === 'running') return;
  dialog.warning({
    title: '确认删除',
    content: `确定要删除任务「${row.fileName || row.id}」吗？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await deleteTask(row.id);
        message.success('已删除');
        loadData();
      } catch (e) {
        message.error(e?.response?.data?.error || e?.message || '删除失败');
      }
    }
  });
}

function handleBatchDelete() {
  const ids = getSelectedIds();
  if (!ids.length) {
    message.warning('请先勾选要删除的任务');
    return;
  }
  dialog.warning({
    title: '确认批量删除',
    content: `确定要删除已选中的 ${ids.length} 个任务吗？运行中的任务无法删除。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const res = await batchDeleteTasks({ ids });
        const failed = res?.failed || {};
        const okCount = res?.ok_ids?.length ?? 0;
        const failCount = Object.keys(failed).length;
        if (failCount > 0) {
          message.warning(`已删除 ${okCount} 个，${failCount} 个失败：${Object.values(failed).join('；')}`);
        } else {
          message.success(`已删除 ${okCount} 个任务`);
        }
        loadData();
      } catch (e) {
        message.error(e?.response?.data?.error || e?.message || '批量删除失败');
      }
    }
  });
}

onMounted(() => {
  loadData();
});
</script>

<style scoped>
.list-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-page);
}

.list-toolbar {
  padding: 14px var(--space-card);
}

.filter-label {
  font-size: 13px;
  color: #64748b;
  margin-right: 4px;
}

/* 抽屉内容区与日志区域铺满可用高度 */
.log-drawer :deep(.n-drawer-body-content-wrapper),
.log-drawer :deep(.n-drawer-body-content) {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

.log-spin {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.log-spin :deep(.n-spin-container) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.log-content {
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.log-line {
  padding: 4px 0;
  border-bottom: 1px solid #f1f5f9;
  word-break: break-all;
}

.log-line:last-child {
  border-bottom: none;
}

.log-time {
  color: #94a3b8;
  margin-right: 8px;
}

.log-level {
  margin-right: 6px;
  font-weight: 500;
}

.log-message {
  color: #334155;
}

.log-empty {
  color: #94a3b8;
  text-align: center;
  padding: 24px;
}
</style>

