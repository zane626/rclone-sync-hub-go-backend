import axios from 'axios';

const http = axios.create({
  baseURL: '',
  timeout: 15000
});

http.interceptors.request.use((config) => {
  const token = localStorage.getItem('rsh_access_token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

http.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401 && !String(error.config?.url || '').includes('/api/auth/login')) {
      localStorage.removeItem('rsh_access_token');
      localStorage.removeItem('rsh_user');
      if (window.location.hash !== '#/login') {
        window.location.hash = '#/login';
      }
    }
    return Promise.reject(error);
  }
);

export default http;

