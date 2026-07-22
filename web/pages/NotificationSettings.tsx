import React, { useEffect, useMemo, useState } from 'react';
import { Bell, CheckCircle2, Plus, Send, Trash2 } from 'lucide-react';
import { api, unwrap, type ResourceCapabilities } from '../services/api';
import type { NotificationField, NotificationTarget, NotificationType, Organization } from '../types';
import { Button, EmptyState, ErrorState, FormActions, FormErrorSummary, Input, LoadingState, Modal, PageHeader, SecretField, Select, Textarea, confirmDialog, toast } from '../components/ui';

const EMPTY_CAPABILITIES: ResourceCapabilities = {
  view: false, manage: false, use: false, execute: false, update: false, approve: false,
};

const displayType = (value: string) => value === 'pagerduty' ? 'PagerDuty' : value.charAt(0).toUpperCase() + value.slice(1);

const NotificationSettings: React.FC = () => {
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [types, setTypes] = useState<NotificationType[]>([]);
  const [organizationId, setOrganizationId] = useState<number>(0);
  const [targets, setTargets] = useState<NotificationTarget[]>([]);
  const [capabilities, setCapabilities] = useState<ResourceCapabilities>(EMPTY_CAPABILITIES);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);

  useEffect(() => {
    let current = true;
    Promise.all([api.getOrganizations(), api.getNotificationTypes()])
      .then(([orgRows, typeRows]) => {
        if (!current) return;
        const orgs = unwrap<Organization>(orgRows);
        setOrganizations(orgs);
        setTypes(unwrap<NotificationType>(typeRows));
        setOrganizationId(orgs[0]?.id ?? 0);
      })
      .catch((error) => current && setLoadError(error.message || 'Notification settings could not be loaded.'))
      .finally(() => current && setLoading(false));
    return () => { current = false; };
  }, []);

  const loadTargets = async (orgId: number) => {
    if (!orgId) {
      setTargets([]);
      setCapabilities(EMPTY_CAPABILITIES);
      return;
    }
    setLoading(true);
    setLoadError('');
    try {
      const [targetRows, caps] = await Promise.all([
        api.getNotificationTemplates(orgId),
        api.getCapabilities('organization', orgId),
      ]);
      setTargets(unwrap<NotificationTarget>(targetRows));
      setCapabilities(caps);
    } catch (error: any) {
      setLoadError(error.message || 'Notification targets could not be loaded.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadTargets(organizationId); }, [organizationId]);

  const selectedOrg = organizations.find(org => org.id === organizationId);

  const testTarget = async (target: NotificationTarget) => {
    setTestingId(target.id);
    try {
      await api.testNotificationTemplate(target.id);
      toast.success(`Test notification delivered through ${target.name}.`);
    } catch (error: any) {
      toast.error(error.message || 'Test notification could not be delivered.');
    } finally {
      setTestingId(null);
    }
  };

  const deleteTarget = async (target: NotificationTarget) => {
    const confirmed = await confirmDialog(`Delete “${target.name}”? Existing template attachments using this target will also be removed.`, {
      title: 'Delete notification target?', confirmText: 'Delete target', destructive: true,
    });
    if (!confirmed) return;
    try {
      await api.deleteNotificationTemplate(target.id);
      toast.success(`Deleted ${target.name}.`);
      await loadTargets(organizationId);
    } catch (error: any) {
      toast.error(error.message || 'Notification target could not be deleted.');
    }
  };

  return (
    <div className="flex min-h-full flex-col">
      <PageHeader
        layout="workspace"
        title="Notifications"
        description="External destinations for automation lifecycle and approval events. Stored credentials are write-only."
        actions={capabilities.manage && targets.length > 0 ? <Button icon={<Plus size={15} />} onClick={() => setCreateOpen(true)} disabled={!organizationId}>Create target</Button> : undefined}
      />

      <div className="border-b border-line px-4 py-3 sm:px-6">
        <Select
          label="Organization"
          value={organizationId || ''}
          onChange={event => setOrganizationId(Number(event.target.value))}
          wrapperClassName="max-w-[340px]"
          disabled={organizations.length === 0}
        >
          {organizations.length === 0 && <option value="">No organizations available</option>}
          {organizations.map(org => <option key={org.id} value={org.id}>{org.name}</option>)}
        </Select>
      </div>

      <div className="flex-1 px-4 py-5 sm:px-6">
        {loading ? <LoadingState label="Loading notification targets" /> : loadError ? (
          <ErrorState title="Notification targets are unavailable" description={loadError} onRetry={() => loadTargets(organizationId)} />
        ) : !organizationId ? (
          <EmptyState title="No organization available" description="Join or create an organization before configuring notification targets." />
        ) : targets.length === 0 ? (
          <EmptyState
            icon={<Bell size={24} />}
            title="No notification targets"
            description={capabilities.manage ? `Create a target for ${selectedOrg?.name ?? 'this organization'}, then send a test before attaching it to automation events.` : 'An organization administrator must configure notification targets.'}
            action={capabilities.manage ? <Button icon={<Plus size={15} />} onClick={() => setCreateOpen(true)}>Create target</Button> : undefined}
          />
        ) : (
          <section aria-label="Notification targets" className="overflow-hidden rounded-xl border border-line bg-panel">
            <div className="grid grid-cols-[minmax(0,1fr)_150px_auto] gap-4 border-b border-line px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.12em] text-dim max-[720px]:hidden">
              <span>Target</span><span>Backend</span><span className="text-right">Actions</span>
            </div>
            {targets.map(target => (
              <div key={target.id} className="flex items-center gap-4 border-b border-line px-4 py-3 last:border-b-0 max-[720px]:items-start max-[720px]:flex-col">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[13px] font-medium text-ink">{target.name}</div>
                  <div className="mt-1 flex items-center gap-1.5 font-mono text-[10.5px] text-ok"><CheckCircle2 size={12} /> configured · secrets hidden</div>
                </div>
                <span className="w-[150px] shrink-0 font-mono text-[11.5px] text-mut max-[720px]:w-auto">{displayType(target.notification_type)}</span>
                {capabilities.manage && (
                  <div className="flex shrink-0 items-center gap-1.5 max-[720px]:w-full">
                    <Button size="sm" variant="secondary" icon={<Send size={13} />} disabled={testingId !== null} onClick={() => testTarget(target)}>
                      {testingId === target.id ? 'Sending…' : 'Send test'}
                    </Button>
                    <Button size="sm" variant="ghost" icon={<Trash2 size={13} />} onClick={() => deleteTarget(target)} aria-label={`Delete ${target.name}`}>Delete</Button>
                  </div>
                )}
              </div>
            ))}
          </section>
        )}
      </div>

      <CreateTargetModal
        open={createOpen}
        organizationId={organizationId}
        types={types}
        close={() => setCreateOpen(false)}
        created={async () => { setCreateOpen(false); await loadTargets(organizationId); }}
      />
    </div>
  );
};

const CreateTargetModal: React.FC<{
  open: boolean;
  organizationId: number;
  types: NotificationType[];
  close: () => void;
  created: () => Promise<void>;
}> = ({ open, organizationId, types, close, created }) => {
  const [name, setName] = useState('');
  const [type, setType] = useState('');
  const [config, setConfig] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    const initialType = types[0]?.type ?? '';
    setName('');
    setType(initialType);
    setConfig(Object.fromEntries((types[0]?.fields ?? []).filter(field => field.default).map(field => [field.id, field.default!] )));
    setErrors([]);
  }, [open, types]);

  const selected = useMemo(() => types.find(entry => entry.type === type), [type, types]);
  const selectType = (next: string) => {
    const backend = types.find(entry => entry.type === next);
    setType(next);
    setConfig(Object.fromEntries((backend?.fields ?? []).filter(field => field.default).map(field => [field.id, field.default!] )));
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    const nextErrors: string[] = [];
    if (!name.trim()) nextErrors.push('Enter a target name.');
    if (!selected) nextErrors.push('Choose a notification backend.');
    for (const field of selected?.fields ?? []) {
      if (!config[field.id]?.trim() && !field.default) nextErrors.push(`Enter ${field.label.toLowerCase()}.`);
    }
    setErrors(nextErrors);
    if (nextErrors.length) return;
    setSubmitting(true);
    try {
      await api.createNotificationTemplate({ organization_id: organizationId, name: name.trim(), notification_type: type, config });
      toast.success(`Created ${name.trim()}. Send a test before attaching it to events.`);
      await created();
    } catch (error: any) {
      setErrors([error.message || 'Notification target could not be created.']);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal isOpen={open} onClose={close} title="Create notification target" size="lg">
      <form onSubmit={save} className="space-y-4">
        <FormErrorSummary errors={errors} />
        <div className="grid grid-cols-2 gap-4 max-[560px]:grid-cols-1">
          <Input label="Name" required value={name} onChange={event => setName(event.target.value)} placeholder="Platform alerts" />
          <Select label="Backend" required value={type} onChange={event => selectType(event.target.value)}>
            {types.map(entry => <option key={entry.type} value={entry.type}>{displayType(entry.type)}</option>)}
          </Select>
        </div>
        {(selected?.fields ?? []).map(field => <DynamicField key={field.id} field={field} value={config[field.id] ?? ''} setValue={value => setConfig(current => ({ ...current, [field.id]: value }))} />)}
        <p className="text-xs leading-relaxed text-mut">Secret fields are encrypted when saved and are never returned by the API. Public destinations must use HTTPS; internal destinations require an explicit server allowlist.</p>
        <FormActions onCancel={close} submitting={submitting} submitLabel="Create target" />
      </form>
    </Modal>
  );
};

const DynamicField: React.FC<{ field: NotificationField; value: string; setValue: (value: string) => void }> = ({ field, value, setValue }) => {
  const common = { label: field.label, required: !field.default, value, onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setValue(event.target.value) };
  if (field.secret) return <SecretField {...common} multiline={field.type === 'textarea'} hint="Write-only. Praetor will not display this value again." />;
  if (field.type === 'textarea') return <Textarea {...common} rows={5} />;
  return <Input {...common} type={field.type === 'password' ? 'password' : 'text'} />;
};

export default NotificationSettings;
