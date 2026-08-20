const Dashboard = () => import('../views/Dashboard.vue');
const FolderManager = () => import('../views/FolderManager.vue');
const TaskList = () => import('../views/TaskList.vue');
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
    path: '/:pathMatch(.*)*',
    redirect: '/dashboard'
  }
];

export default routes;

