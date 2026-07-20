import React from 'react';

type PageWidth = 'narrow' | 'default' | 'wide' | 'full';

const widths: Record<PageWidth, string> = {
  narrow: 'max-w-[800px]',
  default: 'max-w-[1060px]',
  wide: 'max-w-[1180px]',
  full: 'max-w-none',
};

interface PageProps {
  children: React.ReactNode;
  width?: PageWidth;
  layout?: 'document' | 'workspace';
  className?: string;
}

/** Canonical content frame for authenticated Praetor routes. */
export function Page({ children, width = 'default', layout = 'document', className = '' }: PageProps) {
  const layoutClass = layout === 'workspace'
    ? 'flex h-full min-h-0 w-full flex-col'
    : `mx-auto w-full px-4 pb-16 pt-5 sm:px-6 lg:px-8 lg:pt-7 ${widths[width]}`;
  return (
    <main className={`${layoutClass} ${className}`}>
      {children}
    </main>
  );
}

interface PageHeaderProps {
  title: React.ReactNode;
  description?: React.ReactNode;
  meta?: React.ReactNode;
  icon?: React.ReactNode;
  actions?: React.ReactNode;
  layout?: 'document' | 'workspace';
  className?: string;
}

/** A consistent page heading with optional context and route-level actions. */
export function PageHeader({ title, description, meta, icon, actions, layout = 'document', className = '' }: PageHeaderProps) {
  const layoutClass = layout === 'workspace'
    ? 'shrink-0 border-b border-line px-4 pb-4 pt-5 sm:px-6'
    : 'mb-6 border-b border-line pb-5';
  return (
    <header className={`${layoutClass} flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between ${className}`}>
      <div className="min-w-0">
        {meta && <div className="mb-1.5 font-mono text-[10.5px] text-dim">{meta}</div>}
        <div className="flex min-w-0 items-start gap-2.5">
          {icon && <span className="mt-0.5 shrink-0 text-acc" aria-hidden="true">{icon}</span>}
          <h1 className="min-w-0 text-[21px] font-semibold leading-tight tracking-[-0.02em] text-ink">{title}</h1>
        </div>
        {description && <p className="mt-2 max-w-[68ch] text-[12.5px] leading-relaxed text-mut">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

interface PageToolbarProps {
  children: React.ReactNode;
  summary?: React.ReactNode;
  className?: string;
}

/** Search, filters, counts, and view controls directly above a resource list. */
export function PageToolbar({ children, summary, className = '' }: PageToolbarProps) {
  return (
    <div className={`mb-3 flex min-h-9 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between ${className}`}>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{children}</div>
      {summary && <div className="shrink-0 font-mono text-[10.5px] tabular-nums text-dim">{summary}</div>}
    </div>
  );
}

export function PageSection({ children, className = '', labelledBy }: { children: React.ReactNode; className?: string; labelledBy?: string }) {
  return <section className={className} aria-labelledby={labelledBy}>{children}</section>;
}
