<template>
  <main class="login-page">
    <div class="login-grid" />
    <div class="login-aurora is-cyan" />
    <div class="login-aurora is-violet" />
    <button class="icon-button login-theme-toggle" type="button" :title="isDark ? '切换到白天主题' : '切换到黑夜主题'" :aria-label="isDark ? '切换到白天主题' : '切换到黑夜主题'" @click="toggleTheme">
      <UiIcon :name="isDark ? 'sun' : 'moon'" :size="18" />
    </button>

    <section class="login-showcase" aria-label="产品介绍">
      <a class="login-brand" href="#/login">
        <span class="login-brand__mark">
          <span />
          <UiIcon name="layers" :size="26" />
        </span>
        <span><strong>RCLONE</strong><small>SYNC HUB</small></span>
      </a>

      <div class="showcase-copy">
        <span class="eyebrow">TRANSFER INTELLIGENCE</span>
        <h1>让每一次文件流动<br><em>清晰、可靠、可控。</em></h1>
        <p>面向生产环境的文件监控与传输中枢。持续扫描、任务编排、失败恢复和运行洞察，汇聚在同一个控制平面。</p>
      </div>

      <div class="orbital-visual" aria-hidden="true">
        <span class="orbit orbit-one" />
        <span class="orbit orbit-two" />
        <span class="orbit orbit-three" />
        <span class="orbital-core"><UiIcon name="upload" :size="31" /></span>
        <span class="satellite sat-one"><UiIcon name="folder" :size="16" /></span>
        <span class="satellite sat-two"><UiIcon name="database" :size="16" /></span>
        <span class="satellite sat-three"><UiIcon name="shield" :size="16" /></span>
      </div>

      <div class="showcase-metrics">
        <div><strong>24 / 7</strong><span>持续监控</span></div>
        <div><strong>AUTO</strong><span>故障恢复</span></div>
        <div><strong>LIVE</strong><span>实时进度</span></div>
      </div>
    </section>

    <section class="login-access">
      <div class="access-status"><span /> SECURE ACCESS CHANNEL</div>
      <form class="login-card" @submit.prevent="submit">
        <div class="login-card__index">AUTH / 01</div>
        <div class="login-card__heading">
          <span class="eyebrow">OPERATOR LOGIN</span>
          <h2>登录控制台</h2>
          <p>请输入授权凭证以访问传输节点。</p>
        </div>

        <div class="field">
          <label for="username">用户名</label>
          <div class="login-control">
            <UiIcon name="user" :size="17" />
            <input id="username" v-model="form.username" class="ui-control" autocomplete="username" placeholder="operator" autofocus />
          </div>
        </div>
        <div class="field">
          <label for="password">密码</label>
          <div class="login-control">
            <UiIcon name="lock" :size="17" />
            <input
              id="password"
              v-model="form.password"
              class="ui-control"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="current-password"
              placeholder="输入访问密码"
            />
            <button type="button" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword">
              <UiIcon :name="showPassword ? 'eyeOff' : 'eye'" :size="17" />
            </button>
          </div>
        </div>

        <Transition name="error-slide">
          <div v-if="errorMessage" class="login-error" role="alert">
            <UiIcon name="alert" :size="17" />
            <span>{{ errorMessage }}</span>
          </div>
        </Transition>

        <button class="login-submit" type="submit" :disabled="loading">
          <span v-if="loading" class="spinner" />
          <template v-else>
            <span>进入控制平面</span>
            <UiIcon name="arrowLeft" :size="18" class="submit-arrow" />
          </template>
        </button>

        <div class="login-card__footer">
          <span><UiIcon name="shield" :size="14" /> HMAC SIGNED SESSION</span>
          <span>TLS READY</span>
        </div>
      </form>
      <p class="access-footnote">RCLONE SYNC HUB · PRODUCTION CONTROL PLANE</p>
    </section>
  </main>
</template>

