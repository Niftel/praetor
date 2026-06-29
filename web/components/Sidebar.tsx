import React from 'react';
import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Rocket,
  FileCode,
  GitBranch,
  Server,
  Key,
  Calendar,
  Users,
  LogOut,
  Building2,
  Shield,
  UserCog
} from 'lucide-react';

interface SidebarProps {
  onLogout?: () => void;
}

interface NavItem {
  name: string;
  path: string;
  icon: React.ReactNode;
}

interface NavSection {
  title?: string;
  items: NavItem[];
}

const Sidebar: React.FC<SidebarProps> = ({ onLogout }) => {
  const navSections: NavSection[] = [
    {
      items: [
        { name: 'Dashboard', path: '/', icon: <LayoutDashboard size={20} /> },
      ]
    },
    {
      title: 'Resources',
      items: [
        { name: 'Jobs', path: '/jobs', icon: <Rocket size={20} /> },
        { name: 'Templates', path: '/templates', icon: <FileCode size={20} /> },
        { name: 'Projects', path: '/projects', icon: <GitBranch size={20} /> },
        { name: 'Inventories', path: '/inventories', icon: <Server size={20} /> },
        { name: 'Credentials', path: '/credentials', icon: <Key size={20} /> },
        { name: 'Schedules', path: '/schedules', icon: <Calendar size={20} /> },
      ]
    },
    {
      title: 'Access',
      items: [
        { name: 'Organizations', path: '/organizations', icon: <Building2 size={20} /> },
        { name: 'Users', path: '/users', icon: <Users size={20} /> },
        { name: 'Teams', path: '/teams', icon: <UserCog size={20} /> },
        { name: 'Roles', path: '/roles', icon: <Shield size={20} /> },
        { name: 'Auth Providers', path: '/auth-providers', icon: <Key size={20} /> },
      ]
    }
  ];

  return (
    <div className="h-screen w-64 bg-slate-900 text-white flex flex-col fixed left-0 top-0 overflow-y-auto z-20 shadow-xl">
      <div className="p-6 border-b border-slate-800">
        <h1 className="text-2xl font-bold tracking-tight flex items-center gap-2">
          <div className="w-8 h-8 bg-brand-500 rounded-md flex items-center justify-center text-white shadow-lg shadow-brand-500/20">
            P
          </div>
          <span className="text-gray-100">Praetor</span>
        </h1>
      </div>

      <nav className="flex-1 px-4 py-4 space-y-4 overflow-y-auto">
        {navSections.map((section, sectionIdx) => (
          <div key={sectionIdx}>
            {section.title && (
              <div className="px-4 py-2 text-xs font-semibold text-slate-500 uppercase tracking-wider">
                {section.title}
              </div>
            )}
            <div className="space-y-1">
              {section.items.map((item) => (
                <NavLink
                  key={item.name}
                  to={item.path}
                  className={({ isActive }) =>
                    `flex items-center px-4 py-2.5 text-sm font-medium rounded-md transition-all duration-200 ${isActive
                      ? 'bg-brand-600 text-white shadow-md shadow-brand-900/20'
                      : 'text-slate-300 hover:bg-slate-800 hover:text-white'
                    }`
                  }
                >
                  <span className="mr-3">{item.icon}</span>
                  {item.name}
                </NavLink>
              ))}
            </div>
          </div>
        ))}
      </nav>

      <div className="p-4 border-t border-slate-800">
        <button
          onClick={onLogout}
          className="flex items-center w-full px-4 py-2 text-sm font-medium text-slate-400 hover:text-white hover:bg-slate-800 rounded-md transition-colors"
        >
          <LogOut size={20} className="mr-3" />
          Sign Out
        </button>
      </div>
    </div>
  );
};

export default Sidebar;