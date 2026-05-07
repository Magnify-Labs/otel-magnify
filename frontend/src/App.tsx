import { Outlet } from 'react-router-dom'
import { useEffect } from 'react'
import { connectWS, disconnectWS } from './api/websocket'
import { meAPI } from './api/client'
import { useStore } from './store'
import { useTheme } from './hooks/useTheme'

export default function RootLayout() {
  useTheme()
  const setMe = useStore((s) => s.setMe)

  useEffect(() => {
    connectWS()
    // Skip the boot-time hydration when there is no token: RootLayout mounts on
    // /login too, and a 401 here would trip the axios interceptor into a
    // window.location = '/login' reload loop.
    if (localStorage.getItem('token')) {
      meAPI
        .get()
        .then(setMe)
        .catch(() => {})
    }
    return () => disconnectWS()
  }, [setMe])

  return <Outlet />
}
