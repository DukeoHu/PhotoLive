(function () {
  var app = document.getElementById('roomApp');
  if (!app) return;
  var roomId = app.dataset.roomId;
  var roomName = app.dataset.roomName;
  var status = app.dataset.roomStatus;
  var maxFiles = parseInt(app.dataset.maxFiles, 10) || 300;

  var grid = document.getElementById('photoGrid');
  var empty = document.getElementById('emptyState');
  var emptyTitle = document.getElementById('emptyTitle');
  var emptyHint = document.getElementById('emptyHint');
  var tabAll = document.getElementById('tabAll');
  var tabSelected = document.getElementById('tabSelected');
  var tabSelectedCount = document.getElementById('tabSelectedCount');
  var statPhotos = document.getElementById('statPhotos');
  var statSelected = document.getElementById('statSelected');
  var statRemaining = document.getElementById('statRemaining');
  var statusPill = document.getElementById('statusText');
  var statusDot = document.getElementById('statusDot');
  var toggleBtn = document.getElementById('toggleStatus');
  var uploadPanel = document.getElementById('uploadPanel');
  var uploadBar = document.getElementById('uploadBar');
  var uploadText = document.getElementById('uploadText');
  var uploadErrors = document.getElementById('uploadErrors');
  var retryFailed = document.getElementById('retryFailed');
  var fileInput = document.getElementById('fileInput');
  var dropZone = document.getElementById('dropZone');
  var pendingSection = document.getElementById('pendingSection');
  var pendingGrid = document.getElementById('pendingGrid');
  var pendingCount = document.getElementById('pendingCount');

  var photos = [];
  var pending = [];
  var pendingSig = '';
  var showSelected = false;
  var uploading = false;
  var failedFiles = [];
  var lastCount = -1;
  var guestToggle = document.getElementById('guestUploadToggle');
  var guestReviewToggle = document.getElementById('guestReviewToggle');

  if (guestToggle) {
    guestToggle.addEventListener('change', function () {
      var next = guestToggle.checked;
      guestToggle.disabled = true;
      fetch('/api/rooms/' + roomId, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ allow_guest_upload: next })
      }).then(function (res) {
        return res.json().then(function (data) {
          if (!res.ok) throw new Error(data.error || '更新失败');
          PLUI.toast(next ? '已开启访客上传' : '已关闭访客上传');
        });
      }).catch(function (err) {
        guestToggle.checked = !next;
        PLUI.toast(err.message);
      }).then(function () {
        guestToggle.disabled = false;
      });
    });
  }

  if (guestReviewToggle) {
    guestReviewToggle.addEventListener('change', function () {
      var next = guestReviewToggle.checked;
      guestReviewToggle.disabled = true;
      fetch('/api/rooms/' + roomId, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ guest_upload_review: next })
      }).then(function (res) {
        return res.json().then(function (data) {
          if (!res.ok) throw new Error(data.error || '更新失败');
          PLUI.toast(next ? '访客照片将先审核再展示' : '访客照片将直接展示');
          loadPending();
        });
      }).catch(function (err) {
        guestReviewToggle.checked = !next;
        PLUI.toast(err.message);
      }).then(function () {
        guestReviewToggle.disabled = false;
      });
    });
  }

  function pad(n) { return String(n).padStart(2, '0'); }
  function fmt(ts) {
    if (!ts) return '';
    var d = new Date(ts * 1000);
    return d.getFullYear() + '.' + pad(d.getMonth() + 1) + '.' + pad(d.getDate()) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
  }
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function card(p) {
    var detail = '';
    if (showSelected && p.selected) {
      detail = '<div class="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/55 to-transparent p-2.5">' +
        '<p class="truncate text-[10.5px] text-white/90">' + esc(p.name) + '</p>' +
        '<p class="text-[9.5px] text-white/55">拍摄 ' + fmt(p.taken_at) + '</p>' +
        (p.marked_at ? '<p class="text-[9.5px] text-white/40">标记 ' + fmt(p.marked_at) + '</p>' : '') +
      '</div>';
    }
    return '<div class="group relative overflow-hidden rounded-2xl bg-paper/5 aspect-square" data-id="' + p.id + '">' +
      '<img src="' + p.thumb_url + '" loading="lazy" alt="" class="h-full w-full object-cover transition duration-500 group-hover:scale-[1.04]">' +
      (detail || ('<div class="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/75 to-transparent p-2.5 opacity-0 transition group-hover:opacity-100">' +
        '<p class="truncate text-[10.5px] text-white/85">' + esc(p.name) + '</p>' +
        '<p class="text-[9.5px] text-white/45">' + fmt(p.taken_at) + '</p>' +
      '</div>')) +
      (p.selected ? '<span class="absolute left-2 top-2 rounded-full bg-paper px-2 py-0.5 text-[10px] font-semibold text-ink">已选</span>' : '') +
      (p.uploader === 'guest' ? '<span class="absolute bottom-2 left-2 rounded-full bg-black/55 px-2 py-0.5 text-[9.5px] text-white/80 backdrop-blur">访客</span>' : '') +
      '<button class="del absolute right-2 top-2 hidden rounded-full bg-black/60 p-1.5 text-white/85 backdrop-blur transition hover:bg-red-500/80 group-hover:block" data-del="' + p.id + '" title="删除">' +
        '<svg width="12" height="12" viewBox="0 0 12 12" fill="none"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>' +
      '</button>' +
    '</div>';
  }

  function render() {
    var list = photos.slice().sort(function (a, b) { return a.taken_at - b.taken_at || a.id - b.id; });
    if (showSelected) list = list.filter(function (p) { return p.selected; });
    grid.innerHTML = list.map(card).join('');
    var hasAny = photos.length > 0;
    empty.classList.toggle('hidden', list.length > 0);
    if (!hasAny) {
      emptyTitle.textContent = '还没有照片';
      emptyHint.textContent = '上传后照片会按拍摄时间自动排序，并在客户页面实时出现。';
    } else if (list.length === 0) {
      emptyTitle.textContent = '还没有客户选中照片';
      emptyHint.textContent = '客户在手机上点选照片后，这里会实时出现。';
    }
    var n = photos.filter(function (p) { return p.selected; }).length;
    tabSelectedCount.textContent = n;
    statSelected.textContent = n;
    statPhotos.textContent = photos.length;
    statRemaining.textContent = pending.length;
    lastCount = photos.length;
  }

  function applyStatus(next) {
    status = next;
    if (status === 'live') {
      statusPill.textContent = '进行中';
      statusDot.className = 'h-1.5 w-1.5 rounded-full dot-live';
      toggleBtn.textContent = '结束活动';
    } else {
      statusPill.textContent = '已结束';
      statusDot.className = 'h-1.5 w-1.5 rounded-full bg-paper/30';
      toggleBtn.textContent = '重新开启';
    }
  }

  function loadPhotos() {
    return fetch('/api/rooms/' + roomId + '/photos').then(function (res) {
      if (!res.ok) throw new Error('加载照片失败');
      return res.json();
    }).then(function (data) {
      photos = data.photos || [];
      if (data.room) applyStatus(data.room.status);
      render();
    }).catch(function (err) {
      console.error(err);
    });
  }

  function loadPending() {
    return fetch('/api/rooms/' + roomId + '/pending').then(function (res) {
      return res.json();
    }).then(function (data) {
      if (data.error) return;
      pending = data.photos || [];
      pendingCount.textContent = pending.length;
      statRemaining.textContent = pending.length;
      pendingSection.classList.toggle('hidden', pending.length === 0);
      var sig = pending.map(function (p) { return p.id; }).join(',');
      if (sig !== pendingSig) {
        pendingSig = sig;
        renderPending();
      }
    }).catch(function () {});
  }

  function renderPending() {
    pendingGrid.innerHTML = pending.map(function (p) {
      return '<div class="overflow-hidden rounded-2xl border border-paper/10 bg-paper/[0.03]">' +
        '<div class="aspect-square overflow-hidden bg-black/30">' +
          '<img src="' + p.thumb_url + '" loading="lazy" alt="" class="h-full w-full object-cover">' +
        '</div>' +
        '<div class="space-y-2 p-2.5">' +
          '<p class="truncate text-[10.5px] text-paper/35">' + esc(p.name) + '</p>' +
          '<div class="flex gap-1.5">' +
            '<button data-approve="' + p.id + '" class="flex-1 rounded-lg bg-paper py-1.5 text-[11px] font-medium text-ink">通过</button>' +
            '<button data-reject="' + p.id + '" class="flex-1 rounded-lg border border-paper/15 py-1.5 text-[11px] text-paper/55">拒绝</button>' +
          '</div>' +
        '</div>' +
      '</div>';
    }).join('');
  }

  pendingGrid.addEventListener('click', function (e) {
    var ap = e.target.closest('[data-approve]');
    if (ap) { approvePending(ap.getAttribute('data-approve')); return; }
    var rj = e.target.closest('[data-reject]');
    if (rj) rejectPending(rj.getAttribute('data-reject'));
  });

  function dropPending(id) {
    pending = pending.filter(function (p) { return String(p.id) !== String(id); });
    pendingSig = '';
    loadPending();
  }

  function approvePending(id) {
    fetch('/api/photos/' + id + '/approve', { method: 'POST' }).then(function (res) {
      return res.json().then(function (data) {
        if (!res.ok) throw new Error(data.error || '审核失败');
        dropPending(id);
        loadPhotos();
        PLUI.toast('已通过，照片已展示给客户');
      });
    }).catch(function (err) { PLUI.toast(err.message); });
  }

  function rejectPending(id) {
    PLUI.confirm({
      title: '拒绝这张照片？',
      message: '照片将被删除，访客需要重新上传。',
      okText: '拒绝并删除',
      danger: true
    }).then(function (ok) {
      if (!ok) return;
      fetch('/api/photos/' + id, { method: 'DELETE' }).then(function (res) {
        if (!res.ok) throw new Error('删除失败');
        dropPending(id);
        refreshStats();
        PLUI.toast('已拒绝该照片');
      }).catch(function (err) { PLUI.toast(err.message); });
    });
  }

  function refreshStats() {
    return fetch('/api/rooms/' + roomId + '/stats').then(function (res) {
      return res.json();
    }).then(function (s) {
      if (s.error) return;
      applyStatus(s.status);
      statSelected.textContent = s.selected_count;
      tabSelectedCount.textContent = s.selected_count;
      statPhotos.textContent = s.photo_count;
      if (typeof s.pending_count === 'number') {
        statRemaining.textContent = s.pending_count;
        if (s.pending_count !== pending.length) loadPending();
      }
      if (!uploading && s.photo_count !== lastCount) loadPhotos();
    }).catch(function () {});
  }

  grid.addEventListener('click', function (e) {
    var del = e.target.closest('[data-del]');
    if (del) {
      e.stopPropagation();
      var id = del.getAttribute('data-del');
      PLUI.confirm({
        title: '删除这张照片？',
        message: '客户那边的标记也会一并移除，且无法恢复。',
        okText: '删除',
        danger: true
      }).then(function (ok) {
        if (!ok) return;
        fetch('/api/photos/' + id, { method: 'DELETE' }).then(function (res) {
          if (!res.ok) throw new Error('删除失败');
          photos = photos.filter(function (p) { return String(p.id) !== String(id); });
          render();
          PLUI.toast('照片已删除');
        }).catch(function (err) { PLUI.toast(err.message); });
      });
      return;
    }
    var tile = e.target.closest('[data-id]');
    if (tile) {
      var p = photos.filter(function (x) { return String(x.id) === tile.getAttribute('data-id'); })[0];
      if (p) window.open(p.preview_url, '_blank', 'noopener');
    }
  });

  function setTab(selected) {
    showSelected = selected;
    var active = 'rounded-full bg-paper px-4 py-1.5 text-[12px] font-medium text-ink';
    var idle = 'rounded-full px-4 py-1.5 text-[12px] text-paper/55';
    tabAll.className = selected ? idle : active;
    tabSelected.className = selected ? active : idle;
    render();
  }
  tabAll.addEventListener('click', function () { setTab(false); });
  tabSelected.addEventListener('click', function () { setTab(true); });

  // ---------- 上传 ----------
  dropZone.addEventListener('click', function () { fileInput.click(); });
  dropZone.addEventListener('dragover', function (e) {
    e.preventDefault();
    dropZone.classList.add('border-paper/50', 'bg-paper/[0.04]');
  });
  dropZone.addEventListener('dragleave', function () {
    dropZone.classList.remove('border-paper/50', 'bg-paper/[0.04]');
  });
  dropZone.addEventListener('drop', function (e) {
    e.preventDefault();
    dropZone.classList.remove('border-paper/50', 'bg-paper/[0.04]');
    if (e.dataTransfer && e.dataTransfer.files.length) enqueue(e.dataTransfer.files);
  });
  fileInput.addEventListener('change', function () {
    if (fileInput.files.length) enqueue(fileInput.files);
    fileInput.value = '';
  });
  retryFailed.addEventListener('click', function () {
    if (failedFiles.length) enqueue(failedFiles.slice());
  });

  function uploadOne(file) {
    return new Promise(function (resolve, reject) {
      var xhr = new XMLHttpRequest();
      xhr.open('POST', '/api/rooms/' + roomId + '/photos');
      xhr.responseType = 'json';
      xhr.onload = function () {
        var data = xhr.response || {};
        if (xhr.status === 200 || xhr.status === 201) resolve(data);
        else reject(new Error(data.error || ('上传失败（' + xhr.status + '）')));
      };
      xhr.onerror = function () { reject(new Error('网络中断，请重试')); };
      var fd = new FormData();
      fd.append('file', file, file.name);
      xhr.send(fd);
    });
  }

  function enqueue(fileList) {
    if (uploading) { PLUI.toast('上一批还在上传，请稍候'); return; }
    var queue = Array.prototype.slice.call(fileList);
    if (!queue.length) return;
    if (queue.length > maxFiles) {
      queue = queue.slice(0, maxFiles);
      PLUI.toast('单次最多上传 ' + maxFiles + ' 张，本次先处理前 ' + maxFiles + ' 张');
    }
    uploading = true;
    failedFiles = [];
    retryFailed.classList.add('hidden');
    uploadPanel.classList.remove('hidden');
    uploadErrors.innerHTML = '';
    var total = queue.length, finished = 0, active = 0, ok = 0, skipped = 0;

    function progress() {
      uploadBar.style.width = Math.round((finished / total) * 100) + '%';
      uploadText.textContent = '上传中 ' + finished + ' / ' + total + ' · 成功 ' + ok + (skipped ? ' · 跳过 ' + skipped : '');
    }
    progress();

    function next() {
      if (queue.length === 0 && active === 0) {
        uploading = false;
        uploadText.textContent = '已完成 ' + total + ' 个文件 · 成功 ' + ok + (skipped ? ' · 跳过 ' + skipped : '');
        if (failedFiles.length) {
          retryFailed.textContent = '重试失败的 ' + failedFiles.length + ' 项';
          retryFailed.classList.remove('hidden');
        } else {
          setTimeout(function () { uploadPanel.classList.add('hidden'); }, 3500);
        }
        loadPhotos();
        refreshStats();
        return;
      }
      while (active < 3 && queue.length) {
        (function (file) {
          active++;
          uploadOne(file).then(function (data) {
            if (data.skipped) skipped++;
            else if (data.photo) { ok++; photos.push(data.photo); }
          }).catch(function (err) {
            var li = document.createElement('li');
            li.textContent = file.name + '：' + err.message;
            uploadErrors.appendChild(li);
            failedFiles.push(file);
          }).then(function () {
            active--;
            finished++;
            progress();
            next();
          });
        })(queue.shift());
      }
    }
    next();
  }

  // ---------- 复制 ----------
  function copy(text, btn, label) {
    var done = function () {
      var old = btn.textContent;
      btn.textContent = '已复制';
      setTimeout(function () { btn.textContent = old || label; }, 1200);
    };
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done).catch(function () { legacy(text, done); });
    } else {
      legacy(text, done);
    }
  }
  function legacy(text, done) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); } catch (e) {}
    document.body.removeChild(ta);
    done();
  }
  document.getElementById('copyShare').addEventListener('click', function () {
    copy(document.getElementById('shareInput').value, this, '复制');
  });
  document.getElementById('copyCode').addEventListener('click', function () {
    copy(document.getElementById('accessCode').textContent.trim(), this, '复制访问码');
  });

  function applyAccessCode(data) {
    document.getElementById('accessCode').textContent = data.access_code;
    document.getElementById('shareInput').value = data.share_url;
    document.getElementById('qrImg').src = '/api/rooms/' + roomId + '/qr?t=' + Date.now();
    PLUI.toast('访问码已更新');
  }

  var resetCodeBtn = document.getElementById('resetCode');
  if (resetCodeBtn) {
    resetCodeBtn.addEventListener('click', function () {
      PLUI.confirm({
        title: '重置访问码？',
        message: '旧访问码会立即失效，已经进入的客户不受影响；新访问码需要重新发给客户。',
        okText: '重置',
        danger: true
      }).then(function (ok) {
        if (!ok) return;
        fetch('/api/rooms/' + roomId + '/access-code', { method: 'POST' }).then(function (res) {
          return res.json().then(function (data) {
            if (!res.ok) throw new Error(data.error || '重置失败');
            applyAccessCode(data);
          });
        }).catch(function (err) { PLUI.toast(err.message); });
      });
    });
  }

  var editCodeBtn = document.getElementById('editCode');
  if (editCodeBtn) {
    editCodeBtn.addEventListener('click', function () {
      PLUI.prompt({
        title: '修改访问码',
        message: '4–8 位字母或数字，修改后旧访问码立即失效，已进入的客户不受影响。',
        inputLabel: '新访问码',
        placeholder: '例如 TRIP2026',
        okText: '保存',
        value: document.getElementById('accessCode').textContent.trim(),
        validate: function (v) {
          if (!/^[A-Za-z0-9]{4,8}$/.test(v)) return '访问码需为 4–8 位字母或数字';
          return '';
        }
      }).then(function (input) {
        if (input === null) return;
        fetch('/api/rooms/' + roomId + '/access-code', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code: input })
        }).then(function (res) {
          return res.json().then(function (data) {
            if (!res.ok) throw new Error(data.error || '修改失败');
            applyAccessCode(data);
          });
        }).catch(function (err) { PLUI.toast(err.message); });
      });
    });
  }

  // ---------- 状态与删除 ----------
  toggleBtn.addEventListener('click', function () {
    var next = status === 'live' ? 'ended' : 'live';
    var ending = next === 'ended';
    PLUI.confirm({
      title: ending ? '结束活动？' : '重新开启活动？',
      message: ending
        ? '结束后客户只能查看和下载，无法再修改选择。'
        : '重新开启后，客户可以继续标记和上传照片。',
      okText: ending ? '结束活动' : '重新开启',
      danger: ending
    }).then(function (ok) {
      if (!ok) return;
      fetch('/api/rooms/' + roomId, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: next })
      }).then(function (res) {
        return res.json().then(function (data) {
          if (!res.ok) throw new Error(data.error || '操作失败');
          applyStatus(data.status);
          PLUI.toast(ending ? '活动已结束' : '活动已重新开启');
        });
      }).catch(function (err) { PLUI.toast(err.message); });
    });
  });

  document.getElementById('deleteRoom').addEventListener('click', function () {
    PLUI.prompt({
      title: '删除房间',
      message: '删除后无法恢复，照片与标记都会被清除。请输入房间名称确认：',
      inputLabel: '房间名称',
      placeholder: roomName,
      okText: '删除房间',
      danger: true
    }).then(function (input) {
      if (input === null) return;
      if (input !== roomName) { PLUI.toast('房间名称不一致，已取消'); return; }
      fetch('/api/rooms/' + roomId, { method: 'DELETE' }).then(function (res) {
        if (!res.ok) throw new Error('删除失败');
        window.location.href = '/admin';
      }).catch(function (err) { PLUI.toast(err.message); });
    });
  });

  // 导出/下载前先做一次本地校验，避免点开一个只显示 JSON 错误的新标签页
  function guardSelectedLink(e) {
    var n = parseInt(statSelected.textContent, 10) || 0;
    if (n === 0) {
      e.preventDefault();
      PLUI.toast('还没有客户选中照片');
      return;
    }
    if (this.id === 'downloadSelected') {
      e.preventDefault();
      PLUI.downloadZip('/api/rooms/' + roomId + '/download-job', '正在打包 ' + n + ' 张照片…');
    }
  }
  ['exportCsv', 'exportTxt', 'downloadSelected'].forEach(function (id) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('click', guardSelectedLink);
  });

  applyStatus(status);
  loadPhotos();
  loadPending();
  setInterval(refreshStats, 5000);
})();
