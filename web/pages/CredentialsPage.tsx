import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Credential, CredentialType } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { Key, Lock, Plus, Loader, ShieldCheck, Copy, Check } from 'lucide-react';

const CredentialsPage = () => {
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [credentialTypes, setCredentialTypes] = useState<CredentialType[]>([]);
  const [loading, setLoading] = useState(true);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [newCred, setNewCred] = useState<Partial<Credential>>({});
  const [selectedTypeId, setSelectedTypeId] = useState<number | null>(null);
  const [automationKey, setAutomationKey] = useState<string>('');
  const [keyCopied, setKeyCopied] = useState(false);

  // Local state for dynamic form fields
  const [formFields, setFormFields] = useState<Record<string, string>>({});

  // Load credentials and credential types on mount
  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        const [credsData, typesData] = await Promise.all([
          api.getCredentials(),
          api.getCredentialTypes()
        ]);
        const creds = credsData || [];
        const types = typesData || [];
        setCredentials(creds);
        setCredentialTypes(types);
        if (types.length > 0) {
          setSelectedTypeId(types[0].id);
        }
        api.getAutomationKey().then(r => setAutomationKey(r?.public_key || '')).catch(() => { });
      } catch (err) {
        console.error('Failed to load credentials', err);
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  const selectedType = credentialTypes.find(t => t.id === selectedTypeId);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newCred.name || !selectedTypeId) return;

    try {
      const credData = {
        name: newCred.name,
        description: newCred.description || '',
        credential_type_id: selectedTypeId,
        organization_id: 1, // Default org
        inputs: formFields
      };

      const created = await api.createCredential(credData);
      setCredentials([...credentials, created]);
      setIsModalOpen(false);
      setNewCred({});
      setFormFields({});
    } catch (err) {
      console.error('Failed to create credential', err);
    }
  };

  const getTypeLabel = (cred: Credential) => {
    const type = credentialTypes.find(t => t.id === cred.credential_type_id);
    return type?.name || 'Unknown';
  };

  const getTypeKind = (cred: Credential) => {
    const type = credentialTypes.find(t => t.id === cred.credential_type_id);
    const n = (type?.name || '').toLowerCase();
    return n.includes('machine') || n.includes('source control') ? 'SSH' : 'Cloud';
  };

  // Parse schema fields from selected credential type
  const getTypeFields = () => {
    if (!selectedType?.inputs) return [];
    try {
      const schema = typeof selectedType.inputs === 'string'
        ? JSON.parse(selectedType.inputs)
        : selectedType.inputs;
      return schema.fields || [];
    } catch {
      return [];
    }
  };

  const renderFields = () => {
    const fields = getTypeFields();
    return fields.map((field: any) => {
      // A multi-line field (e.g. an SSH private key) needs a textarea — pasting a
      // PEM key into a single-line password box is unusable. Keys are pasted
      // visibly (as in AWX), so a secret textarea still renders as a textarea.
      const isTextarea = field.type === 'textarea' || field.multiline;
      return (
        <div key={field.id}>
          <label className="block text-sm font-medium text-gray-700">{field.label || field.id}</label>
          {isTextarea ? (
            <textarea
              rows={6}
              placeholder={field.secret ? '-----BEGIN OPENSSH PRIVATE KEY-----\n...' : ''}
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 focus:ring-brand-500 focus:border-brand-500 font-mono text-xs"
              value={formFields[field.id] || ''}
              onChange={e => setFormFields({ ...formFields, [field.id]: e.target.value })}
            />
          ) : field.secret ? (
            <input
              type="password"
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 focus:ring-brand-500 focus:border-brand-500"
              value={formFields[field.id] || ''}
              onChange={e => setFormFields({ ...formFields, [field.id]: e.target.value })}
            />
          ) : (
            <input
              type="text"
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 focus:ring-brand-500 focus:border-brand-500"
              value={formFields[field.id] || ''}
              onChange={e => setFormFields({ ...formFields, [field.id]: e.target.value })}
            />
          )}
        </div>
      );
    });
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader className="animate-spin text-brand-600" size={32} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900">Credentials</h1>
        <Button onClick={() => setIsModalOpen(true)} icon={<Plus size={16} />}>Add Credential</Button>
      </div>

      {/* Praetor's automation identity: add this public key to a host's
          authorized_keys and Praetor manages it with no per-host credential. */}
      {automationKey && (
        <Card className="border-brand-100 bg-brand-50/40">
          <div className="flex items-start gap-3">
            <div className="p-2 bg-brand-100 rounded-lg shrink-0"><ShieldCheck className="text-brand-600" size={20} /></div>
            <div className="min-w-0 flex-1">
              <h3 className="text-sm font-bold text-gray-900">Praetor automation key</h3>
              <p className="text-xs text-gray-500 mt-0.5 mb-2">
                Add this <b>public</b> key to a host's <code className="text-[11px] bg-gray-100 px-1 rounded">~/.ssh/authorized_keys</code> — via cloud-init, your image, config management, or by hand — and Praetor can run against it with no per-host credential. The matching private key never leaves Praetor.
              </p>
              <div className="flex items-center gap-2">
                <code className="flex-1 min-w-0 block overflow-x-auto whitespace-nowrap bg-white border border-gray-200 rounded-md px-2 py-1.5 text-[11px] font-mono text-gray-700">{automationKey}</code>
                <Button size="sm" variant="secondary" className="shrink-0" icon={keyCopied ? <Check size={14} /> : <Copy size={14} />}
                  onClick={() => { navigator.clipboard.writeText(automationKey); setKeyCopied(true); setTimeout(() => setKeyCopied(false), 2000); }}>
                  {keyCopied ? 'Copied' : 'Copy'}
                </Button>
              </div>
            </div>
          </div>
        </Card>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {credentials.map(cred => (
          <Card key={cred.id} className="hover:shadow-md transition-shadow">
            <div className="flex items-start justify-between mb-4">
              <div className="p-2 bg-brand-50 rounded-lg">
                <Key className="text-brand-600" size={24} />
              </div>
              <Badge variant="info">{getTypeKind(cred)}</Badge>
            </div>

            <h3 className="text-lg font-bold text-gray-900 mb-1">{cred.name}</h3>
            <p className="text-sm text-gray-500 mb-4">{getTypeLabel(cred)}</p>

            <div className="space-y-2 border-t border-gray-100 pt-4">
              <div className="flex justify-between text-sm items-center">
                <span className="text-gray-500">Secret</span>
                <span className="flex items-center text-xs text-green-600 bg-green-50 px-2 py-0.5 rounded-full">
                  <Lock size={10} className="mr-1" /> Encrypted
                </span>
              </div>
            </div>
          </Card>
        ))}
        {credentials.length === 0 && (
          <p className="text-gray-500 col-span-full text-center py-8">No credentials found. Click "Add Credential" to create one.</p>
        )}
      </div>

      <Modal isOpen={isModalOpen} onClose={() => setIsModalOpen(false)} title="New Credential">
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">Name</label>
            <input
              type="text"
              required
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 focus:ring-brand-500 focus:border-brand-500"
              value={newCred.name || ''}
              onChange={e => setNewCred({ ...newCred, name: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Type</label>
            <select
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 focus:ring-brand-500 focus:border-brand-500"
              value={selectedTypeId || ''}
              onChange={e => {
                setSelectedTypeId(Number(e.target.value));
                setFormFields({}); // Reset fields when type changes
              }}
            >
              {credentialTypes.map(t => (
                <option key={t.id} value={t.id}>{t.name}</option>
              ))}
            </select>
          </div>

          <div className="pt-2 border-t border-gray-100 space-y-4">
            {renderFields()}
          </div>

          <div className="mt-5 flex justify-end gap-3 pt-4">
            <Button type="button" variant="secondary" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};

export default CredentialsPage;