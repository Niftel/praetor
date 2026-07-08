import React, { useState } from 'react';
import Sidebar from './Sidebar';
import { Outlet } from 'react-router-dom';
import { Menu } from 'lucide-react';
import { getCurrentUser } from '../services/api';

interface LayoutProps {
  onLogout: () => void;
}

const Layout: React.FC<LayoutProps> = ({ onLogout }) => {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const user = getCurrentUser();
  const displayName = user?.username ?? 'Unknown';
  const initial = displayName.charAt(0).toUpperCase() || '?';

  return (
    <div className="min-h-screen bg-slate-50">
      <Sidebar onLogout={onLogout} isOpen={sidebarOpen} onNavigate={() => setSidebarOpen(false)} />

      {/* Mobile drawer backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 bg-black/40 z-20 lg:hidden"
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}

      <div className="lg:ml-64 flex flex-col min-w-0 min-h-screen">
        <header className="bg-white shadow-sm h-16 flex items-center px-4 sm:px-8 justify-between sticky top-0 z-10 border-b border-gray-200">
          <div className="flex items-center gap-3 min-w-0">
            <button
              type="button"
              onClick={() => setSidebarOpen(true)}
              aria-label="Open navigation menu"
              className="lg:hidden text-gray-500 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-brand-500 rounded p-1"
            >
              <Menu size={22} />
            </button>
            <h2 className="text-lg sm:text-xl font-semibold text-gray-800 truncate">Praetor Automation Controller</h2>
          </div>
          <div className="flex items-center gap-2">
            <div
              className="w-8 h-8 rounded-full bg-brand-100 flex items-center justify-center text-brand-700 font-bold border border-brand-200"
              title={displayName}
            >
              {initial}
            </div>
            <span className="hidden sm:inline text-sm font-medium text-gray-700">
              {displayName}
              {user?.is_superuser && <span className="ml-1 text-xs text-gray-400">(superuser)</span>}
            </span>
          </div>
        </header>
        <main className="flex-1 p-4 sm:p-8 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
};

export default Layout;
