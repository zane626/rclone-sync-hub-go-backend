import { createApp } from 'vue';
import { createRouter, createWebHashHistory } from 'vue-router';
import {
  createDiscreteApi,
  create,
  NConfigProvider,
  NMessageProvider,
  NDialogProvider,
  NLoadingBarProvider
} from 'naive-ui';
import App from './App.vue';
import routes from './router';
import './assets/styles/global.css';

const app = createApp(App);

const router = createRouter({
  history: createWebHashHistory(),
  routes
});

const naive = create({
  components: [NConfigProvider, NMessageProvider, NDialogProvider, NLoadingBarProvider]
});

app.use(router);
app.use(naive);

app.mount('#app');

