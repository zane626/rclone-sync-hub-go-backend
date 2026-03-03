<template>
  <n-config-provider :locale="zhCN" :date-locale="dateZhCN" :theme-overrides="themeOverrides">
    <n-loading-bar-provider>
      <n-message-provider>
        <n-dialog-provider>
          <n-layout class="app-layout">
            <n-layout-header class="app-header">
              <div class="app-header-left">
                <a href="#/" class="app-logo">
                  <span class="app-logo-icon">◇</span>
                  <span class="app-logo-title">Rclone Sync Hub</span>
                </a>
                <n-menu
                  :value="activeKey"
                  :options="menuOptions"
                  mode="horizontal"
                  class="app-nav"
                  @update:value="handleMenuSelect"
                />
              </div>
              <!-- 设置入口已暂时屏蔽 -->
            </n-layout-header>
            <n-layout-content content-style="padding: 0; display: flex; flex-direction: column; min-height: 0;" class="app-content">
              <div class="app-content-scroll">
                <router-view v-slot="{ Component }">
                  <transition name="page-fade" mode="out-in">
                    <div class="app-page">
                      <component :is="Component" />
                    </div>
                  </transition>
                </router-view>
              </div>
            </n-layout-content>
            <n-layout-footer class="app-footer">
              <div class="app-footer-inner">
                <span class="app-footer-item">{{ footer.projectName }}</span>
                <span class="app-footer-sep">·</span>
                <span class="app-footer-item">v{{ footer.version }}</span>
                <span class="app-footer-sep">·</span>
                <a :href="footer.githubUrl" target="_blank" rel="noopener noreferrer" class="app-footer-link">GitHub</a>
              </div>
            </n-layout-footer>
          </n-layout>
        </n-dialog-provider>
      </n-message-provider>
    </n-loading-bar-provider>
  </n-config-provider>
</template>

<script setup>
import { watch, ref } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import {
  NConfigProvider,
  NLayout,
  NLayoutHeader,
  NLayoutContent,
  NLayoutFooter,
  NMenu,
  NButton,
  zhCN,
  dateZhCN
} from 'naive-ui';
import pkg from '../package.json';

const footer = {
  projectName: 'Rclone Sync Hub',
  version: pkg.version,
  githubUrl: 'https://github.com/rclone/rclone-sync-hub-go-backend'
};

const router = useRouter();
const route = useRoute();

const menuOptions = [
  { label: '工作台', key: '/dashboard' },
  { label: '文件夹管理', key: '/folders' },
  { label: '任务列表', key: '/tasks' }
];

const activeKey = ref(route.path);

watch(
  () => route.path,
  (path) => {
    activeKey.value = path.startsWith('/dashboard')
      || path.startsWith('/folders')
      || path.startsWith('/tasks')
      ? path
      : activeKey.value;
  }
);

const themeOverrides = {
  common: {
    primaryColor: '#0ea5e9',
    primaryColorHover: '#38bdf8',
    primaryColorPressed: '#0284c7',
    borderRadius: '8px',
    fontFamily: 'var(--font-sans)'
  }
};

function handleMenuSelect(key) {
  router.push(key);
}
</script>

<style scoped>
.app-layout {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: var(--content-bg);
}

.app-header {
  height: 56px;
  padding: 0 20px 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  background: var(--card-bg);
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  box-shadow: 0 1px 0 rgba(0, 0, 0, 0.02);
  overflow: visible;
}

.app-header-left {
  display: flex;
  align-items: center;
  gap: 24px;
  overflow: visible;
}

.app-logo {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 56px;
  text-decoration: none;
  color: #1e293b;
  font-weight: 600;
  font-size: 15px;
  flex-shrink: 0;
}

.app-logo:hover {
  color: var(--app-primary);
}

.app-logo-icon {
  font-size: 16px;
  color: var(--app-primary);
  flex-shrink: 0;
}

.app-logo-title {
  white-space: nowrap;
  line-height: 1;
  overflow: visible;
  flex-shrink: 0;
}

.app-nav {
  flex: 0 0 auto;
  height: 56px;
  line-height: 56px;
}

.app-nav :deep(.n-menu-item-content) {
  height: 56px;
  line-height: 56px;
  padding: 0 14px;
  border-radius: var(--radius-sm);
  font-size: 14px;
  color: #64748b;
}

.app-nav :deep(.n-menu-item-content:hover) {
  color: #1e293b;
  background: rgba(0, 0, 0, 0.04);
}

.app-nav :deep(.n-menu-item-content.n-menu-item-content--selected) {
  color: var(--app-primary);
  font-weight: 500;
  background: var(--app-primary-light);
}

.app-header-btn {
  color: #64748b;
  font-size: 13px;
}

.app-header-btn:hover {
  color: var(--app-primary);
}

.btn-icon {
  margin-right: 6px;
  opacity: 0.9;
}

.app-content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.app-content-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.app-footer {
  flex-shrink: 0;
  padding: 10px 20px;
  background: var(--card-bg);
  border-top: 1px solid rgba(0, 0, 0, 0.06);
}

.app-footer-inner {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 12px;
  color: #64748b;
}

.app-footer-item {
  color: #64748b;
}

.app-footer-sep {
  color: #cbd5e1;
  user-select: none;
}

.app-footer-link {
  color: var(--app-primary);
  text-decoration: none;
}

.app-footer-link:hover {
  text-decoration: underline;
}

.page-fade-enter-active,
.page-fade-leave-active {
  transition: opacity 0.15s ease;
}

.page-fade-enter-from,
.page-fade-leave-to {
  opacity: 0;
}
</style>
