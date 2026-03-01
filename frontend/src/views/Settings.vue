<template>
  <div class="settings-page">
    <div class="app-card settings-block">
      <h2 class="app-section-title">全局外观</h2>
      <n-form label-placement="left" label-width="120" class="settings-form">
        <n-form-item label="主题">
          <n-radio-group v-model:value="theme" size="small">
            <n-space>
              <n-radio value="light">浅色</n-radio>
              <n-radio value="dark">深色</n-radio>
              <n-radio value="system">跟随系统</n-radio>
            </n-space>
          </n-radio-group>
        </n-form-item>
        <n-form-item label="默认分页大小">
          <n-input-number v-model:value="pageSize" :min="10" :max="100" size="small" />
        </n-form-item>
      </n-form>
    </div>

    <div class="app-card settings-block">
      <h2 class="app-section-title">默认 Remote / 路径</h2>
      <p class="settings-tip">
        从 <code>/api/rclone/configs</code> 拉取 remote 列表，选择默认使用的 remote 与路径前缀。
      </p>
      <n-form label-placement="left" label-width="120" class="settings-form">
        <n-form-item label="默认 remote">
          <n-select
            v-model:value="defaultRemote"
            :options="remoteOptions"
            placeholder="选择默认 remote"
            size="small"
            style="max-width: 240px"
          />
        </n-form-item>
        <n-form-item label="远端路径前缀">
          <n-input
            v-model:value="defaultRemotePathPrefix"
            placeholder="/backup"
            size="small"
            style="max-width: 240px"
          />
        </n-form-item>
      </n-form>
    </div>

    <div class="app-card settings-block">
      <h2 class="app-section-title">系统信息</h2>
      <n-descriptions :column="2" label-placement="left" bordered size="small" class="settings-desc">
        <n-descriptions-item label="API Host">localhost:8080</n-descriptions-item>
        <n-descriptions-item label="BasePath">/</n-descriptions-item>
        <n-descriptions-item label="API 版本">1.0</n-descriptions-item>
        <n-descriptions-item label="描述">文件上传调度系统 REST API</n-descriptions-item>
      </n-descriptions>
    </div>

    <div class="settings-actions">
      <n-button type="primary" @click="handleSave">保存设置</n-button>
      <n-button quaternary @click="handleReset">重置</n-button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import {
  NCard,
  NForm,
  NFormItem,
  NRadioGroup,
  NRadio,
  NInputNumber,
  NInput,
  NSelect,
  NSpace,
  NButton,
  NDescriptions,
  NDescriptionsItem,
  useMessage
} from 'naive-ui';
import { fetchRcloneConfigs } from '../api/rclone';

const message = useMessage();

const theme = ref('light');
const pageSize = ref(20);
const defaultRemote = ref(null);
const defaultRemotePathPrefix = ref('/backup');

const remoteOptions = ref([]);

function handleSave() {
  message.success('设置已保存到前端状态，后续可接入后端配置接口');
}

function handleReset() {
  theme.value = 'light';
  pageSize.value = 20;
  defaultRemote.value = null;
  defaultRemotePathPrefix.value = '/backup';
}

onMounted(async () => {
  try {
    const data = await fetchRcloneConfigs();
    const options = [];
    if (data && Array.isArray(data.items)) {
      data.items.forEach(({ name }) => {
        options.push({ label: name, value: name });
      });
    } else if (data && typeof data === 'object') {
      Object.keys(data).forEach((name) => {
        options.push({ label: name, value: name });
      });
    }
    remoteOptions.value = options;
  } catch (e) {
    remoteOptions.value = [];
  }
});
</script>

<style scoped>
.settings-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-page);
  max-width: 720px;
}

.settings-block {
  padding: var(--space-card);
}

.settings-form {
  margin-top: 4px;
}

.settings-tip {
  margin: 0 0 14px 0;
  font-size: 12px;
  color: #64748b;
  line-height: 1.5;
}

.settings-tip code {
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 2px 6px;
  background: #f1f5f9;
  border-radius: 4px;
  color: #475569;
}

.settings-desc {
  margin-top: 4px;
}

.settings-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-top: 8px;
}
</style>
