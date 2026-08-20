<template>
  <router-view v-if="route.meta.public" />

  <div v-else class="app-shell">
    <Transition name="backdrop-fade">
      <button v-if="sidebarOpen" class="mobile-backdrop" type="button" aria-label="关闭导航" @click="sidebarOpen = false" />
    </Transition>

    <aside class="app-sidebar" :class="{ 'is-open': sidebarOpen }">
      <a href="#/dashboard" class="brand" aria-label="返回工作台">
        <span class="brand-mark">
          <span class="brand-mark__orbit" />
          <UiIcon name="layers" :size="23" :stroke-width="1.6" />
        </span>
        <span class="brand-copy">
          <strong>RCLONE</strong>
          <small>SYNC HUB</small>
        </span>
      </a>

      <div class="engine-status">
        <span class="engine-status__pulse" />
        <div>
          <strong>传输引擎在线</strong>
          <small>ENGINE / OPERATIONAL</small>
        </div>
        <span class="engine-status__code">01</span>
      </div>

      <nav class="side-nav" aria-label="主导航">
        <span class="side-nav__label">COMMAND</span>
        <router-link v-for="item in navItems" :key="item.path" :to="item.path" class="side-nav__item">
          <span class="side-nav__icon"><UiIcon :name="item.icon" :size="19" /></span>
          <span>
            <strong>{{ item.label }}</strong>
            <small>{{ item.caption }}</small>
          </span>
          <span class="side-nav__index">{{ item.index }}</span>
        </router-link>
      </nav>

      <div class="sidebar-footer">
        <div class="account-card">
          <span class="account-card__avatar">{{ initials }}</span>
          <span class="account-card__copy">
            <strong>{{ user?.username || 'LOCAL USER' }}</strong>
            <small>{{ roleLabel }}</small>
          </span>
          <button class="icon-button" type="button" title="退出登录" aria-label="退出登录" @click="handleLogout">
            <UiIcon name="logout" :size="17" />
          </button>
        </div>
        <div class="build-meta">
          <span>CONTROL NODE</span>
          <span>v{{ version }}</span>
        </div>
      </div>
    </aside>

    <main class="app-main">
      <header class="topbar">
        <div class="topbar__left">
          <button class="icon-button mobile-menu" type="button" aria-label="打开导航" @click="sidebarOpen = true">
            <UiIcon name="menu" :size="20" />
          </button>
          <div class="breadcrumb">
            <span>RSH</span>
            <UiIcon name="chevronRight" :size="14" />
            <strong>{{ pageMeta.label }}</strong>
          </div>
        </div>
        <div class="topbar__right">
          <div class="live-clock">
            <span>{{ clock.date }}</span>
            <strong>{{ clock.time }}</strong>
          </div>
          <div class="node-pill"><span /> API NODE CN-01</div>
          <button class="topbar-account" type="button" title="退出登录" @click="handleLogout">
            <span>{{ initials }}</span>
            <div>
              <strong>{{ user?.username || 'Operator' }}</strong>
              <small>{{ roleLabel }}</small>
            </div>
          </button>
        </div>
      </header>

      <div class="app-viewport">
        <router-view v-slot="{ Component }">
          <Transition name="page-shift" mode="out-in">
            <component :is="Component" />
          </Transition>
        </router-view>
      </div>
    </main>
  </div>

  <UiOverlay />
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import pkg from '../package.json';
import UiIcon from './components/UiIcon.vue';
import UiOverlay from './components/UiOverlay.vue';
import { currentUser, logout } from './api/auth';

const route = useRoute();
const router = useRouter();
const sidebarOpen = ref(false);
const user = ref(currentUser());
const version = pkg.version;
const clock = reactive({ date: '', time: '' });
let clockTimer;

const navItems = [
  { path: '/dashboard', label: '运行总览', caption: 'Overview', icon: 'grid', index: '01' },
  { path: '/folders', label: '监控目录', caption: 'Watch folders', icon: 'folder', index: '02' },
  { path: '/tasks', label: '任务中心', caption: 'Transfer queue', icon: 'tasks', index: '03' }
];

const pageMeta = computed(() => navItems.find((item) => route.path.startsWith(item.path)) || navItems[0]);
const initials = computed(() => String(user.value?.username || 'RS').slice(0, 2).toUpperCase());
const roleLabel = computed(() => user.value?.role === 'viewer' ? '只读观察员' : '系统管理员');

