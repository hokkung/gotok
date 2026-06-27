const root = document.getElementById('authRoot');
const status = document.getElementById('authStatus');
const next = (root && root.dataset.next) || '/feed';

let mode = 'login'; // 'login' | 'register'

function setStatus(msg, kind) {
  status.className = 'status' + (kind ? ' ' + kind : '');
  status.textContent = msg;
}

const titleEl = document.getElementById('authTitle');
const subEl = document.getElementById('authSub');
const nameField = document.getElementById('nameField');
const submitBtn = document.getElementById('credSubmit');
const passwordField = document.getElementById('passwordField');

function setMode(m) {
  mode = m;
  const login = mode === 'login';
  titleEl.textContent = login ? 'Log in to GoTok' : 'Join GoTok';
  subEl.textContent = login
    ? 'Log in to like videos and post comments.'
    : 'Create an account to like and comment.';
  nameField.hidden = login;
  submitBtn.textContent = login ? 'Log in' : 'Sign up';
  passwordField.autocomplete = login ? 'current-password' : 'new-password';
}

document.querySelectorAll('.auth-tab').forEach((tab) => {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.auth-tab').forEach((t) => t.classList.remove('active'));
    tab.classList.add('active');
    setMode(tab.dataset.mode);
  });
});

async function postForm(url, fd) {
  fd.append('next', next);
  setStatus(mode === 'login' ? 'Logging in…' : 'Creating account…', '');
  try {
    const res = await fetch(url, { method: 'POST', body: fd });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) { setStatus(data.error || 'Something went wrong', 'error'); return; }
    location.href = data.redirect || next;
  } catch (err) {
    setStatus('Network error', 'error');
  }
}

document.getElementById('credForm').addEventListener('submit', (e) => {
  e.preventDefault();
  const fd = new FormData(e.target);
  postForm(mode === 'login' ? '/auth/login' : '/auth/register', fd);
});

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
