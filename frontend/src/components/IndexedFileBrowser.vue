<template>
  <Teleport to="body">
    <Transition name="sheet-slide">
      <div v-if="visible" class="sheet-layer indexed-browser-layer" @click.self="emit('close')">
        <aside
          class="side-sheet is-wide indexed-browser"
          :class="{ 'is-tree-view': tree }"
          role="dialog"
          aria-modal="true"
          aria-labelledby="indexed-browser-title"
        >
          <header class="sheet-header">
            <div>
              <span class="eyebrow">{{ tree ? 'PERSISTED FILE TREE' : 'PERSISTED FILE INDEX' }}</span>
              <h2 id="indexed-browser-title">{{ title }}</h2>
              <p v-if="subtitle">{{ subtitle }}</p>
            </div>
            <button class="icon-button" type="button" aria-label="关闭" @click="emit('close')"><UiIcon name="close" :size="18" /></button>
          </header>

          <div v-if="tree" class="index-toolbar is-tree">
            <div class="index-location tree-location">
              <UiIcon name="layers" :size="16" />
              <div><strong>目录树</strong><span class="mono">{{ rootLabel || '/' }}</span></div>
            </div>
            <div class="index-toolbar__actions">
              <button class="ui-button is-small is-ghost" type="button" :disabled="loading || operationBusy" @click="collapseTree">
                <UiIcon name="chevronLeft" :size="15" /> 折叠目录
              </button>
              <button class="ui-button is-small" type="button" :disabled="loading || operationBusy" @click="refreshBrowser">
                <UiIcon name="refresh" :size="15" /> 刷新
              </button>
            </div>
          </div>
          <div v-if="tree && canManage" class="tree-manager">
            <div class="tree-manager__target">
              <span><UiIcon name="folder" :size="16" /></span>
              <div>
                <small>当前操作目录 · 单击目录切换，按住左键拖过文件可批量选择</small>
                <strong class="mono">{{ activeDirectoryLabel }}</strong>
              </div>
            </div>
            <div class="tree-manager__actions">
              <button class="ui-button is-small is-ghost" type="button" :disabled="operationBusy || !activeDirectoryWritable" @click="openCreateFolder">
                <UiIcon name="plus" :size="15" /> 新建文件夹
              </button>
              <button class="ui-button is-small is-ghost" type="button" :disabled="operationBusy || !activeDirectoryPath || !activeDirectoryWritable" @click="openRenameFolder">
                <UiIcon name="edit" :size="15" /> 重命名文件夹
              </button>
              <button class="ui-button is-small is-ghost" type="button" :disabled="operationBusy || (!selectedFileCount && !selectableLoadedFiles.length)" @click="toggleLoadedFileSelection">
                <UiIcon :name="selectedFileCount ? 'close' : 'check'" :size="15" /> {{ selectedFileCount ? '清空选择' : '选择已加载文件' }}
              </button>
              <button class="ui-button is-small is-primary" type="button" :disabled="operationBusy || !selectedFileCount || !activeDirectoryWritable" @click="handleMoveFiles">
                <span v-if="operationBusy" class="mini-spinner is-dark" />
                <UiIcon v-else name="upload" :size="15" />
                {{ operationBusy && moveProgress ? `移动进度 ${moveProgress.processed || 0}/${moveProgress.total || selectedFileCount}` : `移动 ${selectedFileCount} 个文件到此目录` }}
              </button>
            </div>
          </div>
          <Transition name="dialog-fade">
            <section v-if="tree && moveProgress" class="remote-move-progress" :class="`is-${moveProgress.status || 'running'}`" role="status" aria-live="polite">
              <div class="remote-move-progress__summary">
                <span class="remote-move-progress__icon">
                  <span v-if="['queued','running'].includes(moveProgress.status)" class="mini-spinner" />
                  <UiIcon v-else :name="moveProgress.status === 'completed' ? 'check' : 'alert'" :size="17" />
                </span>
                <div>
                  <strong>{{ moveProgressTitle }}</strong>
                  <small>{{ moveProgressDetail }}</small>
                </div>
                <b class="mono">{{ moveProgress.processed || 0 }} / {{ moveProgress.total || 0 }}</b>
              </div>
              <div class="remote-move-progress__track" aria-hidden="true"><i :style="{ width: `${moveProgressPercent}%` }" /></div>
              <div class="remote-move-progress__meta">
                <span>完成度 <strong>{{ moveProgressPercent }}%</strong></span>
                <span>成功 <strong>{{ moveProgress.moved?.length || 0 }}</strong></span>
                <span>失败 <strong>{{ moveProgress.failed?.length || 0 }}</strong></span>
                <span v-if="moveProgressCurrentFile" class="remote-move-progress__file">当前：<strong class="mono">{{ moveProgressCurrentFile }}</strong></span>
              </div>
            </section>
          </Transition>
          <div v-if="!tree" class="index-toolbar">
            <button class="icon-button" type="button" title="返回上一层" :disabled="!currentPath || loading" @click="openPath(parentPath)"><UiIcon name="arrowLeft" :size="17" /></button>
            <div class="index-location"><UiIcon name="folder" :size="16" /><span class="mono">{{ currentPath || '/' }}</span></div>
            <button class="ui-button is-small" type="button" :disabled="loading" @click="refreshBrowser"><UiIcon name="refresh" :size="15" /> 刷新</button>
          </div>

          <div class="indexed-browser__body">
            <template v-if="tree">
              <div v-if="loading && !treeRoot?.loaded" class="loading-layer"><div class="loading-indicator"><span class="spinner" />LOADING FILE TREE</div></div>
              <div v-else-if="treeRoot" class="file-tree-shell">
                <div class="file-tree-grid file-tree-header" role="row">
                  <div>名称</div><div>类型</div><div>状态</div><div>大小</div><div>修改时间</div><div>最近发现</div>
                </div>
                <div class="file-tree" :class="{ 'is-drag-selecting': dragSelectionActive }" role="tree" aria-label="文件目录树">
                  <template v-for="row in flatTreeRows" :key="row.key">
                    <button
                      v-if="row.kind === 'more'"
                      class="file-tree-more"
                      type="button"
                      :style="treeIndent(row.depth)"
                      :disabled="row.parent.loading"
                      @click="loadMore(row.parent)"
                    >
                      <span v-if="row.parent.loading" class="mini-spinner" />
                      <UiIcon v-else name="plus" :size="14" />
                      {{ row.parent.loading ? '正在加载' : '加载更多' }}
                      <small>剩余 {{ number(row.parent.total - row.parent.children.length) }} 项</small>
                    </button>
                    <div v-else-if="row.kind === 'empty'" class="file-tree-empty" :style="treeIndent(row.depth)">
                      <span class="tree-branch" /><UiIcon name="folder" :size="15" /><span>空目录</span>
                    </div>
                    <div
                      v-else
                      class="file-tree-grid file-tree-row"
                      :class="{
                        'is-directory': row.node.type === 'directory',
                        'is-root': row.node.virtual,
                        'is-missing': row.node.status === 'missing',
                        'is-active-directory': row.node.type === 'directory' && row.node.path === activeDirectoryPath,
                        'is-selectable-file': canManage && row.node.type === 'file' && row.node.status !== 'missing' && !operationBusy,
                        'is-selected-file': row.node.type === 'file' && selectedFiles.has(row.node.path),
                        'is-drag-selection-source': dragSelectionActive && row.node.path === dragSelectionSourcePath
                      }"
                      role="treeitem"
                      :aria-level="row.depth + 1"
                      :aria-expanded="row.node.type === 'directory' ? String(row.node.expanded) : undefined"
                      :aria-selected="canManage && row.node.type === 'file' ? String(selectedFiles.has(row.node.path)) : undefined"
                      :data-file-path="row.node.type === 'file' ? row.node.path : undefined"
                      :tabindex="canManage && row.node.type === 'file' && row.node.status !== 'missing' && !operationBusy ? 0 : undefined"
                      @mousedown="startFileDragSelection(row.node, $event)"
                      @mouseover="continueFileDragSelection(row.node)"
                      @mousemove="continueFileDragSelection(row.node)"
                      @click="handleFileRowClick(row.node)"
                      @keydown.enter.prevent="toggleFileRowSelection(row.node)"
                      @keydown.space.prevent="toggleFileRowSelection(row.node)"
                    >
                      <div class="file-tree-primary" :class="{ 'has-selection': canManage }" :style="treeIndent(row.depth)">
                        <input
                          v-if="canManage && row.node.type === 'file'"
                          class="tree-file-checkbox"
                          type="checkbox"
                          :aria-label="`选择文件 ${row.node.name}`"
                          :checked="selectedFiles.has(row.node.path)"
                          :disabled="operationBusy || row.node.status === 'missing'"
                          @click.stop
                          @change="toggleFileSelection(row.node.path, $event)"
                        />
                        <span v-else-if="canManage" class="tree-selection-spacer" />
                        <button
                          v-if="row.node.type === 'directory'"
                          class="tree-toggle"
                          type="button"
                          :aria-label="row.node.expanded ? '折叠目录' : '展开目录'"
                          @click="toggleNode(row.node)"
                        >
                          <span v-if="row.node.loading" class="mini-spinner" />
                          <UiIcon v-else :name="row.node.expanded ? 'chevronDown' : 'chevronRight'" :size="15" />
                        </button>
                        <span v-else class="tree-toggle-spacer" />
                        <span class="tree-node-icon"><UiIcon :name="row.node.type === 'directory' ? 'folder' : 'file'" :size="17" /></span>
                        <button v-if="row.node.type === 'directory'" class="tree-node-title" type="button" @click="selectDirectory(row.node)">
                          <strong>{{ row.node.name }}</strong>
                          <small class="mono">{{ displayPath(row.node) }}</small>
                        </button>
                        <div v-else class="tree-node-title">
                          <strong>{{ row.node.name }}</strong>
                          <small class="mono">{{ displayPath(row.node) }}</small>
                        </div>
                      </div>
                      <div>{{ describeType(row.node) }}</div>
                      <div><StatusBadge :status="row.node.type === 'directory' && row.node.status !== 'missing' ? 'directory' : row.node.status" /></div>
                      <div class="mono">{{ displaySize(row.node) }}</div>
                      <div>{{ formatDate(row.node.mod_time) }}</div>
                      <div>{{ formatDate(row.node.last_seen_at) }}</div>
                    </div>
                  </template>
                </div>
              </div>
              <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="folder" /></span><strong>还没有远端索引</strong><p>完成一次扫描后，目录树会显示在这里。</p></div></div>
            </template>

            <template v-else>
              <div v-if="loading" class="loading-layer"><div class="loading-indicator"><span class="spinner" />LOADING FILE INDEX</div></div>
              <div v-else-if="items.length" class="data-table-shell">
                <table class="data-table index-table">
                  <thead><tr><th>名称</th><th>类型</th><th>状态</th><th>大小</th><th>修改时间</th><th>最近发现</th></tr></thead>
                  <tbody>
                    <tr v-for="item in items" :key="itemKey(item)" :class="{ 'is-directory': item.type === 'directory' }" @dblclick="item.type === 'directory' && openPath(item.path)">
                      <td>
                        <button v-if="item.type === 'directory'" class="index-name is-link" type="button" @click="openPath(item.path)"><span><UiIcon name="folder" :size="17" /></span><div><strong>{{ item.name }}</strong><small class="mono">{{ item.path }}</small></div><UiIcon name="chevronRight" :size="15" /></button>
                        <div v-else class="index-name"><span><UiIcon name="file" :size="17" /></span><div><strong>{{ item.name }}</strong><small class="mono">{{ item.path }}</small></div></div>
                      </td>
                      <td>{{ describeType(item) }}</td>
                      <td><StatusBadge :status="item.type === 'directory' && item.status !== 'missing' ? 'directory' : item.status" /></td>
                      <td class="mono">{{ displaySize(item) }}</td>
                      <td>{{ formatDate(item.mod_time) }}</td>
                      <td>{{ formatDate(item.last_seen_at) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div v-else class="empty-state"><div><span class="empty-state__icon"><UiIcon name="folder" /></span><strong>该层没有已记录项目</strong><p>完成一次扫描后，文件与目录会显示在这里。</p></div></div>
            </template>
          </div>

          <PaginationBar
            v-if="!tree"
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="total"
            :page-sizes="[50, 100, 200, 500]"
            @change="loadList"
          />

          <Transition name="dialog-fade">
            <div v-if="folderDialogMode" class="browser-dialog-layer" @click.self="closeFolderDialog">
              <form class="browser-dialog" role="dialog" aria-modal="true" aria-labelledby="remote-folder-dialog-title" @submit.prevent="submitFolderDialog">
                <header>
                  <div>
                    <span class="eyebrow">REMOTE FOLDER</span>
                    <h3 id="remote-folder-dialog-title">{{ folderDialogMode === 'create' ? '新建文件夹' : '重命名文件夹' }}</h3>
                  </div>
                  <button class="icon-button" type="button" aria-label="关闭" :disabled="operationBusy" @click="closeFolderDialog"><UiIcon name="close" :size="17" /></button>
                </header>
                <div class="browser-dialog__body">
                  <div class="field">
                    <label for="remote-folder-name">{{ folderDialogMode === 'create' ? '文件夹名称' : '新名称' }}</label>
                    <input id="remote-folder-name" ref="folderNameInput" v-model="folderName" class="ui-control" maxlength="255" autocomplete="off" placeholder="请输入文件夹名称" />
                    <p class="field-help">位置：<span class="mono">{{ folderDialogMode === 'create' ? activeDirectoryLabel : parentDirectoryLabel }}</span></p>
                  </div>
                </div>
                <footer>
                  <button class="ui-button is-ghost" type="button" :disabled="operationBusy" @click="closeFolderDialog">取消</button>
                  <button class="ui-button is-primary" type="submit" :disabled="operationBusy || !folderName.trim()">
                    <span v-if="operationBusy" class="mini-spinner is-dark" />
                    <UiIcon v-else name="check" :size="16" /> {{ operationBusy ? '正在处理' : '确认' }}
                  </button>
                </footer>
              </form>
            </div>
          </Transition>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import UiIcon from './UiIcon.vue';
import StatusBadge from './StatusBadge.vue';
import PaginationBar from './PaginationBar.vue';
import { confirmDialog, errorText, toast } from '../composables/useUi';

const props = defineProps({
  visible: { type: Boolean, default: false },
  title: { type: String, default: '文件索引' },
  subtitle: { type: String, default: '' },
  loader: { type: Function, required: true },
  tree: { type: Boolean, default: false },
  rootLabel: { type: String, default: '' },
  treePageSize: { type: Number, default: 200 },
  canManage: { type: Boolean, default: false },
  createFolder: { type: Function, default: null },
  renameFolder: { type: Function, default: null },
  moveFiles: { type: Function, default: null }
});
const emit = defineEmits(['close']);
const loading = ref(false);
const currentPath = ref('');
const parentPath = ref('');
const items = ref([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(100);
const treeRoot = ref(null);
const activeDirectoryPath = ref('');
const selectedFiles = ref(new Set());
const operationBusy = ref(false);
const moveProgress = ref(null);
const dragSelectionActive = ref(false);
const dragSelectionSourcePath = ref('');
const folderDialogMode = ref('');
const folderName = ref('');
const folderNameInput = ref(null);
const maxBatchSelection = 100;

const flatTreeRows = computed(() => {
  const rows = [];
  const visit = (node, depth) => {
    rows.push({ kind: 'node', key: node.key, node, depth });
    if (node.type !== 'directory' || !node.expanded) return;
    for (const child of node.children) visit(child, depth + 1);
    if (node.loaded && !node.loading && node.children.length === 0) {
      rows.push({ kind: 'empty', key: node.key + ':empty', parent: node, depth: depth + 1 });
    }
    if (node.children.length < node.total) {
      rows.push({ kind: 'more', key: node.key + ':more', parent: node, depth: depth + 1 });
    }
  };
  if (treeRoot.value) visit(treeRoot.value, 0);
  return rows;
});

const activeDirectoryNode = computed(() => findTreeNode(activeDirectoryPath.value) || treeRoot.value);
const activeDirectoryWritable = computed(() => Boolean(activeDirectoryNode.value && activeDirectoryNode.value.status !== 'missing'));
const activeDirectoryLabel = computed(() => formatRemoteLocation(activeDirectoryPath.value));
const parentDirectoryLabel = computed(() => formatRemoteLocation(parentIndexPath(activeDirectoryPath.value)));
const selectedFileCount = computed(() => selectedFiles.value.size);
const moveProgressPercent = computed(() => {
  const explicit = Number(moveProgress.value?.percent);
  if (Number.isFinite(explicit)) return Math.min(100, Math.max(0, Math.round(explicit)));
  const total = Number(moveProgress.value?.total || 0);
  return total > 0 ? Math.min(100, Math.round(Number(moveProgress.value?.processed || 0) * 100 / total)) : 0;
});
const moveProgressTitle = computed(() => ({
  queued: '正在创建后台移动任务',
  preparing: '正在检查目标目录',
  validating: '正在检查所选文件',
  moving: '正在移动远端文件',
  refreshing: '正在安排索引刷新',
  completed: '批量移动处理完成',
  failed: '批量移动任务失败'
}[moveProgress.value?.phase] || '正在处理批量移动'));
const moveProgressCurrentFile = computed(() => {
  const path = String(moveProgress.value?.current_path || '');
  return path.split('/').filter(Boolean).pop() || '';
});
const moveProgressDetail = computed(() => {
  if (moveProgress.value?.poll_warning) return moveProgress.value.poll_warning;
  if (moveProgress.value?.error) return moveProgress.value.error;
  if (moveProgressCurrentFile.value) return `正在处理：${moveProgressCurrentFile.value}`;
  if (moveProgress.value?.status === 'completed') return '远端索引将在后台刷新';
  return '任务已在服务端后台执行，关闭窗口不会中断移动';
});
const selectableLoadedFiles = computed(() => {
  const paths = [];
  const collect = (node) => {
    for (const child of node?.children || []) {
      if (child.type === 'file' && child.status !== 'missing') paths.push(child.path);
      if (child.type === 'directory') collect(child);
    }
  };
  collect(treeRoot.value);
  return [...new Set(paths)];
});

function number(value) {
  return new Intl.NumberFormat('zh-CN').format(Number(value || 0));
}

function formatBytes(bytes) {
  const value = Number(bytes || 0);
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return (value / 1024 ** index).toFixed(index > 1 ? 2 : 0) + ' ' + units[index];
}

function formatDate(value) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? String(value)
    : new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(date);
}

function itemKey(item) {
  return [item.type, item.path, item.id || ''].join(':');
}

function parentIndexPath(value) {
  const parts = String(value || '').split('/').filter(Boolean);
  parts.pop();
  return parts.join('/');
}

function formatRemoteLocation(relativePath) {
  const base = String(props.rootLabel || '').replace(/\/+$/, '');
  const suffix = String(relativePath || '').replace(/^\/+/, '');
  if (!suffix) return base || '/';
  return base ? `${base}/${suffix}` : `/${suffix}`;
}

function joinRelativePath(parent, name) {
  return [String(parent || '').replace(/\/+$/, ''), String(name || '').replace(/^\/+/, '')].filter(Boolean).join('/');
}

function displayPath(node) {
  return node.virtual ? '/' : (node.path || '/');
}

function displaySize(node) {
  if (node.type === 'file') return formatBytes(node.size);
  return Number(node.size || 0) > 0 ? formatBytes(node.size) : '—';
}

function describeType(node) {
  if (node.virtual) return '根目录 · ' + number(node.total) + ' 项';
  if (node.type !== 'directory') return '文件';
  return Number(node.file_count || 0) > 0 ? '文件夹 · ' + number(node.file_count) + ' 文件' : '文件夹';
}

function treeIndent(depth) {
  return { '--tree-indent': (12 + Number(depth || 0) * 22) + 'px' };
}

function makeTreeNode(item) {
  return {
    ...item,
    key: itemKey(item),
    children: [],
    expanded: false,
    loaded: false,
    loading: false,
    total: 0,
    page: 0,
    error: ''
  };
}

function makeTreeRoot() {
  return {
    key: 'tree-root',
    virtual: true,
    type: 'directory',
    name: props.rootLabel || '远端根目录',
    path: '',
    status: 'directory',
    size: 0,
    children: [],
    expanded: true,
    loaded: false,
    loading: false,
    total: 0,
    page: 0,
    error: ''
  };
}

function findTreeNode(path, node = treeRoot.value) {
  if (!node) return null;
  if ((node.path || '') === (path || '')) return node;
  for (const child of node.children || []) {
    const match = findTreeNode(path, child);
    if (match) return match;
  }
  return null;
}

function sortTreeChildren(children) {
  children.sort((left, right) => {
    if (left.type !== right.type) return left.type === 'directory' ? -1 : 1;
    return String(left.name || '').localeCompare(String(right.name || ''), 'zh-CN', { numeric: true });
  });
}

async function loadTreeChildren(node, append = false) {
  if (!node || node.loading) return;
  node.loading = true;
  node.error = '';
  if (node.virtual && !node.loaded) loading.value = true;
  const nextPage = append ? node.page + 1 : 1;
  try {
    const result = await props.loader(node.path || '', nextPage, props.treePageSize);
    const children = (result.items || []).map(makeTreeNode);
    if (append) {
      const existing = new Set(node.children.map((child) => child.key));
      node.children = node.children.concat(children.filter((child) => !existing.has(child.key)));
    } else {
      node.children = children;
    }
    node.total = Number(result.total || 0);
    node.page = Number(result.page || nextPage);
    node.loaded = true;
  } catch (error) {
    node.error = errorText(error, '目录加载失败');
    toast(node.error, 'error');
  } finally {
    node.loading = false;
    if (node.virtual) loading.value = false;
  }
}

function resetTree() {
  finishFileDragSelection();
  activeDirectoryPath.value = '';
  selectedFiles.value = new Set();
  folderDialogMode.value = '';
  folderName.value = '';
  treeRoot.value = makeTreeRoot();
  loadTreeChildren(treeRoot.value);
}

function toggleNode(node) {
  if (!node || node.type !== 'directory') return;
  node.expanded = !node.expanded;
  if (node.expanded && !node.loaded) loadTreeChildren(node);
}

function selectDirectory(node) {
  if (!node || node.type !== 'directory') return;
  activeDirectoryPath.value = node.path || '';
  if (!node.expanded) node.expanded = true;
  if (!node.loaded) loadTreeChildren(node);
}

function toggleFileSelection(path, event) {
  const checked = Boolean(event?.target?.checked);
  if (!setFileSelection(path, checked) && event?.target) event.target.checked = false;
}

function setFileSelection(path, checked) {
  const next = new Set(selectedFiles.value);
  if (checked && !next.has(path) && next.size >= maxBatchSelection) {
    toast(`单次最多选择 ${maxBatchSelection} 个文件`, 'warning');
    return false;
  }
  if (checked) next.add(path);
  else next.delete(path);
  selectedFiles.value = next;
  return true;
}

let dragSelectionShouldSelect = true;
let dragSelectionVisited = new Set();
let dragSelectionLimitNotified = false;
let suppressedRowClickPath = '';
let suppressedRowClickTimer;

function isSelectableTreeFile(node) {
  return Boolean(props.canManage && !operationBusy.value && node?.type === 'file' && node.status !== 'missing');
}

function applyFileDragSelection(node) {
  if (!isSelectableTreeFile(node) || dragSelectionVisited.has(node.path)) return;
  dragSelectionVisited.add(node.path);
  if (dragSelectionShouldSelect && !selectedFiles.value.has(node.path) && selectedFiles.value.size >= maxBatchSelection) {
    if (!dragSelectionLimitNotified) {
      dragSelectionLimitNotified = true;
      toast(`单次最多选择 ${maxBatchSelection} 个文件`, 'warning');
    }
    return;
  }
  setFileSelection(node.path, dragSelectionShouldSelect);
}

function applyFileDragSelectionRange(targetNode) {
  if (!isSelectableTreeFile(targetNode)) return;
  const sourceIndex = flatTreeRows.value.findIndex((row) => row.kind === 'node' && row.node.path === dragSelectionSourcePath.value);
  const targetIndex = flatTreeRows.value.findIndex((row) => row.kind === 'node' && row.node.path === targetNode.path);
  if (sourceIndex < 0 || targetIndex < 0) {
    applyFileDragSelection(targetNode);
    return;
  }
  const start = Math.min(sourceIndex, targetIndex);
  const end = Math.max(sourceIndex, targetIndex);
  for (let index = start; index <= end; index++) {
    const row = flatTreeRows.value[index];
    if (row.kind === 'node') applyFileDragSelection(row.node);
  }
}

function startFileDragSelection(node, event) {
  if (!isSelectableTreeFile(node) || event.button !== 0) return;
  if (event.target?.closest?.('input, button, a, select, textarea')) return;
  event.preventDefault();
  event.currentTarget?.focus?.({ preventScroll: true });
  if (suppressedRowClickTimer) window.clearTimeout(suppressedRowClickTimer);
  dragSelectionActive.value = true;
  dragSelectionSourcePath.value = node.path;
  dragSelectionShouldSelect = !selectedFiles.value.has(node.path);
  dragSelectionVisited = new Set();
  dragSelectionLimitNotified = false;
  suppressedRowClickPath = node.path;
  applyFileDragSelection(node);
}

function continueFileDragSelection(node) {
  if (!dragSelectionActive.value) return;
  applyFileDragSelectionRange(node);
}

function finishFileDragSelection(event) {
  if (!dragSelectionActive.value) return;
  if (event?.type === 'mouseup') {
    const row = event.target?.closest?.('.file-tree-row.is-selectable-file');
    const targetPath = row?.dataset?.filePath;
    if (targetPath) applyFileDragSelectionRange(findTreeNode(targetPath));
  }
  dragSelectionActive.value = false;
  dragSelectionSourcePath.value = '';
  dragSelectionVisited = new Set();
  dragSelectionLimitNotified = false;
  if (suppressedRowClickTimer) window.clearTimeout(suppressedRowClickTimer);
  suppressedRowClickTimer = window.setTimeout(() => { suppressedRowClickPath = ''; }, 0);
}

function handleFileRowClick(node) {
  if (suppressedRowClickPath && suppressedRowClickPath === node?.path) {
    suppressedRowClickPath = '';
    return;
  }
  toggleFileRowSelection(node);
}

function toggleFileRowSelection(node) {
  if (!props.canManage || operationBusy.value || node?.type !== 'file' || node.status === 'missing') return;
  setFileSelection(node.path, !selectedFiles.value.has(node.path));
}

function toggleLoadedFileSelection() {
  finishFileDragSelection();
  if (selectedFiles.value.size) {
    selectedFiles.value = new Set();
    return;
  }
  const paths = selectableLoadedFiles.value.slice(0, maxBatchSelection);
  selectedFiles.value = new Set(paths);
  if (selectableLoadedFiles.value.length > maxBatchSelection) {
    toast(`已选择前 ${maxBatchSelection} 个已加载文件；单次批量操作最多 ${maxBatchSelection} 个`, 'warning');
  }
}

function focusFolderNameInput() {
  nextTick(() => {
    folderNameInput.value?.focus();
    folderNameInput.value?.select();
  });
}

function openCreateFolder() {
  if (!props.createFolder || !activeDirectoryWritable.value) return;
  folderDialogMode.value = 'create';
  folderName.value = '';
  focusFolderNameInput();
}

function openRenameFolder() {
  const node = activeDirectoryNode.value;
  if (!props.renameFolder || !node || node.virtual || node.status === 'missing') return;
  folderDialogMode.value = 'rename';
  folderName.value = node.name || '';
  focusFolderNameInput();
}

function closeFolderDialog() {
  if (operationBusy.value) return;
  folderDialogMode.value = '';
  folderName.value = '';
}

function validateFolderName(value) {
  const name = String(value || '').trim();
  if (!name || name === '.' || name === '..' || /[/\\\0\r\n]/.test(name)) {
    toast('文件夹名称不能为空，且不能包含斜杠、反斜杠或换行', 'warning');
    return '';
  }
  return name;
}

function addCreatedFolder(path, name) {
  const parent = activeDirectoryNode.value;
  if (!parent?.loaded || parent.children.some((child) => child.path === path)) return;
  parent.children.push(makeTreeNode({ type: 'directory', name, path, status: 'available', size: 0 }));
  parent.total = Math.max(parent.total + 1, parent.children.length);
  sortTreeChildren(parent.children);
}

function applyRenamedFolder(path, name) {
  const node = activeDirectoryNode.value;
  if (!node || node.virtual) return;
  const oldPath = node.path;
  const rewrite = (entry) => {
    const relative = entry.path === oldPath ? '' : String(entry.path || '').slice(oldPath.length + 1);
    entry.path = relative ? `${path}/${relative}` : path;
    entry.key = itemKey(entry);
    for (const child of entry.children || []) rewrite(child);
  };
  rewrite(node);
  node.name = name;
  activeDirectoryPath.value = path;
  selectedFiles.value = new Set();
  const parent = findTreeNode(parentIndexPath(path));
  if (parent) sortTreeChildren(parent.children);
}

async function submitFolderDialog() {
  const name = validateFolderName(folderName.value);
  if (!name || operationBusy.value) return;
  const mode = folderDialogMode.value;
  const targetPath = activeDirectoryPath.value;
  operationBusy.value = true;
  try {
    if (mode === 'create') {
      if (!props.createFolder) throw new Error('当前页面未启用文件夹创建功能');
      const result = await props.createFolder(targetPath, name);
      addCreatedFolder(result?.path || joinRelativePath(targetPath, name), name);
      toast(`文件夹「${name}」已创建，远端索引正在刷新`, 'success');
    } else {
      if (!props.renameFolder) throw new Error('当前页面未启用文件夹重命名功能');
      const result = await props.renameFolder(targetPath, name);
      applyRenamedFolder(result?.path || joinRelativePath(parentIndexPath(targetPath), name), name);
      toast(`文件夹已重命名为「${name}」，远端索引正在刷新`, 'success');
    }
    folderDialogMode.value = '';
    folderName.value = '';
  } catch (error) {
    toast(errorText(error, mode === 'create' ? '文件夹创建失败' : '文件夹重命名失败'), 'error');
  } finally {
    operationBusy.value = false;
  }
}

function detachLoadedFile(node, sourcePath) {
  if (!node) return null;
  const index = (node.children || []).findIndex((child) => child.type === 'file' && child.path === sourcePath);
  if (index >= 0) {
    const [removed] = node.children.splice(index, 1);
    node.total = Math.max(node.children.length, node.total - 1);
    return removed;
  }
  for (const child of node.children || []) {
    if (child.type !== 'directory') continue;
    const removed = detachLoadedFile(child, sourcePath);
    if (removed) return removed;
  }
  return null;
}

function applyMovedFiles(moved) {
  const target = activeDirectoryNode.value;
  const remaining = new Set(selectedFiles.value);
  for (const item of moved) {
    const sourcePath = item.source_path || item.sourcePath;
    const destinationPath = item.destination_path || item.destinationPath;
    const node = detachLoadedFile(treeRoot.value, sourcePath);
    remaining.delete(sourcePath);
    if (!node || !target?.loaded || target.children.some((child) => child.path === destinationPath)) continue;
    node.path = destinationPath;
    node.name = String(destinationPath || '').split('/').pop() || node.name;
    node.key = itemKey(node);
    target.children.push(node);
    target.total = Math.max(target.total + 1, target.children.length);
  }
  if (target?.loaded) sortTreeChildren(target.children);
  selectedFiles.value = remaining;
}

function moveFailureReason(reason) {
  const messages = {
    'file is already in the target folder': '文件已在目标目录中',
    'another selected file has the same destination name': '所选文件中存在同名目标',
    'source file does not exist': '源文件不存在',
    'source path is a directory': '源路径是文件夹',
    'destination already exists': '目标目录已有同名文件',
    'remote move failed': '远端移动失败'
  };
  return messages[reason] || reason || '未知原因';
}

async function handleMoveFiles() {
  if (!props.moveFiles || !selectedFiles.value.size || operationBusy.value || !activeDirectoryWritable.value) return;
  const sourcePaths = [...selectedFiles.value];
  const targetPath = activeDirectoryPath.value;
  const confirmed = await confirmDialog({
    title: '批量移动文件',
    message: `将 ${sourcePaths.length} 个文件移动到「${activeDirectoryLabel.value}」？目标中已有的同名文件不会被覆盖。`,
    confirmText: '确认移动',
    tone: 'warning'
  });
  if (!confirmed) return;
  if (moveProgressClearTimer) window.clearTimeout(moveProgressClearTimer);
  moveProgress.value = {
    status: 'queued', phase: 'queued', total: sourcePaths.length, processed: 0, percent: 0,
    moved: [], failed: [], current_path: ''
  };
  operationBusy.value = true;
  try {
    const result = await props.moveFiles(sourcePaths, targetPath, (progress) => {
      moveProgress.value = { ...progress, moved: progress?.moved || [], failed: progress?.failed || [] };
    });
    const moved = result?.moved || [];
    const failed = result?.failed || [];
    applyMovedFiles(moved);
    if (moved.length) toast(`已移动 ${moved.length} 个文件，远端索引正在刷新`, 'success');
    if (failed.length) {
      const first = failed[0];
      const source = first.source_path || first.sourcePath || '文件';
      toast(`${failed.length} 个文件未移动：${source} · ${moveFailureReason(first.reason)}`, moved.length ? 'warning' : 'error', 6200);
    }
  } catch (error) {
    const operation = error?.operation;
    moveProgress.value = operation
      ? { ...operation, moved: operation.moved || [], failed: operation.failed || [] }
      : { ...moveProgress.value, status: 'failed', phase: 'failed', error: errorText(error, '批量移动失败') };
    toast(errorText(error, '批量移动失败'), 'error');
  } finally {
    operationBusy.value = false;
    moveProgressClearTimer = window.setTimeout(() => { moveProgress.value = null; }, moveProgress.value?.status === 'completed' ? 2600 : 8000);
  }
}

function loadMore(node) {
  if (node && node.children.length < node.total) loadTreeChildren(node, true);
}

function collapseTree() {
  const collapse = (node) => {
    for (const child of node.children || []) {
      if (child.type === 'directory') {
        child.expanded = false;
        collapse(child);
      }
    }
  };
  if (treeRoot.value) {
    collapse(treeRoot.value);
    treeRoot.value.expanded = true;
  }
}

async function loadList() {
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
  } finally {
    loading.value = false;
  }
}

function openPath(path) {
  currentPath.value = path || '';
  page.value = 1;
  loadList();
}

function refreshBrowser() {
  if (props.tree) resetTree();
  else loadList();
}

function resetBrowser() {
  finishFileDragSelection();
  currentPath.value = '';
  parentPath.value = '';
  page.value = 1;
  items.value = [];
  total.value = 0;
  if (props.tree) resetTree();
  else loadList();
}

function handleKeydown(event) {
  if (event.key !== 'Escape' || !props.visible) return;
  if (folderDialogMode.value) {
    closeFolderDialog();
    return;
  }
  emit('close');
}

watch(
  () => [props.visible, props.tree, props.rootLabel],
  ([visible], previous = []) => {
    if (visible && (!previous[0] || previous[1] !== props.tree || previous[2] !== props.rootLabel)) resetBrowser();
  },
  { immediate: true }
);
onMounted(() => {
  window.addEventListener('keydown', handleKeydown);
  window.addEventListener('mouseup', finishFileDragSelection);
  window.addEventListener('blur', finishFileDragSelection);
});
let moveProgressClearTimer;
onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleKeydown);
  window.removeEventListener('mouseup', finishFileDragSelection);
  window.removeEventListener('blur', finishFileDragSelection);
  if (moveProgressClearTimer) window.clearTimeout(moveProgressClearTimer);
  if (suppressedRowClickTimer) window.clearTimeout(suppressedRowClickTimer);
});
</script>

