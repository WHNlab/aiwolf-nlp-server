import { stageHtml } from './tabletop.js?v=__ASSET_VERSION__';
import { HistoryDrawer } from './history.js?v=__ASSET_VERSION__';

// AI人狼バトル！ SPA — ルーム作成・待機・観戦・自分のAI視点
const $ = (sel, el = document) => el.querySelector(sel);
const app = $('#app');
const toastEl = $('#toast');
const viewBadge = $('#view-badge');
const connBadge = $('#conn-badge');
const headerRoom = $('#header-room-name');

let state = { room: null, events: [], cursor: 0 };
let feed = null;
let homeTimer = null;
let homeSearchTimer = null;
let routeRevision = 0;

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
    '招待トークンなどの接続情報はゲーム内の発言に含めないでください。',
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
  const revision = ++routeRevision;
  stopUpdates();
  clearInterval(homeTimer);
  clearTimeout(homeSearchTimer);
  destroyMatch();
  app.onclick = null;
  const h = location.hash || '#/';
  const m = h.match(/^#\/rooms\/([A-Za-z0-9]+)/);
  if (m) return renderRoom(m[1], revision);
  return renderHome(revision);
}

// ---- Home ----
async function renderHome(revision) {
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
  if (revision !== routeRevision) return;
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
    <section class="room-directory" aria-label="公開ルーム">
      <div class="directory-heading"><div><span class="owner-eyebrow">ROOMS</span><h2>公開ルーム</h2></div><p class="field-hint">ルーム名・RoomIDで検索</p></div>
      <label class="field"><span class="sr-only">公開ルームを検索</span><input id="room-search" type="search" placeholder="ルーム名・RoomIDで検索" autocomplete="off"></label>
      <div class="directory-columns">
        <section class="paper panel"><h3>開催中のルーム</h3><div id="active-rooms" class="directory-list" aria-live="polite"><p class="field-hint">読み込み中…</p></div></section>
        <section class="paper panel"><h3>最近の対戦記録</h3><p class="field-hint">終了後30日間、会話と試合の流れを見返せます。</p><div id="finished-rooms" class="directory-list" aria-live="polite"><p class="field-hint">読み込み中…</p></div></section>
      </div>
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
          <div class="field"><span class="field-label">公開設定</span>
            <div class="radio-cards">
              <label class="radio-card"><input type="radio" name="is_public" value="true" checked>
                <span class="card-inner">公開<small>一覧・検索に表示</small></span></label>
              <label class="radio-card"><input type="radio" name="is_public" value="false">
                <span class="card-inner">非公開<small>URLを知る人だけ</small></span></label>
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
  const showDirectory = (target, rooms, empty) => {
    const el = $(target);
    if (!el) return;
    el.innerHTML = rooms.length ? rooms.map(r => {
      const when = r.finished_at && !r.finished_at.startsWith('0001') ? new Date(r.finished_at).toLocaleString('ja-JP', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : '';
      const detail = ['finished', 'aborted'].includes(r.status) ? `${r.agent_count}人 · ${when}` : r.status === 'waiting' ? `${r.connected}/${r.agent_count} AI接続` : `${r.agent_count}人 · ${r.day || 0}日目`;
      return `<a class="directory-card" href="#/rooms/${encodeURIComponent(r.room_id)}"><span class="directory-card-main"><strong>${escapeHtml(r.name)}</strong><small>RoomID ${escapeHtml(r.room_id)}</small></span><span class="directory-card-side"><span class="chip ${r.status === 'running' ? 'chip-live' : ''}">${statusLabel(r.status)}</span><small>${escapeHtml(detail)}</small></span></a>`;
    }).join('') : `<p class="field-hint directory-empty">${empty}</p>`;
  };
  let directoryRequest = 0;
  const loadDirectory = async () => {
    if (document.body.dataset.page !== 'home' || revision !== routeRevision) return;
    const request = ++directoryRequest;
    const q = encodeURIComponent($('#room-search')?.value.trim() || '');
    const results = await Promise.allSettled(['active', 'finished'].map(status => api(`/api/v1/rooms?status=${status}&q=${q}`)));
    if (document.body.dataset.page !== 'home' || revision !== routeRevision || request !== directoryRequest) return;
    showDirectory('#active-rooms', results[0].status === 'fulfilled' ? results[0].value.rooms || [] : [], results[0].status === 'fulfilled' ? '現在、公開中のルームはありません。' : 'ルームを読み込めませんでした。');
    showDirectory('#finished-rooms', results[1].status === 'fulfilled' ? results[1].value.rooms || [] : [], results[1].status === 'fulfilled' ? 'まだ対戦記録はありません。' : '対戦記録を読み込めませんでした。');
  };
  $('#room-search').oninput = () => { clearTimeout(homeSearchTimer); homeSearchTimer = setTimeout(loadDirectory, 220); };
  loadDirectory();
  homeTimer = setInterval(loadDirectory, 12000);
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
          is_public: f.is_public.value === 'true',
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
async function renderRoom(roomId, revision = routeRevision) {
  stopUpdates();
  document.body.dataset.page = 'room';
  state.events = [];
  state.cursor = 0;
  let data;
  try { data = await api('/api/v1/rooms/' + roomId); }
  catch (e) {
    if (revision !== routeRevision) return;
    app.innerHTML = `<div class="page"><div class="paper panel empty">${escapeHtml(e.message)}</div></div>`;
    return;
  }
  if (revision !== routeRevision) return;
  state.room = data;
  headerRoom.textContent = data.name;
  updateViewBadge(data.viewer);

  if (!data.viewer.joined && ['waiting', 'starting', 'running'].includes(data.status)) return renderJoin(data);
  try { await refreshHistory(roomId, 0); } catch (_) { /* 通知の再接続時に再取得する */ }
  if (revision !== routeRevision) return;
  renderRoomView(data);
  if (['waiting', 'starting', 'running'].includes(data.status)) subscribeEvents(roomId);
  else { connBadge.hidden = true; document.body.classList.remove('connection-lost'); }
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
        ${data.status === 'waiting' ? `<div class="field"><span class="field-label">参加方法</span>
          <div class="radio-cards">
            <label class="radio-card"><input type="radio" name="mode" value="participate" checked>
              <span class="card-inner">AIも参加</span></label>
            <label class="radio-card"><input type="radio" name="mode" value="spectate">
              <span class="card-inner">観戦のみ</span></label>
          </div>
        </div>` : '<p class="field-hint">試合は始まっています。観戦者として入室できます。</p><input type="hidden" name="mode" value="spectate">'}
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
      if (['waiting', 'starting', 'running'].includes(out.status)) subscribeEvents(out.room_id);
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

function renderRoomView(data, hasNewEvents = false, previousRoom = null) {
  if (!['waiting', 'starting'].includes(data.status)) return renderMatch(data, hasNewEvents, previousRoom);
  const mobileView = $('#room-grid')?.className || 'room-grid';
  const inviteOpen = $('#invite-detail') && !$('#invite-detail').hidden;
  const inviteText = $('#invite-text')?.value || '';
  const consultText = $('#consult-form textarea')?.value || '';
  const focusedField = document.activeElement === $('#consult-form textarea') ? 'consult' : '';
  const status = data.status;
  const v = data.viewer;
  const leftCol = seatListHtml(data);
  const rightCol = rightPanelHtml(data);
  const center = lobbyHtml(data);
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
  bindRoomEvents(data);
  if (focusedField) $('#consult-form textarea')?.focus({ preventScroll: true });
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
      <span class="seat-sub">${escapeHtml(sub)}</span>
      <span class="seat-role ${s.is_mine ? 'mine' : ''}">${escapeHtml(roleLabel(s.role))}</span>
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
  if (p) {
    const k = p.knowledge || {};
    let know = `<span class="owner-eyebrow">YOUR AGENT</span><span class="own-role">${escapeHtml(roleLabel(p.role))}</span><p class="field-hint">${p.alive ? 'あなたのAIは生存しています' : 'あなたのAIは死亡しました。神視点で観戦できます。'}</p>`;
    if (k.werewolf_mates && k.werewolf_mates.length) {
      know += `<p class="field-hint">人狼の仲間: ${k.werewolf_mates.map(escapeHtml).join('、')}</p>`;
    }
    if (k.divine_results && k.divine_results.length) {
      know += '<p class="field-hint">占い結果:</p>' + k.divine_results.map(r =>
        `<div class="paper-inset" style="padding:6px 10px;margin-bottom:4px;font-size:0.85rem">${r.day}日目 ${escapeHtml(r.target || '?')} → ${escapeHtml(r.result === 'HUMAN' ? '人狼ではない' : r.result === 'WEREWOLF' ? '人狼' : r.result)}</div>`).join('');
    }
    if (k.medium_results && k.medium_results.length) {
      know += '<p class="field-hint">霊媒結果:</p>' + k.medium_results.map(r =>
        `<div class="paper-inset" style="padding:6px 10px;margin-bottom:4px;font-size:0.85rem">${r.day}日目 ${escapeHtml(r.target || '?')} → ${escapeHtml(r.result === 'HUMAN' ? '人狼ではない' : r.result === 'WEREWOLF' ? '人狼' : r.result)}</div>`).join('');
    }
    html += `<div class="paper panel"><h3>自分のAI</h3>${know}</div>`;
  }
  if (p || v.can_consult) {
    html += `<div class="paper panel">
      <h3>自分のAIに相談</h3>
      <p class="field-hint">あなたのAIにだけ届きます。ゲームの発言・投票はAIが行います。</p>
      <div class="consult-log" id="consult-log"></div>
      ${v.can_consult ? `<form id="consult-form" style="margin-top:10px">
        <textarea name="text" rows="3" maxlength="1000" placeholder="助言を書く…" aria-label="自分のAIへの助言"></textarea>
        <button class="btn btn--small btn--primary" type="submit" style="margin-top:6px">AIに送る</button>
        <p class="field-hint">送信済みの助言は次の通信でAIへ届きます。</p>
      </form>` : '<p class="field-hint">現在は助言を送信できません。</p>'}
    </div>`;
  }
  if (data.status === 'waiting' || data.status === 'starting') {
    html += inviteHtml(data);
  }
  return html || `<div class="paper panel"><span class="owner-eyebrow">SPECTATOR</span><h3>${data.viewer.view_mode === 'omniscient' ? '神視点で観戦中' : '公開視点で観戦中'}</h3><p class="field-hint">席を選ぶと参加者の詳細、履歴からこれまでの会話を確認できます。</p></div>`;
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
    case 'day': return '';
    case 'game_end': return `<div class="event-sys">🏁 ${escapeHtml(e.text)} ${escapeHtml(e.team || '')}</div>`;
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
      const currentField = $('#consult-form textarea');
      if (state.room?.room_id === data.room_id && currentField?.value.trim() === t) currentField.value = '';
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
    data.viewer, data.private_agent, data.roles, data.win_side, data.abort_reason, data.progress]);
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
      const viewChanged = state.room.viewer.view_mode !== fresh.viewer.view_mode || state.room.viewer.own_seat !== fresh.viewer.own_seat || state.room.viewer.user_id !== fresh.viewer.user_id || state.room.day !== fresh.day || state.room.status !== fresh.status;
      const changed = roomViewKey(state.room) !== roomViewKey(fresh);
      let newEvents = 0;
      if (viewChanged || fresh.cursor > state.cursor) {
        newEvents = await refreshHistory(current.roomId, viewChanged ? 0 : state.cursor, current);
        if (!current.active) return;
      }
      const previousRoom = state.room;
      state.room = fresh;
      if (current.es.readyState === EventSource.OPEN) setConnection(true, false);
      headerRoom.textContent = fresh.name;
      updateViewBadge(fresh.viewer);
      if (changed) renderRoomView(fresh, newEvents > 0, previousRoom);
      else if (newEvents && matchUI) renderMatch(fresh, true);
      current.catchingUp = false;
      if (['finished', 'aborted', 'closed'].includes(fresh.status)) {
        stopUpdates();
        connBadge.hidden = true;
        return;
      }
    } while (current.queued);
  } catch (_) {
    setConnection(false);
    // SSEの再接続または定期確認で再試行する。既存の表示は残す。
  } finally {
    current.syncing = false;
  }
}

