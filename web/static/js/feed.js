const feed = document.getElementById('feed');
const loader = document.getElementById('loader');
let loading = false;
let done = false;
const seen = new Set();

// requireLogin inspects a fetch response and, on 401, redirects to the login
// page with the current URL as the return target. Returns true if redirected.
function requireLogin(res) {
  if (res.status === 401) {
    location.href = '/login?next=' + encodeURIComponent(location.pathname + location.search);
    return true;
  }
  return false;
}

const observer = new IntersectionObserver((entries) => {
  entries.forEach((entry) => {
    const card = entry.target;
    const video = card.querySelector('video');
    if (entry.isIntersecting && entry.intersectionRatio > 0.6) {
      feed.querySelectorAll('video').forEach((v) => { if (v !== video) v.pause(); });
      video.play().catch(() => {});
      const id = card.dataset.id;
      if (!seen.has(id)) {
        seen.add(id);
        fetch('/api/videos/' + id + '/view', { method: 'POST' }).catch(() => {});
      }
    } else {
      video.pause();
    }
  });
}, { threshold: [0, 0.6, 1] });

async function loadPage() {
  if (loading || done) return;
  loading = true;
  loader.classList.remove('hide');
  const cursor = feed.lastElementChild && feed.lastElementChild.dataset.id
    ? feed.lastElementChild.dataset.id : 0;
  try {
    const res = await fetch('/api/videos?limit=20&cursor=' + cursor);
    const data = await res.json();
    if (!data.videos || data.videos.length === 0) {
      if (feed.children.length === 0) {
        feed.innerHTML = '<div class="empty">No videos yet. <a href="/upload">Upload one</a>!</div>';
      }
      done = true;
      loader.classList.add('hide');
      return;
    }
    data.videos.forEach(renderCard);
    if (data.videos.length < 20) { done = true; loader.classList.add('hide'); }
  } catch (e) {
    console.error(e);
  } finally {
    loading = false;
  }
}

function renderCard(v) {
  const card = document.createElement('section');
  card.className = 'video-card';
  card.dataset.id = v.id;
  const liked = !!v.liked;
  card.innerHTML =
    '<video muted loop playsinline preload="metadata">' +
      '<source src="/uploads/' + encodeURIComponent(v.filename) + '" type="' + (v.mime_type || 'video/mp4') + '">' +
    '</video>' +
    '<div class="tap-hint">tap: play/pause · double-tap: ❤</div>' +
    '<div class="play-indicator">▶</div>' +
    '<div class="actions">' +
      '<button class="action-btn like" type="button" aria-label="Like">' +
        '<span class="icon heart' + (liked ? ' liked' : '') + '">❤</span>' +
        '<span class="count">' + formatCount(v.likes_count) + '</span>' +
      '</button>' +
      '<button class="action-btn comment" type="button" aria-label="Comments">' +
        '<span class="icon">💬</span>' +
        '<span class="count">' + formatCount(v.comments_count) + '</span>' +
      '</button>' +
    '</div>' +
    '<div class="overlay">' +
      '<div class="title">' + escapeHtml(v.title) + '</div>' +
      authorRow(v) +
      '<div class="meta">' + formatDate(v.created_at) + '</div>' +
    '</div>' +
    '<div class="seek-bar">' +
      '<span class="seek-time seek-cur">0:00</span>' +
      '<button class="seek-skip seek-back" type="button" aria-label="Skip back 10 seconds">⏪</button>' +
      '<div class="seek-track">' +
        '<div class="seek-fill"></div>' +
        '<div class="seek-thumb"></div>' +
      '</div>' +
      '<button class="seek-skip seek-fwd" type="button" aria-label="Skip forward 10 seconds">⏩</button>' +
      '<span class="seek-time seek-dur">0:00</span>' +
      '<button class="seek-mute" type="button" aria-label="Mute / unmute">🔇</button>' +
    '</div>';
  feed.appendChild(card);

  const video = card.querySelector('video');
  // Single click toggles play/pause; double click (or double tap on mobile)
  // likes the video, matching the TikTok gesture. A short timer disambiguates.
  let clickTimer = null;
  video.addEventListener('click', (e) => {
    if (clickTimer) {
      clearTimeout(clickTimer);
      clickTimer = null;
      likeVideo(card, v.id, e);
    } else {
      clickTimer = setTimeout(() => {
        clickTimer = null;
        togglePlay(card, video);
      }, 250);
    }
  });
  card.querySelector('.action-btn.like').addEventListener('click', () => toggleLike(card, v.id));
  card.querySelector('.action-btn.comment').addEventListener('click', () => openComments(card, v.id));
  wireSeekBar(card, video);
  observer.observe(card);
}

