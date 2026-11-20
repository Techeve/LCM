# Checking open ports

On a server's **Security** tab, the **Open ports** card shows which services
the server offers to the network - as LCM sees it. Use it to find services you
should allow, switch off or block in the firewall.

## What LCM checks

- **TCP only, common ports only:** SSH, HTTP/HTTPS, DSM (5000/5001), SMB, NFS,
  rpcbind, FTP, Telnet, databases (MySQL, PostgreSQL, Redis, MongoDB), Docker
  (2375/2376), Kubernetes, Proxmox, RouterOS, MQTT, LDAP, RDP and VNC, plus the
  server's SSH port. UDP services such as SNMP or DNS are left out, a
  connection attempt tells nothing there.
- **Only the server's address:** IPv4 and IPv6 from the host entry; if the
  server reports an IPv6 address itself, that one is checked too. No other
  address can be given.
- **Slowly:** at most four connections at a time, two seconds per port.
  Nothing changes on the server, LCM does not log in.

The scan runs from the network LCM is in. What is reachable from the internet
may differ - for example through a port forward on the router.

## The rating

| Level | Meaning |
|---|---|
| critical | A risky service is reachable: Telnet, Docker without TLS (2375), a database or rpcbind. |
| warning | The port is open but not in the firewall rule set LCM keeps for the server. |
| fine | The port is allowed in the rule set, or it is the SSH port. |

The rating is made when the card is shown, against the current rule set. Allow
a port in the firewall and the warning disappears at once - no new scan
needed. Whether the port is really closed afterwards only the next scan shows.

For Synology DSM and RouterOS, LCM keeps no firewall rule set. There LCM rates
the risky services only.

## Intrusion detection

If CrowdSec or fail2ban with port scan detection runs on the server, the scan
may count as an attack there - in the worst case the server then locks LCM
out. Add the LCM address to the allow list first. For this reason the scan
only runs when you start it, never on a schedule.