function subscribeEvents(roomId) {
  stopUpdates();
  const current = { roomId, catchingUp: true, active: true, syncing: false, queued: false, es: null, pollTimer: null };
  feed = current;
  current.es = new EventSource(`/api/v1/rooms/${roomId}/events`);
  connBadge.hidden = false;
  setConnection(true);
  current.es.addEventListener('room', () => syncRoom(current));
  current.es.onopen = () => {
    if (!current.active) return;
    current.catchingUp = true;
    setConnection(true);
    syncRoom(current);
  };
  current.es.onerror = () => {
    if (!current.active) return;
    current.catchingUp = true;
    setConnection(false);
  };
  // SSEが中継で滞留した場合にも履歴カーソルから追いつく。
  current.pollTimer = setInterval(() => syncRoom(current), 10000);
}

// ---- テーブル観戦 ----
let matchUI = null;
const roleLabel = role => ({ VILLAGER: '村人', SEER: '占い師', MEDIUM: '霊媒師', BODYGUARD: '狩人', WEREWOLF: '人狼', POSSESSED: '狂人', ANY: '未定' }[role] || role || '非公開');

function destroyMatch() {
  if (!matchUI) return;
  matchUI.history.destroy();
  matchUI.sheet.close();
  matchUI.sheet.remove();
  matchUI = null;
}

