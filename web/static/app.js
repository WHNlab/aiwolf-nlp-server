// AI人狼バトル！ SPA — ルーム作成・待機・観戦・自分のAI視点
const $ = (sel, el = document) => el.querySelector(sel);
const app = $('#app');
const toastEl = $('#toast');
const viewBadge = $('#view-badge');
const connBadge = $('#conn-badge');
const headerRoom = $('#header-room-name');

let state = { room: null, events: [], cursor: 0 };
let feed = null;

function toast(msg, ms = 2200) {
  toastEl.textContent = msg;
  toastEl.hidden = false;
  clearTimeout(toast._t);
  toast._t = setTimeout(() => (toastEl.hidden = true), ms);
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    ...opts,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function setupPrompt() {
  return [
    'AI人狼バトル！に、私のAIとして参加する準備をしてください。',
    `参加キット: ${new URL('/downloads/aiwolf-player.zip', location.origin).href}`,
    `操作手順: ${new URL('/agent/SKILL.md', location.origin).href}`,
    '',
    '参加キットを取得・展開し、SKILL.mdを読んでください。接続用プログラムは作り直さず、付属CLIを使ってください。MCPや追加のLLM APIキーは不要です。',
    'まだルームへ接続しないでください。私がWebでAIの席を確保した後、自分専用の招待案内をこの会話で渡します。受け取ったらSKILLの手順で接続し、発言・投票などを判断してください。',
    'ゲーム開始後、info.key_phraseを受け取ったら、この会話で私に個別に伝えてください。招待トークンやキーフレーズはゲーム内の発言に含めないでください。',
    'Python 3.9以上、コマンド実行、外部WebSocket通信、コマンド間で存続するプロセスが必要です。使えない場合は接続を試みず、その制約を教えてください。',
  ].join('\n');
}

async function copyTextarea(textarea, successMessage) {
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable');
    await navigator.clipboard.writeText(textarea.value);
    toast(successMessage);
  } catch (_) {
    textarea.focus();
    textarea.select();
    toast('コピーできませんでした。選択した文を手動でコピーしてください', 4000);
  }
}

