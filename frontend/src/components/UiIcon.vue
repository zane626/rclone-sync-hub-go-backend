<template>
  <svg
    class="ui-icon"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="strokeWidth"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="path" />
  </svg>
</template>

<script setup>
import { computed } from 'vue';

const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 18 },
  strokeWidth: { type: [Number, String], default: 1.8 }
});

const paths = {
  activity: 'M3 12h4l3-8 4 16 3-8h4',
  alert: 'M12 9v4m0 4h.01M10.3 3.6 2.2 18a2 2 0 0 0 1.7 3h16.2a2 2 0 0 0 1.7-3L13.7 3.6a2 2 0 0 0-3.4 0Z',
  arrowLeft: 'm15 18-6-6 6-6',
  check: 'm5 12 4 4L19 6',
  chevronDown: 'm6 9 6 6 6-6',
  chevronLeft: 'm15 18-6-6 6-6',
  chevronRight: 'm9 18 6-6-6-6',
  clock: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Zm0-14v4l3 2',
  close: 'M18 6 6 18M6 6l12 12',
  database: 'M20 6c0 2-3.6 3.5-8 3.5S4 8 4 6s3.6-3.5 8-3.5S20 4 20 6Zm0 0v6c0 2-3.6 3.5-8 3.5S4 14 4 12V6m16 6v6c0 2-3.6 3.5-8 3.5S4 20 4 18v-6',
  edit: 'M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4Z',
  eye: 'M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Zm10 3a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z',
  eyeOff: 'm3 3 18 18M10.6 10.7a2 2 0 0 0 2.7 2.7M9.9 4.2A10.7 10.7 0 0 1 12 4c6.5 0 10 8 10 8a18 18 0 0 1-2.1 3.2M6.6 6.6C3.6 8.4 2 12 2 12s3.5 8 10 8a10.7 10.7 0 0 0 5.4-1.4',
  file: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Zm0 0v6h6',
  filter: 'M4 5h16M7 12h10m-7 7h4',
  folder: 'M3 6h6l2 2h10v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z',
  gauge: 'M4.9 19a9 9 0 1 1 14.2 0M12 13l4-4',
  grid: 'M3 3h7v7H3Zm11 0h7v7h-7ZM3 14h7v7H3Zm11 0h7v7h-7Z',
  info: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Zm0-11v6m0-10h.01',
  layers: 'm12 2 9 5-9 5-9-5Zm-9 10 9 5 9-5M3 17l9 5 9-5',
  lock: 'M6 10V7a6 6 0 0 1 12 0v3m1 0H5a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-8a2 2 0 0 0-2-2Z',
  logout: 'M10 17l5-5-5-5m5 5H3m18 7V5a2 2 0 0 0-2-2h-6',
  menu: 'M4 6h16M4 12h16M4 18h16',
  pause: 'M8 5v14m8-14v14',
  play: 'm7 4 13 8-13 8Z',
  plus: 'M12 5v14M5 12h14',
  radar: 'M12 12 20 4M12 2a10 10 0 1 0 10 10M12 6a6 6 0 1 0 6 6M12 10a2 2 0 1 0 2 2',
  refresh: 'M20 6v5h-5M4 18v-5h5m10.5-4A8 8 0 0 0 6.2 6L4 11m16 2-2.2 5a8 8 0 0 1-13.3-2',
  search: 'm21 21-4.4-4.4M19 11a8 8 0 1 1-16 0 8 8 0 0 1 16 0Z',
  server: 'M4 4h16v6H4Zm0 10h16v6H4ZM8 7h.01M8 17h.01',
  shield: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Zm-3-10 2 2 4-4',
  spark: 'm12 2 1.4 5.6L19 9l-5.6 1.4L12 16l-1.4-5.6L5 9l5.6-1.4ZM19 15l.8 3.2L23 19l-3.2.8L19 23l-.8-3.2L15 19l3.2-.8Z',
  tasks: 'M9 5h11M9 12h11M9 19h11M4 5h.01M4 12h.01M4 19h.01',
  terminal: 'm4 17 6-5-6-5m8 10h8',
  trash: 'M3 6h18M8 6V3h8v3m-9 0 1 15h8l1-15M10 10v7m4-7v7',
  upload: 'M12 16V4m0 0L7 9m5-5 5 5M5 20h14',
  user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM4 21a8 8 0 0 1 16 0'
};

const path = computed(() => paths[props.name] || paths.info);
</script>
