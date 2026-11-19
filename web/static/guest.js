(function () {
  var gateForm = document.getElementById('gateForm');
  if (gateForm) {
    var codeInput = document.getElementById('codeInput');
    var gateError = document.getElementById('gateError');
    var gateBtn = document.getElementById('gateBtn');
    gateForm.addEventListener('submit', function (e) {
      e.preventDefault();
      gateError.classList.add('hidden');
      gateBtn.disabled = true;
      gateBtn.textContent = '验证中…';
      var code = codeInput.value.trim();
      var roomCode = location.pathname.split('/').filter(Boolean).pop();
      fetch('/api/guest/' + encodeURIComponent(roomCode) + '/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: code })
      }).then(function (res) {
        return res.json().catch(function () { return {}; }).then(function (data) {
          if (!res.ok) throw new Error(data.error || '访问码不正确');
          window.location.reload();
        });
      }).catch(function (err) {
        gateError.textContent = err.message;
        gateError.classList.remove('hidden');
        gateBtn.disabled = false;
        gateBtn.textContent = '进入相册';
        codeInput.focus();
      });
    });
    codeInput.focus();
    return;
  }

  var app = document.getElementById('guestApp');
  if (!app) return;

  var code = app.dataset.code;
  var status = app.dataset.status;
  var allowDownload = app.dataset.allowDownload === 'true';

  var apiBase = '/api/guest/' + code;
  var gallery = document.getElementById('gallery');
  var skeleton = document.getElementById('skeleton');
  var emptyState = document.getElementById('emptyState');
  var selectedCountEl = document.getElementById('selectedCount');
  var totalCountEl = document.getElementById('totalCount');
  var headerCountEl = document.getElementById('headerCount');
  var downloadBtn = document.getElementById('downloadBtn');
  var filterBtn = document.getElementById('filterBtn');
  var sortBtn = document.getElementById('sortBtn');
  var refreshBtn = document.getElementById('refreshBtn');
  var newPhotosPill = document.getElementById('newPhotos');
  var newCountEl = document.getElementById('newCount');
  var toastEl = document.getElementById('toast');

  var photos = [];
  var seen = {};
  var onlySelected = false;
  var sortDesc = false;
  var pending = {};
  var lastSelected = {};
  var lightboxIndex = -1;
  var toastTimer = null;

  function pad(n) { return String(n).padStart(2, '0'); }
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }
  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.style.opacity = '1';
    toastEl.style.transform = 'translate(-50%, -6px)';
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () {
      toastEl.style.opacity = '0';
      toastEl.style.transform = 'translate(-50%, 0)';
    }, 2200);
  }

  function sorted() {
    var dir = sortDesc ? -1 : 1;
    return photos.slice().sort(function (a, b) {
      return dir * ((a.taken_at - b.taken_at) || (a.id - b.id));
    });
  }
  function visible() {
    var list = sorted();
    return onlySelected ? list.filter(function (p) { return p.selected; }) : list;
  }
  function selectedCount() {
    return photos.filter(function (p) { return p.selected; }).length;
  }

  function tileHTML(p, animate, delay) {
    var ratio = p.width && p.height ? (p.width / p.height) : 1;
    return '<figure class="tile relative mb-0 cursor-pointer ' + (p.selected ? 'tile-selected ' : '') + (animate ? 'reveal ' : '') +
      '" data-id="' + p.id + '"' + (animate ? ' style="animation-delay:' + delay + 'ms"' : '') + '>' +
      '<div class="tile-frame overflow-hidden rounded-[16px] bg-paper/5 sm:rounded-[18px]">' +
        '<img class="tile-img block w-full object-cover" src="' + p.thumb_url + '" alt="' + esc(p.name) + '" loading="lazy" ' +
          'width="' + (p.width || 600) + '" height="' + (p.height || 800) + '" style="aspect-ratio:' + ratio + '">' +
      '</div>' +
      '<button data-chk class="absolute bottom-2.5 right-2.5 flex h-8 w-8 items-center justify-center rounded-full border text-[13px] font-bold backdrop-blur transition ' +
        (p.selected ? 'border-paper bg-paper text-ink' : 'border-white/55 bg-black/25 text-transparent') + '">✓</button>' +
    '</figure>';
  }

  function paintCounts() {
    var n = selectedCount();
    selectedCountEl.textContent = n;
    totalCountEl.textContent = photos.length;
    headerCountEl.textContent = photos.length;
    downloadBtn.disabled = n === 0;
  }

  function render() {
    var list = visible();
    var html = [];
    var delayed = 0;
    for (var i = 0; i < list.length; i++) {
      var p = list[i];
      var isNew = !seen[p.id];
      seen[p.id] = true;
      html.push(tileHTML(p, isNew, Math.min(delayed * 28, 420)));
      if (isNew) delayed++;
    }
    gallery.className = 'masonry columns-2 sm:columns-3 lg:columns-4 xl:columns-5';
    gallery.innerHTML = html.join('');
    skeleton.classList.add('hidden');
    emptyState.classList.toggle('hidden', photos.length > 0);
    paintCounts();
  }

  function paintTile(el, p) {
    if (!el) return;
    el.classList.toggle('tile-selected', p.selected);
    var chk = el.querySelector('[data-chk]');
    if (!chk) return;
    chk.className = 'absolute bottom-2.5 right-2.5 flex h-8 w-8 items-center justify-center rounded-full border text-[13px] font-bold backdrop-blur transition ' +
      (p.selected ? 'border-paper bg-paper text-ink' : 'border-white/55 bg-black/25 text-transparent');
  }

  function sendMark(photoId, selected, retries) {
    return fetch(apiBase + '/marks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ photo_id: photoId, selected: selected })
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) throw new Error(data.error || '保存失败');
        return data;
      });
    }).catch(function (err) {
      if (retries > 0) {
        return new Promise(function (resolve) { setTimeout(resolve, 350); })
          .then(function () { return sendMark(photoId, selected, retries - 1); });
      }
      throw err;
    });
  }

  function toggle(p) {
    if (status !== 'live') { toast('活动已结束，无法修改选择'); return; }
    if (pending[p.id]) return;
    var next = !p.selected;
    p.selected = next;
    if (next) lastSelected[p.id] = 1;
    else delete lastSelected[p.id];
    if (onlySelected && !next) {
      render();
      if (!lightbox.classList.contains('hidden')) closeLightbox();
    } else {
      paintTile(gallery.querySelector('[data-id="' + p.id + '"]'), p);
      paintCounts();
    }
    updateLightboxButton();

    pending[p.id] = true;
    sendMark(p.id, next, 3).then(function (data) {
      if (typeof data.selected_count === 'number') selectedCountEl.textContent = data.selected_count;
    }).catch(function (err) {
      p.selected = !next;
      if (next) delete lastSelected[p.id];
      else lastSelected[p.id] = 1;
      if (onlySelected) {
        render();
      } else {
        paintTile(gallery.querySelector('[data-id="' + p.id + '"]'), p);
        paintCounts();
      }
      toast(err.message || '保存失败，请重试');
    }).then(function () {
      delete pending[p.id];
    });
  }

  function loadPhotos(silent) {
    return fetch(apiBase + '/photos').then(function (res) {
      if (res.status === 403) { window.location.reload(); return null; }
      if (!res.ok) throw new Error('加载失败');
      return res.json();
    }).then(function (data) {
      if (!data) return;
      photos = data.photos || [];
      var current = {};
      photos.forEach(function (p) { if (p.selected) current[p.id] = 1; });
      var removed = 0;
      Object.keys(lastSelected).forEach(function (id) {
        if (!current[id] && !pending[id]) { removed++; delete lastSelected[id]; }
      });
      Object.keys(current).forEach(function (id) { lastSelected[id] = 1; });
      if (removed > 0) toast('有 ' + removed + ' 张已选照片已被摄影师移除');
      if (data.room) {
        status = data.room.status;
        allowDownload = !!data.room.allow_download;
      }
      if (downloadBtn) downloadBtn.style.display = allowDownload ? '' : 'none';
      render();
      if (!silent) window.scrollTo({ top: 0, behavior: 'smooth' });
    }).catch(function () {
      if (!silent) toast('加载失败，请重试');
    });
  }

  function poll() {
    fetch(apiBase + '/state').then(function (res) {
      if (!res.ok) return null;
      return res.json();
    }).then(function (s) {
      if (!s) return;
      if (s.status !== status) { window.location.reload(); return; }
      if (s.photo_count > photos.length) {
        newCountEl.textContent = s.photo_count - photos.length;
        newPhotosPill.classList.remove('hidden');
        loadPhotos(true);
      } else if (s.photo_count < photos.length) {
        loadPhotos(true);
      }
      if (typeof s.selected_count === 'number' && s.selected_count !== selectedCount()) {
        paintCounts();
      }
    }).catch(function () {});
  }

  // ---------- 预览 ----------
  var lightbox = document.getElementById('lightbox');
  var lightboxImg = document.getElementById('lightboxImg');
  var lightboxName = document.getElementById('lightboxName');
  var lightboxIndexEl = document.getElementById('lightboxIndex');
  var lightboxSelect = document.getElementById('lightboxSelect');

  function currentList() { return visible(); }

  function showLightbox(index) {
    var list = currentList();
    if (index < 0 || index >= list.length) return;
    lightboxIndex = index;
    var p = list[index];
    lightboxImg.src = p.preview_url;
    lightboxName.textContent = p.name;
    lightboxIndexEl.textContent = (index + 1) + ' / ' + list.length;
    updateLightboxButton();
    lightbox.classList.remove('hidden');
    document.body.style.overflow = 'hidden';
  }

  function updateLightboxButton() {
    var list = currentList();
    var p = list[lightboxIndex];
    if (!p) return;
    lightboxSelect.textContent = p.selected ? '取消选中' : '选中这张';
    lightboxSelect.className = p.selected
      ? 'rounded-full bg-paper px-5 py-2 text-[12px] font-semibold text-ink'
      : 'rounded-full border border-paper/25 px-5 py-2 text-[12px] text-paper';
  }

  function closeLightbox() {
    lightbox.classList.add('hidden');
    lightboxImg.src = '';
    lightboxIndex = -1;
    document.body.style.overflow = '';
  }

  function step(delta) {
    var list = currentList();
    if (!list.length) return;
    lightboxIndex = (lightboxIndex + delta + list.length) % list.length;
    showLightbox(lightboxIndex);
  }

  gallery.addEventListener('click', function (e) {
    var chk = e.target.closest('[data-chk]');
    var fig = e.target.closest('[data-id]');
    if (!fig) return;
    var id = parseInt(fig.getAttribute('data-id'), 10);
    var p = photos.filter(function (x) { return x.id === id; })[0];
    if (!p) return;
    if (chk) { toggle(p); return; }
    var list = currentList();
    var idx = list.map(function (x) { return x.id; }).indexOf(id);
    showLightbox(idx);
  });

  document.getElementById('closeLightbox').addEventListener('click', closeLightbox);
  document.getElementById('prevPhoto').addEventListener('click', function (e) { e.stopPropagation(); step(-1); });
  document.getElementById('nextPhoto').addEventListener('click', function (e) { e.stopPropagation(); step(1); });
  lightbox.addEventListener('click', function (e) {
    if (e.target === lightbox) closeLightbox();
  });
  document.addEventListener('keydown', function (e) {
    if (lightbox.classList.contains('hidden')) return;
    if (e.key === 'Escape') closeLightbox();
    if (e.key === 'ArrowLeft') step(-1);
    if (e.key === 'ArrowRight') step(1);
  });

  lightboxSelect.addEventListener('click', function () {
    var list = currentList();
    var p = list[lightboxIndex];
    if (!p) return;
    toggle(p);
    updateLightboxButton();
  });

  // ---------- 顶部操作 ----------
  sortBtn.addEventListener('click', function () {
    sortDesc = !sortDesc;
    sortBtn.textContent = sortDesc ? '倒序' : '正序';
    sortBtn.title = sortDesc ? '当前按拍摄时间倒序' : '当前按拍摄时间正序';
    lightboxIndex = -1;
    render();
  });

  filterBtn.addEventListener('click', function () {
    onlySelected = !onlySelected;
    filterBtn.textContent = onlySelected ? '查看全部' : '只看已选';
    filterBtn.className = onlySelected
      ? 'btn-quiet !border-paper !bg-paper !px-4 !py-2 text-[11.5px] !text-ink'
      : 'btn-quiet !px-4 !py-2 text-[11.5px]';
    render();
  });

  refreshBtn.addEventListener('click', function () {
    newPhotosPill.classList.add('hidden');
    loadPhotos();
  });

  downloadBtn.addEventListener('click', function () {
    if (selectedCount() === 0) { toast('还没有选中照片'); return; }
    PLUI.downloadZip(apiBase + '/download-job', '正在打包已选照片…');
  });

  newPhotosPill.querySelector('button').addEventListener('click', function () {
    newPhotosPill.classList.add('hidden');
    window.scrollTo({ top: 0, behavior: 'smooth' });
  });

  // 支持左右滑动切换
  var touchX = 0;
  lightbox.addEventListener('touchstart', function (e) {
    touchX = e.changedTouches[0].clientX;
  }, { passive: true });
  lightbox.addEventListener('touchend', function (e) {
    var dx = e.changedTouches[0].clientX - touchX;
    if (Math.abs(dx) > 60) step(dx > 0 ? -1 : 1);
  }, { passive: true });

  // ---------- 访客上传 ----------
  var uploadBtn = document.getElementById('guestUploadBtn');
  var uploadInput = document.getElementById('guestFileInput');
  if (uploadBtn && uploadInput) {
    uploadBtn.addEventListener('click', function () { uploadInput.click(); });
    uploadInput.addEventListener('change', function () {
      if (uploadInput.files.length) uploadGuestFiles(uploadInput.files);
      uploadInput.value = '';
    });
  }

  function uploadGuestFiles(fileList) {
    var queue = Array.prototype.slice.call(fileList);
    if (!queue.length) return;
    var total = queue.length, done = 0, ok = 0, pending = 0, failed = 0, active = 0, firstError = '';
    uploadBtn.disabled = true;
    uploadBtn.textContent = '上传中';
    toast('开始上传 ' + total + ' 张');

    function finish() {
      uploadBtn.disabled = false;
      uploadBtn.textContent = '＋ 上传';
      if (ok > 0) {
        toast('已上传 ' + ok + ' 张' + (pending ? '，' + pending + ' 张待确认' : '') + (failed ? '，失败 ' + failed + ' 张' : ''));
        loadPhotos(true);
        window.scrollTo({ top: 0, behavior: 'smooth' });
      } else if (pending > 0) {
        toast('已提交 ' + pending + ' 张，等待摄影师确认后展示');
      } else {
        toast(firstError || '上传失败，请重试');
      }
    }

    function next() {
      if (!queue.length && active === 0) { finish(); return; }
      while (active < 2 && queue.length) {
        (function (file) {
          active++;
          var xhr = new XMLHttpRequest();
          xhr.open('POST', apiBase + '/photos');
          xhr.responseType = 'json';
          xhr.onload = function () {
            var d = xhr.response || {};
            if (xhr.status === 200 || xhr.status === 201) {
              if (d.photo && d.photo.status === 'pending') pending++;
              else ok++;
            } else {
              failed++;
              if (!firstError) firstError = d.error || (file.name + ' 上传失败');
            }
            active--; done++;
            toast('上传中 ' + done + ' / ' + total);
            next();
          };
          xhr.onerror = function () {
            failed++; active--; done++;
            if (!firstError) firstError = '网络中断，请重试';
            next();
          };
          var fd = new FormData();
          fd.append('file', file, file.name);
          xhr.send(fd);
        })(queue.shift());
      }
    }
    next();
  }

  loadPhotos(true);
  setInterval(poll, 3000);
})();