// ---- ルーティング ----
function route() {
  stopUpdates();
  const h = location.hash || '#/';
  const m = h.match(/^#\/rooms\/([A-Za-z0-9]+)/);
  if (m) return renderRoom(m[1]);
  return renderHome();
}

// ---- Home ----
async function renderHome() {
  state.room = null;
  headerRoom.textContent = '';
  viewBadge.hidden = true;
  connBadge.hidden = true;
  document.body.dataset.page = 'home';
  let presets = [];
  try {
    const d = await api('/api/v1/room-presets');
    presets = d.presets || [];
  } catch (e) { presets = []; }
  const cards = presets.map(p => `
    <label class="radio-card">
      <input type="radio" name="agent_count" value="${p.agent_count}" ${p.agent_count === 5 ? 'checked' : ''}>
      <span class="card-inner">${p.agent_count}人<small>${p.mode === 'freeform' ? 'グループチャット' : 'ターン制'}</small></span>
    </label>`).join('');
  app.innerHTML = `
    <section class="hero">
      <h1 class="hero-logo"><img src="/static/images/ai-jinro-battle-logo.png" alt="AI人狼バトル！" width="560" height="224" fetchpriority="high"></h1>
      <p class="hero-lead">あなたのLLMを出場させよう！</p>
      <a class="btn btn--primary btn--large" href="#ai-setup">AIのセットアップ</a>
    </section>
    <div class="home-grid">
      <div class="home-actions">
      <details class="paper home-disclosure" id="new-room">
        <summary>ルーム作成</summary>
        <div class="home-disclosure-content">
        <form id="create-form">
          <label class="field"><span class="field-label">あなたの名前</span>
            <input type="text" name="user_name" maxlength="24" required placeholder="例: ゆうき">
          </label>
          <label class="field"><span class="field-label">ルーム名</span>
            <input type="text" name="room_name" maxlength="48" placeholder="例: 金曜の5人村">
          </label>
          <div class="field"><span class="field-label">参加AI数</span>
            <div class="radio-cards">${cards || '<p class="field-hint">プリセットを読み込めませんでした</p>'}</div>
          </div>
          <div class="field"><span class="field-label">参加方法</span>
            <div class="radio-cards">
              <label class="radio-card"><input type="radio" name="mode" value="participate" checked>
                <span class="card-inner">AIも参加<small>自分のAIを1席確保</small></span></label>
              <label class="radio-card"><input type="radio" name="mode" value="spectate">
                <span class="card-inner">観戦のみ<small>友達のAIだけで対戦</small></span></label>
            </div>
          </div>
          <div id="create-error" class="field-error" hidden></div>
          <button class="btn btn--primary btn--large" type="submit" style="width:100%">ルームを作成</button>
          <p class="field-hint">AIが全員そろったら、あなたがゲームを開始できます。</p>
        </form>
        </div>
      </details>
      <details class="paper home-disclosure" id="open-room">
        <summary>ルームを開く</summary>
        <div class="home-disclosure-content">
          <form id="open-form" class="open-form">
            <input type="text" name="room_id" placeholder="RoomID" aria-label="RoomID">
            <button class="btn" type="submit">開く</button>
          </form>
        </div>
      </details>
      </div>
    <section class="page" id="ai-setup" aria-label="AIのセットアップ">
      <div class="paper panel setup-panel">
        <span class="chip chip-day">AIの準備</span>
        <p>ルームに参加する前に、出場させたいAI Agent（Codex、Claude Codeなど）に伝えて、セットアップしてください。</p>
        <textarea id="setup-prompt" class="setup-prompt" aria-label="AI Agentに渡すセットアップ文" rows="12" readonly spellcheck="false"></textarea>
        <div class="setup-actions">
          <button class="btn btn--primary" id="btn-copy-setup" type="button">文をコピー</button>
        </div>
      </div>
    </section>
    </div>`;
  $('#setup-prompt').value = setupPrompt();
  $('#btn-copy-setup').onclick = () => copyTextarea($('#setup-prompt'), 'AIへの説明をコピーしました');
  $('#create-form').onsubmit = async (e) => {
    e.preventDefault();
    const f = e.target;
    const err = $('#create-error');
    err.hidden = true;
    const btn = f.querySelector('button[type=submit]');
    btn.disabled = true; btn.textContent = '作成中…';
    try {
      const data = await api('/api/v1/rooms', {
        method: 'POST',
        body: JSON.stringify({
          room_name: f.room_name.value,
          user_name: f.user_name.value,
          agent_count: Number(f.agent_count.value || 5),
          mode: f.mode.value,
        }),
      });
      location.hash = '#/rooms/' + data.room_id;
    } catch (ex) {
      err.textContent = ex.message; err.hidden = false;
      btn.disabled = false; btn.textContent = 'ルームを作成';
    }
  };
  $('#open-form').onsubmit = (e) => {
    e.preventDefault();
    const id = e.target.room_id.value.trim();
    if (id) location.hash = '#/rooms/' + id;
  };
}

// ---- Room ----
async function renderRoom(roomId) {
  stopUpdates();
  document.body.dataset.page = 'room';
  state.events = [];
  state.cursor = 0;
  let data;
  try { data = await api('/api/v1/rooms/' + roomId); }
  catch (e) {
    app.innerHTML = `<div class="page"><div class="paper panel empty">${escapeHtml(e.message)}</div></div>`;
    return;
  }
  state.room = data;
  headerRoom.textContent = data.name;
  updateViewBadge(data.viewer);

  if (!data.viewer.joined && data.status === 'waiting') return renderJoin(data);
  try { await refreshHistory(roomId, 0); } catch (_) { /* 通知の再接続時に再取得する */ }
  renderRoomView(data);
  scrollToLatest();
  subscribeEvents(roomId);
}

function updateViewBadge(v) {
  viewBadge.hidden = false;
  const map = { public: '公開視点', agent: '自分のAI', omniscient: '神視点' };
  viewBadge.textContent = map[v.view_mode] || v.view_mode;
}

function renderJoin(data) {
  app.innerHTML = `
    <div class="page"><div class="paper panel" style="max-width:560px;margin:0 auto">
      <span class="sticky">${escapeHtml(data.name)}</span>
      <h2 style="margin-top:14px">このルームに入室</h2>
      <p class="field-hint">参加AI数: ${data.agent_count}人 / 接続済み ${data.connected}</p>
      <form id="join-form">
        <label class="field"><span class="field-label">あなたの名前</span>
          <input type="text" name="name" maxlength="24" required placeholder="例: ゆうき">
        </label>
        <div class="field"><span class="field-label">参加方法</span>
          <div class="radio-cards">
            <label class="radio-card"><input type="radio" name="mode" value="participate" checked>
              <span class="card-inner">AIも参加</span></label>
            <label class="radio-card"><input type="radio" name="mode" value="spectate">
              <span class="card-inner">観戦のみ</span></label>
          </div>
        </div>
        <div id="join-error" class="field-error" hidden></div>
        <button class="btn btn--primary btn--large" type="submit" style="width:100%">入室する</button>
      </form>
    </div></div>`;
  $('#join-form').onsubmit = async (e) => {
    e.preventDefault();
    const f = e.target;
    const err = $('#join-error');
    err.hidden = true;
    try {
      const out = await api(`/api/v1/rooms/${data.room_id}/join`, {
        method: 'POST',
        body: JSON.stringify({ name: f.name.value, mode: f.mode.value }),
      });
      state.room = out;
      try { await refreshHistory(out.room_id, 0); } catch (_) { /* 通知の再接続時に再取得する */ }
      renderRoomView(out);
      scrollToLatest();
      subscribeEvents(out.room_id);
    } catch (ex) { err.textContent = ex.message; err.hidden = false; }
  };
}

async function refreshHistory(roomId, cursor, current = null) {
  const d = await api(`/api/v1/rooms/${roomId}/history?cursor=${cursor}`);
  if (state.room?.room_id !== roomId || (current && !current.active)) return 0;
  if (cursor === 0) state.events = d.events || [];
  else state.events.push(...(d.events || []));
  state.cursor = d.cursor;
  return (d.events || []).length;
}

function renderRoomView(data, hasNewEvents = false) {
  const previousTimeline = $('#timeline');
  const followLatest = !previousTimeline || isLatestVisible();
  const previousScroll = window.scrollY;
  const mobileView = $('#room-grid')?.className || 'room-grid';
  const inviteOpen = $('#invite-detail') && !$('#invite-detail').hidden;
  const inviteText = $('#invite-text')?.value || '';
  const consultText = $('#consult-form textarea')?.value || '';
  const claimText = $('#claim-form input')?.value || '';
  const focusedField = document.activeElement === $('#consult-form textarea') ? 'consult'
    : document.activeElement === $('#claim-form input') ? 'claim' : '';
  const status = data.status;
  const v = data.viewer;
  const leftCol = seatListHtml(data);
  const rightCol = rightPanelHtml(data);
  let center = '';
  if (status === 'waiting' || status === 'starting') center = lobbyHtml(data);
  else center = gameHtml(data);
  app.innerHTML = `
    <div class="page">
      <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:16px">
        <span class="sticky">${escapeHtml(data.name)}</span>
        <span class="chip">RoomID <code>${data.room_id}</code></span>
        <span class="chip chip-live">${statusLabel(status)}</span>
        ${status === 'running' ? `<span class="chip chip-day">${data.day}日目</span>` : ''}
      </div>
      <div class="room-grid" id="room-grid">
        <aside class="col-left">${leftCol}</aside>
        <div class="col-center">${center}</div>
        <aside class="col-right">${rightCol}</aside>
      </div>
    </div>
    <nav class="mobile-tabs">
      <button class="btn btn--small" data-tab="participants">参加者</button>
      <button class="btn btn--small" data-tab="talk">会話</button>
      <button class="btn btn--small" data-tab="ai">自分のAI</button>
    </nav>`;
  if ($('#room-grid')) $('#room-grid').className = mobileView;
  if (inviteOpen && $('#invite-detail')) {
    $('#invite-detail').hidden = false;
    $('#invite-text').value = inviteText;
  }
  if ($('#consult-form textarea')) $('#consult-form textarea').value = consultText;
  if ($('#claim-form input')) $('#claim-form input').value = claimText;
  bindRoomEvents(data);
  renderTimeline(followLatest, hasNewEvents);
  if (previousTimeline && !followLatest) window.scrollTo(0, previousScroll);
  if (focusedField) $(focusedField === 'consult' ? '#consult-form textarea' : '#claim-form input')?.focus({ preventScroll: true });
}

function statusLabel(s) {
  return { waiting: '待機中', starting: '開始中', running: '進行中', finished: '終了', aborted: '中断', closed: '閉室' }[s] || s;
}

function seatListHtml(data) {
  const rows = data.seats.map(s => {
    const cls = ['seat-row'];
    if (!s.user_name && !s.team_name) cls.push('empty');
    if (s.connected) cls.push('connected');
    if (!s.alive) cls.push('dead');
    const name = s.user_name || (s.connected ? s.team_name : '空席');
    const sub = s.is_mine ? 'あなた' : (s.game_name || s.team_name || '');
    return `<li class="${cls.join(' ')}">
      <span class="seat-dot"></span>
      <span class="seat-name">${escapeHtml(name)}</span>
      <span class="seat-sub">${escapeHtml(sub)}${s.claimed ? ' 🔓' : ''}</span>
      <span class="seat-role ${s.is_mine ? 'mine' : ''}">${escapeHtml(s.role)}</span>
    </li>`;
  }).join('');
  return `<div class="paper panel"><h3>参加者 ${data.connected} / ${data.agent_count}</h3>
    <ul class="seat-list">${rows}</ul>
    <details style="margin-top:12px"><summary class="field-hint" style="cursor:pointer;font-weight:700">役職構成</summary>
    <p class="field-hint">${Object.entries(data.roles || {}).filter(([, n]) => n > 0).map(([k, n]) => `${k}×${n}`).join(' / ')}</p></details>
  </div>`;
}

function rightPanelHtml(data) {
  const v = data.viewer;
  const p = data.private_agent;
  let html = '';
  if (v.can_claim) {
    html += `<div class="paper panel key-card">
      <h3>🔑 自分のAIの視点をひらく</h3>
      <p class="field-hint">ゲーム開始後、あなたのAIが個別に伝えたキーフレーズを入力してください。</p>
      <form id="claim-form">
        <label class="field"><input type="password" name="phrase" autocomplete="off" placeholder="キーフレーズ" required></label>
        <div id="claim-error" class="field-error" hidden></div>
        <button class="btn btn--primary" type="submit" style="width:100%">視点をひらく</button>
      </form>
    </div>`;
  }
  if (p) {
    const k = p.knowledge || {};
    let know = `<p class="field-hint">役職: <strong>${escapeHtml(p.role)}</strong> / ${p.alive ? '生存' : '死亡'}</p>`;
    if (k.werewolf_mates && k.werewolf_mates.length) {
      know += `<p class="field-hint">人狼の仲間: ${k.werewolf_mates.map(escapeHtml).join('、')}</p>`;
    }
    if (k.divine_results && k.divine_results.length) {
      know += '<p class="field-hint">占い結果:</p>' + k.divine_results.map(r =>
        `<div class="paper-inset" style="padding:6px 10px;margin-bottom:4px;font-size:0.85rem">${r.day}日目 ${escapeHtml(r.target || '?')} → ${escapeHtml(r.result)}</div>`).join('');
    }
    if (k.medium_results && k.medium_results.length) {
      know += '<p class="field-hint">霊媒結果:</p>' + k.medium_results.map(r =>
        `<div class="paper-inset" style="padding:6px 10px;margin-bottom:4px;font-size:0.85rem">${r.day}日目 ${escapeHtml(r.target || '?')} → ${escapeHtml(r.result)}</div>`).join('');
    }
    html += `<div class="paper panel"><h3>自分のAI</h3>${know}</div>`;
  }
  if (v.can_consult) {
    html += `<div class="paper panel">
      <h3>自分のAIに相談</h3>
      <p class="field-hint">あなたのAIにだけ届きます。ゲームの発言・投票はAIが行います。</p>
      <div class="consult-log" id="consult-log"></div>
      <form id="consult-form" style="margin-top:10px">
        <textarea name="text" rows="3" maxlength="1000" placeholder="助言を書く…"></textarea>
        <button class="btn btn--small btn--primary" type="submit" style="margin-top:6px">AIに送る</button>
        <p class="field-hint">送信済みの助言は次の通信でAIへ届きます。</p>
      </form>
    </div>`;
  }
  if (data.status === 'waiting' || data.status === 'starting') {
    html += inviteHtml(data);
  }
  return html || '<div class="paper panel"><p class="field-hint">観戦中</p></div>';
}

function inviteHtml(data) {
  return `<div class="paper panel invite-box">
    <h3>招待</h3>
    <button class="btn btn--small" id="btn-share">🔗 観戦URLをコピー</button>
    ${data.viewer.own_seat ? '<button class="btn btn--small" id="btn-invite" style="margin-top:8px">🤖 AIへの案内を表示</button>' : ''}
    <div id="invite-detail" hidden style="margin-top:10px">
      <textarea id="invite-text" rows="8" readonly></textarea>
      <button class="btn btn--small" id="btn-copy-guide" style="margin-top:6px">案内をコピー</button>
      <p class="field-hint">案内をAIに渡すと、接続と参加の手順を確認できます。招待設定は自分のAIだけに渡してください。参加キット初版はターン制専用です。</p>
    </div>
  </div>`;
}

function lobbyHtml(data) {
  const v = data.viewer;
  const need = data.agent_count - data.connected;
  return `<div class="paper panel">
    <h2>待機室</h2>
    <p>AI接続済み <strong>${data.connected} / ${data.agent_count}</strong></p>
    ${v.is_host
      ? `<button class="btn btn--primary btn--large" id="btn-start" ${v.can_start ? '' : 'disabled'} style="width:100%">ゲームを開始</button>
         ${v.can_start ? '' : `<p class="field-hint">あと${need}体のAI接続を待っています</p>`}`
      : '<p class="field-hint">部屋主の開始を待っています。</p>'}
    ${v.is_host ? '<button class="btn btn--danger btn--small" id="btn-close" style="margin-top:10px">ルームを閉じる</button>' : ''}
  </div>`;
}

function gameHtml(data) {
  return `<div class="paper panel">
    <div style="display:flex;gap:8px;margin-bottom:12px;flex-wrap:wrap">
      <span class="chip">公開会話</span>
      ${data.viewer.view_mode === 'omniscient' ? '<span class="chip">全情報</span>' : ''}
    </div>
    <div class="timeline" id="timeline"></div>
    <div id="new-msg-bar" hidden>
      <button class="btn btn--small" id="btn-jump">新しい発言 ↓</button>
    </div>
    <div id="result-area"></div>
  </div>`;
}

function isLatestVisible() {
  const tl = $('#timeline');
  if (!tl || !tl.getClientRects().length) return false;
  const last = tl.lastElementChild;
  if (!last) return true;
  const rect = last.getBoundingClientRect();
  return rect.bottom >= 64 && rect.top <= window.innerHeight && rect.bottom <= window.innerHeight + 100;
}

function scrollToLatest() {
  const tl = $('#timeline');
  if (!tl || !state.events.length) return;
  if (!tl.getClientRects().length) {
    // スマホで別タブを見ているときも、新着ボタンから会話へ移動できる。
    const grid = $('#room-grid');
    if (grid?.classList.contains('mobile-participants') || grid?.classList.contains('mobile-ai')) {
      grid.classList.remove('mobile-participants', 'mobile-ai');
      requestAnimationFrame(scrollToLatest);
    }
    return;
  }
  tl.lastElementChild?.scrollIntoView({ block: 'end', behavior: 'smooth' });
  if ($('#new-msg-bar')) $('#new-msg-bar').hidden = true;
}

function renderTimeline(followLatest = isLatestVisible(), hasNewEvents = false) {
  const tl = $('#timeline');
  if (!tl) return;
  let html = '';
  let lastDay = -1;
  for (const e of state.events) {
    if (e.day !== lastDay) {
      lastDay = e.day;
      html += `<div class="event-day"><span class="sticky ${isNightGuess(e) ? 'night' : ''}">${e.day}日目</span></div>`;
    }
    html += eventHtml(e);
  }
  tl.innerHTML = html || '<div class="empty">ゲームが始まると、ここに会話が記録されます</div>';
  if (followLatest) requestAnimationFrame(scrollToLatest);
  else if (hasNewEvents && $('#new-msg-bar')) $('#new-msg-bar').hidden = false;
}

function isNightGuess(e) { return e.type === 'day' && e.day > 0 && false; }

function eventHtml(e) {
  const who = seatNameOf(e.from_idx);
  const to = seatNameOf(e.to_idx);
  switch (e.type) {
    case 'talk': return `<div class="talk-entry"><div class="avatar">${escapeHtml(initialOf(who))}</div>
      <div class="body"><div class="talk-meta"><span class="talk-name">${escapeHtml(who)}</span></div>
      <div class="talk-text">${escapeHtml(e.text)}</div></div></div>`;
    case 'whisper': return `<div class="talk-entry event-whisper"><div class="avatar">🐺</div>
      <div class="body"><div class="talk-meta"><span class="talk-name">${escapeHtml(who)}</span><span>囁き</span></div>
      <div class="talk-text">${escapeHtml(e.text)}</div></div></div>`;
    case 'vote': return `<div class="event-sys">🗳 ${escapeHtml(who)} → ${escapeHtml(to)}</div>`;
    case 'attack_vote': return `<div class="event-sys event-whisper">🐺 ${escapeHtml(who)} → ${escapeHtml(to)}</div>`;
    case 'execute': return `<div class="event-sys">⚖ ${to ? escapeHtml(to) + 'が' : ''}${escapeHtml(e.text)}</div>`;
    case 'attack': return `<div class="event-sys">🌙 ${to ? escapeHtml(to) + 'が' : ''}${escapeHtml(e.text)}</div>`;
    case 'divine_result': return `<div class="event-sys">🔮 占い: ${escapeHtml(to)} → ${escapeHtml(e.result)}</div>`;
    case 'medium_result': return `<div class="event-sys">🕯 霊媒: ${escapeHtml(to)} → ${escapeHtml(e.result)}</div>`;
    case 'guard': return `<div class="event-sys">🛡 護衛: ${escapeHtml(who)} → ${escapeHtml(to)}</div>`;
    case 'game_start': return `<div class="event-sys">🚩 ${escapeHtml(e.text)}</div>`;
    case 'game_end': return `<div class="event-sys">🏁 ${escapeHtml(e.text)} ${e.team || ''}</div>`;
    case 'owner_advice': return `<div class="consult-msg mine" style="align-self:stretch">${escapeHtml(e.text)}<div class="meta">あなた → AI</div></div>`;
    case 'owner_note': return `<div class="consult-msg ai" style="align-self:stretch">${escapeHtml(e.text)}<div class="meta">AI → あなた</div></div>`;
    default: return '';
  }
}

function seatNameOf(idx) {
  if (idx == null || !state.room) return '';
  const s = state.room.seats.find(x => x.agent_idx === idx);
  return s ? (s.game_name || s.team_name || s.user_name || `Agent${idx}`) : `Agent${idx}`;
}

function initialOf(name) { return (name || '?').slice(0, 2); }

function bindRoomEvents(data) {
  const f1 = $('#claim-form');
  if (f1) f1.onsubmit = async (e) => {
    e.preventDefault();
    const err = $('#claim-error'); err.hidden = true;
    try {
      await api(`/api/v1/rooms/${data.room_id}/claim`, { method: 'POST', body: JSON.stringify({ phrase: f1.phrase.value }) });
      toast('自分のAIの視点になりました');
      f1.phrase.value = '';
      renderRoom(data.room_id);
    } catch (ex) { err.textContent = ex.message; err.hidden = false; }
  };
  const bs = $('#btn-start');
  if (bs) bs.onclick = async () => {
    bs.disabled = true; bs.textContent = '開始中…';
    try { const out = await api(`/api/v1/rooms/${data.room_id}/start`, { method: 'POST', body: '{}' }); state.room = out; renderRoomView(out); }
    catch (ex) { toast(ex.message); bs.disabled = false; bs.textContent = 'ゲームを開始'; }
  };
  const bc = $('#btn-close');
  if (bc) bc.onclick = async () => {
    if (!confirm('ルームを閉じますか？')) return;
    try { await api(`/api/v1/rooms/${data.room_id}/close`, { method: 'POST', body: '{}' }); toast('閉室しました'); location.hash = '#/'; }
    catch (ex) { toast(ex.message); }
  };
  const cf = $('#consult-form');
  if (cf) cf.onsubmit = async (e) => {
    e.preventDefault();
    const t = cf.text.value.trim();
    if (!t) return;
    try {
      await api(`/api/v1/rooms/${data.room_id}/consultations`, { method: 'POST', body: JSON.stringify({ text: t }) });
      cf.text.value = '';
      toast('AIへ送信しました');
    } catch (ex) { toast(ex.message); }
  };
  const bsh = $('#btn-share');
  if (bsh) bsh.onclick = () => {
    navigator.clipboard?.writeText(location.href).then(() => toast('観戦URLをコピーしました'));
  };
  const bi = $('#btn-invite');
  if (bi) bi.onclick = async () => {
    try {
      const d = await api(`/api/v1/rooms/${data.room_id}/invite`);
      $('#invite-detail').hidden = false;
      $('#invite-text').value = (d.mode === 'turn' ? `参加キット: ${new URL(d.kit_path, location.origin).href}\n\n` : '') + d.guide_text;
    } catch (ex) { toast(ex.message); }
  };
  const bcg = $('#btn-copy-guide');
  if (bcg) bcg.onclick = () => {
    copyTextarea($('#invite-text'), '案内をコピーしました');
  };
  document.querySelectorAll('.mobile-tabs .btn').forEach(b => {
    b.onclick = () => {
      const g = $('#room-grid');
      g.className = 'room-grid';
      if (b.dataset.tab === 'participants') g.classList.add('mobile-participants');
      if (b.dataset.tab === 'ai') g.classList.add('mobile-ai');
    };
  });
  const jump = $('#btn-jump');
  if (jump) jump.onclick = scrollToLatest;
}

function stopUpdates() {
  if (!feed) return;
  feed.active = false;
  feed.es.close();
  clearInterval(feed.pollTimer);
  feed = null;
}

function roomViewKey(data) {
  if (!data) return '';
  return JSON.stringify([data.name, data.status, data.connected, data.day, data.seats,
    data.viewer, data.private_agent, data.roles, data.win_side, data.abort_reason]);
}

async function syncRoom(current) {
  if (!current.active || state.room?.room_id !== current.roomId) return;
  if (current.syncing) { current.queued = true; return; }
  current.syncing = true;
  try {
    do {
      current.queued = false;
      const fresh = await api('/api/v1/rooms/' + current.roomId);
      if (!current.active || state.room?.room_id !== current.roomId) return;
      const viewChanged = state.room.viewer.view_mode !== fresh.viewer.view_mode;
      const changed = roomViewKey(state.room) !== roomViewKey(fresh);
      let newEvents = 0;
      if (viewChanged || fresh.cursor > state.cursor) {
        newEvents = await refreshHistory(current.roomId, viewChanged ? 0 : state.cursor, current);
        if (!current.active) return;
      }
      state.room = fresh;
      headerRoom.textContent = fresh.name;
      updateViewBadge(fresh.viewer);
      if (changed) renderRoomView(fresh, newEvents > 0);
      else if (newEvents) renderTimeline(isLatestVisible(), true);
      if (['finished', 'aborted', 'closed'].includes(fresh.status)) {
        stopUpdates();
        connBadge.hidden = true;
        return;
      }
    } while (current.queued);
  } catch (_) {
    // SSEの再接続または定期確認で再試行する。既存の表示は残す。
  } finally {
    current.syncing = false;
  }
}

function subscribeEvents(roomId) {
  stopUpdates();
  const current = { roomId, active: true, syncing: false, queued: false, es: null, pollTimer: null };
  feed = current;
  current.es = new EventSource(`/api/v1/rooms/${roomId}/events`);
  connBadge.hidden = false;
  connBadge.textContent = '接続中';
  connBadge.classList.remove('off');
  current.es.addEventListener('room', () => syncRoom(current));
  current.es.onopen = () => {
    if (!current.active) return;
    connBadge.textContent = '接続中';
    connBadge.classList.remove('off');
    syncRoom(current);
  };
  current.es.onerror = () => {
    if (!current.active) return;
    connBadge.textContent = '再接続中';
    connBadge.classList.add('off');
  };
  // SSEが中継で滞留した場合にも履歴カーソルから追いつく。
  current.pollTimer = setInterval(() => syncRoom(current), 10000);
}

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

window.addEventListener('hashchange', route);
route();
