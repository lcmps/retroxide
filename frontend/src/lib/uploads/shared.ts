import { uploadStatus } from '#lib/api/client.js';
import { discApi, uploadDisc } from './disc.js';
import { UploadQueue } from './queue.svelte.js';
import { serverApi, uploadResumable } from './resumable.js';

/**
 * The app's one upload queue. It lives outside any page so uploads carry on, and stay visible,
 * while the user moves around the app; signing out cancels it. Large files go in parts that
 * survive a dropped connection and resume when added again.
 */
export const uploadQueue = new UploadQueue(
	(file, onProgress, onReconnecting, signal, bios) => uploadResumable(file, serverApi, { onProgress, onReconnecting, signal, bios }),
	uploadStatus,
	{ sendDisc: (disc, onProgress, onReconnecting, signal) => uploadDisc(disc, discApi, { onProgress, onReconnecting, signal }) }
);
