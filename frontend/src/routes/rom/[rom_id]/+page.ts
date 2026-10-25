import type { PageLoad } from './$types';

export const load: PageLoad = ({ params }) => {
  return {
    romId: params.rom_id
  };
};