// authorRow renders the uploader line under the title. Legacy videos
// (user_id 0) show "Unknown uploader" with no profile link.
function authorRow(v) {
  const name = v.author_name || 'Unknown uploader';
  const tag = '@' + escapeHtml(name);
  if (v.user_id && v.user_id > 0) {
    return '<a class="author" href="/u/' + v.user_id + '">' + tag + '</a>';
  }
  return '<span class="author">' + tag + '</span>';
}

// formatTime turns a number of seconds into m:ss (e.g. 65 -> "1:05").
function formatTime(sec) {
  if (!isFinite(sec) || sec < 0) sec = 0;
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return m + ':' + (s < 10 ? '0' : '') + s;
}

// togglePlay is the single-tap gesture: play when paused, pause when playing.
// The play/pause indicator is driven by video events in wireSeekBar.
function togglePlay(card, video) {
  if (video.paused) video.play().catch(() => {});
  else video.pause();
}

// wireSeekBar wires up the TikTok/YouTube-style progress bar at the bottom of a
// video card: current second / duration readout, a draggable scrub track, and
// +/-10s skip buttons. Everything is driven by the native <video> element, so
// the logic ports directly to any future framework (Svelte/Next) rewrite.
function wireSeekBar(card, video) {
  const bar = card.querySelector('.seek-bar');
  const cur = bar.querySelector('.seek-cur');
  const dur = bar.querySelector('.seek-dur');
  const fill = bar.querySelector('.seek-fill');
  const thumb = bar.querySelector('.seek-thumb');
  const track = bar.querySelector('.seek-track');

  // render() syncs the readout + fill/thumb position from the video element.
  function render() {
    const d = video.duration;
    const t = video.currentTime;
    cur.textContent = formatTime(t);
    dur.textContent = isFinite(d) ? formatTime(d) : '0:00';
    const pct = (isFinite(d) && d > 0) ? Math.min(1, Math.max(0, t / d)) : 0;
    fill.style.width = (pct * 100) + '%';
    thumb.style.left = (pct * 100) + '%';
  }

  video.addEventListener('loadedmetadata', render);
  video.addEventListener('durationchange', render);
  video.addEventListener('timeupdate', render);
  video.addEventListener('seeking', render);
  video.addEventListener('seeked', render);

  // Skip buttons jump +/-10s, clamped to [0, duration]. stopPropagation keeps
  // the click from also toggling mute on the underlying video gesture handler.
  bar.querySelector('.seek-back').addEventListener('click', (e) => {
    e.stopPropagation();
    if (isFinite(video.duration)) video.currentTime = Math.max(0, video.currentTime - 10);
  });
  bar.querySelector('.seek-fwd').addEventListener('click', (e) => {
    e.stopPropagation();
    if (isFinite(video.duration)) video.currentTime = Math.min(video.duration, video.currentTime + 10);
  });

  // Scrub: pointer events cover mouse, touch and pen. setPointerCapture keeps
  // the drag alive even if the pointer leaves the track, and touch-action:none
  // on the track means a horizontal scrub won't trigger the feed's vertical
  // scroll-snap swipe.
  let scrubbing = false;
  const ratio = (e) => {
    const rect = track.getBoundingClientRect();
    return Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
  };
  const seekTo = (r) => {
    if (isFinite(video.duration)) video.currentTime = r * video.duration;
  };
  track.addEventListener('pointerdown', (e) => {
    e.stopPropagation();
    scrubbing = true;
    bar.classList.add('active');
    try { track.setPointerCapture(e.pointerId); } catch {}
    seekTo(ratio(e));
  });
  track.addEventListener('pointermove', (e) => { if (scrubbing) seekTo(ratio(e)); });
  const endScrub = (e) => {
    if (!scrubbing) return;
    scrubbing = false;
    bar.classList.remove('active');
    try { track.releasePointerCapture(e.pointerId); } catch {}
  };
  track.addEventListener('pointerup', endScrub);
  track.addEventListener('pointercancel', endScrub);

  // Mute toggle button — replaces the old single-tap-to-mute gesture (single
  // tap now pauses/resumes). volumechange keeps the icon in sync if muted is
  // changed from anywhere.
  const muteBtn = bar.querySelector('.seek-mute');
  const syncMute = () => { muteBtn.textContent = video.muted ? '🔇' : '🔊'; };
  muteBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    video.muted = !video.muted;
    syncMute();
  });
  video.addEventListener('volumechange', syncMute);
  syncMute();

  // Centered play/pause indicator: reflect the video's actual play state. This
  // stays correct even though autoplay is driven by the IntersectionObserver.
  const syncPaused = () => card.classList.toggle('is-paused', video.paused);
  video.addEventListener('play', syncPaused);
  video.addEventListener('pause', syncPaused);
  syncPaused();

  render();
}

