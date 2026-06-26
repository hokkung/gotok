// Profile page: paginated grid of a single user's uploaded videos.
(function () {
  const grid = document.getElementById('grid');
  const loader = document.getElementById('profileLoader');
  const userID = grid.dataset.userId;
  const scroller = document.querySelector('.profile-page');
  let loading = false;
  let done = false;

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

  async function loadPage() {
    if (loading || done) return;
    loading = true;
    loader.classList.remove('hide');
    const cursor = grid.lastElementChild ? grid.lastElementChild.dataset.id : 0;
    try {
      const res = await fetch('/api/users/' + userID + '/videos?limit=24&cursor=' + cursor);
      const data = await res.json();
      if (!data.videos || data.videos.length === 0) {
        if (grid.children.length === 0) {
          grid.innerHTML = '<div class="grid-empty">No videos yet.</div>';
        }
        done = true;
        loader.classList.add('hide');
        return;
      }
      data.videos.forEach(renderTile);
      if (data.videos.length < 24) { done = true; loader.classList.add('hide'); }
    } catch (e) {
      console.error(e);
    } finally {
      loading = false;
    }
  }

  function renderTile(v) {
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

    // Tap to play inline; pause when the tile scrolls out of view.
    const video = tile.querySelector('video');
    tile.addEventListener('click', () => {
      if (video.paused) video.play().catch(() => {});
      else video.pause();
    });
    new IntersectionObserver((entries, obs) => {
      entries.forEach((en) => { if (!en.isIntersecting) video.pause(); });
    }, { threshold: 0.25 }).observe(tile);
  }

  scroller.addEventListener('scroll', () => {
    if (scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 400) loadPage();
  });

  loadPage();
})();
