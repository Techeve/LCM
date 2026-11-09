import { css } from 'lit';
import type { TableLabels } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { utilStyles } from '../../components/shared';
import { jobLink } from '../../lib/job-link';
import { t } from '../../lib/i18n';

/** Gemeinsame Stile der Bausteine der Sicherheitsseite. */
export const secStyles = css`
  ${utilStyles}
  :host {
    display: block;
    min-inline-size: 0;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--gap);
    min-inline-size: 0;
  }
  .num {
    font-variant-numeric: tabular-nums;
  }
  a {
    color: var(--accent-text);
    text-decoration: none;
  }
  a:hover {
    text-decoration: underline;
  }
  .summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3) var(--space-5);
  }
  .toolbar .grow {
    flex: 1;
  }
  .toolbar lcm-select {
    min-inline-size: 12rem;
  }
  .title {
    font-weight: var(--fw-semibold);
  }
`;

/** Beschriftungen einer schlichten Tabelle ohne Auswahl, Filterzeile und Spaltenwahl. */
export function plainLabels(caption: string, empty: string): TableLabels {
  return {
    caption,
    filter: '',
    all: '',
    columns: '',
    densityComfortable: '',
    densityCompact: '',
    selectAll: '',
    selectRow: '',
    selected: '',
    clear: '',
    empty,
    sortedAsc: '',
    sortedDesc: '',
  };
}

/** Meldung „gestartet“ mit Sprung zum Job. */
export function jobStartedToast(label: string, jobId: string): void {
  toast({
    tone: 'info',
    title: t('security.job.started', { label }),
    ...jobLink(jobId),
  });
}