async function toggleLike(card, id) {
  const heart = card.querySelector('.heart');
  const count = card.querySelector('.action-btn.like .count');
  heart.classList.add('pop');
  setTimeout(() => heart.classList.remove('pop'), 120);
  try {
    const res = await fetch('/api/videos/' + id + '/like', { method: 'POST' });
    if (requireLogin(res)) return;
    const data = await res.json();
    if (data.liked) heart.classList.add('liked');
    else heart.classList.remove('liked');
    count.textContent = formatCount(data.count);
  } catch (e) { console.error(e); }
}

// isLiked reads the current like state from the heart icon.
function isLiked(card) {
  return card.querySelector('.heart').classList.contains('liked');
}

// likeVideo is the double-click/double-tap handler. It ALWAYS results in a like
// (never unliking an already-liked video, matching TikTok) and plays the big
// floating heart animation at the tap point.
function likeVideo(card, id, e) {
  const rect = card.getBoundingClientRect();
  const x = (e && e.clientX ? e.clientX : rect.left + rect.width / 2) - rect.left;
  const y = (e && e.clientY ? e.clientY : rect.top + rect.height / 2) - rect.top;
  showFloatingHeart(card, x, y);

  const heart = card.querySelector('.heart');
  heart.classList.add('pop');
  setTimeout(() => heart.classList.remove('pop'), 200);
  if (isLiked(card)) return; // already liked: just replay the animation

  heart.classList.add('liked'); // optimistic UI
  fetch('/api/videos/' + id + '/like', { method: 'POST' })
    .then((r) => {
      if (requireLogin(r)) throw 'login'; // signal to skip reconciliation
      return r.json();
    })
    .then((data) => {
      card.querySelector('.action-btn.like .count').textContent = formatCount(data.count);
      if (!data.liked) heart.classList.remove('liked'); // reconcile if needed
    })
    .catch((err) => { if (err !== 'login') { console.error(err); heart.classList.remove('liked'); } });
}

// showFloatingHeart spawns a big heart at (x, y) that scales up and fades out.
function showFloatingHeart(card, x, y) {
  const h = document.createElement('div');
  h.className = 'tap-heart';
  h.textContent = '❤';
  h.style.left = x + 'px';
  h.style.top = y + 'px';
  card.appendChild(h);
  h.addEventListener('animationend', () => h.remove());
}

/* ---------------- Comments modal ---------------- */
// A single shared bottom-sheet modal. Track which card opened it so the comment
// count badge on that card can be updated optimistically after posting.
let activeCard = null;
let activeVideoId = null;
let commentCursor = 0;
let commentsDone = false;
let commentsLoading = false;

function ensureModal() {
  if (document.getElementById('commentModal')) return;
  const m = document.createElement('div');
  m.id = 'commentModal';
  m.className = 'modal-overlay';
  m.innerHTML =
    '<div class="comment-sheet">' +
      '<div class="sheet-header">' +
        '<span class="sheet-title" id="sheetTitle">Comments</span>' +
        '<button class="sheet-close" id="sheetClose" aria-label="Close">✕</button>' +
      '</div>' +
      '<div class="comment-list" id="commentList"></div>' +
      '<form class="comment-input-bar" id="commentForm">' +
        '<input type="text" id="commentText" placeholder="Add a comment..." maxlength="500" autocomplete="off">' +
        '<button type="submit" id="commentSend">Post</button>' +
      '</form>' +
    '</div>';
  document.body.appendChild(m);
  m.addEventListener('click', (e) => { if (e.target === m) closeComments(); });
  document.getElementById('sheetClose').addEventListener('click', closeComments);
  document.getElementById('commentForm').addEventListener('submit', submitComment);
  document.getElementById('commentList').addEventListener('scroll', onCommentScroll);
}

function openComments(card, videoId) {
  ensureModal();
  activeCard = card;
  activeVideoId = videoId;
  commentCursor = 0;
  commentsDone = false;
  document.getElementById('commentList').innerHTML = '<div class="comment-empty">Loading…</div>';
  document.getElementById('sheetTitle').textContent =
    'Comments ' + card.querySelector('.action-btn.comment .count').textContent;
  document.getElementById('commentModal').classList.add('open');
  document.getElementById('commentText').value = '';
  loadComments(false);
}