function renderMatch(data, hasNewEvents = false, previousRoom = null) {
  document.body.dataset.page = 'match';
  if (!matchUI || matchUI.roomId !== data.room_id) {
    destroyMatch();
    app.innerHTML = `<div class="match-page"><div class="match-heading"><h1>${escapeHtml(data.name)}</h1><button class="btn btn--small" data-action="menu">ルーム ⋯</button></div>
      <div class="match-grid"><section class="match-main" aria-label="試合のテーブル"><div id="game-stage"></div><div id="current-speech"></div><div id="match-result"></div></section><aside class="owner-dock"><div id="owner-panel"></div></aside></div>
      <nav class="match-actions" aria-label="観戦メニュー"><button class="btn btn--primary" data-action="history">▤ 履歴をひらく</button>${data.viewer.own_seat ? '<button class="btn owner-open" data-action="owner">自分のAI</button>' : ''}</nav></div>`;
    const sheet = document.createElement('dialog');
    sheet.className = 'game-sheet';
    sheet.setAttribute('aria-labelledby', 'game-sheet-title');
    sheet.innerHTML = `<header class="sheet-heading"><h2 id="game-sheet-title"></h2><button class="btn sheet-close">閉じる ×</button></header><div class="sheet-body"></div>`;
    document.body.append(sheet);
    const history = new HistoryDrawer({ renderEvent: eventHtml, seatName: seatNameOf });
    matchUI = { roomId: data.room_id, history, sheet, ownerKey: '', stageKey: '', speechKey: '', lastTalk: null, status: data.status, channel: 'talk', panel: '', trigger: null, previousRoom };
    sheet.querySelector('.sheet-close').onclick = () => sheet.close();
    sheet.addEventListener('close', () => {
      if (!matchUI || matchUI.sheet !== sheet) return;
      const owner = sheet.querySelector('#owner-panel');
      if (owner) $('.owner-dock')?.append(owner);
      matchUI.panel = '';
      if (matchUI.trigger?.isConnected) matchUI.trigger.focus({ preventScroll: true });
    });
    app.onclick = handleMatchClick;
  }
  const ui = matchUI;
  const latest = state.events.findLast(e => e.type === 'talk');
  const online = !document.body.classList.contains('connection-lost');
  const stageKey = JSON.stringify([data.seats, data.progress, data.status, data.day, data.viewer.own_seat, latest?.seq, online]);
  if (ui.stageKey !== stageKey) {
    const activeSeat = document.activeElement?.dataset.seatId;
    const phaseChanged = ui.previousRoom?.progress?.phase !== data.progress?.phase;
    const animate = (hasNewEvents || phaseChanged) && !!ui.previousRoom && !document.hidden && online && !feed?.catchingUp;
    const talkChanged = ui.lastTalk !== null && latest?.seq !== ui.lastTalk;
    $('#game-stage').innerHTML = stageHtml(data, state.events, { connected: online, animate, talkChanged, previousRoom: ui.previousRoom });
    if (activeSeat) [...$('#game-stage').querySelectorAll('[data-seat-id]')].find(el => el.dataset.seatId === activeSeat)?.focus({ preventScroll: true });
    ui.stageKey = stageKey;
  }
  ui.lastTalk = latest?.seq ?? null;
  ui.previousRoom = data;
  renderCurrentSpeech();
  updateOwnerPanel(data);
  ui.history.update(state.events, data);
  if (ui.panel === 'seat') renderSeatDetail(ui.seatId);
  if (ui.panel === 'menu') renderRoomMenu();
  if (ui.status !== data.status || !ui.resultRendered) {
    const finished = ['finished', 'aborted', 'closed'].includes(data.status);
    $('#match-result').innerHTML = finished ? `<section class="paper match-result ${ui.status === 'running' && data.status === 'finished' ? 'is-new' : ''}"><span class="speech-eyebrow">${data.status === 'finished' ? 'GAME SET' : 'ROOM CLOSED'}</span><h2>${data.status === 'finished' ? `${escapeHtml(data.win_side === 'VILLAGER' ? '村人陣営' : data.win_side === 'WEREWOLF' ? '人狼陣営' : data.win_side || '')}の勝利！` : statusLabel(data.status)}</h2><p>${escapeHtml(data.abort_reason || 'テーブルの席を選んで役職を確認したり、履歴を読み返せます。')}</p><a class="btn" href="#/">新しい部屋へ</a></section>` : '';
    ui.resultRendered = true;
  }
  ui.status = data.status;
}

