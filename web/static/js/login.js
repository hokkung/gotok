const root = document.getElementById('authRoot');
const status = document.getElementById('authStatus');
const next = (root && root.dataset.next) || '/feed';

function setStatus(msg, kind) {
  status.className = 'status' + (kind ? ' ' + kind : '');
  status.textContent = msg;
}

async function postAuth(url) {
  const fd = new FormData();
  fd.append('next', next);
  setStatus('Logging in…', '');
  try {
    const res = await fetch(url, { method: 'POST', body: fd });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) { setStatus(data.error || 'Could not log in', 'error'); return; }
    location.href = data.redirect || next;
  } catch (err) {
    setStatus('Network error', 'error');
  }
}

document.getElementById('demoBtn').addEventListener('click', () => postAuth('/auth/demo'));
document.getElementById('googleBtn').addEventListener('click', () => setStatus('Google sign-in is coming soon.', 'error'));
document.getElementById('facebookBtn').addEventListener('click', () => setStatus('Facebook sign-in is coming soon.', 'error'));