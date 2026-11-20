import { LitElement, css, html } from 'lit';
import '../../components/lcm-button.js';
import '../../components/lcm-card.js';
import '../../components/lcm-combobox.js';
import '../../components/lcm-explainer.js';
import '../../components/lcm-input.js';
import '../../components/lcm-side-nav.js';
import '../../components/lcm-switch.js';
import { baseStyles } from '../../components/shared.js';
import { appNav } from '../app-frame.js';
import { catalogStyles } from '../catalog-styles.js';
import { settingsSections } from '../text.js';
import { followChange } from '../follow-change.js';

export class CatSettings extends LitElement {
  static override properties = { current: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .frame {
        container-type: inline-size;
      }
      .layout {
        display: grid;
        grid-template-columns: 15rem minmax(0, 1fr) auto;
        gap: var(--space-6);
        align-items: start;
      }
      @container (max-width: 70rem) {
        .layout {
          grid-template-columns: 13rem minmax(0, 1fr);
        }
        lcm-explainer {
          grid-column: 2;
        }
      }
      h1 {
        margin: 0 0 var(--space-1);
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .content {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
        min-inline-size: 0;
      }
      .switches {
        display: flex;
        flex-direction: column;
        gap: var(--space-5);
      }
      .two {
        display: grid;
        grid-template-columns: 1.6fr 1fr auto;
        gap: var(--space-4);
        align-items: end;
      }
      lcm-explainer p {
        margin: 0 0 var(--space-3);
      }
    `,
  ];
  declare current: string;
  constructor() {
    super();
    this.current = 'alarme';
  }

  override render() {
    return html`<section>
      <h2>Einstellungen</h2>
      <p class="lead">Seitenmenü links, neu gegliedert in sechs Abschnitte. Rechts die Erklärspalte der Seite. Schalter erklären „an“ und „aus“ beim Überfahren.</p>
      <div class="frame">
        ${appNav('settings')}
        <div class="page">
          <div class="layout">
            <lcm-side-nav label="Einstellungen" .sections=${settingsSections} current=${this.current} @lcm-navigate=${(e: CustomEvent<{ id: string }>) => { e.preventDefault(); this.current = e.detail.id; }}></lcm-side-nav>
            <div class="content">
              <div>
                <h1>Alarme</h1>
                <p class="muted">Wann LCM dir Bescheid gibt und wohin.</p>
              </div>
              <lcm-card level="2" heading="Auslöser" subtitle="Ein Alarm entsteht, wenn der Zustand eines Servers kippt.">
                <div class="switches">
                  <lcm-switch label="Bei „kritisch“ alarmieren" description="Sofort, sobald ein Server auf kritisch springt." checked on-label="An:" off-label="Aus:" on-text="Du bekommst sofort eine Meldung, wenn ein Server kritisch wird." off-text="Kritische Server erscheinen nur im Dashboard." @lcm-change=${followChange}></lcm-switch>
                  <lcm-switch label="Bei „Warnung“ alarmieren" description="Gesammelt einmal am Tag." on-label="An:" off-label="Aus:" on-text="Warnungen kommen einmal täglich um 08:00 als Zusammenfassung." off-text="Warnungen erscheinen nur im Dashboard." @lcm-change=${followChange}></lcm-switch>
                  <lcm-switch label="Wenn LCM handlungsunfähig wird" description="Agent oder SSH nicht erreichbar." checked @lcm-change=${followChange}></lcm-switch>
                </div>
              </lcm-card>
              <lcm-card level="2" heading="Empfänger">
                <div class="two">
                  <lcm-input label="E-Mail-Adresse" type="email" width="l" value="betrieb@techeve.de" icon="bell"></lcm-input>
                  <lcm-combobox label="Gruppe filtern" value="all" empty-text="Keine Gruppe" .options=${[{ value: 'all', label: 'Alle Gruppen' }, { value: 'prod', label: 'Produktion' }, { value: 'db', label: 'Datenbanken' }]}></lcm-combobox>
                  <lcm-button variant="primary" icon="plus">Hinzufügen</lcm-button>
                </div>
              </lcm-card>
            </div>
            <lcm-explainer topic="catalog-settings-alerts" heading="Was sind Alarme?" docs-href="#doku-alarme" docs-label="Mehr in der Doku" collapse-label="Erklärung einklappen" expand-label="Erklärung ausklappen">
              <p>Alarme sind Nachrichten von LCM an dich. Sie entstehen aus der Ampel: Springt ein Server auf Warnung oder kritisch, prüft LCM, ob dazu ein Alarm eingerichtet ist.</p>
              <p><strong>Tipp:</strong> Lege Alarme für „kritisch“ immer an. Warnungen eignen sich besser als tägliche Zusammenfassung.</p>
            </lcm-explainer>
          </div>
        </div>
      </div>
    </section>`;
  }
}
customElements.define('cat-settings', CatSettings);
