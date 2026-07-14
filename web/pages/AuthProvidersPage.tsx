import React from 'react';
import { Navigate } from 'react-router-dom';

// Auth configuration now lives inside the unified Settings surface (dark
// operator-console overhaul). This route redirects to keep old links working.
const AuthProvidersPage: React.FC = () => <Navigate to="/settings" replace />;

export default AuthProvidersPage;
