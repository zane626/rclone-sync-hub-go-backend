import { computed, ref } from 'vue';

const THEME_KEY = 'rsh_theme';
const theme = ref('dark');

function preferredTheme() {
  const saved = localStorage.getItem(THEME_KEY);
  if (saved === 'light' || saved === 'dark') return saved;
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

function applyTheme(value) {
  const next = value === 'light' ? 'light' : 'dark';
  theme.value = next;
  document.documentElement.dataset.theme = next;
  document.documentElement.style.colorScheme = next;
}

export function initializeTheme() {
  applyTheme(preferredTheme());
}

export function useTheme() {
  const isDark = computed(() => theme.value === 'dark');

  function setTheme(value) {
    applyTheme(value);
    localStorage.setItem(THEME_KEY, theme.value);
  }

  function toggleTheme() {
    setTheme(isDark.value ? 'light' : 'dark');
  }

  return { theme, isDark, setTheme, toggleTheme };
}

