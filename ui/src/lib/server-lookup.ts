// `/servers/by-name/web03`: den Namen über die API in die Server-ID auflösen.

import { api } from './api';

interface ServerRef {
  id: number;
  name: string;
}

type ServerList = ServerRef[] | { items: ServerRef[] };

/** Zieladresse für einen Servernamen; unbekannte Namen landen in der Suche der Serverliste. */
export async function serverPathByName(name: string): Promise<string> {
  const result = await api.get<ServerList>(`/api/v1/servers?name=${encodeURIComponent(name)}`);
  const servers = Array.isArray(result) ? result : result.items;
  const server = servers.find((candidate) => candidate.name === name);
  return server ? `/servers/${server.id}` : `/servers?search=${encodeURIComponent(name)}`;
}
