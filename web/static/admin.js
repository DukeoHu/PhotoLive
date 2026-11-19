(function () {
  var modal = document.getElementById('modal');
  var roomForm = document.getElementById('roomForm');
  var roomError = document.getElementById('roomError');
  var dateInput = document.getElementById('roomDate');

  function openModal() {
    if (!modal) return;
    modal.classList.remove('hidden');
    modal.classList.add('flex');
    if (dateInput && !dateInput.value) {
      var d = new Date();
      dateInput.value = d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
    }
    setTimeout(function () {
      var n = document.getElementById('roomName');
      if (n) n.focus();
    }, 60);
  }

  function closeModal() {
    if (!modal) return;
    modal.classList.add('hidden');
    modal.classList.remove('flex');
  }

  ['newRoomBtn', 'newRoomBtn2'].forEach(function (id) {
    var el = document.getElementById(id);
    if (el) el.addEventListener('click', openModal);
  });
  var closeBtn = document.getElementById('closeModal');
  if (closeBtn) closeBtn.addEventListener('click', closeModal);
  if (modal) {
    modal.addEventListener('click', function (e) {
      if (e.target === modal) closeModal();
    });
  }
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeModal();
  });

  if (roomForm) {
    roomForm.addEventListener('submit', function (e) {
      e.preventDefault();
      roomError.classList.add('hidden');
      var btn = roomForm.querySelector('button[type="submit"]');
      btn.disabled = true;
      btn.textContent = '创建中…';
      fetch('/api/rooms', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: document.getElementById('roomName').value.trim(),
          event_date: document.getElementById('roomDate').value,
          note: document.getElementById('roomNote').value.trim(),
          allow_download: document.getElementById('allowDownload').checked
        })
      }).then(function (res) {
        return res.json().catch(function () { return {}; }).then(function (data) {
          if (!res.ok) throw new Error(data.error || '创建失败，请重试');
          window.location.href = '/admin/room/' + data.id;
        });
      }).catch(function (err) {
        roomError.textContent = err.message;
        roomError.classList.remove('hidden');
        btn.disabled = false;
        btn.textContent = '创建房间';
      });
    });
  }

  var logout = document.getElementById('logoutBtn');
  if (logout) {
    logout.addEventListener('click', function () {
      fetch('/api/auth/logout', { method: 'POST' }).then(function () {
        window.location.href = '/';
      });
    });
  }

  function copyText(text, done) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done).catch(function () { fallback(text, done); });
    } else {
      fallback(text, done);
    }
  }

  function fallback(text, done) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); } catch (err) {}
    document.body.removeChild(ta);
    done();
  }

  Array.prototype.forEach.call(document.querySelectorAll('.copyLink'), function (btn) {
    btn.addEventListener('click', function (e) {
      e.preventDefault();
      e.stopPropagation();
      var url = window.location.origin + btn.dataset.link;
      copyText(url, function () {
        var old = btn.getAttribute('title');
        btn.setAttribute('title', '已复制');
        btn.classList.add('text-paper');
        setTimeout(function () {
          btn.setAttribute('title', old || '复制客户链接');
          btn.classList.remove('text-paper');
        }, 1400);
      });
    });
  });
})();
