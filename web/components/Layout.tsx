import React from 'react';
import Sidebar from './Sidebar';
import { Outlet } from 'react-router-dom';

interface LayoutProps {
  onLogout: () => void;
}

const Layout: React.FC<LayoutProps> = ({ onLogout }) => {
  return (
    <div className="flex min-h-screen bg-slate-50">
      <Sidebar onLogout={onLogout} />
      <div className="ml-64 flex-1 flex flex-col min-w-0">
        <header className="bg-white shadow-sm h-16 flex items-center px-8 justify-between sticky top-0 z-10 border-b border-gray-200">
          <h2 className="text-xl font-semibold text-gray-800">Praetor Automation Controller</h2>
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 rounded-full bg-brand-100 flex items-center justify-center text-brand-700 font-bold border border-brand-200">
                A
              </div>
              <span className="text-sm font-medium text-gray-700">Admin User</span>
            </div>
          </div>
        </header>
        <main className="flex-1 p-8 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
};

export default Layout;