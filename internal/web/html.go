package web

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover, user-scalable=no">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<meta name="theme-color" content="#1c1c1e">
<title>BFT</title>
<style>
:root {
  --bg: #000000;
  --bg-secondary: #1c1c1e;
  --bg-tertiary: #2c2c2e;
  --bg-grouped: #1c1c1e;
  --separator: #38383a;
  --label: #ffffff;
  --label-secondary: #8e8e93;
  --blue: #0a84ff;
  --green: #30d158;
  --red: #ff453a;
  --orange: #ff9f0a;
  --tint: #0a84ff;
  --fill: #787880;
  --radius: 10px;
  --safe-top: env(safe-area-inset-top, 0px);
  --safe-bottom: env(safe-area-inset-bottom, 0px);
}
* { box-sizing: border-box; margin: 0; padding: 0; -webkit-tap-highlight-color: transparent; }
body { font-family: -apple-system, 'SF Pro Display', 'SF Pro Text', 'Helvetica Neue', sans-serif; background: var(--bg); color: var(--label); font-size: 17px; -webkit-font-smoothing: antialiased; padding-top: var(--safe-top); padding-bottom: var(--safe-bottom); min-height: 100vh; }

/* Navigation bar */
.nav-bar { background: rgba(28,28,30,0.85); backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px); border-bottom: 0.5px solid var(--separator); padding: 12px 16px; position: sticky; top: 0; z-index: 50; }
.nav-title { font-size: 17px; font-weight: 600; text-align: center; color: var(--label); }
.nav-subtitle { font-size: 13px; color: var(--label-secondary); text-align: center; margin-top: 2px; font-family: 'SF Mono', monospace; }

