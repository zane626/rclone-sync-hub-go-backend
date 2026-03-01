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
            placeholder="名称或路径"
            clearable
            size="small"
            style="width: 220px"
          />
          <n-button size="small" type="primary" @click="handleSearch">查询</n-button>
          <n-button size="small" quaternary @click="handleReset">重置</n-button>
        </div>
        <div class="app-toolbar-actions">
          <n-button type="primary" size="small" @click="openCreate">
            新建监听文件夹
          </n-button>
        </div>
      </div>
    </div>

    <div class="app-table-wrap">
      <n-data-table
        :columns="columns"
        :data="tableData"
        :bordered="false"
        :scroll-x="1400"
        :pagination="pagination"
        striped
      />
    </div>

    <n-drawer v-model:show="drawerVisible" :width="560" placement="right" class="folder-drawer">
      <n-drawer-content :title="drawerMode === 'create' ? '新建监听文件夹' : '编辑监听文件夹'">
        <n-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-placement="left"
          label-width="120"
          class="drawer-form"
        >
          <n-form-item label="名称" path="name">
            <n-input v-model:value="form.name" placeholder="如：视频素材库" />
          </n-form-item>
          <n-form-item label="本地路径" path="local_path" class="form-item-local-path">
            <div class="local-path-field">
              <n-input
                v-model:value="form.local_path"
                placeholder="如：D:/data/videos"
              />
              <n-button tertiary size="small" @click="openPathSelector" class="path-tree-btn">
                通过目录树选择...
              </n-button>
            </div>
          </n-form-item>
          <n-form-item label="remote 名称" path="remote_name">
            <n-select
              v-model:value="form.remote_name"
              :options="remoteOptions"
              :loading="remoteLoading"
              placeholder="请选择"
            />
          </n-form-item>
          <n-form-item label="远端路径" path="remote_path">
            <n-input v-model:value="form.remote_path" placeholder="如：backup/videos" />
          </n-form-item>
          <n-form-item label="最大深度" path="max_depth">
            <n-input-number v-model:value="form.max_depth" :min="0" />
          </n-form-item>
          <n-form-item label="扫描间隔(s)" path="scan_interval_seconds">
            <n-input-number v-model:value="form.scan_interval_seconds" :min="60" />
          </n-form-item>
          <n-form-item label="同步类型" path="sync_type">
            <n-select v-model:value="form.sync_type" :options="syncTypeOptions" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="drawerVisible = false">取消</n-button>
            <n-button type="primary" @click="handleSubmit">
              保存
            </n-button>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
    <n-modal v-model:show="pathSelectorVisible" title="选择本地路径" preset="dialog">
      <div style="margin-bottom: 8px;">
        当前起始路径：{{ (form && form.local_path) || pathRoot }}
      </div>
      <n-spin :show="pathTreeLoading">
        <n-tree
          :data="pathTreeData"
          block-line
          :selectable="true"
          :remote="true"
          :on-load="handleTreeLoad"
          @update:selected-keys="handlePathKeysChange"
        />
      </n-spin>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, onMounted, h } from 'vue';
import {
  NCard,
  NButton,
  NDataTable,
  NDrawer,
  NDrawerContent,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSelect,
  NSpace,
  NTag,
  NModal,
  NTree,
  NSpin,
  useMessage,
  useDialog
} from 'naive-ui';
import { fetchWatchFolders, createWatchFolder, updateWatchFolder, deleteWatchFolder } from '../api/watchFolders';
import { fetchSubdirs } from '../api/fs';
import { fetchRcloneConfigs } from '../api/rclone';

const tableData = ref([]);

const statusOptions = [
  { label: '全部', value: null },
  { label: '检测中', value: 'detecting' },
  { label: '监听中', value: 'watching' },
  { label: '已停止', value: 'stopped' },
  { label: '已暂停', value: 'paused' },
  { label: '异常', value: 'error' }
];

const syncTypeOptions = [
  { label: '本地 -> 远端 (local_to_remote)', value: 'local_to_remote' },
  // { label: '远端 -> 本地 (remote_to_local)', value: 'remote_to_local' }
];

