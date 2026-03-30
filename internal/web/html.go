package web

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>BFT - Bluetooth File Transfer</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0d1117; color: #c9d1d9; }
.header { background: #161b22; border-bottom: 1px solid #30363d; padding: 12px 20px; display: flex; align-items: center; justify-content: space-between; }
.header h1 { font-size: 18px; color: #58a6ff; }
.header .status { font-size: 13px; color: #8b949e; }
.toolbar { background: #161b22; border-bottom: 1px solid #30363d; padding: 8px 20px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.path-bar { flex: 1; min-width: 200px; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; padding: 6px 12px; color: #c9d1d9; font-family: monospace; font-size: 14px; }
.btn { padding: 6px 14px; border: 1px solid #30363d; border-radius: 6px; background: #21262d; color: #c9d1d9; cursor: pointer; font-size: 13px; white-space: nowrap; }
.btn:hover { background: #30363d; }
.btn-primary { background: #238636; border-color: #2ea043; color: #fff; }
.btn-primary:hover { background: #2ea043; }
.btn-danger { background: #da3633; border-color: #f85149; color: #fff; }
.btn-danger:hover { background: #f85149; }
.container { max-width: 1200px; margin: 0 auto; padding: 20px; }
table { width: 100%; border-collapse: collapse; background: #161b22; border: 1px solid #30363d; border-radius: 6px; overflow: hidden; }
th { text-align: left; padding: 10px 16px; background: #21262d; color: #8b949e; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px; }
td { padding: 8px 16px; border-top: 1px solid #21262d; font-size: 14px; }
tr:hover td { background: #1c2128; }
.icon { margin-right: 8px; }
.dir-icon { color: #54aeff; }
.file-icon { color: #8b949e; }
a.name { color: #58a6ff; text-decoration: none; cursor: pointer; }
a.name:hover { text-decoration: underline; }
.size { color: #8b949e; text-align: right; }
.time { color: #8b949e; }
.actions { text-align: right; }
.actions .btn { padding: 3px 10px; font-size: 12px; margin-left: 4px; }
.upload-area { border: 2px dashed #30363d; border-radius: 8px; padding: 30px; text-align: center; margin: 16px 0; color: #8b949e; cursor: pointer; transition: all 0.2s; }
.upload-area:hover, .upload-area.drag { border-color: #58a6ff; color: #58a6ff; background: rgba(88,166,255,0.05); }
.upload-area input { display: none; }
.modal-overlay { display: none; position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.6); z-index: 100; justify-content: center; align-items: center; }
.modal-overlay.show { display: flex; }
.modal { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 24px; min-width: 400px; max-width: 90%; }
.modal h3 { margin-bottom: 16px; color: #c9d1d9; }
.modal input[type=text] { width: 100%; padding: 8px 12px; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; font-size: 14px; margin-bottom: 12px; }
.modal .btn-row { display: flex; gap: 8px; justify-content: flex-end; }
.terminal { background: #0d1117; border: 1px solid #30363d; border-radius: 6px; margin: 16px 0; display: none; }
.terminal.show { display: block; }
.terminal-header { background: #21262d; padding: 8px 12px; border-bottom: 1px solid #30363d; display: flex; align-items: center; justify-content: space-between; }
.terminal-header span { color: #8b949e; font-size: 13px; }
.terminal-body { padding: 12px; font-family: monospace; font-size: 13px; white-space: pre-wrap; max-height: 300px; overflow-y: auto; color: #c9d1d9; }
.terminal-input { display: flex; border-top: 1px solid #30363d; }
.terminal-input input { flex: 1; background: #0d1117; border: none; padding: 8px 12px; color: #c9d1d9; font-family: monospace; font-size: 13px; outline: none; }
.terminal-input .btn { border-radius: 0; border: none; border-left: 1px solid #30363d; }
.stderr { color: #f85149; }
.toast { position: fixed; bottom: 20px; right: 20px; background: #238636; color: #fff; padding: 10px 20px; border-radius: 6px; font-size: 14px; z-index: 200; display: none; }
.toast.error { background: #da3633; }
.toast.show { display: block; }
.progress { display: none; height: 3px; background: #238636; position: fixed; top: 0; left: 0; z-index: 300; transition: width 0.3s; }
.progress.show { display: block; }
</style>
</head>
<body>

<div class="progress" id="progress"></div>

<div class="header">
  <h1>BFT - Bluetooth File Transfer</h1>
  <span class="status" id="status">Connected</span>
</div>

<div class="toolbar">
  <button class="btn" onclick="goUp()">&#8593; Up</button>
  <button class="btn" onclick="refresh()">&#8635; Refresh</button>
  <input class="path-bar" id="pathBar" value="/" readonly>
  <button class="btn btn-primary" onclick="showUpload()">&#8593; Upload</button>
  <button class="btn" onclick="showMkdir()">+ Folder</button>
  <button class="btn" onclick="toggleTerminal()">&#9638; Terminal</button>
</div>

<div class="container">
  <table>
    <thead><tr><th>Name</th><th>Size</th><th>Modified</th><th class="actions">Actions</th></tr></thead>
    <tbody id="fileList"></tbody>
  </table>

  <div class="upload-area" id="uploadArea" onclick="document.getElementById('fileInput').click()" ondragover="event.preventDefault();this.classList.add('drag')" ondragleave="this.classList.remove('drag')" ondrop="handleDrop(event)">
    Drop files here or click to upload
    <input type="file" id="fileInput" multiple onchange="uploadFiles(this.files)">
  </div>

  <div class="terminal" id="terminal">
    <div class="terminal-header">
      <span>Remote Terminal</span>
      <button class="btn" onclick="toggleTerminal()">Close</button>
    </div>
    <div class="terminal-body" id="termOutput"></div>
    <div class="terminal-input">
      <input id="termInput" placeholder="Enter command..." onkeydown="if(event.key==='Enter')runCmd()">
      <button class="btn btn-primary" onclick="runCmd()">Run</button>
    </div>
  </div>
</div>

<div class="modal-overlay" id="mkdirModal">
  <div class="modal">
    <h3>Create Folder</h3>
    <input type="text" id="mkdirName" placeholder="Folder name" onkeydown="if(event.key==='Enter')doMkdir()">
    <div class="btn-row">
      <button class="btn" onclick="hideModal('mkdirModal')">Cancel</button>
      <button class="btn btn-primary" onclick="doMkdir()">Create</button>
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
  setTimeout(() => t.classList.remove('show'), 3000);
}

function formatSize(b) {
  if (b >= 1073741824) return (b/1073741824).toFixed(2) + ' GB';
  if (b >= 1048576) return (b/1048576).toFixed(2) + ' MB';
  if (b >= 1024) return (b/1024).toFixed(1) + ' KB';
  return b + ' B';
}

async function loadDir(path) {
  try {
    const r = await api('/api/ls?path=' + encodeURIComponent(path));
    const data = await r.json();
    currentPath = data.path;
    document.getElementById('pathBar').value = currentPath;

    const tbody = document.getElementById('fileList');
    tbody.innerHTML = '';
    data.entries.sort((a,b) => {
      if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    data.entries.forEach(e => {
      const tr = document.createElement('tr');
      const isDir = e.type === 'dir';
      const icon = isDir ? '<span class="icon dir-icon">&#128193;</span>' : '<span class="icon file-icon">&#128196;</span>';
      const fullPath = currentPath === '/' ? '/' + e.name : currentPath + '/' + e.name;

      let nameHtml;
      if (isDir) {
        nameHtml = '<a class="name" onclick="loadDir(\'' + fullPath.replace(/'/g,"\\'") + '\')">' + icon + e.name + '</a>';
      } else {
        nameHtml = '<span>' + icon + e.name + '</span>';
      }

      let actions = '<button class="btn btn-danger" onclick="doRm(\'' + fullPath.replace(/'/g,"\\'") + '\')">Delete</button>';
      if (!isDir) {
        actions = '<button class="btn" onclick="doDownload(\'' + fullPath.replace(/'/g,"\\'") + '\')">Download</button>' + actions;
      }

      tr.innerHTML = '<td>' + nameHtml + '</td><td class="size">' + (isDir ? '-' : formatSize(e.size)) + '</td><td class="time">' + e.time + '</td><td class="actions">' + actions + '</td>';
      tbody.appendChild(tr);
    });
  } catch(e) {
    toast('Error: ' + e.message, true);
  }
}

function refresh() { loadDir(currentPath); }

function goUp() {
  if (currentPath === '/') return;
  const parts = currentPath.split('/').filter(Boolean);
  parts.pop();
  loadDir('/' + parts.join('/'));
}

function doDownload(path) {
  window.location.href = '/api/download?path=' + encodeURIComponent(path);
}

async function doRm(path) {
  if (!confirm('Delete ' + path + '?')) return;
  try {
    await api('/api/rm?path=' + encodeURIComponent(path), {method:'POST'});
    toast('Deleted');
    refresh();
  } catch(e) { toast('Error: ' + e.message, true); }
}

function showUpload() { document.getElementById('uploadArea').style.display = 'block'; }

function handleDrop(e) {
  e.preventDefault();
  e.target.classList.remove('drag');
  uploadFiles(e.dataTransfer.files);
}

async function uploadFiles(files) {
  for (const file of files) {
    const fd = new FormData();
    fd.append('file', file);
    try {
      await api('/api/upload?path=' + encodeURIComponent(currentPath), {method:'POST', body: fd});
      toast('Uploaded: ' + file.name);
    } catch(e) { toast('Upload error: ' + e.message, true); }
  }
  refresh();
}

function showMkdir() {
  document.getElementById('mkdirModal').classList.add('show');
  document.getElementById('mkdirName').value = '';
  document.getElementById('mkdirName').focus();
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
  } catch(e) { toast('Error: ' + e.message, true); }
}

function toggleTerminal() {
  const t = document.getElementById('terminal');
  t.classList.toggle('show');
  if (t.classList.contains('show')) document.getElementById('termInput').focus();
}

async function runCmd() {
  const input = document.getElementById('termInput');
  const cmd = input.value.trim();
  if (!cmd) return;
  input.value = '';

  const out = document.getElementById('termOutput');
  out.innerHTML += '<div style="color:#58a6ff">$ ' + cmd.replace(/</g,'&lt;') + '</div>';

  try {
    const r = await api('/api/exec?cmd=' + encodeURIComponent(cmd), {method:'POST'});
    const data = await r.json();
    const stdout = atob(data.stdout);
    const stderr = atob(data.stderr);
    if (stdout) out.innerHTML += '<div>' + stdout.replace(/</g,'&lt;') + '</div>';
    if (stderr) out.innerHTML += '<div class="stderr">' + stderr.replace(/</g,'&lt;') + '</div>';
    if (data.exit_code !== 0) out.innerHTML += '<div class="stderr">exit code: ' + data.exit_code + '</div>';
  } catch(e) {
    out.innerHTML += '<div class="stderr">Error: ' + e.message + '</div>';
  }
  out.scrollTop = out.scrollHeight;
}

// Init
loadDir('/');
</script>
</body>
</html>`
