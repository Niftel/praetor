import React from 'react';
import { Loader } from 'lucide-react';

// Full-page loading spinner used by list/detail pages while their initial data
// loads. Replaces the block that was copy-pasted across ~11 pages.
export const PageSpinner: React.FC = () => (
  <div className="flex items-center justify-center h-64">
    <Loader className="animate-spin text-brand-600" size={32} />
  </div>
);

export default PageSpinner;
