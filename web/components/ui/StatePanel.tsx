import React from 'react';
import { AlertTriangle, Inbox, Loader2 } from 'lucide-react';
import Button from './Button';

interface StatePanelProps {
  title: string;
  description?: React.ReactNode;
  icon?: React.ReactNode;
  action?: React.ReactNode;
  role?: 'status' | 'alert';
  className?: string;
}

/** Shared, bounded feedback surface for empty, error, and blocked route states. */
export function StatePanel({ title, description, icon, action, role = 'status', className = '' }: StatePanelProps) {
  return (
    <div role={role} className={`flex min-h-52 flex-col items-center justify-center rounded-xl border border-line bg-panel px-6 py-10 text-center ${className}`}>
      {icon && <div className="mb-3 text-dim" aria-hidden="true">{icon}</div>}
      <h2 className="text-sm font-semibold text-ink">{title}</h2>
      {description && <p className="mt-1.5 max-w-[58ch] text-[12.5px] leading-relaxed text-mut">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function EmptyState(props: Omit<StatePanelProps, 'icon'> & { icon?: React.ReactNode }) {
  return <StatePanel icon={props.icon ?? <Inbox size={24} />} {...props} />;
}

export function ErrorState({ onRetry, retryLabel = 'Try again', ...props }: Omit<StatePanelProps, 'icon' | 'role' | 'action'> & { onRetry?: () => void; retryLabel?: string }) {
  return (
    <StatePanel
      {...props}
      role="alert"
      icon={<AlertTriangle size={24} className="text-err" />}
      action={onRetry ? <Button variant="secondary" onClick={onRetry}>{retryLabel}</Button> : undefined}
    />
  );
}

export function LoadingState({ label = 'Loading' }: { label?: string }) {
  return (
    <div role="status" aria-live="polite" className="flex min-h-52 items-center justify-center gap-2.5 text-sm text-mut">
      <Loader2 size={18} className="animate-spin text-acc" aria-hidden="true" />
      <span>{label}…</span>
    </div>
  );
}
