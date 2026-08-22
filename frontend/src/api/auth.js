import http from './http';

const TOKEN_KEY = 'rsh_access_token';
const USER_KEY = 'rsh_user';

// Migrate sessions created by older versions. localStorage is shared by tabs,
// while sessionStorage forced every newly opened tab to log in again.
for (const key of [TOKEN_KEY, USER_KEY]) {
  if (!localStorage.getItem(key) && sessionStorage.getItem(key)) {
    localStorage.setItem(key, sessionStorage.getItem(key));
  }
  sessionStorage.removeItem(key);
}

export async function getAuthConfig() {
  const { data } = await http.get('/api/auth/config');
  return data;
}

export async function login(username, password) {
  const { data } = await http.post('/api/auth/login', { username, password });
  localStorage.setItem(TOKEN_KEY, data.access_token);
  localStorage.setItem(USER_KEY, JSON.stringify(data.user));
  return data;
}

export function logout() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
}

export function hasToken() {
  return Boolean(localStorage.getItem(TOKEN_KEY));
}

export function currentUser() {
  try {
    return JSON.parse(localStorage.getItem(USER_KEY) || 'null');
  } catch {
    return null;
  }
}
