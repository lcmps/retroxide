import { assembleDisc, type UploadSession } from '#lib/api/client.js';
import type { UploadAcceptedResponse } from '#lib/api/types.js';
import { serverApi, uploadPart, type ResumableApi, type ResumableCallbacks, type ResumableEnvironment } from './resumable.js';

/** A disc picked as its sheet (CUE or GDI) and the files that sheet names. */
export type DiscSelection = {
	sheet: File;
	/** The sheet's text, read once when grouping. */
	text: string;
	tracks: File[];
	/** Files the sheet names that were not selected. */
	missing: string[];
	/** Whether to compress the disc to CHD once stored. */
	compress?: boolean;
};

const SHEET = /\.(cue|gdi)$/i;

/** Lists the files a CUE sheet or GDI file names, in order. */
export function sheetFileNames(name: string, text: string): string[] {
	const names: string[] = [];
	const lines = text.replace(/^\uFEFF/, '').split(/\r?\n/);
	if (/\.gdi$/i.test(name)) {
		for (const line of lines.slice(1)) {
			const match = line.trim().match(/^\d+\s+\d+\s+\d+\s+\d+\s+("([^"]+)"|(\S+))/);
			if (match) names.push(match[2] ?? match[3]);
		}
	} else {
		for (const line of lines) {
			const match = line.trim().match(/^FILE\s+("([^"]+)"|(\S+))\s+\S+$/i);
			if (match) names.push(match[2] ?? match[3]);
		}
	}
	return [...new Set(names)];
}

/**
 * Splits picked files into discs (each sheet with the files it names) and the remaining files,
 * which upload on their own. Names match regardless of case, as file systems often differ.
 */
export async function groupDiscs(files: File[]): Promise<{ discs: DiscSelection[]; others: File[] }> {
	const sheets = files.filter((file) => SHEET.test(file.name));
	const used = new Set<File>(sheets);
	const discs: DiscSelection[] = [];
	for (const sheet of sheets) {
		const text = await sheet.text();
		const tracks: File[] = [];
		const missing: string[] = [];
		for (const name of sheetFileNames(sheet.name, text)) {
			const match = files.find((file) => !SHEET.test(file.name) && file.name.toLowerCase() === name.toLowerCase());
			if (match) {
				tracks.push(match);
				used.add(match);
			} else {
				missing.push(name);
			}
		}
		discs.push({ sheet, text, tracks, missing });
	}
	return { discs, others: files.filter((file) => !used.has(file)) };
}

export type DiscApi = ResumableApi & {
	assemble(sheetName: string, sheet: string, parts: string[], compress?: boolean): Promise<UploadAcceptedResponse>;
};

/** The server API for disc uploads. */
export const discApi: DiscApi = { ...serverApi, assemble: assembleDisc };

/**
 * Uploads a disc: each track file as a resumable part, one after another, then the sheet to
 * assemble them. Progress covers all of the disc's files.
 */
export async function uploadDisc(
	disc: DiscSelection,
	api: DiscApi,
	{ onProgress, onReconnecting }: ResumableCallbacks,
	environment: ResumableEnvironment = {}
): Promise<UploadAcceptedResponse> {
	const total = disc.tracks.reduce((sum, file) => sum + file.size, 0);
	let finished = 0;
	const parts: string[] = [];
	for (const track of disc.tracks) {
		const before = finished;
		parts.push(
			await uploadPart(track, api, { onProgress: (loaded) => onProgress(before + loaded, total), onReconnecting }, environment)
		);
		finished += track.size;
	}
	// The sheet names files as they were picked; the server matches them exactly.
	const sheet = renameSheetFiles(disc.text, disc.tracks);
	return api.assemble(disc.sheet.name, sheet, parts, disc.compress ?? false);
}

/** Rewrites a sheet's file names to the picked files' exact names (which may differ in case). */
function renameSheetFiles(text: string, tracks: File[]): string {
	let result = text;
	for (const track of tracks) {
		const pattern = new RegExp(track.name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi');
		result = result.replace(pattern, track.name);
	}
	return result;
}

export type { UploadSession };
