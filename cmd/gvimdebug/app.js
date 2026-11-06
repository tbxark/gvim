// The gvimdebug page. It draws the screen that /events pushes, and sends the input it is
// given to gvim. Automation uses the data-testid attributes, the data-key attributes of the
// quick keys, and window.gvimdebug. README.md lists them.
(function () {
  'use strict';

  // ---- elements --------------------------------------------------------------

  const $ = name => document.querySelector(`[data-testid="${name}"]`);
  const screen = $('screen');
  const screenCard = $('screen-card');
  const measure = $('measure');
  const statusChip = $('status');
  const connectionChip = $('connection');
  const message = $('message');
  const sizeLabel = $('size');
  const cursorLabel = $('cursor');
  const bottombar = document.querySelector('.bottombar');
  const colsInput = $('cols-input');
  const rowsInput = $('rows-input');
  const fitToggle = $('fit-toggle');
  const fontLabel = $('font-size');

  // The terminal size of the last state. It bounds the mouse positions.
  let geometry = { cols: 0, rows: 0 };

  // The size limits the server accepts for /resize.
  const MIN_COLS = 10, MAX_COLS = 400, MIN_ROWS = 4, MAX_ROWS = 200;

  // ---- input: POST requests, one after another -------------------------------

  let queue = Promise.resolve();

  // post sends one request after the previous one, so input reaches gvim in order. The
  // promise rejects when the request fails, and the queue goes on regardless.
  function post(path, body, type = 'text/plain') {
    const done = queue.then(() => request(path, body, type));
    queue = done.catch(() => {});
    return done;
  }

  async function request(path, body, type) {
    try {
      const res = await fetch(path, { method: 'POST', body, headers: { 'Content-Type': type } });
      if (!res.ok) {
        throw new Error((await res.text()).trim() || res.statusText);
      }
      showMessage('');
    } catch (err) {
      showMessage(err.message);
      throw err;
    }
  }

  // send is post for the controls. A failure only shows in the message line.
  function send(path, body, type) {
    post(path, body, type).catch(() => {});
  }

  function showMessage(text) {
    message.textContent = text;
  }

  // ---- screen: drawn from the pushed state -----------------------------------

  let frames = 0;   // states drawn so far
  let waiters = []; // waitForFrame calls that are not satisfied yet

  function draw(state) {
    screen.innerHTML = state.frame;
    statusChip.textContent = state.status;
    statusChip.dataset.state = statusKind(state.status);
    geometry = { cols: state.cols, rows: state.rows };
    sizeLabel.textContent = `${state.cols}×${state.rows}`;
    cursorLabel.textContent = state.cursor.visible
      ? `cursor ${state.cursor.row}, ${state.cursor.col}`
      : 'cursor hidden';
    // The size fields follow gvim, except while the user is typing in one of them.
    for (const [input, value] of [[colsInput, state.cols], [rowsInput, state.rows]]) {
      if (document.activeElement !== input) input.value = value;
    }
    frames++;
    const ready = waiters.filter(w => frames > w.after);
    waiters = waiters.filter(w => frames <= w.after);
    for (const w of ready) w.resolve(frames);
  }

  // statusKind maps a status line to the word that sets the chip's colour.
  function statusKind(status) {
    if (status.startsWith('exited')) return 'exited';
    if (status.includes('failed')) return 'failed';
    return status;
  }

  // waitForFrame resolves with the frame count once more than `after` states have been
  // drawn. It rejects when that takes longer than timeoutMs.
  function waitForFrame(after, timeoutMs = 5000) {
    if (frames > after) return Promise.resolve(frames);
    return new Promise((resolve, reject) => {
      const waiter = { after, resolve };
      waiters.push(waiter);
      setTimeout(() => {
        if (!waiters.includes(waiter)) return;
        waiters = waiters.filter(w => w !== waiter);
        reject(new Error(`no new frame within ${timeoutMs} ms`));
      }, timeoutMs);
    });
  }

  // ---- events: the server pushes the state on every change -------------------

  // EventSource reconnects by itself. The server sends the state again when the new
  // stream opens, so the page catches up without a separate request.
  const source = new EventSource('/events');
  source.onopen = () => setConnection('live');
  source.onerror = () => setConnection('reconnecting');
  source.onmessage = event => draw(JSON.parse(event.data));

  function setConnection(state) {
    connectionChip.dataset.state = state;
    connectionChip.textContent = state;
  }

  // ---- keyboard: keys typed outside a field go to gvim -----------------------

  const keyNames = {
    ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right',
    Enter: 'enter', Escape: 'esc', Tab: 'tab', Backspace: 'backspace', Delete: 'delete',
    ' ': 'space', Home: 'home', End: 'end', PageUp: 'pgup', PageDown: 'pgdown',
  };

  // keySpec turns a key event into a key spec, or null for a key the page keeps.
  function keySpec(e) {
    if (e.metaKey || e.isComposing) return null;
    let key = keyNames[e.key];
    if (!key && e.key.length === 1) key = e.key;
    if (!key && /^F([1-9]|1[0-2])$/.test(e.key)) key = e.key.toLowerCase();
    if (!key) return null;
    const mods = [];
    if (e.ctrlKey) mods.push('ctrl');
    if (e.altKey) mods.push('alt');
    if (e.shiftKey && key.length > 1) mods.push('shift');
    return [...mods, key].join('+');
  }

  document.addEventListener('keydown', e => {
    if (e.target.closest('input, textarea, button')) return;
    const key = keySpec(e);
    if (key === null) return;
    e.preventDefault();
    send('/key', key);
  });

  // A button keeps the focus after a click. Blur it, so the next key goes to gvim.
  document.addEventListener('click', e => {
    const button = e.target.closest('button');
    if (button) button.blur();
  });

  // ---- size and font: how big the screen is, kept in this browser ------------

  const SETTINGS_KEY = 'gvimdebug.settings';
  const FONT_MIN = 8, FONT_MAX = 28;
  // The layout's bottom padding, which the fit leaves room for.
  const LAYOUT_PADDING = 16;

  const settings = { fit: false, fontPx: 14, ...loadSettings() };

  function loadSettings() {
    try {
      return JSON.parse(localStorage.getItem(SETTINGS_KEY)) || {};
    } catch {
      return {};
    }
  }

  function saveSettings() {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
  }

  // requestSize asks the server to resize gvim's terminal.
  function requestSize(cols, rows) {
    send('/resize', JSON.stringify({ cols, rows }), 'application/json');
  }

  $('resize-apply').addEventListener('click', () => {
    requestSize(Number(colsInput.value), Number(rowsInput.value));
  });

  // setFont sets the screen's font size, in CSS pixels. A fitted screen fits again, because
  // the size of a cell changed.
  function setFont(px) {
    settings.fontPx = clamp(px, FONT_MIN, FONT_MAX);
    document.documentElement.style.setProperty('--term-size', `${settings.fontPx}px`);
    fontLabel.textContent = `${settings.fontPx}px`;
    saveSettings();
    if (settings.fit) scheduleFit(0);
  }

  $('font-smaller').addEventListener('click', () => setFont(settings.fontPx - 1));
  $('font-larger').addEventListener('click', () => setFont(settings.fontPx + 1));

  fitToggle.addEventListener('change', () => {
    settings.fit = fitToggle.checked;
    saveSettings();
    scheduleFit(0);
  });

  // The fit runs after the window settles, so a drag does not send a size per pixel.
  let fitTimer = 0;
  function scheduleFit(delay = 150) {
    clearTimeout(fitTimer);
    fitTimer = setTimeout(fitToWindow, delay);
  }

  // fitToWindow sizes gvim to the room the screen card has. The room depends on the window
  // and the card's position, not on the screen's content, so it does not change when gvim
  // draws.
  function fitToWindow() {
    if (!settings.fit) return;
    const cell = measure.getBoundingClientRect();
    const cellW = cell.width / 10;
    const cellH = cell.height;
    const style = getComputedStyle(screenCard);
    const padX = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
    const padY = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
    const borderY = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
    const top = screenCard.getBoundingClientRect().top;
    const availW = screenCard.clientWidth - padX;
    const availH = window.innerHeight - top - bottombar.offsetHeight - LAYOUT_PADDING - padY - borderY;
    const cols = clamp(Math.floor(availW / cellW), MIN_COLS, MAX_COLS);
    const rows = clamp(Math.floor(availH / cellH), MIN_ROWS, MAX_ROWS);
    if (cols !== geometry.cols || rows !== geometry.rows) requestSize(cols, rows);
  }

  window.addEventListener('resize', () => scheduleFit());

  // ---- controls --------------------------------------------------------------

  const quickKeys = [
    'esc', 'enter', 'tab', 'shift+tab', 'backspace', 'delete',
    'up', 'down', 'left', 'right', 'pgup', 'pgdown', 'home', 'end',
    'f1', 'f5', 'f10',
    'ctrl+b', 'ctrl+e', 'ctrl+f', 'ctrl+p', 'ctrl+s', 'ctrl+t', 'ctrl+w', 'alt+f',
  ];
  const quickKeysBox = $('quick-keys');
  for (const key of quickKeys) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = key;
    button.dataset.key = key;
    button.addEventListener('click', () => send('/key', key));
    quickKeysBox.append(button);
  }

  const keyInput = $('key-input');
  $('key-send').addEventListener('click', () => {
    if (keyInput.value) send('/key', keyInput.value);
  });
  keyInput.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      e.preventDefault();
      $('key-send').click();
    }
  });

  const textInput = $('text-input');
  $('text-send').addEventListener('click', () => {
    if (textInput.value) send('/text', textInput.value);
  });

  $('restart').addEventListener('click', () => send('/restart', ''));

  // ---- mouse: the cell under the pointer gets the event ----------------------

  // cellAt returns the zero-based column and row under the pointer, kept on the screen.
  function cellAt(e) {
    const cell = measure.getBoundingClientRect();
    const box = screen.getBoundingClientRect();
    const x = Math.floor((e.clientX - box.left) / (cell.width / 10));
    const y = Math.floor((e.clientY - box.top) / cell.height);
    return {
      x: clamp(x, 0, geometry.cols - 1),
      y: clamp(y, 0, geometry.rows - 1),
    };
  }

  function clamp(v, lo, hi) {
    return Math.min(Math.max(v, lo), Math.max(hi, lo));
  }

  function mouse(action, button, e) {
    send('/mouse', JSON.stringify({ action, button, ...cellAt(e) }), 'application/json');
  }

  screen.addEventListener('click', e => mouse('click', 'left', e));
  screen.addEventListener('contextmenu', e => {
    e.preventDefault();
    mouse('click', 'right', e);
  });
  screen.addEventListener('wheel', e => {
    e.preventDefault();
    if (e.deltaY !== 0) mouse(e.deltaY < 0 ? 'wheel-up' : 'wheel-down', '', e);
  }, { passive: false });

  // ---- start -----------------------------------------------------------------

  // loadScreenFont loads the font file the server was started with, if there is one. Browsers
  // match installed fonts by names in their own ways, and Safari did not find this one by
  // name. The file is the same in every browser. Its cells are measured again once it loads.
  async function loadScreenFont() {
    const res = await fetch('/font');
    if (!res.ok) return;
    const face = new FontFace('gvim screen', await res.arrayBuffer());
    await face.load();
    document.fonts.add(face);
    scheduleFit(0);
  }
  loadScreenFont().catch(err => showMessage(`font: ${err.message}`));

  fitToggle.checked = settings.fit;
  setFont(settings.fontPx);
  // The cell size is known once the page's fonts load. Fit again then.
  if (document.fonts) document.fonts.ready.then(() => scheduleFit(0));

  // ---- automation API --------------------------------------------------------

  // Each method returns a promise that resolves once the request is accepted, and rejects
  // with the server's message when it is not. waitForFrame waits for the next pushed state.
  window.gvimdebug = {
    key: spec => post('/key', spec),
    text: text => post('/text', text),
    mouse: event => post('/mouse', JSON.stringify(event), 'application/json'),
    resize: (cols, rows) => post('/resize', JSON.stringify({ cols, rows }), 'application/json'),
    restart: () => post('/restart', ''),
    waitForFrame,
    screenText: () => screen.innerText,
    get frames() { return frames; },
  };
})();
