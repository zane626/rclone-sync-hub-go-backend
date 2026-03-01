import Dashboard from '../views/Dashboard.vue';
import FolderManager from '../views/FolderManager.vue';
import TaskList from '../views/TaskList.vue';
import Settings from '../views/Settings.vue';

const routes = [
  {
    path: '/',
    redirect: '/dashboard'
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: Dashboard,
    meta: { title: '工作台' }
  },
  {
    path: '/folders',
    name: 'FolderManager',
    component: FolderManager,
    meta: { title: '文件夹管理' }
  },
  {
    path: '/tasks',
    name: 'TaskList',
    component: TaskList,
    meta: { title: '任务列表' }
  },
  {
    path: '/settings',
    name: 'Settings',
    component: Settings,
    meta: { title: '全局设置' }
  }
];

export default routes;

