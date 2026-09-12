(() => {
  const mounts = [...document.querySelectorAll('[data-auth-mount]')];
  if (!mounts.length) return;

  const storageKey = 'mujian-prototype-authenticated';
  let authenticated = false;
  let activeMode = 'login';
  let toastTimer;

  try {
    authenticated = sessionStorage.getItem(storageKey) === 'true';
  } catch (_) {
    authenticated = false;
  }

  document.body.insertAdjacentHTML('beforeend', `
    <dialog class="auth-dialog" id="authDialog" aria-labelledby="authDialogTitle">
      <div class="auth-dialog-shell">
        <header class="auth-dialog-header">
          <div>
            <p class="auth-eyebrow">幕鉴账号</p>
            <h2 class="auth-dialog-title" id="authDialogTitle">欢迎回来</h2>
          </div>
          <button class="auth-close" type="button" data-auth-close aria-label="关闭账号窗口" title="关闭">
            <i data-lucide="x" aria-hidden="true"></i>
          </button>
        </header>

        <div class="auth-tabs" id="authTabs" role="tablist" aria-label="账号操作">
          <button class="auth-tab active" type="button" role="tab" aria-selected="true" data-auth-mode="login">登录</button>
          <button class="auth-tab" type="button" role="tab" aria-selected="false" data-auth-mode="register">注册</button>
        </div>

        <section class="auth-panel" data-auth-panel="login">
          <form class="auth-form" id="loginForm" novalidate>
            <div class="auth-field">
              <label for="loginEmail">邮箱</label>
              <input id="loginEmail" name="email" type="email" autocomplete="email" required placeholder="name@example.com">
            </div>
            <div class="auth-field">
              <label for="loginPassword">密码</label>
              <input id="loginPassword" name="password" type="password" autocomplete="current-password" minlength="8" required placeholder="输入密码">
            </div>
            <p class="auth-error" id="loginError" role="alert"></p>
            <button class="auth-submit" type="submit"><i data-lucide="log-in" aria-hidden="true"></i>登录</button>
            <div class="auth-form-actions">
              <button class="auth-link-button" type="button" data-auth-mode="reset">忘记密码</button>
              <button class="auth-link-button" type="button" data-auth-mode="register">创建账号</button>
            </div>
          </form>
        </section>

        <section class="auth-panel" data-auth-panel="register" hidden>
          <form class="auth-form" id="registerForm" novalidate>
            <div class="auth-field">
              <label for="registerEmail">邮箱</label>
              <input id="registerEmail" name="email" type="email" autocomplete="email" required placeholder="name@example.com">
            </div>
            <div class="auth-field">
              <label for="registerCode">邮箱验证码</label>
              <div class="auth-code-row">
                <input id="registerCode" name="code" type="text" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required placeholder="6 位验证码">
                <button class="auth-code-send" type="button" data-send-code="register">发送验证码</button>
              </div>
            </div>
            <div class="auth-field">
              <label for="registerPassword">设置密码</label>
              <input id="registerPassword" name="password" type="password" autocomplete="new-password" minlength="8" required placeholder="至少 8 位">
            </div>
            <label class="auth-check" for="registerConsent">
              <input id="registerConsent" type="checkbox" required>
              <span>我已满 18 周岁，并同意服务条款与隐私说明</span>
            </label>
            <p class="auth-error" id="registerError" role="alert"></p>
            <button class="auth-submit" type="submit"><i data-lucide="user-plus" aria-hidden="true"></i>创建账号</button>
          </form>
        </section>

        <section class="auth-panel" data-auth-panel="reset" hidden>
          <form class="auth-form" id="resetForm" novalidate>
            <div class="auth-field">
              <label for="resetEmail">已验证邮箱</label>
              <input id="resetEmail" name="email" type="email" autocomplete="email" required placeholder="name@example.com">
            </div>
            <div class="auth-field">
              <label for="resetCode">邮箱验证码</label>
              <div class="auth-code-row">
                <input id="resetCode" name="code" type="text" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required placeholder="6 位验证码">
                <button class="auth-code-send" type="button" data-send-code="reset">发送验证码</button>
              </div>
            </div>
            <div class="auth-field">
              <label for="resetPassword">新密码</label>
              <input id="resetPassword" name="password" type="password" autocomplete="new-password" minlength="8" required placeholder="至少 8 位">
            </div>
            <p class="auth-error" id="resetError" role="alert"></p>
            <button class="auth-submit" type="submit"><i data-lucide="key-round" aria-hidden="true"></i>重置密码</button>
            <button class="auth-link-button" type="button" data-auth-mode="login">返回登录</button>
          </form>
        </section>

        <p class="auth-panel-note">注册、重置密码和关闭账号均使用邮箱验证码。账号关闭入口位于账号设置。</p>
      </div>
    </dialog>`);

  const dialog = document.getElementById('authDialog');
  const title = document.getElementById('authDialogTitle');
  const tabs = document.getElementById('authTabs');

  function refreshIcons() {
    if (window.lucide) {
      window.lucide.createIcons({ attrs: { width: 18, height: 18, 'stroke-width': 1.8 } });
    }
  }

  function showToast(message) {
    const toast = document.getElementById('toast');
    if (!toast) return;
    clearTimeout(toastTimer);
    toast.textContent = message;
    toast.classList.add('visible');
    toastTimer = setTimeout(() => toast.classList.remove('visible'), 2600);
  }

  function guestMarkup() {
    return `
      <div class="auth-actions">
        <button class="auth-button auth-login" type="button" data-auth-open="login"><i data-lucide="log-in" aria-hidden="true"></i><span>登录</span></button>
        <button class="auth-button auth-register" type="button" data-auth-open="register"><i data-lucide="user-plus" aria-hidden="true"></i><span>注册</span></button>
      </div>`;
  }

  function accountMarkup() {
    return `
      <button class="auth-account-button" type="button" data-account-toggle aria-haspopup="menu" aria-expanded="false">
        <span class="auth-avatar" aria-hidden="true">演</span><span class="auth-account-label">演示用户</span><i data-lucide="chevron-down" aria-hidden="true"></i>
      </button>
      <div class="account-menu" data-account-menu role="menu" hidden>
        <div class="account-menu-head"><strong>演示用户</strong><span>邮箱已验证</span></div>
        <div class="account-menu-list">
          <button class="account-menu-item" type="button" role="menuitem" data-account-action="收藏作品"><i data-lucide="bookmark" aria-hidden="true"></i>收藏作品</button>
          <button class="account-menu-item" type="button" role="menuitem" data-account-action="关注人物"><i data-lucide="user-check" aria-hidden="true"></i>关注人物</button>
          <button class="account-menu-item" type="button" role="menuitem" data-account-action="浏览历史"><i data-lucide="history" aria-hidden="true"></i>浏览历史</button>
          <button class="account-menu-item" type="button" role="menuitem" data-account-action="账号设置"><i data-lucide="settings" aria-hidden="true"></i>账号设置</button>
          <button class="account-menu-item logout" type="button" role="menuitem" data-auth-logout><i data-lucide="log-out" aria-hidden="true"></i>退出登录</button>
        </div>
      </div>`;
  }

  function bindMount(mount) {
    mount.querySelectorAll('[data-auth-open]').forEach((button) => {
      button.addEventListener('click', () => openDialog(button.dataset.authOpen));
    });

    const accountToggle = mount.querySelector('[data-account-toggle]');
    const accountMenu = mount.querySelector('[data-account-menu]');
    if (accountToggle && accountMenu) {
      accountToggle.addEventListener('click', () => {
        const opening = accountMenu.hidden;
        closeAccountMenus();
        accountMenu.hidden = !opening;
        accountToggle.setAttribute('aria-expanded', String(opening));
      });
    }

    mount.querySelectorAll('[data-account-action]').forEach((button) => {
      button.addEventListener('click', () => {
        showToast(`${button.dataset.accountAction}页面为原型占位`);
        closeAccountMenus();
      });
    });

    mount.querySelector('[data-auth-logout]')?.addEventListener('click', () => {
      setAuthenticated(false);
      showToast('已退出登录');
    });
  }

  function renderMounts() {
    mounts.forEach((mount) => {
      mount.innerHTML = authenticated ? accountMarkup() : guestMarkup();
      bindMount(mount);
    });
    refreshIcons();
  }

  function closeAccountMenus() {
    document.querySelectorAll('[data-account-menu]').forEach((menu) => { menu.hidden = true; });
    document.querySelectorAll('[data-account-toggle]').forEach((button) => button.setAttribute('aria-expanded', 'false'));
  }

  function setMode(mode) {
    activeMode = mode;
    const titles = { login: '欢迎回来', register: '创建账号', reset: '重置密码' };
    title.textContent = titles[mode];
    tabs.hidden = mode === 'reset';

    document.querySelectorAll('[data-auth-panel]').forEach((panel) => {
      panel.hidden = panel.dataset.authPanel !== mode;
    });
    document.querySelectorAll('.auth-tab').forEach((tab) => {
      const selected = tab.dataset.authMode === mode;
      tab.classList.toggle('active', selected);
      tab.setAttribute('aria-selected', String(selected));
    });
    document.querySelectorAll('.auth-error').forEach((error) => { error.textContent = ''; });
  }

  function openDialog(mode = 'login') {
    closeAccountMenus();
    setMode(mode);
    if (!dialog.open) dialog.showModal();
    const panel = dialog.querySelector(`[data-auth-panel="${mode}"]`);
    const firstInput = panel?.querySelector('input');
    if (firstInput) requestAnimationFrame(() => firstInput.focus());
  }

  function setAuthenticated(value) {
    authenticated = value;
    try {
      if (value) sessionStorage.setItem(storageKey, 'true');
      else sessionStorage.removeItem(storageKey);
    } catch (_) {
      authenticated = value;
    }
    if (!value) {
      document.querySelectorAll('[data-requires-auth].is-active').forEach((button) => {
        button.classList.remove('is-active');
        button.setAttribute('aria-pressed', 'false');
        const label = button.querySelector('[data-action-label]');
        if (label && button.dataset.defaultLabel) label.textContent = button.dataset.defaultLabel;
      });
    }
    renderMounts();
    document.dispatchEvent(new CustomEvent('mujian:auth-change', { detail: { authenticated } }));
  }

  function submitAuth(form, errorId, successMessage) {
    const error = document.getElementById(errorId);
    if (!form.checkValidity()) {
      error.textContent = '请检查邮箱、验证码和密码格式';
      form.reportValidity();
      return;
    }
    setAuthenticated(true);
    dialog.close();
    form.reset();
    showToast(successMessage);
  }

  function startCodeTimer(button) {
    const panel = button.closest('.auth-panel');
    const email = panel.querySelector('input[type="email"]');
    const error = panel.querySelector('.auth-error');
    if (!email.checkValidity()) {
      error.textContent = '请先填写有效邮箱';
      email.reportValidity();
      return;
    }
    error.textContent = '';
    let remaining = 60;
    button.disabled = true;
    button.textContent = `${remaining} 秒后重发`;
    const timer = setInterval(() => {
      remaining -= 1;
      button.textContent = remaining > 0 ? `${remaining} 秒后重发` : '重新发送';
      if (remaining === 0) {
        clearInterval(timer);
        button.disabled = false;
      }
    }, 1000);
    showToast('验证码发送请求已提交');
  }

  document.querySelectorAll('[data-auth-mode]').forEach((button) => {
    button.addEventListener('click', () => setMode(button.dataset.authMode));
  });

  document.querySelectorAll('[data-send-code]').forEach((button) => {
    button.addEventListener('click', () => startCodeTimer(button));
  });

  document.getElementById('loginForm').addEventListener('submit', (event) => {
    event.preventDefault();
    submitAuth(event.currentTarget, 'loginError', '登录成功');
  });

  document.getElementById('registerForm').addEventListener('submit', (event) => {
    event.preventDefault();
    submitAuth(event.currentTarget, 'registerError', '账号创建成功');
  });

  document.getElementById('resetForm').addEventListener('submit', (event) => {
    event.preventDefault();
    submitAuth(event.currentTarget, 'resetError', '密码已重置并登录');
  });

  document.querySelector('[data-auth-close]').addEventListener('click', () => dialog.close());
  dialog.addEventListener('click', (event) => {
    const rect = dialog.getBoundingClientRect();
    const outside = event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom;
    if (outside) dialog.close();
  });

  document.addEventListener('click', (event) => {
    if (!event.target.closest('[data-auth-mount]')) closeAccountMenus();
  });

  document.querySelectorAll('[data-requires-auth]').forEach((button) => {
    button.addEventListener('click', (event) => {
      if (!authenticated) {
        event.preventDefault();
        openDialog('login');
        return;
      }

      if (button.dataset.activeLabel) {
        const active = !button.classList.contains('is-active');
        button.classList.toggle('is-active', active);
        button.setAttribute('aria-pressed', String(active));
        const label = button.querySelector('[data-action-label]');
        if (label) label.textContent = active ? button.dataset.activeLabel : button.dataset.defaultLabel;
        showToast(active ? button.dataset.activeLabel : `已取消${button.dataset.defaultLabel}`);
      } else if (button.dataset.authAction) {
        showToast(`${button.dataset.authAction}入口为原型占位`);
      }
    });
  });

  renderMounts();
  setMode(activeMode);
})();