/* Toolbar */
.toolbar { display: flex; gap: 8px; padding: 8px 16px; overflow-x: auto; -webkit-overflow-scrolling: touch; }
.toolbar::-webkit-scrollbar { display: none; }
.pill { padding: 7px 14px; border-radius: 20px; background: var(--bg-tertiary); color: var(--label); font-size: 15px; font-weight: 500; border: none; cursor: pointer; white-space: nowrap; display: flex; align-items: center; gap: 5px; }
.pill:active { opacity: 0.6; }
.pill.blue { background: var(--blue); color: #fff; }
.pill.green { background: var(--green); color: #000; }

/* List (iOS grouped style) */
.list { margin: 8px 16px; background: var(--bg-grouped); border-radius: var(--radius); overflow: hidden; }
.list-item { display: flex; align-items: center; padding: 11px 16px; border-bottom: 0.5px solid var(--separator); cursor: pointer; transition: background 0.1s; gap: 12px; }
.list-item:last-child { border-bottom: none; }
.list-item:active { background: var(--bg-tertiary); }
.list-icon { font-size: 28px; flex-shrink: 0; width: 32px; text-align: center; }
.list-content { flex: 1; min-width: 0; }
.list-title { font-size: 17px; color: var(--label); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.list-detail { font-size: 13px; color: var(--label-secondary); margin-top: 2px; }
.list-accessory { color: var(--label-secondary); font-size: 13px; display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.list-chevron { color: var(--fill); font-size: 14px; }
.list-actions { display: flex; gap: 6px; }
.action-btn { padding: 5px 12px; border-radius: 14px; border: none; font-size: 13px; font-weight: 600; cursor: pointer; }
.action-btn:active { opacity: 0.6; }
.action-dl { background: var(--blue); color: #fff; }
.action-rm { background: rgba(255,69,58,0.15); color: var(--red); }

/* Upload area */
.upload-area { margin: 8px 16px; border: 2px dashed var(--separator); border-radius: var(--radius); padding: 32px 16px; text-align: center; color: var(--label-secondary); font-size: 15px; cursor: pointer; transition: all 0.2s; }
.upload-area:active, .upload-area.drag { border-color: var(--blue); color: var(--blue); background: rgba(10,132,255,0.05); }
.upload-area input { display: none; }
.upload-icon { font-size: 36px; margin-bottom: 8px; }

/* Terminal */
.terminal { margin: 8px 16px; background: var(--bg-grouped); border-radius: var(--radius); overflow: hidden; display: none; }
.terminal.show { display: block; }
.terminal-bar { padding: 10px 16px; background: var(--bg-tertiary); display: flex; align-items: center; justify-content: space-between; border-bottom: 0.5px solid var(--separator); }
.terminal-bar span { font-size: 15px; font-weight: 600; }
.terminal-body { padding: 12px 16px; font-family: 'SF Mono', 'Menlo', monospace; font-size: 13px; line-height: 1.5; white-space: pre-wrap; max-height: 300px; overflow-y: auto; -webkit-overflow-scrolling: touch; color: var(--label); }
.terminal-input { display: flex; border-top: 0.5px solid var(--separator); }
.terminal-input input { flex: 1; background: transparent; border: none; padding: 12px 16px; color: var(--label); font-family: 'SF Mono', 'Menlo', monospace; font-size: 15px; outline: none; }
.terminal-input button { background: var(--blue); color: #fff; border: none; padding: 12px 20px; font-size: 15px; font-weight: 600; cursor: pointer; }
.terminal-input button:active { opacity: 0.6; }
.cmd-line { color: var(--blue); }
.stderr { color: var(--red); }

/* Modal */
.modal-overlay { display: none; position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px); z-index: 100; justify-content: center; align-items: flex-end; }
.modal-overlay.show { display: flex; }
.modal { background: var(--bg-secondary); border-radius: 14px 14px 0 0; padding: 20px 16px; width: 100%; max-width: 500px; padding-bottom: calc(20px + var(--safe-bottom)); }
.modal h3 { font-size: 17px; font-weight: 600; margin-bottom: 16px; text-align: center; }
.modal input[type=text] { width: 100%; padding: 12px 16px; background: var(--bg-tertiary); border: none; border-radius: var(--radius); color: var(--label); font-size: 17px; margin-bottom: 12px; outline: none; }
.modal input[type=text]::placeholder { color: var(--fill); }
.modal .btn-row { display: flex; gap: 8px; }
.modal .btn-row button { flex: 1; padding: 14px; border-radius: var(--radius); border: none; font-size: 17px; font-weight: 600; cursor: pointer; }
.modal .btn-cancel { background: var(--bg-tertiary); color: var(--label); }
.modal .btn-ok { background: var(--blue); color: #fff; }

/* Toast */
.toast { position: fixed; bottom: calc(20px + var(--safe-bottom)); left: 50%; transform: translateX(-50%); background: var(--bg-tertiary); color: var(--label); padding: 12px 24px; border-radius: 22px; font-size: 15px; z-index: 200; display: none; backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px); box-shadow: 0 4px 12px rgba(0,0,0,0.4); }
.toast.error { background: var(--red); color: #fff; }
.toast.show { display: block; }

/* Top progress bar */
.progress { display: none; height: 3px; background: var(--blue); position: fixed; top: 0; left: 0; z-index: 300; transition: width 0.3s; border-radius: 0 2px 2px 0; }
.progress.show { display: block; }

/* Transfer overlay */
.transfer-overlay { display: none; position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.6); backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px); z-index: 250; justify-content: center; align-items: center; }
.transfer-overlay.show { display: flex; }
.transfer-card { background: var(--bg-secondary); border-radius: 14px; padding: 24px; width: 300px; text-align: center; }
.transfer-icon { font-size: 40px; margin-bottom: 12px; }
.transfer-filename { font-size: 15px; color: var(--label); font-weight: 600; margin-bottom: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.transfer-status { font-size: 13px; color: var(--label-secondary); margin-bottom: 16px; }
.progress-track { height: 6px; background: var(--bg-tertiary); border-radius: 3px; overflow: hidden; margin-bottom: 8px; }
.progress-fill { height: 100%; background: var(--blue); border-radius: 3px; transition: width 0.2s; width: 0%; }
.progress-fill.indeterminate { width: 30%; animation: indeterminate 1.5s ease-in-out infinite; }
@keyframes indeterminate { 0% { transform: translateX(-100%); } 100% { transform: translateX(400%); } }
.transfer-percent { font-size: 22px; font-weight: 700; color: var(--label); font-variant-numeric: tabular-nums; }
.transfer-detail { font-size: 13px; color: var(--label-secondary); margin-top: 4px; }
.transfer-stats { display: flex; justify-content: space-between; margin-top: 8px; font-size: 12px; color: var(--label-secondary); font-variant-numeric: tabular-nums; }
.transfer-stats span { display: flex; align-items: center; gap: 3px; }

/* Connection status bar */
.conn-bar { display: flex; align-items: center; gap: 8px; padding: 8px 16px; background: var(--bg-secondary); border-bottom: 0.5px solid var(--separator); }
.conn-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; transition: background 0.3s; }
.conn-dot.on { background: var(--green); box-shadow: 0 0 6px var(--green); }
.conn-dot.off { background: var(--red); box-shadow: 0 0 6px var(--red); }
.conn-text { font-size: 13px; color: var(--label-secondary); flex: 1; }
.conn-addr { font-family: 'SF Mono', monospace; color: var(--label); }

/* Empty state */
.empty { text-align: center; padding: 48px 16px; color: var(--label-secondary); }
.empty-icon { font-size: 48px; margin-bottom: 12px; opacity: 0.5; }
.empty-text { font-size: 17px; }

@media (min-width: 768px) {
  .modal { border-radius: 14px; align-self: center; }
  .modal { padding-bottom: 20px; }
  .list-item { padding: 12px 20px; }
}
</style>
</head>
<body>

<div class="progress" id="progress"></div>

<div class="nav-bar">
  <div class="nav-title">Bluetooth File Transfer</div>
  <div class="nav-subtitle" id="pathBar">/</div>
</div>

<div class="conn-bar" id="connBar">
  <span class="conn-dot on" id="connDot"></span>
  <span class="conn-text" id="connText">Checking...</span>
  <button class="pill" id="connBtn" onclick="toggleConnection()" style="margin-left:auto;font-size:13px;padding:5px 14px;">&#8230;</button>
</div>

<div class="toolbar">
  <button class="pill" onclick="goUp()">&#9664; Back</button>
  <button class="pill" onclick="refresh()">&#8635; Refresh</button>
  <button class="pill green" onclick="showUpload()">&#8593; Upload</button>
  <button class="pill blue" onclick="showMkdir()">+ Folder</button>
  <button class="pill" onclick="toggleTerminal()">&#9654; Terminal</button>
</div>

<div id="fileListContainer">
  <div class="list" id="fileList"></div>
</div>

<div class="upload-area" id="uploadArea" onclick="document.getElementById('fileInput').click()" ondragover="event.preventDefault();this.classList.add('drag')" ondragleave="this.classList.remove('drag')" ondrop="handleDrop(event)">
  <div class="upload-icon">&#9729;</div>
  Drop files here or tap to upload
  <input type="file" id="fileInput" multiple onchange="uploadFiles(this.files)">
</div>

<div class="terminal" id="terminal">
  <div class="terminal-bar">
    <span>Terminal</span>
    <button class="pill" onclick="toggleTerminal()" style="font-size:13px;padding:4px 12px;">Close</button>
  </div>
  <div class="terminal-body" id="termOutput"></div>
  <div class="terminal-input">
    <input id="termInput" placeholder="Enter command..." onkeydown="if(event.key==='Enter')runCmd()">
    <button onclick="runCmd()">Run</button>
  </div>
</div>

<div class="modal-overlay" id="mkdirModal" onclick="if(event.target===this)hideModal('mkdirModal')">
  <div class="modal">
    <h3>New Folder</h3>
    <input type="text" id="mkdirName" placeholder="Folder name" onkeydown="if(event.key==='Enter')doMkdir()">
    <div class="btn-row">
      <button class="btn-cancel" onclick="hideModal('mkdirModal')">Cancel</button>
      <button class="btn-ok" onclick="doMkdir()">Create</button>
    </div>
  </div>
</div>

<div class="transfer-overlay" id="transferOverlay">
  <div class="transfer-card">
    <div class="transfer-icon" id="transferIcon">&#8595;</div>
    <div class="transfer-filename" id="transferName">filename.txt</div>
    <div class="transfer-status" id="transferStatus">Preparing...</div>
    <div class="progress-track"><div class="progress-fill" id="transferBar"></div></div>
    <div class="transfer-percent" id="transferPercent">0%</div>
    <div class="transfer-detail" id="transferDetail"></div>
    <div class="transfer-stats">
      <span id="transferSpeed">-- Kbps</span>
      <span id="transferElapsed">0:00</span>
      <span id="transferEta">ETA --</span>
    </div>
  </div>
</div>

<div class="toast" id="toast"></div>

<script>
let currentPath = '/';

async function api(url, opts) {
  const p = document.getElementById('progress');
  p.style.width = '30%'; p.classList.add('show');
  try {
    const r = await fetch(url, opts);
    p.style.width = '100%';
    setTimeout(() => { p.classList.remove('show'); p.style.width = '0'; }, 300);
    if (!r.ok) { const t = await r.text(); throw new Error(t); }
    return r;
  } catch(e) {
    p.classList.remove('show'); p.style.width = '0';
    throw e;
  }
}

function toast(msg, isError) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast show' + (isError ? ' error' : '');
  setTimeout(() => t.classList.remove('show'), 2500);
}

function formatSize(b) {
  if (b >= 1073741824) return (b/1073741824).toFixed(1) + ' GB';
  if (b >= 1048576) return (b/1048576).toFixed(1) + ' MB';
  if (b >= 1024) return (b/1024).toFixed(0) + ' KB';
  return b + ' B';
}

async function loadDir(path) {
  try {
    const r = await api('/api/ls?path=' + encodeURIComponent(path));
    const data = await r.json();
    currentPath = data.path;
    document.getElementById('pathBar').textContent = currentPath;

    const list = document.getElementById('fileList');
    list.innerHTML = '';

    data.entries.sort((a,b) => {
      if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
      return a.name.localeCompare(b.name);
    });

    // Cache file sizes for download progress estimation
    fileInfoCache = {};
    data.entries.forEach(e => { if (e.type === 'file') fileInfoCache[e.name] = e.size; });

    if (data.entries.length === 0) {
      list.innerHTML = '<div class="empty"><div class="empty-icon">&#128193;</div><div class="empty-text">Empty folder</div></div>';
      return;
    }

    data.entries.forEach(e => {
      const isDir = e.type === 'dir';
      const fullPath = currentPath === '/' ? '/' + e.name : currentPath + '/' + e.name;
      const escapedPath = fullPath.replace(/'/g, "\\'");

      const item = document.createElement('div');
      item.className = 'list-item';

      if (isDir) {
        item.onclick = () => loadDir(fullPath);
        item.innerHTML =
          '<div class="list-icon">&#128193;</div>' +
          '<div class="list-content"><div class="list-title">' + e.name + '</div>' +
          '<div class="list-detail">' + e.time + '</div></div>' +
          '<div class="list-accessory"><div class="list-actions">' +
          '<button class="action-btn action-rm" onclick="event.stopPropagation();doRm(\'' + escapedPath + '\')">Delete</button>' +
          '</div><span class="list-chevron">&#10095;</span></div>';
      } else {
        item.innerHTML =
          '<div class="list-icon">&#128196;</div>' +
          '<div class="list-content"><div class="list-title">' + e.name + '</div>' +
          '<div class="list-detail">' + formatSize(e.size) + ' &middot; ' + e.time + '</div></div>' +
          '<div class="list-accessory"><div class="list-actions">' +
          '<button class="action-btn action-dl" onclick="event.stopPropagation();doDownload(\'' + escapedPath + '\')">Get</button>' +
          '<button class="action-btn action-rm" onclick="event.stopPropagation();doRm(\'' + escapedPath + '\')">Del</button>' +
          '</div></div>';
      }

      list.appendChild(item);
    });
  } catch(e) {
    toast(e.message, true);
  }
}

function refresh() { loadDir(currentPath); }

function goUp() {
  if (currentPath === '/') return;
  const parts = currentPath.split('/').filter(Boolean);
  parts.pop();
  loadDir('/' + parts.join('/'));
}

let transferStartTime = 0;

function formatTime(sec) {
  if (sec < 0 || !isFinite(sec)) return '--:--';
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return m + ':' + (s < 10 ? '0' : '') + s;
}

function formatSpeed(bytesPerSec) {
  const bps = bytesPerSec * 8;
  if (bps >= 1000000) return (bps/1000000).toFixed(1) + ' Mbps';
  if (bps >= 1000) return (bps/1000).toFixed(0) + ' Kbps';
  return Math.round(bps) + ' bps';
}

function showTransfer(icon, name, status) {
  document.getElementById('transferIcon').textContent = icon;
  document.getElementById('transferName').textContent = name;
  document.getElementById('transferStatus').textContent = status;
  document.getElementById('transferPercent').textContent = '';
  document.getElementById('transferDetail').textContent = '';
  document.getElementById('transferBar').style.width = '0%';
  document.getElementById('transferBar').classList.remove('indeterminate');
  document.getElementById('transferSpeed').textContent = '-- Kbps';
  document.getElementById('transferElapsed').textContent = '0:00';
  document.getElementById('transferEta').textContent = 'ETA --';
  document.getElementById('transferOverlay').classList.add('show');
  transferStartTime = Date.now();
}

function updateTransfer(percent, detail) {
  document.getElementById('transferBar').style.width = percent + '%';
  document.getElementById('transferBar').classList.remove('indeterminate');
  document.getElementById('transferPercent').textContent = Math.round(percent) + '%';
  if (detail) document.getElementById('transferDetail').textContent = detail;
}

function updateTransferStats(bytesTransferred, totalBytes) {
  const elapsed = (Date.now() - transferStartTime) / 1000;
  document.getElementById('transferElapsed').textContent = formatTime(elapsed);

  if (elapsed > 0.5 && bytesTransferred > 0) {
    const speed = bytesTransferred / elapsed;
    document.getElementById('transferSpeed').textContent = formatSpeed(speed);

    if (totalBytes > 0 && bytesTransferred < totalBytes) {
      const remaining = totalBytes - bytesTransferred;
      const eta = remaining / speed;
      document.getElementById('transferEta').textContent = 'ETA ' + formatTime(eta);
    } else {
      document.getElementById('transferEta').textContent = '';
    }
  }
}

function hideTransfer() {
  document.getElementById('transferOverlay').classList.remove('show');
}

// fileInfoCache stores file sizes from the last directory listing
let fileInfoCache = {};

async function doDownload(path) {
  const name = path.split('/').pop();
  const fileSize = fileInfoCache[name] || 0;
  showTransfer('\u2B07', name, 'Transferring via Bluetooth...');

  // Estimate BT transfer time (~170 KB/s) and animate progress
  const btSpeed = 170 * 1024; // bytes/sec estimate
  const estimatedMs = fileSize > 0 ? (fileSize / btSpeed) * 1000 : 5000;
  let startTime = Date.now();
  let cancelled = false;

  const progressTimer = setInterval(() => {
    if (cancelled) return;
    const elapsed = Date.now() - startTime;
    const pct = Math.min(90, (elapsed / estimatedMs) * 90);
    const estTransferred = fileSize > 0 ? Math.min(fileSize, Math.floor(fileSize * pct / 100)) : 0;
    updateTransfer(pct, fileSize > 0 ? formatSize(estTransferred) + ' / ' + formatSize(fileSize) : 'Please wait...');
    document.getElementById('transferStatus').textContent = 'Transferring via Bluetooth...';
    if (fileSize > 0) updateTransferStats(estTransferred, fileSize);
  }, 200);

  try {
    const r = await fetch('/api/download?path=' + encodeURIComponent(path));
    clearInterval(progressTimer);
    cancelled = true;

    if (!r.ok) { const t = await r.text(); throw new Error(t); }

    document.getElementById('transferStatus').textContent = 'Saving...';
    updateTransfer(95, fileSize > 0 ? formatSize(fileSize) : '');

    const blob = await r.blob();

    updateTransfer(100, formatSize(blob.size));
    document.getElementById('transferStatus').textContent = 'Complete!';
    updateTransferStats(blob.size, blob.size);
    document.getElementById('transferEta').textContent = '';

    // Trigger browser save
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = name;
    document.body.appendChild(a); a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

    setTimeout(hideTransfer, 1000);
  } catch(e) {
    clearInterval(progressTimer);
    cancelled = true;
    hideTransfer();
    toast('Download failed: ' + e.message, true);
  }
}

async function doRm(path) {
  if (!confirm('Delete ' + path.split('/').pop() + '?')) return;
  try {
    await api('/api/rm?path=' + encodeURIComponent(path), {method:'POST'});
    toast('Deleted');
    refresh();
  } catch(e) { toast(e.message, true); }
}

function showUpload() {
  document.getElementById('fileInput').click();
}

function handleDrop(e) {
  e.preventDefault();
  document.getElementById('uploadArea').classList.remove('drag');
  uploadFiles(e.dataTransfer.files);
}

async function uploadFiles(files) {
  for (let i = 0; i < files.length; i++) {
    const file = files[i];
    const label = files.length > 1 ? '(' + (i+1) + '/' + files.length + ') ' + file.name : file.name;
    showTransfer('\u2B06', label, 'Sending to server...');

    try {
      // Phase 1: HTTP upload (browser -> web server, fast on LAN)
      const httpDone = new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open('POST', '/api/upload?path=' + encodeURIComponent(currentPath));

        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            // HTTP upload is ~10% of total (fast local network)
            const pct = (e.loaded / e.total) * 10;
            updateTransfer(pct, 'Sending: ' + formatSize(e.loaded) + ' / ' + formatSize(e.total));
          }
        };

        xhr.onload = () => {
          if (xhr.status >= 200 && xhr.status < 300) resolve();
          else reject(new Error(xhr.responseText));
        };
        xhr.onerror = () => reject(new Error('Network error'));

        const fd = new FormData();
        fd.append('file', file);
        xhr.send(fd);
      });

      // Phase 2 starts when phase 1 completes
      // The XHR.onload fires AFTER the server finishes BT transfer
      // So we estimate BT progress during the wait

      const btSpeed = 170 * 1024;
      const estimatedMs = (file.size / btSpeed) * 1000;
      let startTime = Date.now();
      let btDone = false;

      // Start BT progress estimation once HTTP upload starts
      // (server receives file, then transfers via BT)
      const progressTimer = setInterval(() => {
        if (btDone) return;
        const elapsed = Date.now() - startTime;
        const btPct = Math.min(85, (elapsed / estimatedMs) * 85);
        const pct = 10 + btPct;
        const estBytes = Math.min(file.size, Math.floor(file.size * pct / 100));
        updateTransfer(pct, formatSize(estBytes) + ' / ' + formatSize(file.size));
        document.getElementById('transferStatus').textContent = 'Transferring via Bluetooth...';
        updateTransferStats(estBytes, file.size);
      }, 200);

      await httpDone;
      clearInterval(progressTimer);
      btDone = true;

      updateTransfer(100, formatSize(file.size));
      document.getElementById('transferStatus').textContent = 'Complete!';
      updateTransferStats(file.size, file.size);
      document.getElementById('transferEta').textContent = '';
      await new Promise(r => setTimeout(r, 500));
      toast('Uploaded: ' + file.name);
    } catch(e) {
      hideTransfer();
      toast('Upload failed: ' + e.message, true);
    }
  }

  hideTransfer();
  document.getElementById('fileInput').value = '';
  refresh();
}