function renderCurrentSpeech() {
  const ui = matchUI;
  const hasWhisper = state.events.some(e => e.type === 'whisper');
  if (!hasWhisper) ui.channel = 'talk';
  const latest = state.events.findLast(e => e.type === ui.channel);
  const key = JSON.stringify([latest, ui.channel, hasWhisper]);
  if (key === ui.speechKey) return;
  ui.speechKey = key;
  const focus = document.activeElement?.dataset.action;
  $('#current-speech').innerHTML = `<section class="paper current-speech" aria-label="最新の発言"><div class="speech-heading"><span class="speech-number">${latest ? String(latest.from_idx).padStart(2, '0') : '…'}</span><div><span class="speech-eyebrow">${ui.channel === 'whisper' ? '人狼だけの会話' : 'LATEST TALK · 最新の公開発言'}</span><h2>${latest ? escapeHtml(seatNameOf(latest.from_idx)) : '最初の発言を待っています'}</h2></div></div><p class="speech-text">${escapeHtml(latest?.text || 'AIたちの会話が始まると、ここに届きます。')}</p><div class="speech-actions"><span class="field-hint">${latest ? `${latest.day}日目` : '発言・投票はAIが行います'}</span><div>${hasWhisper ? `<button class="btn btn--small" data-action="channel">${ui.channel === 'talk' ? '人狼の囁きへ' : '公開会話へ'}</button> ` : ''}${latest ? `<button class="btn btn--small" data-action="full-speech" data-seq="${Number(latest.seq)}">全文を読む ↗</button>` : ''}</div></div></section>`;
  if (focus) [...$('#current-speech').querySelectorAll('[data-action]')].find(el => el.dataset.action === focus)?.focus({ preventScroll: true });
}