const filter = ref({
  status: null,
  keyword: ''
});

const pagination = ref({
  page: 1,
  pageSize: 10,
  itemCount: 0,
  showSizePicker: false,
  onChange: (page) => {
    pagination.value.page = page;
    loadData();
  }
});

const message = useMessage();
const dialog = useDialog();
const remoteOptions = ref([]);
const remoteLoading = ref(false);

function formatDateTime(v) {
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

const columns = [
  { title: '名称', key: 'name' },
  { title: '本地路径', key: 'localPath' },
  { title: 'remote 名称', key: 'remoteName' },
  { title: '远端路径', key: 'remotePath' },
  {
    title: '状态',
    key: 'status',
    render(row) {
      const map = {
        detecting: { label: '检测中', type: 'warning' },
        watching: { label: '监听中', type: 'success' },
        stopped: { label: '已停止', type: 'default' },
        paused: { label: '已暂停', type: 'warning' },
        error: { label: '异常', type: 'error' }
      };
      const info = map[row.status] || { label: row.status, type: 'default' };
      return h(
        NTag,
        { type: info.type, size: 'small' },
        { default: () => info.label }
      );
    }
  },
  {
    title: '最近一次扫描时间',
    key: 'lastScanAt',
    render: (row) => formatDateTime(row.LastScanAt)
  },
  {
    title: '最近一次同步时间',
    key: 'lastSyncAt',
    render: (row) => formatDateTime(row.LastSyncAt)
  },
  {
    title: '创建时间',
    key: 'createdAt',
    render: (row) => formatDateTime(row.CreatedAt)
  },
  {
    title: '操作',
    key: 'actions',
    fixed: 'right',
    width: 200,
    render(row) {
      const toggleLabel = row.status === 'paused' ? '启动' : '暂停';
      return h(
        NSpace,
        { size: 'small' },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                type: 'primary',
                quaternary: true,
                onClick: () => openEdit(row)
              },
              { default: () => '编辑' }
            ),
            h(
              NButton,
              {
                size: 'small',
                type: 'warning',
                quaternary: true,
                onClick: () => handleToggleStatus(row)
              },
              { default: () => toggleLabel }
            ),
            h(
              NButton,
              {
                size: 'small',
                type: 'error',
                quaternary: true,
                onClick: () => handleDelete(row)
              },
              { default: () => '删除' }
            )
          ]
        }
      );
    }
  }
];

const drawerVisible = ref(false);
const drawerMode = ref('create'); // create | edit
const formRef = ref(null);
const form = ref({
  name: '',
  local_path: '',
  remote_name: '',
  remote_path: '',
  max_depth: 5,
  scan_interval_seconds: 300,
  sync_type: 'local_to_remote'
});

const editingId = ref(null);

const rules = {
  name: { required: true, message: '请输入名称', trigger: 'blur' },
  local_path: { required: true, message: '请输入本地路径', trigger: 'blur' },
  remote_name: { required: true, message: '请输入 remote 名称', trigger: 'blur' },
  remote_path: { required: true, message: '请输入远端路径', trigger: 'blur' }
};

// 本地路径选择树
const pathSelectorVisible = ref(false);
const pathTreeData = ref([]);
const pathTreeLoading = ref(false);
const pathRoot = ref('/volumes');

async function loadSubdirsForNode(path) {
  const res = await fetchSubdirs(path);
  const items = res.items || [];
  return items.map((item) => ({
    key: item.path,
    label: item.name,
    isLeaf: !item.has_sub_dirs
  }));
}

async function initPathTree() {
  pathTreeLoading.value = true;
  try {
    const rootPath = form.value.local_path || pathRoot.value;
    const children = await loadSubdirsForNode(rootPath);
    pathTreeData.value = [
      {
        key: rootPath,
        label: rootPath,
        isLeaf: children.length === 0,
        children
      }
    ];
  } catch (e) {
    pathTreeData.value = [];
  } finally {
    pathTreeLoading.value = false;
  }
}

function openPathSelector() {
  pathSelectorVisible.value = true;
  initPathTree();
}

