const form = document.getElementById('uploadForm');
const fileInput = document.getElementById('file');
const dztext = document.getElementById('dztext');
const dropzone = document.getElementById('dropzone');
const submitBtn = document.getElementById('submitBtn');
const status = document.getElementById('status');

fileInput.addEventListener('change', () => {
  if (fileInput.files[0]) dztext.textContent = fileInput.files[0].name;
});

['dragover', 'dragenter'].forEach((ev) =>
  dropzone.addEventListener(ev, (e) => { e.preventDefault(); dropzone.style.borderColor = '#ff3b5c'; }));
['dragleave', 'drop'].forEach((ev) =>
  dropzone.addEventListener(ev, (e) => { e.preventDefault(); dropzone.style.borderColor = ''; }));
dropzone.addEventListener('drop', (e) => {
  if (e.dataTransfer.files.length) {
    fileInput.files = e.dataTransfer.files;
    dztext.textContent = fileInput.files[0].name;
  }
});

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  status.className = 'status';
  if (!fileInput.files[0]) {
    status.className = 'status error';
    status.textContent = 'Please choose a video.';
    return;
  }
  const fd = new FormData();
  fd.append('file', fileInput.files[0]);
  fd.append('title', document.getElementById('title').value);
  submitBtn.disabled = true;
  status.textContent = 'Uploading…';
  try {
    const res = await fetch('/api/upload', { method: 'POST', body: fd });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'upload failed');
    status.className = 'status ok';
    status.textContent = 'Uploaded! Redirecting to feed…';
    setTimeout(() => (location.href = '/feed'), 900);
  } catch (err) {
    status.className = 'status error';
    status.textContent = err.message;
    submitBtn.disabled = false;
  }
});
