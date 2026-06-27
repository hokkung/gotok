(function () {
  var toggle = document.getElementById('menuToggle');
  var sidebar = document.getElementById('sidebar');
  var overlay = document.getElementById('sidebarOverlay');
  var closeBtn = document.getElementById('sidebarClose');
  if (!toggle || !sidebar || !overlay) return;

  function open() {
    sidebar.classList.add('open');
    overlay.classList.add('open');
    toggle.setAttribute('aria-expanded', 'true');
    sidebar.setAttribute('aria-hidden', 'false');
  }

  function close() {
    sidebar.classList.remove('open');
    overlay.classList.remove('open');
    toggle.setAttribute('aria-expanded', 'false');
    sidebar.setAttribute('aria-hidden', 'true');
  }

  function isOpen() { return sidebar.classList.contains('open'); }

  toggle.addEventListener('click', function () { isOpen() ? close() : open(); });
  if (closeBtn) closeBtn.addEventListener('click', close);
  overlay.addEventListener('click', close);
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && isOpen()) close();
  });
})();
