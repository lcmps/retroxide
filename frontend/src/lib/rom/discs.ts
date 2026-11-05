import type { RomDiscEntry } from '#lib/api/types.js';

/** The disc chosen, or the first when none is chosen or it no longer exists; null without discs. */
export function pickDisc(discs: RomDiscEntry[], selected: number | null): RomDiscEntry | null {
  return discs.find((entry) => entry.number === selected) ?? discs[0] ?? null;
}

/**
 * How a round of compression went, given the discs (by rom_file_id) that were waiting before the
 * latest refresh: still running while any is waiting, compressed only if all became CHDs.
 */
export function compressionOutcome(wasRequested: string[], discs: RomDiscEntry[]): 'running' | 'compressed' | 'failed' {
  const requested = discs.filter((entry) => wasRequested.includes(entry.rom_file_id));
  if (requested.some((entry) => entry.disc?.compression_requested)) return 'running';
  const allCompressed = requested.length === wasRequested.length && requested.every((entry) => entry.disc?.format === 'chd');
  return allCompressed ? 'compressed' : 'failed';
}

/** Adds the discs now waiting to those already seen waiting this round, so earlier finishers are not forgotten. */
export function rememberRequested(known: string[], discs: RomDiscEntry[]): string[] {
  const waiting = discs.filter((entry) => entry.disc?.compression_requested).map((entry) => entry.rom_file_id);
  return [...new Set([...known, ...waiting])];
}
