/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useRef, useState } from 'react'
import { toast } from 'sonner'
import { isPasswordVerificationRequiredError } from '@/lib/secure-verification'
import { verifyPassword } from '../api'

type ApiCall = (() => Promise<unknown>) | null

/**
 * Mirrors `useSecureVerification` but for login-password re-verification.
 *
 * Flow: wrap an api call with `withPasswordVerification`. If the backend
 * responds with a PASSWORD_VERIFICATION_REQUIRED 403, the password dialog
 * opens; on submit the password is verified (setting a short-lived session)
 * and the original api call is retried. The retry may itself trigger the
 * 2FA/Passkey flow (`useSecureVerification`) when both gates are enabled.
 */
export function usePasswordVerification() {
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const apiCallRef = useRef<ApiCall>(null)

  const withPasswordVerification = useCallback(
    async (apiCall: () => Promise<unknown>) => {
      try {
        return await apiCall()
      } catch (error) {
        if (isPasswordVerificationRequiredError(error)) {
          apiCallRef.current = apiCall
          setError(null)
          setOpen(true)
          return null
        }
        throw error
      }
    },
    []
  )

  const submit = useCallback(async (password: string) => {
    setLoading(true)
    setError(null)
    try {
      await verifyPassword(password)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Password verification failed')
      setLoading(false)
      return
    }

    // Password verified: close the dialog and retry the original request.
    // The retry may open the 2FA/Passkey dialog next (handled by useSecureVerification).
    setLoading(false)
    setOpen(false)
    try {
      await apiCallRef.current?.()
    } catch (err) {
      // Unexpected error during retry; surface it so the user is not left hanging.
      const message = err instanceof Error ? err.message : 'Request failed'
      toast.error(message)
    }
  }, [])

  const cancel = useCallback(() => {
    setOpen(false)
    setError(null)
    setLoading(false)
  }, [])

  return {
    open,
    loading,
    error,
    setOpen,
    withPasswordVerification,
    submit,
    cancel,
  }
}
