// Remote sign-in browser: draws the server-side Chrome's screencast on a
// canvas and sends pointer and key events back. Nothing typed is stored here.
(function () {
  const $ = (id) => document.getElementById(id);
  const open = $('remote-open');
  if (!open) return;
  const close = $('remote-close'), canvas = $('remote-canvas'), status = $('remote-status');
  const msgs = $('remote-msgs').dataset;
  const ctx = canvas.getContext('2d');
  let es = null, lastMove = 0, gotFrame = false;

  const send = (ev) => fetch('/strava/remote/input', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(ev),
  }).catch(() => {});

  function pos(e) {
    const r = canvas.getBoundingClientRect();
    return { x: (e.clientX - r.left) * canvas.width / r.width, y: (e.clientY - r.top) * canvas.height / r.height };
  }
  const mod = (e) => (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
  const btn = (e) => (e.button === 2 ? 'right' : e.button === 1 ? 'middle' : 'left');

  function ui(running, text) {
    open.style.display = running ? 'none' : '';
    close.style.display = running ? '' : 'none';
    canvas.style.display = running ? 'block' : 'none';
    status.textContent = text || '';
  }

  function end(text) {
    if (es) { es.close(); es = null; }
    ui(false, text);
    if (text === msgs.done) setTimeout(() => location.reload(), 1200);
  }

  open.addEventListener('click', async () => {
    open.disabled = true;
    status.textContent = msgs.starting;
    const r = await fetch('/strava/remote/start', { method: 'POST' }).catch(() => null);
    open.disabled = false;
    if (!r || !r.ok) {
      const why = r ? (await r.text()).trim().slice(0, 300) : '';
      status.textContent = r && r.status === 409 ? msgs.busy : msgs.error + (why ? ': ' + why : '');
      return;
    }
    gotFrame = false;
    ui(true, msgs.waiting);
    canvas.focus();
    es = new EventSource('/strava/remote/frames');
    es.onmessage = (m) => {
      if (!gotFrame) { gotFrame = true; status.textContent = msgs.hint; }
      const img = new Image();
      img.onload = () => ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
      img.src = 'data:image/jpeg;base64,' + m.data;
    };
    es.addEventListener('done', (m) => end(m.data === 'ok' ? msgs.done : msgs.failed));
    es.onerror = () => { if (es && es.readyState === EventSource.CLOSED) end(msgs.failed); };
  });

  close.addEventListener('click', () => {
    fetch('/strava/remote/stop', { method: 'POST' }).catch(() => {});
    end(msgs.closed);
  });

  canvas.addEventListener('contextmenu', (e) => e.preventDefault());
  canvas.addEventListener('mousemove', (e) => {
    const now = performance.now();
    if (now - lastMove < 40) return;
    lastMove = now;
    send({ type: 'move', ...pos(e), mod: mod(e) });
  });
  canvas.addEventListener('mousedown', (e) => { canvas.focus(); send({ type: 'down', ...pos(e), button: btn(e), mod: mod(e) }); });
  canvas.addEventListener('mouseup', (e) => send({ type: 'up', ...pos(e), button: btn(e), mod: mod(e) }));
  canvas.addEventListener('wheel', (e) => {
    e.preventDefault();
    send({ type: 'wheel', ...pos(e), dx: e.deltaX, dy: e.deltaY });
  }, { passive: false });

  canvas.addEventListener('keydown', (e) => {
    // Let the browser's own paste/copy shortcuts through so 'paste' fires.
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v') return;
    e.preventDefault();
    send({ type: 'keydown', key: e.key, code: e.code, mod: mod(e) });
  });
  canvas.addEventListener('keyup', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v') return;
    e.preventDefault();
    send({ type: 'keyup', key: e.key, code: e.code, mod: mod(e) });
  });
  canvas.addEventListener('paste', (e) => {
    e.preventDefault();
    const t = e.clipboardData && e.clipboardData.getData('text');
    if (t) send({ type: 'text', text: t });
  });
})();
