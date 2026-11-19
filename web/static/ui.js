/* PhotoLive UI —— 统一的提示条与确认弹层，替代浏览器原生 alert / confirm / prompt */
(function () {
  var overlay = null;
  var els = null;
  var resolveFn = null;
  var mode = 'confirm';
  var options = {};
  var toastWrap = null;

  function build() {
    if (overlay) return;
    overlay = document.createElement('div');
    overlay.className = 'pl-overlay';
    overlay.innerHTML =
      '<div class="pl-dialog" role="dialog" aria-modal="true">' +
        '<p class="pl-eyebrow">PHOTOLIVE</p>' +
        '<h3 class="pl-title"></h3>' +
        '<p class="pl-message"></p>' +
        '<div class="pl-field" style="display:none">' +
          '<label class="pl-field-label"></label>' +
          '<input class="pl-input" type="text" autocomplete="off">' +
        '</div>' +
        '<p class="pl-error"></p>' +
        '<div class="pl-actions">' +
          '<button type="button" class="pl-btn pl-cancel">取消</button>' +
          '<button type="button" class="pl-btn pl-btn-primary pl-ok">确定</button>' +
        '</div>' +
      '</div>';
    document.body.appendChild(overlay);

    var dialog = overlay.querySelector('.pl-dialog');
    els = {
      title: overlay.querySelector('.pl-title'),
      message: overlay.querySelector('.pl-message'),
      field: overlay.querySelector('.pl-field'),
      label: overlay.querySelector('.pl-field-label'),
      input: overlay.querySelector('.pl-input'),
      error: overlay.querySelector('.pl-error'),
      ok: overlay.querySelector('.pl-ok'),
      cancel: overlay.querySelector('.pl-cancel')
    };

    els.ok.addEventListener('click', function () { submit(); });
    els.cancel.addEventListener('click', function () { close(mode === 'prompt' ? null : false); });
    overlay.addEventListener('mousedown', function (e) {
      if (e.target === overlay) close(mode === 'prompt' ? null : false);
    });
    els.input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') { e.preventDefault(); submit(); }
    });
    document.addEventListener('keydown', function (e) {
      if (!resolveFn) return;
      if (e.key === 'Escape') close(mode === 'prompt' ? null : false);
      if (e.key === 'Enter' && mode !== 'prompt') { e.preventDefault(); submit(); }
    });
    void dialog;
  }

  function open(opts, kind) {
    build();
    mode = kind;
    options = opts || {};
    return new Promise(function (resolve) {
      resolveFn = resolve;
      els.title.textContent = options.title || '';
      els.message.textContent = options.message || '';
      els.message.style.display = options.message ? '' : 'none';
      els.field.style.display = kind === 'prompt' ? '' : 'none';
      els.label.textContent = options.inputLabel || '';
      els.input.value = options.value || '';
      els.input.placeholder = options.placeholder || '';
      els.error.textContent = '';
      els.error.classList.remove('show');
      els.ok.textContent = options.okText || (kind === 'prompt' ? '确定' : '确定');
      els.cancel.textContent = options.cancelText || '取消';
      els.cancel.style.display = options.hideCancel ? 'none' : '';
      els.ok.className = 'pl-btn pl-ok ' + (options.danger ? 'pl-btn-danger' : 'pl-btn-primary');
      overlay.classList.add('open');
      overlay.style.display = 'flex';
      overlay.setAttribute('aria-hidden', 'false');
      document.body.style.overflow = 'hidden';
      setTimeout(function () {
        if (kind === 'prompt') {
          els.input.focus();
          els.input.select();
        } else {
          els.ok.focus();
        }
      }, 40);
    });
  }

  function submit() {
    if (mode === 'prompt') {
      var value = els.input.value.trim();
      if (options.required !== false && value === '') {
        showError(options.emptyText || '请输入内容');
        return;
      }
      if (typeof options.validate === 'function') {
        var err = options.validate(value);
        if (err) { showError(err); return; }
      }
      close(value);
      return;
    }
    close(true);
  }

  function showError(msg) {
    els.error.textContent = msg;
    els.error.classList.add('show');
    els.input.focus();
  }

  function close(value) {
    if (!resolveFn) return;
    overlay.classList.remove('open');
    overlay.style.display = 'none';
    overlay.setAttribute('aria-hidden', 'true');
    document.body.style.overflow = '';
    var fn = resolveFn;
    resolveFn = null;
    if (fn) fn(value);
  }

  function toast(msg) {
    if (!toastWrap) {
      toastWrap = document.createElement('div');
      toastWrap.className = 'pl-toasts';
      document.body.appendChild(toastWrap);
    }
    var el = document.createElement('div');
    el.className = 'pl-toast';
    el.textContent = msg;
    toastWrap.appendChild(el);
    requestAnimationFrame(function () { el.classList.add('show'); });
    setTimeout(function () {
      el.classList.remove('show');
      setTimeout(function () {
        if (el.parentNode) el.parentNode.removeChild(el);
      }, 260);
    }, 2400);
  }

  function zipName(res, fallback) {
    var cd = res.headers.get('Content-Disposition') || '';
    var m = /filename\*=UTF-8''([^;]+)/i.exec(cd);
    if (m) {
      try { return decodeURIComponent(m[1]); } catch (e) {}
    }
    m = /filename="([^"]+)"/i.exec(cd);
    return m ? m[1] : fallback;
  }

  // downloadZip 触发服务端后台打包，关闭页面后任务仍会继续。
  function downloadZip(jobUrl, label) {
    toast(label || '正在打包…');
    var tries = 0;

    function save(url, fallback) {
      return fetch(url, { credentials: 'same-origin' }).then(function (res) {
        if (!res.ok) {
          return res.json().catch(function () { return {}; }).then(function (d) {
            throw new Error(d.error || '下载失败');
          });
        }
        var name = zipName(res, fallback || 'photolive.zip');
        return res.blob().then(function (blob) {
          var href = URL.createObjectURL(blob);
          var a = document.createElement('a');
          a.href = href;
          a.download = name;
          document.body.appendChild(a);
          a.click();
          a.remove();
          setTimeout(function () { URL.revokeObjectURL(href); }, 5000);
          toast('下载已开始');
        });
      });
    }

    function poll(job) {
      tries++;
      if (tries > 300) throw new Error('打包时间过长，请稍后重试');
      if (job.status === 'ready') return save(jobUrl + '/file', job.filename);
      if (job.status === 'failed') throw new Error(job.error || '打包失败');
      if (job.total) toast('正在打包 ' + job.total + ' 张照片…');
      return new Promise(function (resolve) { setTimeout(resolve, 1200); })
        .then(function () {
          return fetch(jobUrl, { credentials: 'same-origin' }).then(function (res) {
            return res.json();
          });
        })
        .then(poll);
    }

    return fetch(jobUrl, { method: 'POST', credentials: 'same-origin' })
      .then(function (res) {
        return res.json().catch(function () { return {}; }).then(function (d) {
          if (!res.ok) throw new Error(d.error || '打包失败');
          return d;
        });
      })
      .then(poll)
      .catch(function (err) { toast(err.message); });
  }

  window.PLUI = {
    confirm: function (opts) { return open(opts, 'confirm'); },
    prompt: function (opts) { return open(opts, 'prompt'); },
    toast: toast,
    downloadZip: downloadZip
  };
})();
