import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { BrowserMultiFormatReader, type IScannerControls } from '@zxing/browser';
import { BarcodeFormat, DecodeHintType } from '@zxing/library';
import { validBarcode } from '../../lib/model';

const hints = new Map([[DecodeHintType.POSSIBLE_FORMATS, [BarcodeFormat.EAN_13, BarcodeFormat.UPC_A, BarcodeFormat.EAN_8]]]);

/**
 * Live camera barcode reader (rear camera on phones). A code is reported once it is read twice in
 * a row with a valid check digit, which filters out misreads. Cameras need a secure context:
 * localhost or HTTPS (on a phone, through an HTTPS reverse proxy).
 */
export default function CameraScanner({ paused, onCode }: { paused: boolean; onCode: (code: string) => void }) {
  const { t } = useTranslation();
  const video = useRef<HTMLVideoElement>(null);
  const onCodeRef = useRef(onCode);
  onCodeRef.current = onCode;
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const [error, setError] = useState('');

  useEffect(() => {
    if (!window.isSecureContext) {
      setError(t('scan.camera.insecure'));
      return;
    }
    let controls: IScannerControls | undefined;
    let cancelled = false;
    let last = '';
    const reader = new BrowserMultiFormatReader(hints, { delayBetweenScanAttempts: 150 });
    reader
      .decodeFromConstraints({ video: { facingMode: { ideal: 'environment' } } }, video.current!, (result) => {
        if (!result || pausedRef.current) return;
        const code = result.getText();
        if (!validBarcode(code)) return;
        if (code === last) {
          last = '';
          navigator.vibrate?.(80);
          onCodeRef.current(code);
        } else {
          last = code;
        }
      })
      .then((c) => {
        if (cancelled) c.stop();
        else controls = c;
      })
      .catch((e: unknown) => setError(e instanceof Error && e.name === 'NotAllowedError' ? t('scan.camera.denied') : t('scan.camera.error', { error: String(e) })));
    return () => {
      cancelled = true;
      controls?.stop();
    };
  }, [t]);

  if (error) return <div className="alert warn">{error}</div>;
  return (
    <div className={`camera ${paused ? 'paused' : ''}`}>
      <video ref={video} muted playsInline />
      <div className="camera-guide" aria-hidden="true" />
      <p className="camera-hint">{paused ? t('scan.camera.paused') : t('scan.camera.hint')}</p>
    </div>
  );
}
