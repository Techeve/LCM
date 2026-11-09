// Texte der Speicherleiste (`lcm-save-bar`), gemeinsam für alle Seiten, die ohne Fenster speichern.

import type { SaveBarLabels } from '../components/lcm-save-bar';
import { t } from './i18n';

/** `save` beschriftet den Knopf; ein leeres `discard` blendet „Verwerfen“ aus. */
export function saveBarLabels(save = t('settings.form.save'), discard = t('settings.form.discard')): SaveBarLabels {
  return {
    region: t('settings.form.barLabel'),
    unsaved: t('settings.form.unsaved'),
    hint: t('settings.form.shortcut'),
    save,
    discard,
    jump: t('settings.form.jump'),
    fieldError: t('settings.form.fieldError'),
  };
}
