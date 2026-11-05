// Kopieren ist der vorgesehene Weg, ein mitgeliefertes Profil oder einen Baustein anzupassen.

import { toast } from '../../components/lcm-toaster';
import { blocksApi, profilesApi, type Block, type Profile } from '../../api/linux-users';
import { errorText, t } from '../../lib/i18n';
import { blockName } from './profile-rules';
import { freeCopyName, freeSlug } from './profile-model';
import { MAX_BLOCK_SLUG } from './block-model';

/** Legt eine Kopie an und liefert deren ID; bei einem Fehler erscheint ein Hinweis und die Rückgabe ist `null`. */
export async function cloneProfile(profile: Profile, all: Profile[]): Promise<number | null> {
  const name = freeCopyName(profile.name, t('profiles.copySuffix'), all.map((p) => p.name));
  try {
    const copy = await profilesApi.clone(profile.id, freeSlug(name, all.map((p) => p.slug)), name);
    toast({ tone: 'ok', title: t('profiles.notice.copied', { name }) });
    return copy.id;
  } catch (error) {
    toast({ tone: 'crit', title: t('profiles.notice.copyFailed'), message: errorText(error) });
    return null;
  }
}

export async function cloneBlock(block: Block, all: Block[]): Promise<number | null> {
  const taken = all.flatMap((b) => [b.name, b.name_en].filter(Boolean));
  const name = freeCopyName(blockName(block), t('profileBlocks.copySuffix'), taken);
  try {
    const copy = await blocksApi.clone(block.id, freeSlug(name, all.map((b) => b.slug), MAX_BLOCK_SLUG), name);
    toast({ tone: 'ok', title: t('profileBlocks.notice.copied', { name }) });
    return copy.id;
  } catch (error) {
    toast({ tone: 'crit', title: t('profileBlocks.notice.copyFailed'), message: errorText(error) });
    return null;
  }
}