function handleTreeLoad(node) {
  return loadSubdirsForNode(node.key).then((children) => {
    node.children = children;
  });
}

function handlePathKeysChange(keys) {
  if (keys && keys.length > 0) {
    form.value.local_path = keys[0];
    pathSelectorVisible.value = false;
  }
}

async function loadRemoteOptions() {
  remoteLoading.value = true;
  try {
    const data = await fetchRcloneConfigs();
    const opts = [];
    data.items.forEach(({name}) => {
      opts.push({
        label: name,
        value: name
      });
    });
    remoteOptions.value = opts;
  } catch (e) {
    remoteOptions.value = [];
  } finally {
    remoteLoading.value = false;
  }
}

async function loadData() {
  try {
    const res = await fetchWatchFolders({
      status: filter.value.status || undefined,
      keyword: filter.value.keyword || undefined,
      page: pagination.value.page,
      page_size: pagination.value.pageSize
    });
    tableData.value = (res.items || []).map((item) => ({
      ...item,
      id: item.ID,
      name: item.Name,
      localPath: item.LocalPath,
      remoteName: item.RemoteName,
      remotePath: item.RemotePath,
      status: item.Status
    }));
    if (typeof res.total === 'number') {
      pagination.value.itemCount = res.total;
    }
  } catch (e) {
    message.error('加载监听文件夹失败');
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

async function handleToggleStatus(row) {
  const targetStatus = row.status === 'paused' ? 'watching' : 'paused';
  try {
    await updateWatchFolder(row.Id, { status: targetStatus });
    message.success(targetStatus === 'paused' ? '已暂停' : '已启动');
    loadData();
  } catch (e) {
    message.error('状态更新失败');
  }
}

function handleDelete(row) {
  dialog.warning({
    title: '确认删除',
    content: `确定要删除监听文件夹「${row.name}」吗？此操作不可恢复。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        console.log(row);
        await deleteWatchFolder(row.id);
        message.success('删除成功');
        loadData();
      } catch (e) {
        message.error('删除失败');
      }
    }
  });
}

function openCreate() {
  drawerMode.value = 'create';
  editingId.value = null;
  Object.assign(form.value, {
    name: '',
    local_path: '',
    remote_name: '',
    remote_path: '',
    max_depth: 5,
    scan_interval_seconds: 300,
    sync_type: 'local_to_remote'
  });
  drawerVisible.value = true;
}

function openEdit(row) {
  drawerMode.value = 'edit';
  editingId.value = row.id;
  Object.assign(form.value, {
    name: row.name,
    local_path: row.localPath,
    remote_name: row.remoteName,
    remote_path: row.remotePath,
    max_depth: row.maxDepth || 5,
    scan_interval_seconds: row.scanIntervalSeconds || 300,
    sync_type: row.syncType || 'local_to_remote'
  });
  drawerVisible.value = true;
}

function handleSubmit() {
  formRef.value?.validate((errors) => {
    if (!errors) {
      const payload = {
        name: form.value.name,
        local_path: form.value.local_path,
        remote_name: form.value.remote_name,
        remote_path: form.value.remote_path,
        max_depth: form.value.max_depth || 5,
        scan_interval_seconds: form.value.scan_interval_seconds || 300,
        sync_type: form.value.sync_type
      };
      const req = drawerMode.value === 'create'
        ? createWatchFolder(payload)
        : updateWatchFolder(editingId.value, payload);
      req
        .then(() => {
          message.success('已保存监听文件夹');
          drawerVisible.value = false;
          loadData();
        })
        .catch(() => {
          message.error('保存失败');
        });
    }
  });
}

onMounted(() => {
  loadData();
  loadRemoteOptions();
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

/* 抽屉表单：标签宽度、本地路径一行排列并留间距 */
.folder-drawer :deep(.n-form-item-label) {
  width: 120px;
}

.local-path-field {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.local-path-field .n-input {
  flex: 1;
  min-width: 0;
}

.path-tree-btn {
  flex-shrink: 0;
}

.drawer-form :deep(.n-form-item-blank) {
  align-items: center;
}
</style>

