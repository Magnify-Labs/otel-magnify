import React, { Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import { createBrowserRouter, RouterProvider, Navigate } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import './i18n'
import './styles/global.css'
import RootLayout from './App'
import Layout from './components/layout/Layout'
import ProtectedRoute from './components/ProtectedRoute'
import Dashboard from './pages/Dashboard'
import ConfigDriftDashboard from './pages/ConfigDriftDashboard'
import Workloads from './pages/Workloads'
import WorkloadDetail from './pages/WorkloadDetail'
import Configs from './pages/Configs'
import Alerts from './pages/Alerts'
import Audit from './pages/Audit'
import Profile from './pages/Profile'
import Admin from './pages/Admin'
import SSOProviders from './pages/admin/sso/Providers'
import ProviderEdit from './pages/admin/sso/ProviderEdit'
import Login from './pages/Login'
import { queryClient } from './api/queryClient'

const router = createBrowserRouter([
  {
    element: <RootLayout />,
    children: [
      {
        element: (
          <ProtectedRoute>
            <Layout />
          </ProtectedRoute>
        ),
        children: [
          { path: '/', element: <Dashboard /> },
          { path: '/config-safety/drift', element: <ConfigDriftDashboard /> },
          { path: '/inventory', element: <Workloads /> },
          { path: '/workloads/:id', element: <WorkloadDetail /> },
          { path: '/configs', element: <Configs /> },
          { path: '/alerts', element: <Alerts /> },
          { path: '/audit', element: <Audit /> },
          { path: '/profile', element: <Profile /> },
          { path: '/admin', element: <Admin /> },
          { path: '/admin/sso/providers', element: <SSOProviders /> },
          { path: '/admin/sso/providers/new', element: <ProviderEdit /> },
          { path: '/admin/sso/providers/:id', element: <ProviderEdit /> },
        ],
      },
      { path: '/login', element: <Login /> },
      { path: '*', element: <Navigate to="/" /> },
    ],
  },
])

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Suspense fallback={null}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </Suspense>
  </React.StrictMode>,
)
