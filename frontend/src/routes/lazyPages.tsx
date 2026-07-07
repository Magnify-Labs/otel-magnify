import React from 'react'

export const Dashboard = React.lazy(() => import('../pages/Dashboard'))
export const ConfigDriftDashboard = React.lazy(() => import('../pages/ConfigDriftDashboard'))
export const Workloads = React.lazy(() => import('../pages/Workloads'))
export const WorkloadDetail = React.lazy(() => import('../pages/WorkloadDetail'))
export const Configs = React.lazy(() => import('../pages/Configs'))
export const Alerts = React.lazy(() => import('../pages/Alerts'))
export const Audit = React.lazy(() => import('../pages/Audit'))
export const Profile = React.lazy(() => import('../pages/Profile'))
export const Admin = React.lazy(() => import('../pages/Admin'))
export const SSOProviders = React.lazy(() => import('../pages/admin/sso/Providers'))
export const ProviderEdit = React.lazy(() => import('../pages/admin/sso/ProviderEdit'))
export const Login = React.lazy(() => import('../pages/Login'))
