<script>
  // Betriebssystem-Logo für die Server-Anzeige. Die 256×256-PNGs liegen in
  // src/assets/os (aus offiziellen Vektorquellen gerendert, siehe dortige
  // ATTRIBUTION.md) und werden von Vite gebündelt. Das Logo ist bereits mit
  // festem Rand (68,75 % der Fläche) ins PNG gerendert; hier kommt nur noch
  // der weiße Kreis als Hintergrund dazu.
  //
  // Alle Icons werden identisch dargestellt (fixe Quadratgröße + weißer
  // Kreis), damit sie unabhängig vom Servertyp gleich groß und einheitlich
  // wirken. Der weiße Kreis sorgt zugleich dafür, dass dunkle Logo-Anteile
  // (Proxmox-Spitzen, Tux, Debian-Schriftzug) im Dark Mode sichtbar bleiben.
  import ubuntuIcon from '../assets/os/ubuntu.png';
  import debianIcon from '../assets/os/debian.png';
  import proxmoxIcon from '../assets/os/proxmox.png';
  import linuxIcon from '../assets/os/linux.png';
  // Das LCM-Logo liegt als statisches Asset unter public/ (wie in Navbar/Footer).
  const lcmIcon = '/logo.svg';

  // proxmox: Produkttyp ("pve"/"pbs"/"pmg") - hat Vorrang vor dem OS-Namen,
  // weil Proxmox-Systeme im os-release schlicht als Debian erscheinen.
  // host/port: ist der Server der LCM-Host selbst (Loopback + Standard-Port),
  // zeigen wir das LCM-Logo statt des OS-Logos. Der Port zählt mit: über
  // 127.0.0.1:<hoher Port> gejointe Systeme sind Port-Forwards auf ANDERE
  // Maschinen (NAT/Container) - kein LCM-Host.
  let { os = '', proxmox = '', host = '', port = 22, size = 22 } = $props();

  const proxmoxNames = {
    pve: 'Proxmox VE',
    pbs: 'Proxmox Backup Server',
    pmg: 'Proxmox Mail Gateway',
    pdm: 'Proxmox Datacenter Manager',
  };
  // Hosts, die den LCM-Host selbst bezeichnen.
  const LCM_HOSTS = new Set(['localhost', '127.0.0.1', '::1']);

  // Logo + Anzeigename aus LCM-Host / Proxmox-Typ / OS-Namen ableiten.
  let icon = $derived.by(() => {
    if (LCM_HOSTS.has((host || '').trim().toLowerCase()) && (!port || port === 22)) return { src: lcmIcon, label: 'LCM-Host', lcm: true };
    if (proxmox) return { src: proxmoxIcon, label: proxmoxNames[proxmox] ?? 'Proxmox' };
    const s = (os || '').toLowerCase();
    if (s.includes('routeros') || s.includes('mikrotik')) return { src: linuxIcon, label: 'MikroTik RouterOS' };
    if (s.includes('synology') || s.includes('dsm')) return { src: linuxIcon, label: 'Synology DSM' };
    if (s.includes('ubuntu')) return { src: ubuntuIcon, label: 'Ubuntu' };
    if (s.includes('debian')) return { src: debianIcon, label: 'Debian' };
    return { src: linuxIcon, label: 'Linux' };
  });
</script>

<img class="os-logo" class:os-logo-lcm={icon.lcm} src={icon.src} alt={icon.label} title={icon.label} width={size} height={size} />

<style>
  /* Feste Quadratgröße, weißer Kreis als Hintergrund. KEIN prozentuales
     Padding - Prozent-Padding bezöge sich auf die Elternbreite und würde das
     Bild in breiten Tabellenzellen bis zur Unsichtbarkeit schrumpfen. Der
     Abstand zum Kreisrand steckt bereits im PNG. */
  .os-logo {
    display: inline-block;
    background: #fff;
    border-radius: 50%;
    object-fit: contain;
    flex-shrink: 0;
    vertical-align: middle;
  }
  /* Das LCM-Logo bringt seine eigene (dunkle) Form mit - kein weißer Kreis
     dahinter (der clasht), und etwas größer, damit es aus dem Kreis-Slot
     ausbricht und den hohen Detailgrad zeigt. */
  .os-logo-lcm {
    background: transparent;
    border-radius: 0;
    transform: scale(1.3);
  }
</style>
