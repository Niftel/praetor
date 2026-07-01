// Host connection details are stored as ansible_* keys inside a host's free-form
// `variables` JSON. These helpers split those known connection keys out into
// first-class form fields while preserving any other ("extra") vars untouched,
// and merge them back for saving.

export interface HostConnection {
  ansible_host: string;
  ansible_port: string; // kept as a string in the form; coerced to a number on save
  ansible_user: string;
  ansible_connection: string;
  ansible_python_interpreter: string;
}

export const CONNECTION_KEYS = [
  'ansible_host',
  'ansible_port',
  'ansible_user',
  'ansible_connection',
  'ansible_python_interpreter',
];

export const emptyConnection = (): HostConnection => ({
  ansible_host: '',
  ansible_port: '',
  ansible_user: '',
  ansible_connection: '',
  ansible_python_interpreter: '',
});

// parseVars normalises a host's variables (which may arrive as a JSON string or
// an object) into a plain object.
export const parseVars = (raw: any): Record<string, any> => {
  if (!raw) return {};
  if (typeof raw === 'string') { try { return JSON.parse(raw || '{}'); } catch { return {}; } }
  if (typeof raw === 'object') return raw;
  return {};
};

// splitConnection separates the known connection keys from everything else.
export const splitConnection = (raw: any): { conn: HostConnection; extra: Record<string, any> } => {
  const v = parseVars(raw);
  const conn: HostConnection = {
    ansible_host: v.ansible_host != null ? String(v.ansible_host) : '',
    ansible_port: v.ansible_port != null ? String(v.ansible_port) : '',
    ansible_user: v.ansible_user != null ? String(v.ansible_user) : '',
    ansible_connection: v.ansible_connection != null ? String(v.ansible_connection) : '',
    ansible_python_interpreter: v.ansible_python_interpreter != null ? String(v.ansible_python_interpreter) : '',
  };
  const extra: Record<string, any> = {};
  for (const [k, val] of Object.entries(v)) {
    if (!CONNECTION_KEYS.includes(k)) extra[k] = val;
  }
  return { conn, extra };
};

// mergeConnection rebuilds the variables object: extras first, then any non-empty
// connection fields. Empty fields are omitted so we don't write blank ansible_*
// keys (which would override sensible ansible defaults like ansible_host=<name>).
export const mergeConnection = (conn: HostConnection, extra: Record<string, any>): Record<string, any> => {
  const out: Record<string, any> = { ...extra };
  const set = (k: keyof HostConnection, v: string) => { if (v.trim()) out[k] = v.trim(); };
  set('ansible_host', conn.ansible_host);
  if (conn.ansible_port.trim()) {
    const n = Number(conn.ansible_port);
    out.ansible_port = Number.isFinite(n) && conn.ansible_port.trim() !== '' ? n : conn.ansible_port.trim();
  }
  set('ansible_user', conn.ansible_user);
  set('ansible_connection', conn.ansible_connection);
  set('ansible_python_interpreter', conn.ansible_python_interpreter);
  return out;
};
