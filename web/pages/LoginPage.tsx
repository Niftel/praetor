import React, { useState } from 'react';
import Button from '../components/ui/Button';
import { Lock } from 'lucide-react';
import { api, setAuthToken } from '../services/api';

interface LoginPageProps {
  onLogin: () => void;
}

const LoginPage: React.FC<LoginPageProps> = ({ onLogin }) => {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('password');
  const [isLoading, setIsLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true);
    try {
      const data = await api.login({ username, password });
      setAuthToken(data.token);
      onLogin();
    } catch (err) {
      console.error(err);
      alert('Login failed. Please check your credentials.');
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-slate-900 flex items-center justify-center p-4">
      <div className="bg-white rounded-lg shadow-xl w-full max-w-md overflow-hidden">
        <div className="p-8 pb-6 text-center border-b border-gray-100 bg-gray-50">
          <div className="w-12 h-12 bg-brand-600 rounded-lg flex items-center justify-center text-white mx-auto mb-4 shadow-lg shadow-brand-500/30">
            <span className="font-bold text-2xl">P</span>
          </div>
          <h1 className="text-2xl font-bold text-gray-900">Welcome to Praetor</h1>
          <p className="text-gray-500 mt-2 text-sm">Sign in to access the automation controller</p>
        </div>

        <div className="p-8">
          <form onSubmit={handleSubmit} className="space-y-5">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Username</label>
              <input
                type="text"
                required
                className="w-full rounded-md border-gray-300 shadow-sm border p-2.5 focus:ring-brand-500 focus:border-brand-500 transition-colors"
                value={username}
                onChange={e => setUsername(e.target.value)}
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Password</label>
              <div className="relative">
                <input
                  type="password"
                  required
                  className="w-full rounded-md border-gray-300 shadow-sm border p-2.5 pl-10 focus:ring-brand-500 focus:border-brand-500 transition-colors"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                />
                <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                  <Lock className="h-4 w-4 text-gray-400" />
                </div>
              </div>
            </div>

            <div className="flex items-center justify-between text-sm">
              <label className="flex items-center text-gray-600 cursor-pointer">
                <input type="checkbox" className="rounded border-gray-300 text-brand-600 focus:ring-brand-500 mr-2" />
                Remember me
              </label>
              <a href="#" className="font-medium text-brand-600 hover:text-brand-500">
                Forgot password?
              </a>
            </div>

            <Button
              type="submit"
              className="w-full justify-center"
              size="lg"
              disabled={isLoading}
            >
              {isLoading ? 'Signing in...' : 'Sign In'}
            </Button>
          </form>
        </div>

        <div className="px-8 py-4 bg-gray-50 border-t border-gray-100 text-center text-xs text-gray-400">
          &copy; 2023 Praetor Automation. All rights reserved.
        </div>
      </div>
    </div>
  );
};

export default LoginPage;