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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { resetApiKeyWindowQuota } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import { useApiKeys } from './api-keys-provider'

export function ApiKeysResetWindowQuotaDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, triggerRefresh } = useApiKeys()
  const [isResetting, setIsResetting] = useState(false)

  const handleReset = async () => {
    if (!currentRow) return

    setIsResetting(true)
    try {
      const result = await resetApiKeyWindowQuota(currentRow.id)
      if (result.success) {
        toast.success(t(SUCCESS_MESSAGES.WINDOW_QUOTA_RESET))
        setOpen(null)
        triggerRefresh()
      } else {
        toast.error(
          result.message || t(ERROR_MESSAGES.WINDOW_QUOTA_RESET_FAILED)
        )
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsResetting(false)
    }
  }

  return (
    <AlertDialog
      open={open === 'reset-window-quota'}
      onOpenChange={(open) => !open && setOpen(null)}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('Reset rolling limit usage?')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(
              'This will clear the current 5h/7d window usage of API key {{name}}. Remaining quota is not affected.',
              { name: currentRow?.name }
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isResetting}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction onClick={handleReset} disabled={isResetting}>
            {isResetting ? t('Resetting...') : t('Reset')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
