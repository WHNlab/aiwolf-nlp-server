// AI人狼バトル！ — 終了試合のイベント履歴を時系列で再生するための純粋関数。
// DOM・通信に触れず、保存済みの Event(seq,type,day,from_idx,to_idx,at) 列から
// 「その時点の部屋表示」と「次イベントまでの間」を計算する。
// 履歴には1イベント時点の部屋進行が保存されないため、day・フェーズ・生死は
// イベント内容から再構成する（recorder.go の記録順序に対応）。

export const REPLAYABLE_STATUS = ['finished', 'aborted', 'closed'];

// 「Over」はターン終了のプロトコル制御語で会話ではないため、表示対象から外す。
// 判定はテキスト完全一致のみ（プロトコル処理や履歴データそのものは変えない）。
export function isHiddenControlEvent(e) {
  return !!e && (e.type === 'talk' || e.type === 'whisper') && e.text === 'Over';
}

// 表示用タイムライン。seq 順に安定化し、制御語イベントを取り除く。
export function visibleTimeline(events) {
  return (Array.isArray(events) ? events.slice() : [])
    .filter(e => e && !isHiddenControlEvent(e))
    .sort((a, b) => (a.seq || 0) - (b.seq || 0));
}

// イベント適用直後の公開フェーズ。vote までは議論中、execute 以降は昼の結果発表。
export function phaseAfterEvent(e) {
  switch (e?.type) {
    case 'vote': return 'day_vote';
    case 'execute':
    case 'medium_result': return 'day_result';
    case 'whisper':
    case 'attack_vote':
    case 'divine_result':
    case 'guard':
    case 'attack': return 'night';
    case 'result':
    case 'game_end': return 'finished';
    case 'talk':
    case 'day':
    case 'game_start': return 'day_discussion';
    default: return null; // owner_advice / owner_note など：フェーズ不変
  }
}

// 指定位置までイベントを適用した部屋表示を合成する。
// revealResult=false の間は役職・勝敗・終了ステータスを隠し、再生中の見た目を
// 実況と同じ「進行中」に揃える。
export function projectRoom(room, timeline, upto, { revealResult = false } = {}) {
  const events = Array.isArray(timeline) ? timeline : [];
  const n = Math.max(0, Math.min(upto, events.length));
  const view = { ...room, seats: (room?.seats || []).map(s => ({ ...s })) };
  view.progress = { ...(room?.progress || {}) };
  const byIdx = new Map(view.seats.map(s => [s.agent_idx, s]));
  // 終了後の部屋データは「最終状態」なので、再生中は生死と日付を最初から積み直す。
  for (const seat of view.seats) seat.alive = true;
  let day = 0;
  let phase = 'starting';
  for (let i = 0; i < n; i++) {
    const e = events[i];
    if (typeof e.day === 'number') day = e.day;
    const next = phaseAfterEvent(e);
    if (next) phase = next;
    if ((e.type === 'execute' || e.type === 'attack') && e.to_idx != null) {
      const guarded = e.type === 'attack' && e.guarded === true;
      const seat = byIdx.get(e.to_idx);
      if (seat && !guarded) seat.alive = false;
    }
  }
  if (day != null) view.day = day;
  view.progress.phase = phase;
  view.progress.active_public_turn = null;
  view.status = revealResult ? room.status : 'running';
  if (!revealResult) {
    for (const seat of view.seats) {
      if (!seat.is_mine) seat.role = '非公開';
    }
  }
  return view;
}

// upto イベント適用後のフェーズだけを返す軽量版（projectRoom と同じ規則）。
export function phaseAt(timeline, upto) {
  const events = Array.isArray(timeline) ? timeline : [];
  const n = Math.max(0, Math.min(upto, events.length));
  let phase = 'starting';
  for (let i = 0; i < n; i++) {
    const next = phaseAfterEvent(events[i]);
    if (next) phase = next;
  }
  return phase;
}

// ステージのフェーズバナーに出す短い進行ラベル。
export function bannerText(e, day) {
  switch (e?.type) {
    case 'game_start': return 'ゲーム開始';
    case 'day': return `${day}日目のはじまり`;
    case 'vote': return '昼・投票';
    case 'execute': return '処刑の発表';
    case 'whisper':
    case 'attack_vote':
    case 'divine_result':
    case 'guard':
    case 'attack': return '夜';
    case 'result':
    case 'game_end': return 'ゲーム終了';
    default: return '';
  }
}

// 1イベントずつ順に再生するときの、イベント表示「前」の待ち時間(ms)。
// 実際の時刻差を圧縮しつつ、フェーズの境目では説得力のある間を取る。
export function replayDelayFor(prev, e, phaseChanged, speed = 1) {
  const s = speed > 0 ? speed : 1;
  let ms;
  if (!prev) {
    ms = 600;
  } else {
    const gap = Date.parse(e?.at || '') - Date.parse(prev?.at || '');
    if (!Number.isFinite(gap) || gap < 0) ms = 800;
    else if (gap < 400) ms = 300 + gap * 0.4;
    else ms = Math.min(2600, 550 + gap * 0.3);
  }
  if (phaseChanged) ms = Math.max(ms, 950);
  return Math.max(120, Math.round(ms / s));
}
