import http from './http';

const TOKEN_KEY = 'rsh_access_token';
const USER_KEY = 'rsh_user';

export async function getAuthConfig() {
  const { data } = await http.get('/api/auth/config');
  return data;
}

export async function login(username, password) {
  const { data } = await http.post('/api/auth/login', { username, password });
  sessionStorage.setItem(TOKEN_KEY, data.access_token);
  sessionStorage.setItem(USER_KEY, JSON.stringify(data.user));
  return data;
}

export function logout() {
  sessionStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(USER_KEY);
}

export function hasToken() {
  return Boolean(sessionStorage.getItem(TOKEN_KEY));
}

export function currentUser() {
  try {
    return JSON.parse(sessionStorage.getItem(USER_KEY) || 'null');
  } catch {
    return null;
  }
}