<script setup>
import { reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import UiIcon from '../components/UiIcon.vue';
import { login } from '../api/auth';
import { useTheme } from '../composables/useTheme';

const route = useRoute();
const router = useRouter();
const loading = ref(false);
const showPassword = ref(false);
const errorMessage = ref('');
const form = reactive({ username: '', password: '' });
const { isDark, toggleTheme } = useTheme();

async function submit() {
  if (!form.username.trim() || !form.password) {
    errorMessage.value = '请输入用户名和密码';
    return;
  }
  loading.value = true;
  errorMessage.value = '';
  try {
    await login(form.username.trim(), form.password);
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard';
    await router.replace(redirect);
  } catch (error) {
    errorMessage.value = error.response?.status === 429 ? '登录尝试过多，请稍后再试' : '用户名或密码错误';
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
.login-page { position: relative; min-height: 100vh; display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(420px, .85fr); overflow: hidden; background: #070b12; isolation: isolate; }
.login-theme-toggle { position: absolute; z-index: 5; top: 32px; left: calc(57.5% + 28px); }
.login-grid { position: absolute; inset: 0; z-index: -3; opacity: .28; background-image: linear-gradient(rgba(104,134,171,.13) 1px, transparent 1px), linear-gradient(90deg, rgba(104,134,171,.13) 1px, transparent 1px); background-size: 52px 52px; mask-image: radial-gradient(circle at 44% 45%, black, transparent 76%); }
.login-aurora { position: absolute; z-index: -2; width: 520px; height: 520px; border-radius: 50%; filter: blur(120px); opacity: .12; }
.login-aurora.is-cyan { top: -220px; left: 22%; background: var(--cyan); }
.login-aurora.is-violet { right: -180px; bottom: -240px; background: var(--violet); }
.login-showcase { position: relative; display: flex; flex-direction: column; min-height: 100vh; padding: clamp(28px, 5vw, 68px); border-right: 1px solid var(--line); }
.login-brand { display: flex; align-items: center; gap: 14px; color: var(--text-strong); text-decoration: none; }
.login-brand__mark { position: relative; width: 48px; height: 48px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.28); border-radius: 15px; background: rgba(88,224,255,.08); box-shadow: 0 0 34px rgba(88,224,255,.08); }
.login-brand__mark span { position: absolute; inset: -5px; border: 1px solid rgba(88,224,255,.1); border-radius: 18px; transform: rotate(8deg); }
.login-brand > span:last-child { display: flex; flex-direction: column; line-height: 1; }
.login-brand strong { font-size: 17px; letter-spacing: .18em; }
.login-brand small { margin-top: 7px; color: var(--text-muted); font: 600 9px var(--font-mono); letter-spacing: .32em; }
.showcase-copy { position: relative; z-index: 2; max-width: 650px; margin-top: clamp(70px, 13vh, 150px); }
.showcase-copy h1 { margin: 19px 0 22px; color: var(--text-strong); font-size: clamp(38px, 5vw, 72px); font-weight: 620; line-height: 1.05; letter-spacing: -.055em; }
.showcase-copy h1 em { color: transparent; background: linear-gradient(95deg, var(--cyan), #a690ff 72%); background-clip: text; font-style: normal; }
.showcase-copy p { max-width: 570px; margin: 0; color: #78869a; font-size: clamp(13px, 1.3vw, 16px); line-height: 1.9; }
.orbital-visual { position: absolute; right: clamp(20px, 6vw, 90px); bottom: clamp(95px, 14vh, 170px); width: 290px; height: 290px; opacity: .72; }
.orbit { position: absolute; inset: 50%; border: 1px solid rgba(88,224,255,.18); border-radius: 50%; transform: translate(-50%,-50%); }
.orbit::after { content: ''; position: absolute; top: 50%; left: -3px; width: 6px; height: 6px; border-radius: 50%; background: var(--cyan); box-shadow: 0 0 12px var(--cyan); }
.orbit-one { width: 118px; height: 118px; animation: orbit-spin 10s linear infinite; }
.orbit-two { width: 202px; height: 202px; border-color: rgba(140,118,255,.17); animation: orbit-spin 16s linear reverse infinite; }
.orbit-two::after { background: var(--violet); box-shadow: 0 0 12px var(--violet); }
.orbit-three { width: 286px; height: 286px; animation: orbit-spin 24s linear infinite; }
.orbital-core { position: absolute; inset: 50%; width: 78px; height: 78px; display: grid; place-items: center; color: var(--cyan); border: 1px solid rgba(88,224,255,.36); border-radius: 25px; background: rgba(88,224,255,.08); box-shadow: inset 0 0 24px rgba(88,224,255,.1), 0 0 44px rgba(88,224,255,.12); transform: translate(-50%,-50%) rotate(45deg); }
.orbital-core .ui-icon { transform: rotate(-45deg); }
.satellite { position: absolute; width: 38px; height: 38px; display: grid; place-items: center; color: var(--text-muted); border: 1px solid var(--line-strong); border-radius: 12px; background: #101722; }
.sat-one { top: 13px; left: 127px; }.sat-two { right: 18px; bottom: 62px; }.sat-three { left: 8px; bottom: 73px; }
.showcase-metrics { position: relative; z-index: 2; display: flex; gap: clamp(24px, 5vw, 66px); margin-top: auto; }
.showcase-metrics div { padding-left: 12px; border-left: 1px solid rgba(88,224,255,.24); }
.showcase-metrics strong, .showcase-metrics span { display: block; }
.showcase-metrics strong { color: var(--text-strong); font: 600 13px var(--font-mono); letter-spacing: .08em; }
.showcase-metrics span { margin-top: 5px; color: var(--text-muted); font-size: 10px; }
.login-access { position: relative; min-height: 100vh; display: grid; place-items: center; padding: 60px clamp(25px, 6vw, 90px); }
.access-status { position: absolute; top: 38px; right: 42px; display: flex; align-items: center; gap: 8px; color: #506278; font: 600 9px var(--font-mono); letter-spacing: .12em; }
.access-status span { width: 7px; height: 7px; border-radius: 50%; background: var(--green); box-shadow: 0 0 12px var(--green); }
.login-card { position: relative; width: min(440px, 100%); padding: clamp(26px, 4vw, 42px); border: 1px solid var(--line-strong); border-radius: 22px; background: linear-gradient(145deg, rgba(19,28,42,.94), rgba(10,15,24,.96)); box-shadow: 0 35px 100px rgba(0,0,0,.45), inset 0 1px 0 rgba(255,255,255,.035); backdrop-filter: blur(24px); }
.login-card::before, .login-card::after { content: ''; position: absolute; width: 16px; height: 16px; border-color: var(--cyan); opacity: .5; }
.login-card::before { top: -1px; left: -1px; border-top: 1px solid; border-left: 1px solid; border-radius: 22px 0 0; }
.login-card::after { right: -1px; bottom: -1px; border-right: 1px solid; border-bottom: 1px solid; border-radius: 0 0 22px; }
.login-card__index { position: absolute; top: 18px; right: 20px; color: #465366; font: 500 8px var(--font-mono); letter-spacing: .12em; }
.login-card__heading { margin-bottom: 30px; }
.login-card__heading h2 { margin: 12px 0 5px; color: var(--text-strong); font-size: 28px; font-weight: 620; letter-spacing: -.035em; }
.login-card__heading p { margin: 0; color: var(--text-muted); font-size: 12px; }
.login-card .field + .field { margin-top: 18px; }
.login-control { position: relative; }
.login-control > .ui-icon { position: absolute; z-index: 1; top: 50%; left: 14px; color: #5d6a7d; transform: translateY(-50%); }
.login-control .ui-control { height: 48px; padding-left: 43px; }
.login-control > button { position: absolute; top: 50%; right: 8px; width: 34px; height: 34px; display: grid; place-items: center; color: #59667a; border: 0; background: transparent; cursor: pointer; transform: translateY(-50%); }
.login-control > button:hover { color: var(--cyan); }
.login-error { display: flex; align-items: center; gap: 9px; margin-top: 18px; padding: 11px 12px; color: #ff9eae; border: 1px solid rgba(255,98,125,.2); border-radius: 10px; background: rgba(255,98,125,.07); font-size: 11px; }
.login-submit { width: 100%; height: 49px; display: flex; align-items: center; justify-content: center; gap: 10px; margin-top: 22px; color: #041014; border: 0; border-radius: 11px; background: var(--cyan); box-shadow: 0 10px 32px rgba(88,224,255,.17); cursor: pointer; font-weight: 700; transition: 180ms ease; }
.login-submit:hover:not(:disabled) { background: #83e9ff; transform: translateY(-1px); box-shadow: 0 14px 38px rgba(88,224,255,.23); }
.login-submit:disabled { opacity: .6; cursor: wait; }
.login-submit .spinner { width: 20px; height: 20px; border-color: rgba(4,16,20,.2); border-top-color: #041014; }
.submit-arrow { transform: rotate(180deg); }
.login-card__footer { display: flex; justify-content: space-between; gap: 12px; margin-top: 25px; padding-top: 18px; color: #465366; border-top: 1px solid var(--line); font: 500 8px var(--font-mono); letter-spacing: .08em; }
.login-card__footer span { display: flex; align-items: center; gap: 5px; }
.access-footnote { position: absolute; bottom: 27px; margin: 0; color: #3f4a5a; font: 500 8px var(--font-mono); letter-spacing: .14em; }
.error-slide-enter-active, .error-slide-leave-active { transition: .18s ease; }.error-slide-enter-from, .error-slide-leave-to { opacity: 0; transform: translateY(-5px); }
@keyframes orbit-spin { to { transform: translate(-50%,-50%) rotate(360deg); } }

@media (max-width: 1080px) {
  .login-page { grid-template-columns: 1fr 480px; }
  .orbital-visual { display: none; }
}
@media (max-width: 820px) {
  .login-page { display: block; }
  .login-showcase { min-height: auto; padding: 26px 22px 40px; border-right: 0; border-bottom: 1px solid var(--line); }
  .showcase-copy { margin-top: 55px; }.showcase-copy h1 { font-size: 42px; }
  .showcase-metrics { margin-top: 45px; }
  .login-access { min-height: 620px; padding: 70px 18px; }
  .login-theme-toggle { top: 24px; right: 22px; left: auto; }
}
@media (max-width: 480px) {
  .showcase-copy h1 { font-size: 34px; }.showcase-metrics { gap: 19px; }
  .access-status { top: 25px; right: 20px; }.login-card { padding: 28px 20px; }
}

/* Keep secondary copy readable in both themes; the original visual hierarchy is
   retained through size and weight instead of low-contrast text colors. */
.login-page { background: var(--bg-deep); }
.showcase-copy p { color: var(--text-muted); }
.satellite { background: var(--surface-solid); }
.access-status { color: var(--text-muted); }
.login-card { background: var(--panel-background); box-shadow: var(--shadow-card), inset 0 1px 0 rgba(255,255,255,.035); }
.login-card__index,
.login-control > .ui-icon,
.login-control > button,
.login-card__footer { color: var(--text-muted); }
.login-error { color: var(--red); border-color: rgba(var(--red-rgb), .28); background: rgba(var(--red-rgb), .08); }
.login-submit { color: var(--text-on-accent); background: var(--cyan); box-shadow: 0 10px 32px rgba(var(--cyan-rgb),.17); }
.login-submit:hover:not(:disabled) { color: var(--text-on-accent); background: var(--cyan-hover); box-shadow: 0 14px 38px rgba(var(--cyan-rgb),.23); }
.login-submit .spinner { border-color: rgba(var(--cyan-rgb),.2); border-top-color: var(--text-on-accent); }
.access-footnote { color: var(--text-subtle); }
</style>