function closeComments() {
  const m = document.getElementById('commentModal');
  if (m) m.classList.remove('open');
  activeCard = null;
  activeVideoId = null;
}

// Load a page of comments. append=false replaces the list (first page);
// append=true adds older comments at the bottom for infinite scroll.
async function loadComments(append) {
  if (commentsLoading || commentsDone || !activeVideoId) return;
  commentsLoading = true;
  try {
    const res = await fetch('/api/videos/' + activeVideoId + '/comments?cursor=' + commentCursor + '&limit=20');
    const data = await res.json();
    const list = document.getElementById('commentList');
    if (!append) list.innerHTML = '';
    if (!data.comments || data.comments.length === 0) {
      if (!append) list.innerHTML = '<div class="comment-empty">No comments yet. Be the first!</div>';
      commentsDone = true;
    } else {
      // Server returns newest first; append in order so top=newest.
      data.comments.forEach((c) => renderComment(c, false));
      commentCursor = data.next || 0;
      if (data.comments.length < 20) commentsDone = true;
    }
  } catch (e) {
    console.error(e);
  } finally {
    commentsLoading = false;
  }
}

function renderComment(c, prepend) {
  const li = document.createElement('div');
  li.className = 'comment-item';
  var avatarHtml;
  if (c.avatar_url) {
    avatarHtml = '<img class="avatar avatar-img" src="' + escapeHtml(c.avatar_url) + '" alt="">';
  } else {
    avatarHtml = '<div class="avatar">' + escapeHtml((c.author || '?').slice(-1).toUpperCase()) + '</div>';
  }
  var authorHtml;
  if (c.user_id && c.user_id > 0) {
    avatarHtml = '<a href="/u/' + c.user_id + '">' + avatarHtml + '</a>';
    authorHtml = '<a class="comment-author" href="/u/' + c.user_id + '">' + escapeHtml(c.author) + '</a>';
  } else {
    authorHtml = '<span class="comment-author">' + escapeHtml(c.author) + '</span>';
  }
  li.innerHTML =
    avatarHtml +
    '<div class="comment-body">' +
      '<div>' + authorHtml +
        '<span class="comment-time"> · ' + timeAgo(c.created_at) + '</span>' +
      '</div>' +
      '<div class="comment-text">' + escapeHtml(c.text) + '</div>' +
    '</div>';
  const list = document.getElementById('commentList');
  // A freshly posted comment goes to the top (prepend); loaded pages append.
  if (prepend) list.prepend(li);
  else list.appendChild(li);
}

async function submitComment(e) {
  e.preventDefault();
  if (!activeVideoId) return;
  const input = document.getElementById('commentText');
  const text = input.value.trim();
  if (!text) return;
  input.value = '';
  const btn = document.getElementById('commentSend');
  btn.disabled = true;
  try {
    const fd = new FormData();
    fd.append('text', text);
    const res = await fetch('/api/videos/' + activeVideoId + '/comments', { method: 'POST', body: fd });
    if (requireLogin(res)) { input.value = text; return; }
    const data = await res.json();
    if (!res.ok) { input.value = text; return; }
    const list = document.getElementById('commentList');
    const empty = list.querySelector('.comment-empty');
    if (empty) empty.remove();
    renderComment(data.comment, true);
    if (activeCard) activeCard.querySelector('.action-btn.comment .count').textContent = formatCount(data.count);
    document.getElementById('sheetTitle').textContent = 'Comments ' + formatCount(data.count);
  } catch (err) {
    console.error(err);
    input.value = text;
  } finally {
    btn.disabled = false;
  }
}

function onCommentScroll() {
  const list = document.getElementById('commentList');
  if (list.scrollTop + list.clientHeight >= list.scrollHeight - 200) loadComments(true);
}

function timeAgo(s) {
  try {
    const d = (Date.now() - new Date(s).getTime()) / 1000;
    if (d < 60) return 'now';
    if (d < 3600) return Math.floor(d / 60) + 'm';
    if (d < 86400) return Math.floor(d / 3600) + 'h';
    if (d < 604800) return Math.floor(d / 86400) + 'd';
    return new Date(s).toLocaleDateString();
  } catch { return ''; }
}

function formatCount(n) {
  n = Number(n) || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(n);
}
function formatDate(s) {
  try { return new Date(s).toLocaleString(); } catch { return ''; }
}
function escapeHtml(s) {
  return String(s || '').replace(/[&<>"']/g, (m) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m]));
}

feed.addEventListener('scroll', () => {
  if (feed.scrollTop + feed.clientHeight >= feed.scrollHeight - 600) loadPage();
});

loadPage();
