// Profile page: tabbed grid of a user's uploaded and liked videos, plus an
// inline edit-profile modal for the profile owner.
(function () {
  const page = document.querySelector('.profile-page');
  const gridVideos = document.getElementById('grid');
  const gridLiked = document.getElementById('gridLiked');
  const loader = document.getElementById('profileLoader');
  const userID = gridVideos.dataset.userId;

  function escapeHtml(s) {
    return String(s || '').replace(/[&<>"']/g, (m) =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m]));
  }
  function formatCount(n) {
    n = Number(n) || 0;
    if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K';
    return String(n);
  }

  // Each tab keeps its own pagination state. The cursor is the server-provided
  // `next` value (a video id for the Videos tab, a like id for the Liked tab),
  // so pagination works regardless of ordering.
  const tabs = {
    videos: { grid: gridVideos, endpoint: '/api/users/' + userID + '/videos', loading: false, done: false, cursor: 0, started: false },
    liked: { grid: gridLiked, endpoint: '/api/users/' + userID + '/liked', loading: false, done: false, cursor: 0, started: false },
  };
  let active = 'videos';

  function showEmpty(t) {
    const empty = document.createElement('div');
    empty.className = 'grid-empty';
    empty.textContent = active === 'videos' ? 'No videos yet.' : 'No liked videos.';
    t.grid.appendChild(empty);
  }

  async function loadPage(name) {
    const t = tabs[name];
    if (t.loading || t.done) return;
    t.loading = true;
    loader.classList.remove('hide');
    try {
      const res = await fetch(t.endpoint + '?limit=24&cursor=' + t.cursor);
      const data = await res.json();
      if (!data.videos || data.videos.length === 0) {
        if (t.grid.children.length === 0) showEmpty(t);
        t.done = true;
      } else {
        data.videos.forEach((v) => renderTile(t.grid, v));
        t.cursor = data.next;
        if (data.next === 0 || data.videos.length < 24) t.done = true;
      }
    } catch (e) {
      console.error(e);
    } finally {
      t.loading = false;
      if (tabs[active].done) loader.classList.add('hide');
    }
  }

  function renderTile(grid, v) {
    const tile = document.createElement('div');
    tile.className = 'video-tile';
    tile.dataset.id = v.id;
    tile.innerHTML =
      '<video muted loop playsinline preload="metadata">' +
        '<source src="/uploads/' + encodeURIComponent(v.filename) + '" type="' + (v.mime_type || 'video/mp4') + '">' +
      '</video>' +
      '<div class="tile-overlay">' +
        '<div class="tile-title">' + escapeHtml(v.title) + '</div>' +
        '<div class="tile-stats">❤ ' + formatCount(v.likes_count) + ' · 💬 ' + formatCount(v.comments_count) + '</div>' +
      '</div>';
    grid.appendChild(tile);

    const video = tile.querySelector('video');
    tile.addEventListener('click', () => {
      if (video.paused) video.play().catch(() => {});
      else video.pause();
    });
    new IntersectionObserver((entries) => {
      entries.forEach((en) => { if (!en.isIntersecting) video.pause(); });
    }, { threshold: 0.25 }).observe(tile);
  }

  // Tab switching: toggle buttons + visible grid, and lazy-load the Liked tab.
  const tabBtns = document.querySelectorAll('.profile-tab');
  tabBtns.forEach((btn) => {
    btn.addEventListener('click', () => {
      const name = btn.dataset.tab;
      if (name === active) return;
      active = name;
      tabBtns.forEach((b) => b.classList.toggle('active', b.dataset.tab === name));
      tabs.videos.grid.classList.toggle('active', name === 'videos');
      tabs.liked.grid.classList.toggle('active', name === 'liked');
      if (!tabs[name].started) { tabs[name].started = true; loadPage(name); }
      if (tabs[name].done) loader.classList.add('hide'); else loader.classList.remove('hide');
    });
  });

  page.addEventListener('scroll', () => {
    if (page.scrollTop + page.clientHeight >= page.scrollHeight - 400) loadPage(active);
  });

  tabs.videos.started = true;
  loadPage('videos');

  // ----- Edit profile modal (owner only; button only exists when IsOwner) -----
  const editBtn = document.getElementById('editProfileBtn');
  if (editBtn) {
    const modal = document.getElementById('editModal');
    const closeBtn = document.getElementById('editClose');
    const form = document.getElementById('editForm');
    const fileInput = form.querySelector('input[type=file]');
    const status = document.getElementById('editStatus');
    const submitBtn = document.getElementById('editSubmit');
    const avatarPreview = document.getElementById('editAvatarPreview');
    const avatarBadge = document.getElementById('editAvatarBadge');

    editBtn.addEventListener('click', () => {
      status.textContent = '';
      status.className = 'status';
      modal.classList.add('open');
    });
    closeBtn.addEventListener('click', () => modal.classList.remove('open'));
    modal.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });

    fileInput.addEventListener('change', () => {
      const f = fileInput.files[0];
      if (!f) return;
      avatarPreview.src = URL.createObjectURL(f);
      avatarPreview.hidden = false;
      avatarBadge.hidden = true;
    });

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      submitBtn.disabled = true;
      status.textContent = 'Saving…';
      status.className = 'status';
      try {
        const res = await fetch('/api/profile', { method: 'POST', body: new FormData(form) });
        const data = await res.json();
        if (!res.ok) throw new Error(data.error || 'could not save');
        window.location.reload();
      } catch (err) {
        status.textContent = err.message;
        status.className = 'status error';
        submitBtn.disabled = false;
      }
    });
  }

  var messageBtn = document.getElementById('messageBtn');
  if (messageBtn) {
    messageBtn.addEventListener('click', async function() {
      messageBtn.disabled = true;
      var userId = parseInt(messageBtn.getAttribute('data-user-id'), 10);
      try {
        var res = await fetch('/api/conversations', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ user_id: userId })
        });
        if (res.status === 401) { window.location.href = '/login?next=' + encodeURIComponent(window.location.pathname); return; }
        var data = await res.json();
        if (!res.ok) throw new Error(data.error || 'could not start conversation');
        window.location.href = '/chat?c=' + data.conversation.id;
      } catch (err) {
        messageBtn.disabled = false;
        alert(err.message);
      }
    });
  }
})();