function updateClock() {
  const now = new Date();
  clock.date = new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', weekday: 'short' }).format(now);
  clock.time = new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(now);
}

function handleLogout() {
  logout();
  user.value = null;
  router.replace('/login');
}

watch(
  () => route.fullPath,
  () => {
    user.value = currentUser();
    sidebarOpen.value = false;
    document.title = `${route.meta.title || '控制台'} · Rclone Sync Hub`;
  },
  { immediate: true }
);

onMounted(() => {
  updateClock();
  clockTimer = window.setInterval(updateClock, 1000);
});
onBeforeUnmount(() => window.clearInterval(clockTimer));
</script>

<style scoped>
.app-shell { min-height: 100vh; }

.app-sidebar {
  position: fixed;
  inset: 0 auto 0 0;
  z-index: 40;
  width: var(--sidebar-width);
  display: flex;
  flex-direction: column;
  padding: 24px 18px 18px;
  background: rgba(7, 11, 18, 0.94);
  border-right: 1px solid var(--line);
  box-shadow: 24px 0 80px rgba(0, 0, 0, 0.28);
  backdrop-filter: blur(24px);
}

.brand { display: flex; align-items: center; gap: 13px; min-height: 52px; padding: 0 8px; color: var(--text-strong); text-decoration: none; }
.brand-mark { position: relative; width: 44px; height: 44px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88, 224, 255, 0.3); border-radius: 14px; background: linear-gradient(145deg, rgba(88, 224, 255, 0.16), rgba(124, 92, 255, 0.1)); box-shadow: inset 0 0 20px rgba(88, 224, 255, 0.08), 0 0 26px rgba(88, 224, 255, 0.08); }
.brand-mark__orbit { position: absolute; inset: -5px; border: 1px solid rgba(88, 224, 255, 0.14); border-radius: 17px; transform: rotate(8deg); }
.brand-copy { display: flex; flex-direction: column; line-height: 1; }
.brand-copy strong { font-size: 16px; letter-spacing: 0.16em; }
.brand-copy small { margin-top: 7px; color: var(--text-muted); font: 600 9px/1 var(--font-mono); letter-spacing: 0.32em; }

