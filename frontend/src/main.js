import { createApp } from 'vue';
import { createRouter, createWebHashHistory } from 'vue-router';
import App from './App.vue';
import routes from './router';
import './assets/styles/global.css';
import { getAuthConfig, hasToken } from './api/auth';

const app = createApp(App);
const router = createRouter({ history: createWebHashHistory(), routes });

let authConfigPromise;
router.beforeEach(async (to) => {
  authConfigPromise ||= getAuthConfig().catch(() => ({ enabled: true }));
  const authConfig = await authConfigPromise;
  if (!authConfig.enabled) return to.path === '/login' ? '/dashboard' : true;
  if (to.meta.public) return hasToken() ? '/dashboard' : true;
  if (!hasToken()) return { path: '/login', query: { redirect: to.fullPath } };
  return true;
});

app.use(router);
app.mount('#app');
