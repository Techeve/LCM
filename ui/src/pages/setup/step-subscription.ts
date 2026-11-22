import { html, type TemplateResult } from 'lit';
import '../settings/subscription';
import type { SettingsSubscription } from '../settings/subscription';
import { t } from '../../lib/i18n';
import { SetupStep } from './setup-step';

/**
 * Schritt „Subscription und Kanal“: die Seite der Subscription eingebettet. Wer einen Key hat, aktiviert ihn (die Seite
 * empfiehlt dann den Enterprise-Kanal); sonst geht es in den Beta-Kanal oder bei Community bleiben. Die Kanalwahl wählt nur
 * aus: Mit „Weiter“ stellt der Schritt auf den gewählten Kanal um (bei unveränderter Wahl geht er nur weiter), „Überspringen“
 * lässt den Kanal, wie er ist. Lässt sich überspringen.
 */
export class SetupSubscription extends SetupStep {
  readonly step = 'subscription' as const;

  protected override saveLabel(): string {
    return t('setup.next');
  }

  protected override async save(): Promise<void> {
    await this.renderRoot.querySelector<SettingsSubscription>('lcm-settings-subscription')?.applyPicked();
    await this.advance();
  }

  protected override body(): TemplateResult {
    return html`<lcm-settings-subscription embedded></lcm-settings-subscription>`;
  }
}
customElements.define('lcm-setup-subscription', SetupSubscription);
