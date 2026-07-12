import React from 'react';

interface BadgeProps {
  children: React.ReactNode;
  variant?: 'success' | 'warning' | 'error' | 'info' | 'neutral';
  /** Show a leading status dot — use only when the badge conveys live state. */
  dot?: boolean;
}

const Badge: React.FC<BadgeProps> = ({ children, variant = 'neutral', dot = false }) => {
  // Ring-based tokens read as calmer, more "system" than solid pill fills.
  const styles = {
    success: 'bg-ok/10 text-ok ring-ok/25',
    warning: 'bg-changed/10 text-changed ring-changed/25',
    error: 'bg-err/10 text-err ring-err/25',
    info: 'bg-run/10 text-run ring-run/25',
    neutral: 'bg-white/5 text-mut ring-white/10',
  };

  const dotColor = {
    success: 'bg-ok',
    warning: 'bg-changed',
    error: 'bg-err',
    info: 'bg-run',
    neutral: 'bg-dim',
  };

  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-medium ring-1 ring-inset ${styles[variant]}`}
    >
      {dot && <span className={`w-1.5 h-1.5 rounded-full ${dotColor[variant]}`} />}
      {children}
    </span>
  );
};

export default Badge;
