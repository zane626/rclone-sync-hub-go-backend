import http from './http';

/**
 * 获取仪表盘全量数据：概览、按状态/按文件夹/按时间、最近任务与失败任务列表
 * @param {number} [days=7] 趋势图统计天数
 * @returns {Promise<DashboardData>}
 */
export function getDashboard(days = 7) {
  return http
    .get('/api/analytics/dashboard', { params: { days } })
    .then((res) => res.data);
}
