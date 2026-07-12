import React from 'react';
import { Loader2 } from 'lucide-react';

// Full-page loading indicator used by list/detail pages while their initial data
// loads. Replaces the block that was copy-pasted across ~11 pages.
export const PageSpinner: React.FC<{ label?: string }> = ({ label = 'Loading' }) => (
  <div className="flex flex-col items-center justify-center h-64 gap-3 text-mut">
    <Loader2 className="animate-spin text-acc" size={28} />
    <span className="text-sm">{label}…</span>
  </div>
);

export default PageSpinner;
