import React from 'react';

export function DataValue({ children, muted = false, className = '' }: { children: React.ReactNode; muted?: boolean; className?: string }) {
  return <span className={`font-mono tabular-nums ${muted ? 'text-mut' : 'text-ink2'} ${className}`}>{children}</span>;
}

export function TimestampValue({ value, fallback = '—', className = '' }: { value?: string | null; fallback?: string; className?: string }) {
  if (!value) return <DataValue muted className={className}>{fallback}</DataValue>;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return <DataValue muted className={className}>{fallback}</DataValue>;
  return <time dateTime={date.toISOString()} className={`font-mono tabular-nums text-mut ${className}`}>{date.toLocaleString()}</time>;
}

type StatusTone = 'success' | 'warning' | 'error' | 'info' | 'neutral';
const toneClasses: Record<StatusTone, string> = {
  success: 'text-ok',
  warning: 'text-changed',
  error: 'text-err',
  info: 'text-run',
  neutral: 'text-mut',
};

export function StatusValue({ children, tone = 'neutral', live = false, className = '' }: { children: React.ReactNode; tone?: StatusTone; live?: boolean; className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 ${toneClasses[tone]} ${className}`}>
      {live && <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />}
      <span>{children}</span>
    </span>
  );
}
