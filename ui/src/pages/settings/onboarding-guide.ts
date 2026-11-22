import { css, html, nothing, type TemplateResult } from 'lit';
import { t } from '../../lib/i18n';
import './system/copy-field';
import { installKeyCommands, switchUserCommand } from './onboarding-model';

/** Die Stile der Anleitung; die Seite, die sie zeigt, bindet sie in ihre Stile ein. */
export const onboardingGuideStyles = css`
  .guide {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .guide p {
    margin: 0;
  }
  .guide h4 {
    margin: 0;
    font-size: var(--fs-m);
    font-weight: var(--fw-semibold);
  }
`;

const FAMILIES = ['debian', 'rhel', 'other'] as const;

/** Befehle gibt es für Debian und die RHEL-Familie; für andere Systeme gelten die von Debian, dort steht nur der Text. */
function familySection(family: (typeof FAMILIES)[number], key: string): TemplateResult {
  return html`<section class="guide" data-family=${family}>
    <h4>${t(`settings.general.key.guide.${family}.title`)}</h4>
    <p class="muted">${t(`settings.general.key.guide.${family}.text`)}</p>
    ${family === 'other' ? nothing : html`<lcm-copy-field block value=${installKeyCommands(key, family)}></lcm-copy-field>`}
  </section>`;
}

/**
 * Anleitung unter dem Onboarding-Schlüssel: wie er für `user` auf einen neuen Server kommt, je Systemfamilie mit
 * Befehlen zum Kopieren. Der Aufrufer setzt sie in seine Karte.
 */
export function onboardingGuide(key: string, user: string): TemplateResult {
  return html`<div class="guide" data-field="key-guide">
    <p class="muted">${t('settings.general.key.guide.intro', { user })}</p>
    <lcm-copy-field label=${t('settings.general.key.guide.switchUser')} value=${switchUserCommand(user)}></lcm-copy-field>
    ${FAMILIES.map((family) => familySection(family, key))}
  </div>`;
}
