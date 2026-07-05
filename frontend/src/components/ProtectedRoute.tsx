import { Navigate } from 'react-router-dom'
import { useStore } from '../store'

export default function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const me = useStore((s) => s.me)
  const sessionChecked = useStore((s) => s.sessionChecked)

  if (!sessionChecked) return null
  if (!me) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}
