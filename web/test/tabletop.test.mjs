import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const source = await readFile(new URL('../static/tabletop.js', import.meta.url), 'utf8');
const { stageHtml, latestSpeakerIdx } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
const seats = Array.from({ length: 13 }, (_, i) => ({ seat_id: `s${i + 1}`, agent_idx: i + 1, game_name: `AI${i + 1}`, alive: true, role: '非公開' }));
const fixture = () => ({ status: 'running', day: 2, seats: seats.map(s => ({ ...s })), viewer: { own_seat: 's7', view_mode: 'agent' }, progress: { phase: 'day_discussion', active_public_turn: { agent_idx: 4, state: 'waiting' } } });
const seatMarkup = html => [...html.matchAll(/<li\b[^>]*data-side="([^"]+)"[^>]*>[\s\S]*?<\/li>/g)];

test('本人が07でも手前になり、死亡で席番号や位置が変わらない', () => {
  const room = fixture();
  const before = seatMarkup(stageHtml(room));
  const positions = list => list.map(m => [m[0].match(/data-seat-id="([^"]+)"/)[1], m[1], m[0].match(/--m-row:([^;]+)/)[1]]);
  assert.equal(before.find(m => m[1] === 'bottom')[0].match(/data-agent-idx="(\d+)"/)[1], '7');
  room.seats[3].alive = false;
  assert.deepEqual(positions(seatMarkup(stageHtml(room))), positions(before));
});

test('公開発言者と応答待ちを区別し、囁き・私信では発言者を変えない', () => {
  const events = [{ seq: 1, type: 'talk', from_idx: 3 }, { seq: 2, type: 'whisper', from_idx: 9 }, { seq: 3, type: 'owner_note', from_idx: 7 }];
  assert.equal(latestSpeakerIdx(events), 3);
  const html = stageHtml(fixture(), events);
  assert.match(html, /03 AI3[^\"]*最新の発言者/);
  assert.match(html, /04 AI4[^\"]*応答待ち/);
  assert.doesNotMatch(html, /09 AI9[^\"]*最新の発言者/);
});

test('参加者の名前・席IDをHTMLとして実行させない', () => {
  const room = fixture();
  room.seats[0].game_name = '<script>alert("x")</script>';
  room.seats[0].seat_id = '" autofocus onfocus="alert(1)';
  const html = stageHtml(room);
  assert.doesNotMatch(html, /<script>|data-seat-id="" autofocus/);
  assert.match(html, /&lt;script&gt;/);
  assert.match(html, /data-seat-id="&quot; autofocus/);
});

test('役職が解放されてもロボットの座席色は変わらない', () => {
  const room = fixture();
  const colors = html => [...html.matchAll(/--rb:([^;]+)/g)].map(m => m[1]);
  const before = colors(stageHtml(room));
  room.seats.forEach((s, i) => { s.role = i % 2 ? 'WEREWOLF' : 'SEER'; });
  assert.deepEqual(colors(stageHtml(room)), before);
});