<style scoped>
.indexed-browser-layer { z-index: 180; }
.indexed-browser.is-tree-view { width: min(1120px, 98vw); }
.indexed-browser .sheet-header p { max-width: 650px; margin: 6px 0 0; color: var(--text-muted); font-size: 10px; }
.index-toolbar { display: grid; grid-template-columns: 34px minmax(0,1fr) auto; align-items: center; gap: 10px; padding: 13px 18px; border-bottom: 1px solid var(--line); background: var(--surface-soft); }
.index-toolbar.is-tree { grid-template-columns: minmax(0,1fr) auto; }
.index-toolbar__actions { display: flex; align-items: center; gap: 8px; }
.index-location { min-width: 0; display: flex; align-items: center; gap: 9px; color: var(--cyan); }
.index-location span { overflow: hidden; color: var(--text); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.tree-location div { min-width: 0; display: flex; align-items: baseline; gap: 10px; }
.tree-location strong { color: var(--text-strong); font-size: 11px; }
.tree-manager { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 11px 18px; border-bottom: 1px solid var(--line); background: rgba(4,8,14,.32); }
.tree-manager__target { min-width: 220px; display: flex; align-items: center; gap: 10px; }
.tree-manager__target > span { width: 32px; height: 32px; display: grid; flex: 0 0 auto; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.14); border-radius: 9px; background: rgba(88,224,255,.05); }
.tree-manager__target div { min-width: 0; }
.tree-manager__target small,.tree-manager__target strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-manager__target small { color: var(--text-muted); font-size: 8px; }
.tree-manager__target strong { max-width: 420px; margin-top: 4px; color: var(--text-strong); font-size: 9px; }
.tree-manager__actions { display: flex; align-items: center; justify-content: flex-end; gap: 7px; flex-wrap: wrap; }
.remote-move-progress { padding: 12px 18px 14px; border-bottom: 1px solid rgba(88,224,255,.17); background: linear-gradient(90deg,rgba(88,224,255,.07),rgba(105,93,255,.035)); }
.remote-move-progress.is-failed { border-bottom-color: rgba(255,98,125,.2); background: rgba(255,98,125,.055); }
.remote-move-progress__summary { display: grid; grid-template-columns: 34px minmax(0,1fr) auto; align-items: center; gap: 10px; }
.remote-move-progress__icon { width: 32px; height: 32px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.18); border-radius: 9px; background: rgba(88,224,255,.06); }
.remote-move-progress.is-completed .remote-move-progress__icon { color: var(--green); border-color: rgba(61,225,162,.2); background: rgba(61,225,162,.06); }
.remote-move-progress.is-failed .remote-move-progress__icon { color: var(--red); border-color: rgba(255,98,125,.2); background: rgba(255,98,125,.06); }
.remote-move-progress__summary strong,.remote-move-progress__summary small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.remote-move-progress__summary strong { color: var(--text-strong); font-size: 11px; }
.remote-move-progress__summary small { margin-top: 4px; color: var(--text-muted); font-size: 9px; }
.remote-move-progress__summary > b { color: var(--cyan); font-size: 12px; }
.remote-move-progress__track { height: 5px; margin: 10px 0 8px 44px; overflow: hidden; border-radius: 999px; background: rgba(141,165,194,.1); }
.remote-move-progress__track i { display: block; height: 100%; border-radius: inherit; background: linear-gradient(90deg,var(--cyan),var(--violet)); box-shadow: 0 0 16px rgba(88,224,255,.25); transition: width .3s ease; }
.remote-move-progress__meta { min-width: 0; display: flex; align-items: center; gap: 18px; margin-left: 44px; color: var(--text-muted); font-size: 8px; }
.remote-move-progress__meta strong { color: var(--text); font-size: 9px; }
.remote-move-progress__file { min-width: 0; margin-left: auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.remote-move-progress__file strong { display: inline; }
.indexed-browser__body { position: relative; min-height: 0; flex: 1; overflow: auto; }
.index-table { min-width: 900px; }
.index-table tbody tr.is-directory { cursor: pointer; }
.index-name { width: 100%; min-width: 280px; display: grid; grid-template-columns: 34px minmax(0,1fr) auto; align-items: center; gap: 9px; padding: 0; color: inherit; border: 0; background: transparent; text-align: left; }
.index-name > span { width: 32px; height: 32px; display: grid; place-items: center; color: var(--text-muted); border: 1px solid var(--line); border-radius: 9px; background: var(--surface-soft); }
.index-name.is-link { cursor: pointer; }
.index-name.is-link > span,.index-name.is-link > .ui-icon { color: var(--cyan); }
.index-name strong,.index-name small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.index-name strong { color: var(--text-strong); font-size: 11px; }
.index-name small { max-width: 390px; margin-top: 3px; color: var(--text-muted); font-size: 8px; }

.file-tree-shell { min-width: 990px; }
.file-tree-grid { display: grid; grid-template-columns: minmax(390px, 2fr) 126px 110px 105px 150px 150px; align-items: center; }
.file-tree-header { position: sticky; top: 0; z-index: 3; height: 46px; color: var(--text-muted); border-bottom: 1px solid var(--line); background: var(--table-header-background); font: 600 9px var(--font-mono); letter-spacing: .1em; text-transform: uppercase; }
.file-tree-header > div,.file-tree-row > div { min-width: 0; padding: 0 12px; }
.file-tree-row { min-height: 62px; color: #aeb9c8; border-bottom: 1px solid rgba(150,176,210,.07); font-size: 12px; transition: background .15s ease; }
.file-tree-row:hover { background: rgba(88,224,255,.025); }
.file-tree-row.is-root { background: rgba(88,224,255,.035); }
.file-tree-row.is-active-directory { background: rgba(88,224,255,.075); box-shadow: inset 3px 0 0 var(--cyan); }
.file-tree-row.is-selectable-file { cursor: pointer; }
.file-tree-row.is-selected-file { background: rgba(88,224,255,.09); box-shadow: inset 3px 0 0 var(--cyan); }
.file-tree-row.is-selected-file:hover { background: rgba(88,224,255,.12); }
.file-tree.is-drag-selecting,.file-tree.is-drag-selecting .file-tree-row { user-select: none; }
.file-tree.is-drag-selecting .file-tree-row.is-selectable-file { cursor: crosshair; }
.file-tree-row.is-drag-selection-source { outline: 1px solid rgba(88,224,255,.42); outline-offset: -1px; }
.file-tree-row.is-missing { opacity: .62; }
.file-tree-primary { min-width: 0; display: grid; grid-template-columns: 24px 34px minmax(0,1fr); align-items: center; gap: 8px; padding-left: var(--tree-indent) !important; }
.file-tree-primary.has-selection { grid-template-columns: 20px 24px 34px minmax(0,1fr); }
.tree-file-checkbox { width: 15px; height: 15px; margin: 0; accent-color: var(--cyan); cursor: pointer; }
.tree-file-checkbox:disabled { cursor: not-allowed; }
.tree-selection-spacer { width: 20px; }
.tree-toggle { width: 24px; height: 28px; display: grid; place-items: center; padding: 0; color: var(--cyan); border: 0; background: transparent; cursor: pointer; }
.tree-toggle-spacer { width: 24px; }
.tree-node-icon { width: 32px; height: 32px; display: grid; place-items: center; color: var(--text-muted); border: 1px solid var(--line); border-radius: 9px; background: var(--surface-soft); }
.is-directory .tree-node-icon { color: var(--cyan); border-color: rgba(88,224,255,.18); background: rgba(88,224,255,.05); }
.tree-node-title { min-width: 0; display: block; padding: 0; color: inherit; border: 0; background: transparent; text-align: left; }
button.tree-node-title { cursor: pointer; }
.tree-node-title strong,.tree-node-title small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-node-title strong { color: var(--text-strong); font-size: 11px; }
.tree-node-title small { margin-top: 3px; color: var(--text-muted); font-size: 8px; }
.file-tree-more { width: 100%; min-height: 44px; display: flex; align-items: center; gap: 8px; padding: 0 16px 0 calc(var(--tree-indent) + 12px); color: var(--cyan); border: 0; border-bottom: 1px solid rgba(150,176,210,.07); background: rgba(88,224,255,.02); cursor: pointer; text-align: left; }
.file-tree-more small { color: var(--text-muted); }
.file-tree-more:disabled { opacity: .55; cursor: wait; }
.file-tree-empty { min-height: 42px; display: flex; align-items: center; gap: 8px; padding-left: calc(var(--tree-indent) + 12px); color: var(--text-muted); border-bottom: 1px solid rgba(150,176,210,.05); font-size: 10px; }
.tree-branch { width: 14px; height: 18px; border-bottom: 1px solid var(--line-strong); border-left: 1px solid var(--line-strong); }
.mini-spinner { width: 15px; height: 15px; display: inline-block; flex: 0 0 auto; border: 2px solid rgba(88,224,255,.17); border-top-color: var(--cyan); border-radius: 50%; animation: spin .7s linear infinite; }
.mini-spinner.is-dark { border-color: rgba(4,16,20,.2); border-top-color: #041014; }

.browser-dialog-layer { position: fixed; inset: 0; z-index: 240; display: grid; place-items: center; padding: 20px; background: rgba(1,4,8,.76); backdrop-filter: blur(7px); }
.browser-dialog { width: min(480px,100%); overflow: hidden; border: 1px solid var(--line-strong); border-radius: 17px; background: #0e1520; box-shadow: 0 30px 90px rgba(0,0,0,.58); }
.browser-dialog > header { display: flex; align-items: center; justify-content: space-between; padding: 19px 21px; border-bottom: 1px solid var(--line); }
.browser-dialog h3 { margin: 5px 0 0; color: var(--text-strong); font-size: 16px; }
.browser-dialog__body { padding: 21px; }
.browser-dialog__body .field { display: flex; flex-direction: column; gap: 8px; }
.browser-dialog__body label { color: var(--text); font-size: 10px; }
.browser-dialog__body .field-help { margin: 0; overflow-wrap: anywhere; color: var(--text-muted); font-size: 9px; }
.browser-dialog > footer { display: flex; justify-content: flex-end; gap: 8px; padding: 14px 20px; border-top: 1px solid var(--line); background: rgba(4,8,14,.32); }

@media (max-width: 720px) {
  .indexed-browser.is-tree-view { width: 100vw; }
  .index-toolbar.is-tree { align-items: start; }
  .tree-location div { display: block; }
  .tree-location span { display: block; margin-top: 3px; }
  .index-toolbar__actions .is-ghost { display: none; }
  .tree-manager { align-items: stretch; flex-direction: column; }
  .tree-manager__target strong { max-width: 100%; }
  .tree-manager__actions { justify-content: flex-start; }
  .remote-move-progress__meta { gap: 10px; flex-wrap: wrap; }
  .remote-move-progress__file { width: 100%; margin-left: 0; }
}

/* Theme-aware contrast normalization. */
.tree-manager { background: var(--surface-inset); }
.file-tree-row { color: var(--text); border-bottom-color: var(--row-line); }
.file-tree-row:hover { background: var(--surface-hover); }
.file-tree-row.is-root { background: var(--surface-soft); }
.file-tree-row.is-active-directory,
.file-tree-row.is-selected-file { background: var(--surface-selected); }
.file-tree-row.is-selected-file:hover { background: var(--surface-hover); }
.mini-spinner.is-dark { border-color: rgba(var(--cyan-rgb),.22); border-top-color: var(--text-on-accent); }
.browser-dialog-layer { background: var(--overlay-background); }
.browser-dialog { background: var(--modal-background); box-shadow: var(--shadow-card); }
.browser-dialog > footer { background: var(--surface-inset); }
</style>
