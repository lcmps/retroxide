import { describe, expect, it, vi } from 'vitest';
import { groupDiscs, sheetFileNames, uploadDisc, type DiscApi } from './disc.js';

const cue = `FILE "Game (Track 1).bin" BINARY
  TRACK 01 MODE2/2352
    INDEX 01 00:00:00
FILE "Game (Track 2).bin" BINARY
  TRACK 02 AUDIO
    INDEX 01 00:00:00
`;

describe('disc uploads', () => {
	it('reads the files CUE sheets and GDI files name', () => {
		expect(sheetFileNames('Game.cue', cue)).toEqual(['Game (Track 1).bin', 'Game (Track 2).bin']);
		expect(sheetFileNames('Disc.gdi', '2\n1 0 4 2352 track01.bin 0\n2 450 0 2352 "track 02.raw" 0\n')).toEqual(['track01.bin', 'track 02.raw']);
	});

	it('groups a sheet with its files, matching names regardless of case', async () => {
		const sheet = new File([cue], 'Game.cue');
		const one = new File(['a'], 'game (track 1).BIN');
		const two = new File(['b'], 'Game (Track 2).bin');
		const other = new File(['c'], 'Other.sfc');
		const { discs, others } = await groupDiscs([one, sheet, other, two]);
		expect(others).toEqual([other]);
		expect(discs).toHaveLength(1);
		expect(discs[0].tracks).toEqual([one, two]);
		expect(discs[0].missing).toEqual([]);

		const missing = await groupDiscs([sheet, two]);
		expect(missing.discs[0].missing).toEqual(['Game (Track 1).bin']);
	});

	it('uploads every track as a part, then assembles the disc with the picked names', async () => {
		const sheet = new File([cue], 'Game.cue');
		const one = new File([new Uint8Array(10)], 'game (track 1).bin');
		const two = new File([new Uint8Array(30)], 'Game (Track 2).bin');
		const { discs } = await groupDiscs([sheet, one, two]);
		let parts = 0;
		const api: DiscApi = {
			createSession: vi.fn(async (file: File, part?: boolean) => {
				expect(part).toBe(true);
				return { session_id: `ups_${++parts}`, size: file.size, received: 0, max_chunk_size: 1024 };
			}),
			getSession: vi.fn(),
			sendPart: vi.fn(async (sessionId: string, offset: number, part: Blob, onProgress: (loaded: number) => void) => {
				onProgress(part.size);
				return { session_id: sessionId, size: part.size, received: offset + part.size, max_chunk_size: 1024, part_id: `part-${sessionId}` };
			}),
			uploadWhole: vi.fn(),
			checkDuplicate: vi.fn(),
			assemble: vi.fn(async () => ({ upload_id: 'upl_disc', status: 'processing' as const }))
		};
		const progress: number[] = [];
		const storage = { getItem: () => null, setItem: () => {}, removeItem: () => {} };
		const accepted = await uploadDisc({ ...discs[0], compress: true }, api, { onProgress: (loaded, total) => progress.push(loaded / total) }, { storage });
		expect(accepted.upload_id).toBe('upl_disc');
		expect(api.uploadWhole).not.toHaveBeenCalled();
		expect(api.checkDuplicate).not.toHaveBeenCalled();
		const [name, text, partIDs, compress] = vi.mocked(api.assemble).mock.calls[0];
		expect(name).toBe('Game.cue');
		expect(compress).toBe(true);
		expect(partIDs).toEqual(['part-ups_1', 'part-ups_2']);
		expect(text).toContain('FILE "game (track 1).bin" BINARY');
		expect(progress.at(-1)).toBe(1);
	});

	it('does not assemble a disc cancelled after its last track', async () => {
		const track = new File(['aa'], 'Game (Track 1).bin');
		const controller = new AbortController();
		const api: DiscApi = {
			createSession: vi.fn(async () => ({ session_id: 'ups_1', size: 2, received: 0, max_chunk_size: 8 })),
			getSession: vi.fn(),
			sendPart: vi.fn(async () => {
				controller.abort();
				return { session_id: 'ups_1', size: 2, received: 2, max_chunk_size: 8, part_id: 'part_1' };
			}),
			uploadWhole: vi.fn(),
			checkDuplicate: vi.fn(),
			assemble: vi.fn()
		};
		const upload = uploadDisc({ sheet: new File([cue], 'Game.cue'), text: cue, tracks: [track], missing: [] }, api, { onProgress: vi.fn(), signal: controller.signal });
		await expect(upload).rejects.toMatchObject({ name: 'AbortError' });
		expect(api.assemble).not.toHaveBeenCalled();
	});
});