function showMkdir() {
  document.getElementById('mkdirModal').classList.add('show');
  const input = document.getElementById('mkdirName');
  input.value = '';
  setTimeout(() => input.focus(), 100);
}

function hideModal(id) { document.getElementById(id).classList.remove('show'); }

async function doMkdir() {
  const name = document.getElementById('mkdirName').value.trim();
  if (!name) return;
  const path = currentPath === '/' ? '/' + name : currentPath + '/' + name;
  try {
    await api('/api/mkdir?path=' + encodeURIComponent(path), {method:'POST'});
    toast('Folder created');
    hideModal('mkdirModal');
    refresh();
  } catch(e) { toast(e.message, true); }
}

function toggleTerminal() {
  const t = document.getElementById('terminal');
  t.classList.toggle('show');
  if (t.classList.contains('show')) setTimeout(() => document.getElementById('termInput').focus(), 100);
}

async function runCmd() {
  const input = document.getElementById('termInput');
  const cmd = input.value.trim();
  if (!cmd) return;
  input.value = '';

  const out = document.getElementById('termOutput');
  out.innerHTML += '<div class="cmd-line">$ ' + cmd.replace(/</g,'&lt;') + '</div>';

  try {
    const r = await api('/api/exec?cmd=' + encodeURIComponent(cmd), {method:'POST'});
    const data = await r.json();
    const stdout = atob(data.stdout);
    const stderr = atob(data.stderr);
    if (stdout) out.innerHTML += '<div>' + stdout.replace(/</g,'&lt;') + '</div>';
    if (stderr) out.innerHTML += '<div class="stderr">' + stderr.replace(/</g,'&lt;') + '</div>';
    if (data.exit_code !== 0) out.innerHTML += '<div class="stderr">exit ' + data.exit_code + '</div>';
  } catch(e) {
    out.innerHTML += '<div class="stderr">' + e.message + '</div>';
  }
  out.scrollTop = out.scrollHeight;
}

