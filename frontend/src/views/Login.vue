<template>
  <div class="login-page">
    <n-card class="login-card" :bordered="false">
      <div class="login-brand">
        <span class="login-icon">◇</span>
        <div>
          <h1>Rclone Sync Hub</h1>
          <p>登录后管理文件扫描与上传任务</p>
        </div>
      </div>
      <n-form ref="formRef" :model="form" :rules="rules" @submit.prevent="submit">
        <n-form-item path="username" label="用户名">
          <n-input v-model:value="form.username" autocomplete="username" placeholder="请输入用户名" />
        </n-form-item>
        <n-form-item path="password" label="密码">
          <n-input
            v-model:value="form.password"
            type="password"
            show-password-on="click"
            autocomplete="current-password"
            placeholder="请输入密码"
            @keyup.enter="submit"
          />
        </n-form-item>
        <n-alert v-if="errorMessage" type="error" class="login-error">{{ errorMessage }}</n-alert>
        <n-button type="primary" block attr-type="submit" :loading="loading">登录</n-button>
      </n-form>
    </n-card>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NAlert, NButton, NCard, NForm, NFormItem, NInput } from 'naive-ui';
import { login } from '../api/auth';

const route = useRoute();
const router = useRouter();
const formRef = ref(null);
const loading = ref(false);
const errorMessage = ref('');
const form = reactive({ username: '', password: '' });
const rules = {
  username: { required: true, message: '请输入用户名', trigger: ['blur', 'input'] },
  password: { required: true, message: '请输入密码', trigger: ['blur', 'input'] }
};

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  loading.value = true;
  errorMessage.value = '';
  try {
    await login(form.username.trim(), form.password);
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard';
    await router.replace(redirect);
  } catch (error) {
    if (error.response?.status === 429) {
      errorMessage.value = '登录尝试过多，请稍后再试';
    } else {
      errorMessage.value = '用户名或密码错误';
    }
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background:
    radial-gradient(circle at 20% 10%, rgba(14, 165, 233, 0.16), transparent 34%),
    radial-gradient(circle at 90% 90%, rgba(139, 92, 246, 0.12), transparent 38%),
    #f1f5f9;
}

.login-card {
  width: min(420px, 100%);
  padding: 14px;
  border-radius: 16px;
  box-shadow: 0 20px 60px rgba(15, 23, 42, 0.12);
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 24px;
}

.login-icon {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  border-radius: 12px;
  color: white;
  font-size: 24px;
  background: linear-gradient(135deg, #0ea5e9, #6366f1);
}

h1 {
  margin: 0;
  color: #0f172a;
  font-size: 20px;
}

p {
  margin: 4px 0 0;
  color: #64748b;
  font-size: 13px;
}

.login-error {
  margin-bottom: 16px;
}
</style>
