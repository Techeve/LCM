import type { ActionLabels } from '../../../components/lcm-server-action';
import { t } from '../../../lib/i18n';

/** Beschriftungen der Erklärblase für Knöpfe, die keine Befehlsvorschau haben. */
export function helpLabels(): ActionLabels {
  return {
    meaning: t('settings.form.meaning'),
    executes: '',
    commands: '',
    packages: '',
    reboot: '',
    reversible: '',
    duration: '',
    yes: '',
    no: '',
    docs: '',
    loading: '',
  };
}
