import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { BrowserMultiFormatReader, type IScannerControls } from '@zxing/browser';
import { BarcodeFormat, DecodeHintType } from '@zxing/library';
import { validBarcode } from '../../lib/model';

/** The browser's own barcode reader (Shape Detection API): not in TypeScript's DOM types yet. */
interface NativeDetector {
  detect(source: HTMLVideoElement): Promise<{ rawValue: string }[]>;
}
interface NativeDetectorClass {
  new (options: { formats: string[] }): NativeDetector;
  getSupportedFormats(): Promise<string[]>;
}

const NATIVE_FORMATS = ['ean_13', 'upc_a', 'ean_8'];

/**
 * Chrome's BarcodeDetector (Apple's Vision framework on macOS, Google's on Android) reads blurry
 * and small codes far better than a JavaScript decoder. Safari and Firefox lack it: zxing then.
 */
async function nativeDetector(): Promise<NativeDetector | null> {
  const Detector = (window as unknown as { BarcodeDetector?: NativeDetectorClass }).BarcodeDetector;
  if (!Detector) return null;
  try {
    const supported = await Detector.getSupportedFormats();
    const formats = NATIVE_FORMATS.filter((f) => supported.includes(f));
    return formats.includes('ean_13') ? new Detector({ formats }) : null;
  } catch {
    return null;
  }
}

const zxingHints = new Map<DecodeHintType, unknown>([
  [DecodeHintType.POSSIBLE_FORMATS, [BarcodeFormat.EAN_13, BarcodeFormat.UPC_A, BarcodeFormat.EAN_8]],
  // Slower per frame, but finds codes that are small or slightly rotated in the picture.
  [DecodeHintType.TRY_HARDER, true],
]);

/**
 * Live camera barcode reader (rear camera on phones). A code is reported once it is read twice
 * with a valid check digit, which filters out misreads. Cameras need a secure context: localhost
 * or HTTPS (on a phone, through an HTTPS reverse proxy).
 *
 * The camera is asked for a high resolution (by default browsers give 640×480, where a barcode is
 * a few blurry pixels) and continuous focus where it has it. A front or laptop camera is shown
 * mirrored, so the box moves on screen the way it moves in your hand.
 *
 * While paused the picture freezes, so the box that was read stays on screen; `searching` is the
 * code being looked up, shown over it at once (lookups can take seconds).
 */
export default function CameraScanner({ paused, searching = '', onCode }: { paused: boolean; searching?: string; onCode: (code: string) => void }) {
  const { t } = useTranslation();
  const video = useRef<HTMLVideoElement>(null);
  const onCodeRef = useRef(onCode);
  onCodeRef.current = onCode;
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const [error, setError] = useState('');
  const [mirrored, setMirrored] = useState(false);

  useEffect(() => {
    if (!window.isSecureContext || !navigator.mediaDevices) {
      setError(t('scan.camera.insecure'));
      return;
    }
    let cancelled = false;
    let stream: MediaStream | undefined;
    let controls: IScannerControls | undefined;
    let timer: number | undefined;
    let last = '';
    // The box just reported is usually still in front of the camera after you confirm it: its code
    // is ignored until it has been out of sight for a moment, so it is not looked up again.
    let reported = '';
    let reportedSeenAt = 0;

    const handle = (code: string) => {
      if (!validBarcode(code)) return;
      const now = Date.now();
      if (code === reported && now - reportedSeenAt < 1500) {
        reportedSeenAt = now;
        return;
      }
      if (pausedRef.current) return;
      if (code === last) {
        last = '';
        reported = code;
        reportedSeenAt = now;
        navigator.vibrate?.(80);
        onCodeRef.current(code);
      } else {
        last = code;
      }
    };

    const start = async () => {
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: 'environment' }, width: { ideal: 1920 }, height: { ideal: 1080 } },
      });
      if (cancelled) return;
      const track = stream.getVideoTracks()[0];
      // Phones refocus as the box moves; laptop webcams have a fixed focus and ignore this.
      await track?.applyConstraints({ advanced: [{ focusMode: 'continuous' } as MediaTrackConstraintSet] }).catch(() => undefined);
      setMirrored(track?.getSettings().facingMode !== 'environment');

      const v = video.current!;
      const detector = await nativeDetector();
      if (cancelled) return;
      if (detector) {
        v.srcObject = stream;
        await v.play();
        const scan = async () => {
          if (cancelled) return;
          if (!pausedRef.current && v.readyState >= 2) {
            try {
              for (const c of await detector.detect(v)) handle(c.rawValue);
            } catch {
              /* a frame that could not be read: try the next one */
            }
          }
          timer = window.setTimeout(scan, 100);
        };
        scan();
      } else {
        const reader = new BrowserMultiFormatReader(zxingHints, { delayBetweenScanAttempts: 100 });
        controls = await reader.decodeFromStream(stream, v, (result) => result && handle(result.getText()));
        if (cancelled) controls.stop();
      }
    };

    start().catch((e: unknown) => {
      if (cancelled) return;
      setError(e instanceof Error && e.name === 'NotAllowedError' ? t('scan.camera.denied') : t('scan.camera.error', { error: String(e) }));
    });

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      controls?.stop();
      stream?.getTracks().forEach((tr) => tr.stop());
    };
  }, [t]);

  // Freeze the picture on the box that was read; frames are ignored while paused anyway.
  useEffect(() => {
    const v = video.current;
    if (!v || !v.srcObject) return;
    if (paused) v.pause();
    else v.play().catch(() => undefined);
  }, [paused]);

  if (error) return <div className="alert warn">{error}</div>;
  return (
    <div className={`camera ${paused ? 'paused' : ''} ${searching ? 'found' : ''} ${mirrored ? 'mirrored' : ''}`}>
      <video ref={video} muted playsInline />
      <div className="camera-guide" aria-hidden="true" />
      {searching ? (
        <p className="camera-found" role="status">
          <span className="spinner" aria-hidden="true" />
          <span><code>{searching}</code> · {t('scan.searching')}</span>
        </p>
      ) : (
        <p className="camera-hint">{paused ? t('scan.camera.paused') : t(mirrored ? 'scan.camera.hintLaptop' : 'scan.camera.hint')}</p>
      )}
    </div>
  );
}
