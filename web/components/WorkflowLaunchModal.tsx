import React, { useEffect, useState } from 'react';
import { Rocket } from 'lucide-react';
import Modal from './ui/Modal';
import Button from './ui/Button';
import { Input, Textarea } from './ui/Input';

export interface WorkflowLaunchOptions {
  extra_vars?: Record<string, unknown>;
  limit?: string;
}

interface Props {
  isOpen: boolean;
  workflowName: string;
  onClose: () => void;
  onLaunch: (options: WorkflowLaunchOptions) => Promise<void>;
}

const WorkflowLaunchModal: React.FC<Props> = ({ isOpen, workflowName, onClose, onLaunch }) => {
  const [variables, setVariables] = useState('');
  const [limit, setLimit] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setVariables('');
    setLimit('');
    setError('');
    setSubmitting(false);
  }, [isOpen]);

  const submit = async () => {
    const options: WorkflowLaunchOptions = {};
    if (variables.trim()) {
      try {
        const parsed = JSON.parse(variables);
        if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
          setError('Variables must be a JSON object.');
          return;
        }
        options.extra_vars = parsed as Record<string, unknown>;
      } catch {
        setError('Variables contain invalid JSON.');
        return;
      }
    }
    if (limit.trim()) options.limit = limit.trim();

    setError('');
    setSubmitting(true);
    try {
      await onLaunch(options);
    } catch (e: any) {
      setError(e?.message || 'Workflow launch failed.');
      setSubmitting(false);
    }
  };

  return (
    <Modal isOpen={isOpen} onClose={submitting ? () => {} : onClose} title={`Launch: ${workflowName}`} size="md">
      <div className="space-y-4">
        <p className="text-sm leading-relaxed text-mut">Inputs are applied to every job node in this workflow. Leave them empty to use each template's saved configuration.</p>
        <Textarea
          label="Variables (JSON)"
          hint="Launch variables override matching template variables."
          rows={6}
          className="font-mono text-sm"
          placeholder={'{\n  "release": "canary"\n}'}
          value={variables}
          onChange={e => setVariables(e.target.value)}
          disabled={submitting}
        />
        <Input
          label="Limit"
          hint="Optional Ansible host pattern, for example web-* or app:&staging."
          className="font-mono"
          placeholder="host pattern"
          value={limit}
          onChange={e => setLimit(e.target.value)}
          disabled={submitting}
        />
        {error && <p role="alert" className="text-sm text-err">{error}</p>}
        <div className="flex justify-end gap-3 pt-1">
          <Button variant="secondary" onClick={onClose} disabled={submitting}>Cancel</Button>
          <Button onClick={submit} disabled={submitting} icon={<Rocket size={14} />}>
            {submitting ? 'Launching…' : 'Launch'}
          </Button>
        </div>
      </div>
    </Modal>
  );
};

export default WorkflowLaunchModal;