// --- Connection status ---
let btConnected = true;
let btServer = '';

async function checkStatus() {
  try {
    const r = await fetch('/api/status');
    if (!r.ok) return;
    const d = await r.json();
    btConnected = d.connected;
    btServer = d.server || '';
    updateConnUI();
  } catch(e) {}
}

function updateConnUI() {
  const dot = document.getElementById('connDot');
  const text = document.getElementById('connText');
  const btn = document.getElementById('connBtn');
  if (btConnected) {
    dot.className = 'conn-dot on';
    text.innerHTML = 'Connected' + (btServer ? ' &mdash; <span class="conn-addr">' + btServer + '</span>' : '');
    btn.textContent = 'Disconnect';
    btn.className = 'pill';
  } else {
    dot.className = 'conn-dot off';
    text.textContent = 'Disconnected';
    btn.textContent = 'Connect';
    btn.className = 'pill blue';
  }
}

async function toggleConnection() {
  const btn = document.getElementById('connBtn');
  btn.disabled = true;
  btn.textContent = '...';
  try {
    if (btConnected) {
      await fetch('/api/disconnect', {method:'POST'});
      toast('Disconnected');
    } else {
      const r = await fetch('/api/connect', {method:'POST'});
      if (!r.ok) { const t = await r.text(); throw new Error(t); }
      toast('Connected');
    }
  } catch(e) {
    toast(e.message, true);
  }
  btn.disabled = false;
  await checkStatus();
  if (btConnected) refresh();
}

checkStatus();
setInterval(checkStatus, 5000);

loadDir('/');
</script>
</body>
</html>`
