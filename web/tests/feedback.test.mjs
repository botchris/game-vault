import assert from 'node:assert/strict';
import { test } from 'node:test';

// A Web Audio stand-in that records resumes and tones.
class FakeAudio {
  static made = [];
  constructor() { this.state = 'suspended'; this.resumes = 0; this.tones = 0; this.currentTime = 0; this.destination = {}; FakeAudio.made.push(this); }
  resume() { this.resumes++; this.state = 'running'; return Promise.resolve(); }
  createOscillator() { const a = this; return { frequency: {}, connect: (g) => g, start() { a.tones++; }, stop() {} }; }
  createGain() { return { gain: { setValueAtTime() {}, exponentialRampToValueAtTime() {} }, connect: (d) => d }; }
}

test('a read resumes audio the phone suspended (a locked screen), so the beep is not lost', async () => {
  globalThis.AudioContext = FakeAudio;
  Object.defineProperty(globalThis, 'navigator', { value: {}, configurable: true });
  const { signal, unlockAudio } = await import('../src/features/scan/feedback.ts');
  unlockAudio();
  const audio = FakeAudio.made[0];
  audio.state = 'interrupted';
  const before = audio.resumes;
  signal('new', false);
  assert.equal(audio.resumes, before + 1);
  assert.equal(audio.tones, 1);
});
