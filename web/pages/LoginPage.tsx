import React, { useState } from 'react';
import Button from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { api, setAuthToken } from '../services/api';
import { toast } from '../components/ui/toast';

interface LoginPageProps {
  onLogin: () => void;
}

const LoginPage: React.FC<LoginPageProps> = ({ onLogin }) => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
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
      toast.error('Login failed. Please check your credentials.');
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
            <Input
              label="Username"
              type="text"
              required
              autoComplete="username"
              value={username}
              onChange={e => setUsername(e.target.value)}
            />

            <Input
              label="Password"
              type="password"
              required
              autoComplete="current-password"
              value={password}
              onChange={e => setPassword(e.target.value)}
            />

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
          Praetor Automation Platform
        </div>
      </div>
    </div>
  );
};

export default LoginPage;