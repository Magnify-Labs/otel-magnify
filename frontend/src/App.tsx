import { Outlet, useLocation } from 'react-router-dom'
import { useEffect } from 'react'
import { connectWS, disconnectWS } from './api/websocket'
import { meAPI } from './api/client'
import { useStore } from './store'
import { useTheme } from './hooks/useTheme'

export default function RootLayout() {
  useTheme()
  const setMe = useStore((s) => s.setMe)
  const setSessionChecked = useStore((s) => s.setSessionChecked)
  const location = useLocation()

  useEffect(() => {
    let cancelled = false
    disconnectWS()
    if (location.pathname === '/login') {
      setSessionChecked(true)
      return () => {
        cancelled = true
        disconnectWS()
      }
    }

    setSessionChecked(false)
    meAPI
      .get()
      .then((me) => {
        if (cancelled) return
        setMe(me)
        connectWS()
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setSessionChecked(true)
      })

    return () => {
      cancelled = true
      disconnectWS()
    }
  }, [location.pathname, setMe, setSessionChecked])

  return <Outlet />
}
