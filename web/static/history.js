// 履歴を読む位置は、試合の最新状態と独立して保持する。
export class HistoryDrawer {
  constructor({ renderEvent, seatName }) {
    this.renderEvent = renderEvent;
    this.seatName = seatName;
    this.events = [];
    this.unread = 0;
    this.follow = true;
    this.opened = false;
    this.dialog = document.createElement('dialog');
    this.dialog.className = 'game-sheet history-sheet';
    this.dialog.setAttribute('aria-labelledby', 'history-title');
    this.dialog.innerHTML = `<header class="sheet-heading"><div><small>GAME NOTE</small><h2 id="history-title">会話の履歴</h2></div><button class="btn sheet-close" aria-label="履歴を閉じる">閉じる ×</button></header>
      <p class="sheet-hint">履歴を読んでいる間も、試合は進みます。</p>
      <details class="history-filter-panel"><summary>履歴を絞り込む</summary><div class="history-filters"><label>日付<select data-filter="day"><option value="">すべて</option></select></label><label>発言者<select data-filter="speaker"><option value="">全員</option></select></label><label>種類<select data-filter="channel"><option value="all">会話と出来事</option><option value="talk">公開会話</option><option value="event">出来事</option></select></label></div></details>
      <div class="history-scroll" tabindex="0" aria-label="試合の履歴"><div class="timeline"></div></div>
      <footer class="history-footer"><button class="btn btn--primary" data-latest>最新へ戻る ↓</button><span class="history-new" role="status"></span></footer>`;
    document.body.append(this.dialog);
    this.scroller = this.dialog.querySelector('.history-scroll');
    this.list = this.dialog.querySelector('.timeline');
    this.dialog.querySelector('.sheet-close').onclick = () => this.dialog.close();
    this.dialog.querySelector('[data-latest]').onclick = () => this.latest();
    this.dialog.querySelectorAll('select').forEach(select => select.onchange = () => {
      this.follow = true;
      this.render();
    });
    this.scroller.addEventListener('scroll', () => {
      this.follow = this.scroller.scrollHeight - this.scroller.scrollTop - this.scroller.clientHeight < 64;
      if (this.follow) { this.unread = 0; this.updateCount(); }
    });
    this.dialog.addEventListener('close', () => this.trigger?.isConnected && this.trigger.focus({ preventScroll: true }));
    this.dialog.addEventListener('click', e => { if (e.target === this.dialog && e.clientX < this.dialog.getBoundingClientRect().left) this.dialog.close(); });
  }

  update(events, room) {
    // 私信は公開ノートに混ぜず、本人パネルにだけ載せる。
    const visible = events.filter(e => !['owner_advice', 'owner_note'].includes(e.type));
    const old = new Set(this.events.map(e => e.seq));
    const added = visible.filter(e => !old.has(e.seq) && ['talk', 'whisper'].includes(e.type));
    if (this.opened && !this.follow) this.unread += added.length;
    const permissionsChanged = this.room && JSON.stringify([this.room.viewer.view_mode, this.room.viewer.own_seat, this.room.viewer.user_id]) !== JSON.stringify([room.viewer.view_mode, room.viewer.own_seat, room.viewer.user_id]);
    this.events = visible;
    this.room = room;
    this.setOptions('day', [...new Set(visible.map(e => e.day))].map(day => [String(day), `${day}日目`]), 'すべて');
    this.setOptions('speaker', room.seats.map(s => [String(s.agent_idx), `${String(s.agent_idx).padStart(2, '0')} ${this.seatName(s.agent_idx)}`]), '全員');
    const channel = this.dialog.querySelector('[data-filter="channel"]');
    const whisper = channel.querySelector('[value="whisper"]');
    if (visible.some(e => e.type === 'whisper') && !whisper) channel.add(new Option('人狼の囁き', 'whisper'));
    if (!visible.some(e => e.type === 'whisper') && whisper) { if (channel.value === 'whisper') channel.value = 'all'; whisper.remove(); }
    if (this.dialog.open || permissionsChanged) this.render();
    else this.updateCount();
  }

  setOptions(name, entries, first) {
    const select = this.dialog.querySelector(`[data-filter="${name}"]`);
    const value = select.value;
    const signature = JSON.stringify(entries);
    if (select.dataset.signature === signature) return;
    select.replaceChildren(new Option(first, ''), ...entries.map(([v, label]) => new Option(label, v)));
    select.value = entries.some(([v]) => v === value) ? value : '';
    select.dataset.signature = signature;
  }

  render() {
    const day = this.dialog.querySelector('[data-filter="day"]').value;
    const speaker = this.dialog.querySelector('[data-filter="speaker"]').value;
    const channel = this.dialog.querySelector('[data-filter="channel"]').value;
    const anchor = [...this.list.children].find(el => el.getBoundingClientRect().bottom > this.scroller.getBoundingClientRect().top);
    const anchorID = anchor?.dataset.seq;
    const offset = anchor ? anchor.getBoundingClientRect().top - this.scroller.getBoundingClientRect().top : 0;
    const scrollTop = this.scroller.scrollTop;
    const events = this.events.filter(e => (day === '' || String(e.day) === day) && (speaker === '' || String(e.from_idx) === speaker) &&
      (channel === 'all' ? e.type !== 'whisper' : channel === 'event' ? !['talk', 'whisper'].includes(e.type) : e.type === channel));
    let lastDay = null;
    const html = events.map(e => {
      const content = this.renderEvent(e);
      if (!content) return '';
      const heading = e.day !== lastDay ? `<div class="event-day"><span class="sticky">${Number(e.day)}日目</span></div>` : '';
      lastDay = e.day;
      return `<article data-seq="${Number(e.seq)}" tabindex="-1">${heading}${content}</article>`;
    }).join('') || '<p class="empty">この条件の履歴はまだありません。</p>';
    if (this.list.innerHTML !== html) {
      this.list.innerHTML = html;
      if (this.dialog.open) {
        if (this.follow) this.latest();
        else {
          const same = anchorID && this.list.querySelector(`[data-seq="${Number(anchorID)}"]`);
          this.scroller.scrollTop = same ? scrollTop + same.getBoundingClientRect().top - this.scroller.getBoundingClientRect().top - offset : scrollTop;
        }
      }
    }
    this.updateCount();
  }

  updateCount() {
    this.dialog.querySelector('.history-new').textContent = this.unread ? `新しい発言 ${this.unread}件` : '';
  }

  open(trigger, seq = null) {
    this.trigger = trigger;
    if (seq !== null) {
      this.dialog.querySelectorAll('select').forEach(s => { s.value = s.dataset.filter === 'channel' ? (this.events.find(e => e.seq === seq)?.type === 'whisper' ? 'whisper' : 'all') : ''; });
      this.follow = false;
    }
    this.dialog.showModal();
    this.render();
    if (!this.opened || this.follow) this.latest();
    this.opened = true;
    if (seq !== null) {
      const target = this.list.querySelector(`[data-seq="${Number(seq)}"]`);
      target?.scrollIntoView({ block: 'start' });
      target?.focus({ preventScroll: true });
    }
  }

  latest() {
    this.scroller.scrollTop = this.scroller.scrollHeight;
    this.follow = true;
    this.unread = 0;
    this.updateCount();
  }

  destroy() { this.dialog.close(); this.dialog.remove(); }
}