function updateOwnerPanel(data) {
  const ui = matchUI;
  const key = JSON.stringify([data.viewer, data.private_agent, data.status]);
  if (key !== ui.ownerKey) {
    const panel = $('#owner-panel');
    const text = panel.querySelector('textarea')?.value || '';
    const focused = panel.contains(document.activeElement) ? document.activeElement.name : '';
    const selection = focused ? [document.activeElement.selectionStart, document.activeElement.selectionEnd] : null;
    panel.innerHTML = rightPanelHtml(data);
    if (panel.querySelector('textarea')) panel.querySelector('textarea').value = text;
    bindRoomEvents(data);
    if (focused) {
      const field = [...panel.querySelectorAll('input, textarea')].find(el => el.name === focused);
      field?.focus({ preventScroll: true });
      if (field && selection) field.setSelectionRange(...selection);
    }
    ui.ownerKey = key;
  }
  const log = $('#consult-log');
  if (log) {
    const notes = state.events.filter(e => ['owner_advice', 'owner_note'].includes(e.type));
    const key = JSON.stringify(notes);
    if (log.dataset.events !== key) {
      const follow = log.scrollHeight - log.scrollTop - log.clientHeight < 40;
      const top = log.scrollTop;
      log.innerHTML = notes.map(eventHtml).join('') || '<p class="field-hint">AIとの個別メッセージが届きます。</p>';
      log.dataset.events = key;
      log.scrollTop = follow ? log.scrollHeight : top;
    }
  }
}

