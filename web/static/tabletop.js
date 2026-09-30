// AI人狼バトル！ — テーブル観戦ステージ (doc/ja/tabletop-ui-design.md)
// main は #game-stage に stageHtml() の返り値を innerHTML で流し込み、
// 席ボタンの [data-seat-id] クリック委譲で参加者詳細を開く。
// このモジュールは DOM・通信に触れない純粋なHTML生成だけを行う。
// 装飾アニメを止めたい場合(非表示タブなど)は body に "tt-paused" を
// 付けると CSS 側で一時停止できる。prefers-reduced-motion でも自動で静止する。

const esc = (v) => String(v ?? '').replace(/[&<>"']/g, (c) => (
  { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
));

// 席の見た目色。役職ではなく agent_idx から決める中立なパレット。
const PALETTE = [
  ['#9be0be', '#55a37c', '#e9faf0'], // ミント
  ['#f6a79a', '#cc6f60', '#fdebe7'], // コーラル
  ['#ffd98a', '#d3a04a', '#fff3da'], // 黄色
  ['#a9d3f2', '#5f93bb', '#e9f5fc'], // 青
  ['#ccbdf1', '#8d74c0', '#f2edfb'], // 紫
  ['#f4b8c8', '#cb8298', '#fdeef2'], // ピンク
  ['#9fdcc8', '#58a28c', '#ecf8f4'], // ティール
  ['#f7c09a', '#cd8d5f', '#fdeee2'], // オレンジ
  ['#bcc7f2', '#7a86c4', '#eef1fb'], // 藍
  ['#cde3a0', '#93ab5e', '#f4f9e6'], // 若草
  ['#eeaec0', '#c27490', '#fbe9f0'], // 薔薇
  ['#a5d9ef', '#62a4c2', '#e9f6fc'], // 空
  ['#dcc9a8', '#ab8f68', '#f7efdf'], // 砂
];

// 席リングの半径(%)と中心位置。手前=下が基準で、時計回りに並ぶ。
const RING = {
  d: {
    l: { rx: 39, ry: 36, cy: 48 },
    m: { rx: 38, ry: 37, cy: 48 },
    s: { rx: 38, ry: 37, cy: 48 },
  },
  m: {
    l: { rx: 35, ry: 33, cy: 47 },
    m: { rx: 40, ry: 40, cy: 49 },
    s: { rx: 40, ry: 40, cy: 49 },
  },
};

const PHASE_LABEL = {
  waiting: ['待機中', 'is-run', 'bot'],
  starting: ['開始中', 'is-run', 'bot'],
  day_discussion: ['昼・議論', 'is-day', 'sun'],
  day_vote: ['昼・投票', 'is-day', 'sun'],
  day_result: ['昼・結果', 'is-day', 'sun'],
  night: ['夜', 'is-night', 'moon'],
  finished: ['終了', 'is-end', 'flag'],
};

const ROLE_LABEL = {
  VILLAGER: '村人',
  SEER: '占い師',
  MEDIUM: '霊媒師',
  BODYGUARD: '狩人',
  WEREWOLF: '人狼',
  POSSESSED: '狂人',
};

const IC = {
  sun: '<svg viewBox="0 0 16 16" class="tt-ic" aria-hidden="true"><circle cx="8" cy="8" r="3" fill="currentColor"/><path d="M8 1.6v1.9M8 12.5v1.9M1.6 8h1.9M12.5 8h1.9M3.5 3.5l1.3 1.3M11.2 11.2l1.3 1.3M12.5 3.5l-1.3 1.3M4.8 11.2l-1.3 1.3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" fill="none"/></svg>',
  moon: '<svg viewBox="0 0 16 16" class="tt-ic" aria-hidden="true"><path d="M13.4 9.6A5.9 5.9 0 1 1 6.4 2.6a4.7 4.7 0 1 0 7 7z" fill="currentColor"/></svg>',
  flag: '<svg viewBox="0 0 16 16" class="tt-ic" aria-hidden="true"><path d="M3.5 14V2.8c2.6-1.6 5.6 1 9-.4v7.2c-3.4 1.4-6.4-1.2-9 .4" fill="currentColor"/></svg>',
  bot: '<svg viewBox="0 0 16 16" class="tt-ic" aria-hidden="true"><rect x="3" y="4.5" width="10" height="8" rx="3.4" fill="none" stroke="currentColor" stroke-width="1.5"/><circle cx="6.4" cy="8.5" r="1.15" fill="currentColor"/><circle cx="9.6" cy="8.5" r="1.15" fill="currentColor"/><path d="M8 4.5V2.2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
  talk: '<svg viewBox="0 0 16 16" class="tt-ic" aria-hidden="true"><path d="M2.6 3.2h10.8v6.4H8.4l-3.1 2.6V9.6H2.6z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><path d="M5.3 6.2h5.4M5.3 8h3.4" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/></svg>',
  think: '<svg viewBox="0 0 16 16" class="tt-ic" aria-hidden="true"><path d="M2.6 3.2h10.8v6.4H8.4l-3.1 2.6V9.6H2.6z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><circle cx="5.4" cy="6.5" r=".95" fill="currentColor"/><circle cx="8" cy="6.5" r=".95" fill="currentColor"/><circle cx="10.6" cy="6.5" r=".95" fill="currentColor"/></svg>',
};

// テーブル中央の肉球モチーフ(非インタラクティブ)
const PAW = '<svg viewBox="0 0 64 56" class="tt-paw" aria-hidden="true"><g fill="currentColor"><ellipse cx="32" cy="38" rx="13" ry="10"/><ellipse cx="13.5" cy="24" rx="5.4" ry="7" transform="rotate(-18 13.5 24)"/><ellipse cx="26.5" cy="16" rx="5.4" ry="7"/><ellipse cx="37.5" cy="16" rx="5.4" ry="7"/><ellipse cx="50.5" cy="24" rx="5.4" ry="7" transform="rotate(18 50.5 24)"/></g></svg>';

const PLANT = '<svg viewBox="0 0 44 58" class="tt-prop-svg" aria-hidden="true"><path d="M22 34V16" stroke="var(--tt-plum,#45335a)" stroke-width="3" stroke-linecap="round"/><ellipse cx="13" cy="18" rx="7" ry="10" transform="rotate(-28 13 18)" fill="#9be0be" stroke="var(--tt-plum,#45335a)" stroke-width="2.6"/><ellipse cx="31" cy="16" rx="7" ry="10" transform="rotate(28 31 16)" fill="#9be0be" stroke="var(--tt-plum,#45335a)" stroke-width="2.6"/><ellipse cx="22" cy="10" rx="6" ry="8" fill="#cde3a0" stroke="var(--tt-plum,#45335a)" stroke-width="2.6"/><path d="M9 36h26l-3 18H12z" fill="#f6a79a" stroke="var(--tt-plum,#45335a)" stroke-width="2.6" stroke-linejoin="round"/><path d="M9 36h26" stroke="var(--tt-plum,#45335a)" stroke-width="2.6"/></svg>';

const MUG = '<svg viewBox="0 0 44 40" class="tt-prop-svg" aria-hidden="true"><path d="M19 4c-2 2.4-2 4.6 0 7M27 4c-2 2.4-2 4.6 0 7" stroke="var(--tt-plum,#45335a)" stroke-width="2.2" stroke-linecap="round" fill="none"/><path d="M7 14h26v14a7 7 0 0 1-7 7H14a7 7 0 0 1-7-7z" fill="#fffdf8" stroke="var(--tt-plum,#45335a)" stroke-width="2.6" stroke-linejoin="round"/><path d="M33 17h3.5a4.5 4.5 0 0 1 0 9H33" fill="none" stroke="var(--tt-plum,#45335a)" stroke-width="2.6"/><circle cx="15" cy="24" r="2" fill="#f6a79a"/><circle cx="22" cy="24" r="2" fill="#f6a79a"/><circle cx="18.5" cy="29.5" r="2.6" fill="#f6a79a"/></svg>';

const SVG_OPEN = '<svg viewBox="0 0 72 84" class="tt-svg" aria-hidden="true" focusable="false">';

// 正面(テーブル奥側の席): こちらを向いている
const ROBOT_FRONT = SVG_OPEN + `
  <ellipse class="f-shadow" cx="36" cy="80" rx="18" ry="3.4"/>
  <path class="ln nofill" d="M36 15V9"/>
  <circle class="f-acc ln" cx="36" cy="6.5" r="4.2"/>
  <rect class="f-shade ln" x="6" y="28" width="7" height="13" rx="3.5"/>
  <rect class="f-shade ln" x="59" y="28" width="7" height="13" rx="3.5"/>
  <rect class="f-body ln" x="12" y="15" width="48" height="33" rx="15"/>
  <rect class="f-dark" x="19" y="22" width="34" height="19" rx="9.5"/>
  <ellipse class="f-eye" cx="29.5" cy="31" rx="3.4" ry="4.8"/>
  <ellipse class="f-eye" cx="42.5" cy="31" rx="3.4" ry="4.8"/>
  <path class="ln-eye" d="M33 37.2q3 2.4 6 0"/>
  <rect class="f-shade ln" x="4" y="50" width="9" height="17" rx="4.5" transform="rotate(-12 8.5 58)"/>
  <rect class="f-shade ln" x="59" y="50" width="9" height="17" rx="4.5" transform="rotate(12 63.5 58)"/>
  <rect class="f-body ln" x="16" y="49" width="40" height="24" rx="11"/>
  <rect class="f-pale" x="25" y="54" width="22" height="13" rx="6.5"/>
  <circle class="f-shade" cx="31.5" cy="60.5" r="1.7"/>
  <circle class="f-shade" cx="40.5" cy="60.5" r="1.7"/>
  <rect class="f-shade ln" x="19" y="72" width="13" height="7" rx="3.5"/>
  <rect class="f-shade ln" x="40" y="72" width="13" height="7" rx="3.5"/>
</svg>`;

// 背面(手前=自分の席): 中央を向いて背中が見える
const ROBOT_BACK = SVG_OPEN + `
  <ellipse class="f-shadow" cx="36" cy="80" rx="18" ry="3.4"/>
  <path class="ln nofill" d="M36 15V9"/>
  <circle class="f-acc ln" cx="36" cy="6.5" r="4.2"/>
  <rect class="f-shade ln" x="6" y="28" width="7" height="13" rx="3.5"/>
  <rect class="f-shade ln" x="59" y="28" width="7" height="13" rx="3.5"/>
  <rect class="f-body ln" x="12" y="15" width="48" height="33" rx="15"/>
  <rect class="f-shade" x="21" y="22" width="30" height="19" rx="8"/>
  <path class="ln-thin" d="M26 27h20M26 31.5h20M26 36h13"/>
  <circle class="f-acc" cx="45" cy="35.5" r="1.8"/>
  <rect class="f-shade ln" x="4" y="50" width="9" height="17" rx="4.5" transform="rotate(-12 8.5 58)"/>
  <rect class="f-shade ln" x="59" y="50" width="9" height="17" rx="4.5" transform="rotate(12 63.5 58)"/>
  <rect class="f-body ln" x="16" y="49" width="40" height="24" rx="11"/>
  <rect class="f-shade" x="23" y="53" width="26" height="15" rx="6"/>
  <path class="ln-thin" d="M36 55v11"/>
  <rect class="f-shade ln" x="19" y="72" width="13" height="7" rx="3.5"/>
  <rect class="f-shade ln" x="40" y="72" width="13" height="7" rx="3.5"/>
</svg>`;

// 斜め(左右の席): 右向きに描き、左側はCSSで反転する
const ROBOT_SIDE = SVG_OPEN + `
  <ellipse class="f-shadow" cx="36" cy="80" rx="18" ry="3.4"/>
  <path class="ln nofill" d="M37 15l4-6"/>
  <circle class="f-acc ln" cx="42.5" cy="6.5" r="4.2"/>
  <rect class="f-shade ln" x="7" y="28" width="7" height="13" rx="3.5"/>
  <rect class="f-body ln" x="13" y="15" width="47" height="33" rx="15"/>
  <rect class="f-dark" x="27" y="22" width="29" height="19" rx="9.5"/>
  <ellipse class="f-eye" cx="37" cy="31" rx="3.4" ry="4.8"/>
  <ellipse class="f-eye" cx="48.5" cy="31" rx="3.4" ry="4.8"/>
  <path class="ln-eye" d="M41.5 37.2q3 2.4 6 0"/>
  <rect class="f-shade ln" x="8" y="52" width="8" height="15" rx="4" opacity=".8"/>
  <rect class="f-body ln" x="17" y="49" width="39" height="24" rx="11"/>
  <rect class="f-pale" x="31" y="54" width="20" height="13" rx="6.5"/>
  <circle class="f-shade" cx="37.5" cy="60.5" r="1.7"/>
  <circle class="f-shade" cx="45.5" cy="60.5" r="1.7"/>
  <rect class="f-shade ln" x="57" y="49" width="9" height="18" rx="4.5" transform="rotate(14 61.5 58)"/>
  <rect class="f-shade ln" x="21" y="72" width="13" height="7" rx="3.5"/>
  <rect class="f-shade ln" x="41" y="72" width="13" height="7" rx="3.5"/>
</svg>`;

function robotSvg(view) {
  if (view === 'back') return ROBOT_BACK;
  if (view === 'side') return ROBOT_SIDE;
  return ROBOT_FRONT;
}

// 席の並び: agent_idx 優先、未割当は seat_id で安定化する。
export function seatListOrder(seats) {
  return (Array.isArray(seats) ? seats.slice() : []).sort((a, b) => {
    const ai = a.agent_idx > 0 ? a.agent_idx : Number.MAX_SAFE_INTEGER;
    const bi = b.agent_idx > 0 ? b.agent_idx : Number.MAX_SAFE_INTEGER;
    return ai - bi || String(a.seat_id).localeCompare(String(b.seat_id));
  });
}

// 最新の公開発言( type==="talk" )の話者 idx。囁き・出来事は話者マークに使わない。
export function latestSpeakerIdx(events) {
  if (!Array.isArray(events)) return null;
  let best = null;
  let bestSeq = -Infinity;
  for (const e of events) {
    if (!e || e.type !== 'talk' || e.from_idx == null) continue;
    const seq = typeof e.seq === 'number' ? e.seq : 0;
    if (seq >= bestSeq) { bestSeq = seq; best = e.from_idx; }
  }
  return best;
}

function seatName(s) {
  return s.agent_name || s.game_name || s.team_name || s.user_name || '';
}

function seatNum(s, orderPos) {
  const idx = s.agent_idx > 0 ? s.agent_idx : null;
  return { idx, text: idx != null ? String(idx).padStart(2, '0') : String(orderPos + 1).padStart(2, '0') };
}

// 向き: 奥(上)は正面、手前(下)は背面、左右は斜め。右側の席はSVGを反転する。
function facingOf(angle) {
  const a = ((angle % 360) + 360) % 360;
  if (a >= 225 && a < 315) return { view: 'front', flip: false };
  if (a >= 45 && a < 135) return { view: 'back', flip: false };
  if (a >= 135 && a < 225) return { view: 'side', flip: false }; // 左側の席は右を向く
  return { view: 'side', flip: true };                          // 右側の席は左を向く
}

function sizeBucket(n) {
  if (n <= 6) return 'l';
  if (n <= 10) return 'm';
  return 's';
}

function phaseInfo(room) {
  const p = room.progress && room.progress.phase;
  if (p && PHASE_LABEL[p]) {
    const [label, cls, icon] = PHASE_LABEL[p];
    return { label, cls, icon };
  }
  if (p) return { label: String(p), cls: 'is-run', icon: '' };
  const fb = {
    waiting: '待機中', starting: '開始中', running: '進行中',
    finished: '終了', aborted: '中断', closed: '閉室',
  };
  const label = fb[room.status] || '進行中';
  const cls = room.status === 'finished' ? 'is-end' : room.status === 'aborted' ? 'is-end' : 'is-run';
  return { label, cls, icon: room.status === 'finished' ? 'flag' : '' };
}

/**
 * 進行バー + テーブルステージのHTMLを返す。
 * @param {object} room   RoomView(seats/viewer/day/status + 任意の progress)
 * @param {Array}  events 閲覧者に見えている履歴。最新の公開talkが話者マークになる
 * @param {object} options { connected=接続中か, animate=新着演出を付けるか, previousRoom }
 */
export function stageHtml(room, events = [], options = {}) {
  room = room || {};
  const seats = seatListOrder(room.seats);
  const n = seats.length;
  const size = sizeBucket(n);
  const viewer = room.viewer || {};
  const ownId = viewer.own_seat || viewer.seat_id || '';
  const animate = options.animate === true;
  const connected = options.connected !== false;
  const prev = options.previousRoom || null;

  // 自分の席を手前(0番ポジション)へ。観戦者は先頭の席を手前に固定する。
  let anchor = seats.findIndex((s) => (ownId && s.seat_id === ownId) || s.is_mine === true);
  if (anchor < 0) anchor = 0;

  const latest = latestSpeakerIdx(events);
  const turn = room.progress && room.progress.active_public_turn;
  const pendingIdx = turn && turn.agent_idx != null ? turn.agent_idx : null;
  const pendingWord = turn ? (turn.state === 'waiting' ? '応答待ち' : '応答確認中') : '';

  const prevAlive = new Map();
  if (prev && Array.isArray(prev.seats)) {
    for (const s of prev.seats) prevAlive.set(s.seat_id, s.alive);
  }
  const justStarted = animate && !!prev && prev.status !== 'running' && room.status === 'running';

  const dl = RING.d[size];
  const ml = RING.m[size];
  const byIdx = new Map(seats.map((s) => [s.agent_idx, s]));

  const lis = seats.map((s, i) => {
    const p = (i - anchor + n) % n;
    const ang = 90 + (p * 360) / n;
    const half = Math.floor(n / 2);
    const side = p === 0 ? 'bottom' : p <= half ? 'left' : 'right';
    const row = p === 0 ? half + 1 : p <= half ? half - p + 1 : p - half;
    const rad = (ang * Math.PI) / 180;
    const cos = Math.cos(rad);
    const sin = Math.sin(rad);
    const { view, flip } = facingOf(ang);
    const { idx, text: num } = seatNum(s, i);
    const color = PALETTE[(idx != null ? idx - 1 : i) % PALETTE.length];
    const name = seatName(s) || 'AI';
    const short = name.length > 9 ? `${name.slice(0, 8)}…` : name;
    const isMine = s.is_mine === true || (ownId && s.seat_id === ownId);
    const isDead = s.alive === false;
    const isLatest = idx != null && idx === latest;
    const isPending = idx != null && idx === pendingIdx;
    const roleJa = s.role && s.role !== '非公開' ? (ROLE_LABEL[s.role] || String(s.role)) : '';

    const cls = ['tt-seat'];
    if (isDead) cls.push('is-dead');
    if (isLatest) cls.push('is-latest');
    if (isPending) cls.push('is-pending');
    if (isMine) cls.push('is-mine');
    const fx = [];
    if (justStarted) fx.push('tt-fx-land');
    if (animate && options.talkChanged !== false && isLatest) fx.push('tt-fx-pop');
    if (animate && isDead && prevAlive.get(s.seat_id) === true) fx.push('tt-fx-death');

    const aria = [`${num} ${name}`];
    if (isMine) aria.push('あなたのAI');
    if (isDead) aria.push('死亡');
    if (isPending) aria.push(pendingWord);
    if (isLatest) aria.push('最新の発言者');
    if (roleJa) aria.push(`役職 ${roleJa}`);

    const style = [
      `--tx:${(50 + dl.rx * cos).toFixed(2)}%`, `--ty:${(dl.cy + dl.ry * sin).toFixed(2)}%`,
      `--mx:${(50 + ml.rx * cos).toFixed(2)}%`, `--my:${(ml.cy + ml.ry * sin).toFixed(2)}%`,
      `--m-row:${row}`, `--m-col:${side === 'right' ? 3 : 1}`, `--rb:${color[0]}`, `--rbd:${color[1]}`, `--rbp:${color[2]}`,
    ];
    if (justStarted) style.push(`--d:${Math.min(p * 15, 150)}ms`);

    const waitFlag = isPending
      ? `<span class="tt-flag tt-flag-wait">${IC.think}<span>${pendingWord}</span></span>` : '';
    const talkFlag = isLatest
      ? `<span class="tt-flag tt-flag-talk">${IC.talk}<span>公開発言</span></span>` : '';
    const ribbon = isDead ? '<span class="tt-ribbon" aria-hidden="true">死亡</span>' : '';
    const ring = isPending ? '<span class="tt-ring" aria-hidden="true"></span>' : '';
    const extra = (isMine || roleJa)
      ? `<span class="tt-extra">${isMine ? '<span class="tt-mine">あなたのAI</span>' : ''}${roleJa ? `<span class="tt-role">${esc(roleJa)}</span>` : ''}</span>`
      : '';

    return `<li class="${cls.join(' ')} ${fx.join(' ')}" data-side="${side}" style="${style.join(';')}">
      <button type="button" class="tt-seat-btn" data-seat-id="${esc(s.seat_id)}"${idx != null ? ` data-agent-idx="${idx}"` : ''} aria-label="${esc(aria.join('、'))}">
        <span class="tt-robot${flip ? ' tt-flip' : ''}">${ring}${robotSvg(view)}</span>
        ${ribbon}
        <span class="tt-plate"><span class="tt-num">${num}</span><span class="tt-name">${esc(short)}</span></span>
        ${extra}
        ${waitFlag}${talkFlag}
      </button>
    </li>`;
  }).join('');

  // ---- 進行バー: 日付・フェーズ・生存数・(分かれば)応答待ち ----
  const alive = seats.filter((s) => s.alive !== false).length;
  const phase = phaseInfo(room);
  const chips = [];
  if (room.day != null) chips.push(`<span class="tt-chip tt-chip-day">${esc(room.day)}日目</span>`);
  chips.push(`<span class="tt-chip tt-chip-phase ${phase.cls}">${phase.icon ? IC[phase.icon] : ''}<span>${esc(phase.label)}</span></span>`);
  chips.push(`<span class="tt-chip tt-chip-alive">${IC.bot}<span>生存 ${alive}/${n}</span></span>`);
  if (turn) {
    const ps = pendingIdx != null ? byIdx.get(pendingIdx) : null;
    const pn = ps ? seatNum(ps, seats.indexOf(ps)).text : '';
    const pnm = ps ? seatName(ps) : '';
    chips.push(`<span class="tt-chip tt-chip-wait">${IC.think}<span>${pendingWord}${pn ? ` ${pn}` : ''}${pnm ? ` ${esc(pnm.length > 9 ? `${pnm.slice(0, 8)}…` : pnm)}` : ''}</span></span>`);
  }
  if (!connected) chips.push('<span class="tt-chip tt-chip-off">更新待ち</span>');

  const stageCls = ['tt-stage'];
  if (!connected) stageCls.push('is-off');
  if (animate) stageCls.push('is-fx');
  if (animate && prev?.progress?.phase !== room.progress?.phase) stageCls.push('tt-phase-change');

  return `<section class="${stageCls.join(' ')}" data-n="${n}" data-size="${size}" data-phase="${esc(room.progress?.phase || '')}" aria-label="対戦テーブル">
  <div class="tt-progress" aria-label="進行状況">${chips.join('')}</div>
  <div class="tt-arena">
    <div class="tt-table" aria-hidden="true"><span class="tt-table-top"><span class="tt-table-motif">${PAW}</span></span></div>
    <span class="tt-prop tt-prop-plant" aria-hidden="true">${PLANT}</span>
    <span class="tt-prop tt-prop-mug" aria-hidden="true">${MUG}</span>
    <ol class="tt-seats">${lis}</ol>
    ${n === 0 ? '<p class="tt-empty">AIが着席するとここに表示されます</p>' : ''}
  </div>
</section>`;
}
