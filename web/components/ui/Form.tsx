import React, { useEffect, useId, useRef } from 'react';
import { AlertCircle, Loader2 } from 'lucide-react';
import Button from './Button';
import { Input, Textarea } from './Input';
import { confirmDialog } from './toast';

export function FormSection({ title, description, children, className = '' }: { title?: string; description?: React.ReactNode; children: React.ReactNode; className?: string }) {
  const titleId = useId();
  return (
    <section aria-labelledby={title ? titleId : undefined} className={`space-y-4 border-t border-line pt-4 first:border-t-0 first:pt-0 ${className}`}>
      {(title || description) && <div>{title && <h3 id={titleId} className="text-sm font-semibold text-ink">{title}</h3>}{description && <p className="mt-1 text-xs leading-relaxed text-mut">{description}</p>}</div>}
      {children}
    </section>
  );
}

export function FormErrorSummary({ title = 'Check the form', errors }: { title?: string; errors: string[] }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => { if (errors.length) ref.current?.focus(); }, [errors]);
  if (!errors.length) return null;
  return (
    <div ref={ref} tabIndex={-1} role="alert" className="rounded-lg border border-err/30 bg-err/10 p-3 text-sm text-err focus:outline-none">
      <div className="flex items-center gap-2 font-medium"><AlertCircle size={16} aria-hidden="true" />{title}</div>
      <ul className="mt-1.5 list-disc space-y-1 pl-6 text-xs">{errors.map(error => <li key={error}>{error}</li>)}</ul>
    </div>
  );
}

export function FormActions({ onCancel, submitting = false, submitLabel = 'Save', cancelLabel = 'Cancel', disabled = false }: { onCancel: () => void; submitting?: boolean; submitLabel?: string; cancelLabel?: string; disabled?: boolean }) {
  return (
    <div className="flex justify-end gap-3 border-t border-line pt-4">
      <Button type="button" variant="secondary" onClick={onCancel} disabled={submitting}>{cancelLabel}</Button>
      <Button type="submit" disabled={disabled || submitting} aria-busy={submitting} icon={submitting ? <Loader2 size={14} className="animate-spin" /> : undefined}>
        {submitting ? `${submitLabel}…` : submitLabel}
      </Button>
    </div>
  );
}

/** Warns on browser exit and provides one shared guard for cancel/close actions. */
export function useDirtyFormGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', beforeUnload);
    return () => window.removeEventListener('beforeunload', beforeUnload);
  }, [dirty]);

  return async () => !dirty || confirmDialog('Discard your unsaved changes?', {
    title: 'Discard changes?',
    confirmText: 'Discard changes',
    destructive: true,
  });
}

type SecretFieldProps = {
  label: string;
  value: string;
  onChange: React.ChangeEventHandler<HTMLInputElement | HTMLTextAreaElement>;
  multiline?: boolean;
  placeholder?: string;
  error?: string;
};

/** Write-only secret control. It intentionally has no stored-value/default-value API. */
export function SecretField({ label, value, onChange, multiline = false, placeholder, error }: SecretFieldProps) {
  const hint = 'Write-only. Leave blank to keep the stored value.';
  return multiline
    ? <Textarea label={label} value={value} onChange={onChange} rows={6} placeholder={placeholder} error={error} hint={hint} autoComplete="off" spellCheck={false} className="font-mono text-xs" />
    : <Input label={label} type="password" value={value} onChange={onChange} placeholder={placeholder} error={error} hint={hint} autoComplete="new-password" spellCheck={false} />;
}
