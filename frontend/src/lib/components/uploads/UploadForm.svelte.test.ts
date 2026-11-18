import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import UploadForm from './UploadForm.svelte';

/** A drop's DataTransfer: each entry is a file, or a folder when isDirectory is set. */
function dataTransfer(entries: Array<{ file: File; isDirectory?: boolean }>) {
	return {
		files: entries.map((entry) => entry.file),
		items: entries.map((entry) => ({
			kind: 'file',
			getAsFile: () => entry.file,
			webkitGetAsEntry: () => ({ isDirectory: Boolean(entry.isDirectory), isFile: !entry.isDirectory })
		})),
		types: ['Files']
	};
}

const picker = () => screen.getByText('Choose ROM files').closest('label')!;

describe('UploadForm drag and drop', () => {
	it('takes dropped files as the selection, ready to upload', async () => {
		const onupload = vi.fn();
		render(UploadForm, { onupload });
		const files = [new File(['a'], 'a.sfc'), new File(['b'], 'b.gba')];
		await fireEvent.dragOver(picker(), { dataTransfer: dataTransfer([]) });
		expect(picker().classList.contains('dragging')).toBe(true);
		await fireEvent.drop(picker(), { dataTransfer: dataTransfer(files.map((file) => ({ file }))) });
		expect(picker().classList.contains('dragging')).toBe(false);
		expect(screen.getByText('2 files selected')).toBeTruthy();

		await fireEvent.click(screen.getByRole('button', { name: 'Upload 2 ROMs' }));
		expect(onupload).toHaveBeenCalledWith(files, false, undefined);
	});

	it('ignores a dropped folder without failing', async () => {
		render(UploadForm, { onupload: vi.fn() });
		await fireEvent.drop(picker(), { dataTransfer: dataTransfer([{ file: new File([], 'Games'), isDirectory: true }]) });
		expect(screen.getByText('No file selected.')).toBeTruthy();
		expect((screen.getByRole('button', { name: 'Upload ROM' }) as HTMLButtonElement).disabled).toBe(true);
	});
});
