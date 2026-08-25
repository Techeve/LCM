// Gemeinsames Job-Polling: wartet auf den Abschluss eines asynchronen Jobs,
// indem die Job-Historie des Servers gepollt wird. Rückgabe: das fertige
// Job-Objekt (status 'success'/'failed'), oder null, wenn der Job nach dem
// Zeitfenster noch läuft - der Aufrufer darf dann NICHT „abgeschlossen"
// melden, sondern verweist auf den Jobs-Tab. WICHTIG: auch ein fertiger Job
// kann fehlgeschlagen sein (status 'failed') - Erfolgsmeldungen erst nach
// Status-Prüfung.
import { api } from '../api';

export async function waitForJob(serverId, jobId, tries = 20) {
  for (let i = 0; i < tries; i++) {
    await new Promise((r) => setTimeout(r, 1000));
    const hist = (await api.jobs.history(serverId, { page_size: 20 })).items;
    const job = hist.find((j) => j.id === jobId);
    if (job && job.status !== 'running') return job;
  }
  return null;
}
