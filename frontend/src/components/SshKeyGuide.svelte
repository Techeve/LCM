<script>
  // Anleitung, wie der frisch erzeugte private SSH-Schlüssel unter
  // Linux/macOS bzw. Windows abgelegt und benutzt wird. Wird überall dort
  // eingeblendet, wo LCM einen privaten Schlüssel einmalig ausgibt
  // (Admin-Modal in LinuxUsers, Self-Service-Aktivierung).
  import { i18n } from '../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let { username } = $props();

  const file = $derived(`id_ed25519_${username}`);
</script>

<div class="border-top pt-3 mt-3">
  <h3 class="h6">{t('sshKeyGuide.title')}</h3>
  <p class="small text-body-secondary">{t('sshKeyGuide.intro')}</p>

  <h4 class="small fw-semibold mb-1">{t('sshKeyGuide.linuxTitle')}</h4>
  <ol class="small mb-2">
    <li>
      {t('sshKeyGuide.linSaveA')}<code>~/.ssh/</code>{t('sshKeyGuide.linSaveB')}<code>{file}</code>{t('sshKeyGuide.linSaveC')}
    </li>
    <li>
      {t('sshKeyGuide.linPerm')}<br />
      <code>chmod 600 ~/.ssh/{file}</code>
    </li>
    <li>
      {t('sshKeyGuide.connect')}<br />
      <code>ssh -i ~/.ssh/{file} {username}@&lt;server&gt;</code>
    </li>
  </ol>
  <p class="small mb-1">{t('sshKeyGuide.linConfig')}</p>
  <pre class="small bg-body-secondary p-2 rounded mb-3"><code>Host &lt;alias&gt;
  HostName &lt;server-ip&gt;
  User {username}
  IdentityFile ~/.ssh/{file}</code></pre>

  <h4 class="small fw-semibold mb-1">{t('sshKeyGuide.winTitle')}</h4>
  <ol class="small mb-2">
    <li>
      {t('sshKeyGuide.winSaveA')}<code>C:\Users\&lt;name&gt;\.ssh\</code>{t('sshKeyGuide.winSaveB')}<code>{file}</code>{t('sshKeyGuide.winSaveC')}
    </li>
    <li>
      {t('sshKeyGuide.winDefault')}<br />
      <span class="text-body-secondary">C:\Users\&lt;name&gt;\.ssh\<strong>config</strong></span>
      <pre class="small bg-body-secondary p-2 rounded mt-1 mb-0"><code>Host *
  IdentityFile C:\Users\&lt;name&gt;\.ssh\{file}</code></pre>
    </li>
    <li>
      {t('sshKeyGuide.winConnect')}<br />
      <code>ssh {username}@&lt;server&gt;</code>
    </li>
  </ol>
  <p class="small text-body-secondary mb-0">
    {t('sshKeyGuide.winPerm')}
    {t('sshKeyGuide.puttyHint')}
  </p>
</div>