function openGameSheet(title, panel, trigger) {
  const ui = matchUI;
  if (ui.sheet.open) ui.sheet.close();
  ui.panel = panel;
  ui.menuKey = '';
  ui.trigger = trigger;
  $('#game-sheet-title', ui.sheet).textContent = title;
  $('.sheet-body', ui.sheet).replaceChildren();
}

function handleMatchClick(e) {
  const button = e.target.closest('button');
  if (!button || !matchUI) return;
  const action = button.dataset.action;
  if (button.dataset.seatId) {
    openGameSheet('参加者', 'seat', button);
    matchUI.seatId = button.dataset.seatId;
    renderSeatDetail(matchUI.seatId);
    matchUI.sheet.showModal();
  } else if (action === 'history' || action === 'full-speech') matchUI.history.open(button, action === 'full-speech' ? Number(button.dataset.seq) : null);
  else if (action === 'owner') {
    openGameSheet('自分のAI', 'owner', button);
    $('.sheet-body', matchUI.sheet).append($('#owner-panel'));
    matchUI.sheet.showModal();
  } else if (action === 'menu') {
    openGameSheet('ルーム', 'menu', button);
    renderRoomMenu();
    matchUI.sheet.showModal();
  } else if (action === 'channel') {
    matchUI.channel = matchUI.channel === 'talk' ? 'whisper' : 'talk';
    renderCurrentSpeech();
  }
}

function renderSeatDetail(id) {
  const seat = state.room.seats.find(s => s.seat_id === id);
  if (!seat) return;
  $('.sheet-body', matchUI.sheet).innerHTML = `<div class="paper panel"><span class="speech-number">${String(seat.agent_idx).padStart(2, '0')}</span><h3>${escapeHtml(seatNameOf(seat.agent_idx))}</h3><p>${seat.is_mine ? 'あなたのAI · ' : ''}${seat.alive ? '生存' : '死亡'}</p><p>参加者：${escapeHtml(seat.user_name || '—')}</p><p>チーム：${escapeHtml(seat.team_name || '—')}</p><span class="own-role">${escapeHtml(roleLabel(seat.role))}</span></div>`;
}

function renderRoomMenu() {
  const data = state.room;
  const key = JSON.stringify([data.name, data.seats, data.roles, data.connected]);
  if (matchUI.menuKey === key) return;
  matchUI.menuKey = key;
  const body = $('.sheet-body', matchUI.sheet);
  const focus = document.activeElement?.dataset.action;
  const expanded = body.querySelector('details')?.open;
  body.innerHTML = `<p><strong>${escapeHtml(data.name)}</strong></p><p class="field-hint">RoomID <code>${escapeHtml(data.room_id)}</code></p><button class="btn" data-action="share">観戦URLをコピー</button><div style="margin-top:20px">${seatListHtml(data)}</div><p><a class="btn" href="#/">トップへ戻る</a></p>`;
  if (expanded) body.querySelector('details').open = true;
  const share = $('[data-action="share"]', body);
  share.onclick = async () => { try { await navigator.clipboard.writeText(location.href); toast('観戦URLをコピーしました'); } catch (_) { toast('アドレス欄のURLをコピーしてください'); } };
  if (focus === 'share') share.focus({ preventScroll: true });
}


function setConnection(connected, render = true) {
  const changed = document.body.classList.contains('connection-lost') === connected;
  connBadge.textContent = connected ? '接続中' : '更新待ち';
  connBadge.classList.toggle('off', !connected);
  document.body.classList.toggle('connection-lost', !connected);
  if (changed && render && matchUI && state.room) renderMatch(state.room);
}

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

document.addEventListener('visibilitychange', () => {
  document.body.classList.toggle('tab-hidden', document.hidden);
  if (!document.hidden && feed) { feed.catchingUp = true; syncRoom(feed); }
});
window.addEventListener('hashchange', route);
route();
