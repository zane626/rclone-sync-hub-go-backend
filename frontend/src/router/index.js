const Dashboard = () => import('../views/Dashboard.vue');
const FolderManager = () => import('../views/FolderManager.vue');
const TaskList = () => import('../views/TaskList.vue');
const OperationsCenter = () => import('../views/OperationsCenter.vue');
const Login = () => import('../views/Login.vue');

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: Login,
    meta: { title: '登录', public: true }
  },
  {
    path: '/',
    redirect: '/dashboard'
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: Dashboard,
    meta: { title: '运行总览' }
  },
  {
    path: '/folders',
    name: 'FolderManager',
    component: FolderManager,
    meta: { title: '监控目录' }
  },
  {
    path: '/tasks',
    name: 'TaskList',
    component: TaskList,
    meta: { title: '任务中心' }
  },
  {
    path: '/operations',
    name: 'OperationsCenter',
    component: OperationsCenter,
    meta: { title: '运行中心' }
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/dashboard'
  }
];

export default routes;

