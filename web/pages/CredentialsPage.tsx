import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Credential, CredentialType } from '../types';
import Card from '../components/ui/Card';
import { Input, Textarea, Select } from '../components/ui/Input';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { Key, Lock, Plus, Loader } from 'lucide-react';

const CredentialsPage = () => {
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [credentialTypes, setCredentialTypes] = useState<CredentialType[]>([]);
  const [orgs, setOrgs] = useState<any[]>([]);
  const [orgId, setOrgId] = useState<number | ''>('');
  const [loading, setLoading] = useState(true);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [newCred, setNewCred] = useState<Partial<Credential>>({});
  const [selectedTypeId, setSelectedTypeId] = useState<number | null>(null);

  // Local state for dynamic form fields
  const [formFields, setFormFields] = useState<Record<string, string>>({});

  // Load credentials and credential types on mount
  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        const [credsData, typesData, orgsData] = await Promise.all([
          api.getCredentials(),
          api.getCredentialTypes(),
          api.getOrganizations().catch(() => [])
        ]);
        const creds = credsData || [];
        const types = typesData || [];
        const orgList = orgsData?.items || orgsData || [];
        setCredentials(creds);
        setCredentialTypes(types);
        setOrgs(orgList);
        if (orgList.length > 0) setOrgId(orgList[0].id);
        if (types.length > 0) {
          setSelectedTypeId(types[0].id);
        }
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
    if (!newCred.name || !selectedTypeId || orgId === '') return;

    try {
      const credData = {
        name: newCred.name,
        description: newCred.description || '',
        credential_type_id: selectedTypeId,
        organization_id: orgId,
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
      const label = field.label || field.id;
      return isTextarea ? (
        <Textarea
          key={field.id}
          label={label}
          rows={6}
          placeholder={field.secret ? '-----BEGIN OPENSSH PRIVATE KEY-----\n...' : ''}
          className="font-mono text-xs"
          value={formFields[field.id] || ''}
          onChange={e => setFormFields({ ...formFields, [field.id]: e.target.value })}
        />
      ) : (
        <Input
          key={field.id}
          label={label}
          type={field.secret ? 'password' : 'text'}
          value={formFields[field.id] || ''}
          onChange={e => setFormFields({ ...formFields, [field.id]: e.target.value })}
        />
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
          <Select
            label="Organization"
            value={orgId}
            onChange={e => setOrgId(Number(e.target.value))}
          >
            <option value="">Select organization…</option>
            {orgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
          </Select>
          <Input
            label="Name"
            type="text"
            required
            value={newCred.name || ''}
            onChange={e => setNewCred({ ...newCred, name: e.target.value })}
          />
          <Select
            label="Type"
            value={selectedTypeId || ''}
            onChange={e => {
              setSelectedTypeId(Number(e.target.value));
              setFormFields({}); // Reset fields when type changes
            }}
          >
            {credentialTypes.map(t => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </Select>

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