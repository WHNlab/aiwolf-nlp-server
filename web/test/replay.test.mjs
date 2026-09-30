import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const source = await readFile(new URL('../static/replay.js', import.meta.url), 'utf8');
const { isHiddenControlEvent, visibleTimeline, phaseAfterEvent, phaseAt, projectRoom, bannerText, replayDelayFor } =
  await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));

const seats = Array.from({ length: 5 }, (_, i) => ({ seat_id: `s${i + 1}`, agent_idx: i + 1, game_name: `AI${i + 1}`, alive: true, role: 'WEREWOLF', is_mine: false }));
const roomFixture = () => ({ status: 'finished', day: 3, win_side: 'VILLAGER', seats: seats.map(s => ({ ...s })), viewer: { own_seat: 's1' }, progress: { phase: 'finished' } });

const timeline = [
  { seq: 1, type: 'game_start', day: 0, at: '2026-09-30T10:00:00Z' },
  { seq: 2, type: 'day', day: 0, at: '2026-09-30T10:00:01Z' },
  { seq: 3, type: 'talk', day: 0, from_idx: 1, text: 'おはよう', at: '2026-09-30T10:00:05Z' },
  { seq: 4, type: 'talk', day: 0, from_idx: 2, text: 'Over', at: '2026-09-30T10:00:06Z' },
  { seq: 5, type: 'vote', day: 0, from_idx: 1, to_idx: 3, at: '2026-09-30T10:00:20Z' },
  { seq: 6, type: 'execute', day: 0, to_idx: 3, text: '追放されました', at: '2026-09-30T10:00:25Z' },
  { seq: 7, type: 'attack', day: 0, to_idx: 2, guarded: false, text: '襲撃されました', at: '2026-09-30T10:00:40Z' },
  { seq: 8, type: 'attack', day: 1, to_idx: 4, guarded: true, text: '襲撃は防がれました', at: '2026-09-30T10:01:00Z' },
  { seq: 9, type: 'game_end', day: 3, team: 'VILLAGER', text: 'ゲームが終了しました', at: '2026-09-30T10:02:00Z' },
];

test('Over はタイムラインから除外し、Skip と本文は残す', () => {
  assert.equal(isHiddenControlEvent({ type: 'talk', text: 'Over' }), true);
  assert.equal(isHiddenControlEvent({ type: 'whisper', text: 'Over' }), true);
  assert.equal(isHiddenControlEvent({ type: 'talk', text: 'over' }), false);
  assert.equal(isHiddenControlEvent({ type: 'talk', text: 'Overおつかれ' }), false);
  const list = visibleTimeline(timeline);
  assert.equal(list.length, 8);
  assert.equal(list.some(e => e.text === 'Over'), false);
});

test('フェーズはイベントから再構成される', () => {
  assert.equal(phaseAfterEvent({ type: 'game_start' }), 'day_discussion');
  assert.equal(phaseAfterEvent({ type: 'vote' }), 'day_vote');
  assert.equal(phaseAfterEvent({ type: 'execute' }), 'day_result');
  assert.equal(phaseAfterEvent({ type: 'attack' }), 'night');
  assert.equal(phaseAfterEvent({ type: 'game_end' }), 'finished');
  assert.equal(phaseAfterEvent({ type: 'owner_note' }), null);
  assert.equal(phaseAt(timeline, 6), 'day_result');
  assert.equal(phaseAt(timeline, 8), 'night');
  assert.equal(phaseAt(timeline, 9), 'finished');
});

test('再生途中は進行中表示で死亡だけ反映し、最後に実データへ戻る', () => {
  const room = roomFixture();
  const mid = projectRoom(room, timeline, 7);
  assert.equal(mid.status, 'running');
  assert.equal(mid.progress.phase, 'night');
  assert.equal(mid.seats.find(s => s.agent_idx === 3).alive, false);
  assert.equal(mid.seats.find(s => s.agent_idx === 2).alive, false);
  assert.equal(mid.seats.find(s => s.agent_idx === 4).alive, true, 'guard成功は死亡しない');
  assert.equal(mid.seats.every(s => s.role === '非公開'), true, '公開前は役職を出さない');
  const end = projectRoom(room, timeline, 9, { revealResult: true });
  assert.equal(end.status, 'finished');
  assert.equal(end.seats[0].role, 'WEREWOLF');
  assert.equal(end.win_side, 'VILLAGER');
});

test('実データは書き換えない', () => {
  const room = roomFixture();
  projectRoom(room, timeline, 7);
  assert.equal(room.seats.every(s => s.alive), true);
  assert.equal(room.status, 'finished');
});

test('間は時刻差を圧縮し、フェーズ境目では最低の間を取る', () => {
  const a = { at: '2026-09-30T10:00:00Z' };
  const b = { at: '2026-09-30T10:00:00.300Z' };
  const normal = replayDelayFor(a, b, false, 1);
  const boundary = replayDelayFor(a, b, true, 1);
  assert.ok(normal < boundary);
  assert.ok(boundary >= 900);
  const faster = replayDelayFor(a, b, false, 2);
  assert.ok(faster < normal);
  assert.ok(replayDelayFor(null, a, false, 1) <= 600);
});

test('バナー文言', () => {
  assert.equal(bannerText({ type: 'day' }, 2), '2日目のはじまり');
  assert.equal(bannerText({ type: 'talk' }, 0), '');
});
