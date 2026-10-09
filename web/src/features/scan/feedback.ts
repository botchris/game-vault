// What a read feels and sounds like, so the user keeps scanning without looking at the screen:
// a short high beep for a new box, two low tones for one already in the list. Tones are made with
// Web Audio (no files); vibration where the device has it.

let audio: AudioContext | null = null;

/** Browsers start audio only after a tap: call it from one (opening the camera, unmuting). */
export function unlockAudio() {
  try {
    audio ??= new AudioContext();
    void audio.resume();
  } catch {
    /* no Web Audio: vibration and the flash still tell */
  }
}

export function signal(kind: 'new' | 'repeat', muted: boolean) {
  navigator.vibrate?.(kind === 'new' ? 60 : [40, 60, 40]);
  if (muted || !audio) return;
  const tones = kind === 'new' ? [1320] : [330, 330];
  tones.forEach((frequency, i) => {
    const t0 = audio!.currentTime + i * 0.13;
    const osc = audio!.createOscillator();
    const gain = audio!.createGain();
    osc.type = 'sine';
    osc.frequency.value = frequency;
    gain.gain.setValueAtTime(0.0001, t0);
    gain.gain.exponentialRampToValueAtTime(0.25, t0 + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.1);
    osc.connect(gain).connect(audio!.destination);
    osc.start(t0);
    osc.stop(t0 + 0.11);
  });
}
