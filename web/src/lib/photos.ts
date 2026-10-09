import { UNAUTHENTICATED_EVENT, baseUrl } from '../api/client';
import { insertSegment, readExifSegment, resetOrientation } from './exif';

const MAX_SIDE = 2560;
const THUMB_SIDE = 400;

/** A photo ready to upload: the reduced JPEG, with the original's metadata, and its thumbnail. */
export interface PreparedPhoto {
  photo: Blob;
  thumb: Blob;
}

/** The browser cannot decode the picked file (HEIC outside Safari, a broken file…). */
export class UnreadablePhotoError extends Error {}

/** Reduces a picked image to at most 2560 px (JPEG 0.85) plus a 400 px thumbnail (JPEG 0.8). */
export async function preparePhoto(file: File): Promise<PreparedPhoto> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
  } catch {
    throw new UnreadablePhotoError(file.name);
  }
  try {
    const photo = await encode(bitmap, MAX_SIDE, 0.85);
    const thumb = await encode(bitmap, THUMB_SIDE, 0.8);
    // The metadata (date, camera, location) is kept on purpose: it is the user's own record.
    const exif = readExifSegment(new Uint8Array(await file.arrayBuffer()));
    if (!exif) return { photo, thumb };
    const withExif = insertSegment(new Uint8Array(await photo.arrayBuffer()), resetOrientation(exif));
    return { photo: new Blob([withExif], { type: 'image/jpeg' }), thumb };
  } finally {
    bitmap.close();
  }
}

async function encode(bitmap: ImageBitmap, maxSide: number, quality: number): Promise<Blob> {
  const scale = Math.min(1, maxSide / Math.max(bitmap.width, bitmap.height));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(bitmap.width * scale));
  canvas.height = Math.max(1, Math.round(bitmap.height * scale));
  const ctx = canvas.getContext('2d')!;
  ctx.fillStyle = '#fff'; // transparent PNGs would turn black in a JPEG
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  return new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('the image could not be encoded'))), 'image/jpeg', quality));
}

/** The server's answer to an upload. */
export interface UploadedPhoto {
  id: string;
  takenAt?: Date;
}

/** Uploads a prepared photo; XMLHttpRequest because fetch cannot report upload progress. */
export function uploadPhoto(p: PreparedPhoto, onProgress: (fraction: number) => void): Promise<UploadedPhoto> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${baseUrl}/media/photos`);
    xhr.withCredentials = true;
    xhr.setRequestHeader('X-Gamevault-Upload', '1');
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
    xhr.onerror = () => reject(new Error(xhr.statusText || 'network error'));
    xhr.onload = () => {
      if (xhr.status === 401) window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT));
      if (xhr.status !== 200) {
        reject(new Error(xhr.responseText.trim() || `HTTP ${xhr.status}`));
        return;
      }
      const res = JSON.parse(xhr.responseText) as { id: string; takenAt?: string };
      resolve({ id: res.id, takenAt: res.takenAt ? new Date(res.takenAt) : undefined });
    };
    const form = new FormData();
    form.append('photo', p.photo, 'photo.jpg');
    form.append('thumb', p.thumb, 'thumb.jpg');
    xhr.send(form);
  });
}