.engine-status { display: grid; grid-template-columns: auto 1fr auto; align-items: center; gap: 10px; margin: 25px 5px 28px; padding: 12px; border: 1px solid rgba(61, 225, 162, 0.15); border-radius: 12px; background: rgba(61, 225, 162, 0.045); }
.engine-status__pulse { width: 8px; height: 8px; border-radius: 50%; background: var(--green); box-shadow: 0 0 0 5px rgba(61, 225, 162, 0.08), 0 0 14px var(--green); }
.engine-status strong { display: block; color: #dffcf2; font-size: 12px; font-weight: 600; }
.engine-status small { display: block; margin-top: 4px; color: #4e7d6b; font: 500 8px/1 var(--font-mono); letter-spacing: 0.1em; }
.engine-status__code { color: #3f6d5c; font: 600 10px var(--font-mono); }

.side-nav { display: flex; flex-direction: column; gap: 7px; }
.side-nav__label { margin: 0 12px 8px; color: #475366; font: 600 9px var(--font-mono); letter-spacing: 0.22em; }
.side-nav__item { position: relative; display: grid; grid-template-columns: 38px 1fr auto; align-items: center; gap: 9px; min-height: 60px; padding: 8px 12px; color: var(--text-muted); text-decoration: none; border: 1px solid transparent; border-radius: 13px; transition: 180ms ease; }
.side-nav__item::before { content: ''; position: absolute; left: -19px; width: 2px; height: 0; border-radius: 2px; background: var(--cyan); box-shadow: 0 0 14px var(--cyan); transition: 180ms ease; }
.side-nav__item:hover { color: var(--text); background: rgba(255,255,255,.025); transform: translateX(2px); }
.side-nav__item.router-link-active { color: var(--text-strong); border-color: rgba(88,224,255,.13); background: linear-gradient(90deg, rgba(88,224,255,.1), rgba(88,224,255,.025)); }
.side-nav__item.router-link-active::before { height: 28px; }
.side-nav__icon { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 10px; background: rgba(255,255,255,.03); }
.router-link-active .side-nav__icon { color: var(--cyan); background: rgba(88,224,255,.1); }
.side-nav__item strong { display: block; font-size: 13px; font-weight: 600; }
.side-nav__item small { display: block; margin-top: 3px; color: #596477; font: 500 9px var(--font-mono); text-transform: uppercase; letter-spacing: .05em; }
.side-nav__index { color: #3d4757; font: 500 9px var(--font-mono); }

.sidebar-footer { margin-top: auto; }
.account-card { display: grid; grid-template-columns: 36px 1fr 32px; align-items: center; gap: 10px; padding: 12px 10px; border: 1px solid var(--line); border-radius: 13px; background: rgba(255,255,255,.025); }
.account-card__avatar, .topbar-account > span { display: grid; place-items: center; border-radius: 10px; color: var(--cyan); background: linear-gradient(145deg, rgba(88,224,255,.16), rgba(124,92,255,.12)); font: 700 11px var(--font-mono); }
.account-card__avatar { width: 36px; height: 36px; }
.account-card__copy { min-width: 0; }
.account-card__copy strong { display: block; overflow: hidden; color: var(--text); font-size: 11px; text-overflow: ellipsis; }
.account-card__copy small { display: block; margin-top: 3px; color: var(--text-muted); font-size: 10px; }
.build-meta { display: flex; justify-content: space-between; padding: 13px 5px 0; color: #414b5a; font: 500 8px var(--font-mono); letter-spacing: .1em; }

.app-main { min-height: 100vh; margin-left: var(--sidebar-width); }
.topbar { position: sticky; top: 0; z-index: 30; height: var(--topbar-height); display: flex; align-items: center; justify-content: space-between; padding: 0 30px; border-bottom: 1px solid var(--line); background: rgba(9, 14, 22, .78); backdrop-filter: blur(22px); }
.topbar__left, .topbar__right, .breadcrumb, .topbar-account { display: flex; align-items: center; }
.breadcrumb { gap: 10px; color: #556174; font: 500 11px var(--font-mono); letter-spacing: .08em; }
.breadcrumb strong { color: var(--text); font-weight: 600; }
.topbar__right { gap: 14px; }
.live-clock { display: flex; align-items: baseline; gap: 10px; padding-right: 15px; border-right: 1px solid var(--line); font: 500 10px var(--font-mono); }
.live-clock span { color: var(--text-muted); }
.live-clock strong { color: var(--text); letter-spacing: .08em; }
.node-pill { display: flex; align-items: center; gap: 7px; padding: 8px 11px; color: #718093; border: 1px solid var(--line); border-radius: 999px; font: 500 9px var(--font-mono); letter-spacing: .07em; }
.node-pill span { width: 6px; height: 6px; border-radius: 50%; background: var(--green); box-shadow: 0 0 8px var(--green); }
.topbar-account { gap: 9px; padding: 0; color: inherit; border: 0; background: none; cursor: pointer; }
.topbar-account > span { width: 34px; height: 34px; }
.topbar-account div { text-align: left; }
.topbar-account strong, .topbar-account small { display: block; }
.topbar-account strong { color: var(--text); font-size: 11px; }
.topbar-account small { margin-top: 2px; color: var(--text-muted); font-size: 9px; }
.mobile-menu { display: none; }
.app-viewport { position: relative; min-height: calc(100vh - var(--topbar-height)); overflow: hidden; }
.mobile-backdrop { display: none; }
.page-shift-enter-active, .page-shift-leave-active { transition: opacity .18s ease, transform .18s ease; }
.page-shift-enter-from { opacity: 0; transform: translateY(7px); }
.page-shift-leave-to { opacity: 0; transform: translateY(-4px); }

@media (max-width: 980px) {
  .app-sidebar { transform: translateX(-105%); transition: transform .24s ease; }
  .app-sidebar.is-open { transform: translateX(0); }
  .app-main { margin-left: 0; }
  .mobile-menu { display: grid; margin-right: 12px; }
  .mobile-backdrop { position: fixed; inset: 0; z-index: 35; display: block; border: 0; background: rgba(0,0,0,.62); backdrop-filter: blur(3px); }
  .live-clock, .node-pill { display: none; }
}

@media (max-width: 600px) {
  .topbar { padding: 0 16px; }
  .topbar-account div { display: none; }
}
</style>
