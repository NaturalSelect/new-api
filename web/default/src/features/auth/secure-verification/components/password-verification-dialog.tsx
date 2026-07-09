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
import { KeyRound, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

interface PasswordVerificationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  loading: boolean
  error: string | null
  onSubmit: (password: string) => void | Promise<void>
  onCancel: () => void
}

export function PasswordVerificationDialog(
  props: PasswordVerificationDialogProps
) {
  const { t } = useTranslation()
  const [password, setPassword] = useState('')

  const handleSubmit = () => {
    const trimmed = password.trim()
    if (!trimmed) return
    void props.onSubmit(trimmed)
  }

  const verifyDisabled = props.loading || !password.trim()

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) {
          setPassword('')
          props.onCancel()
        } else {
          props.onOpenChange(open)
        }
      }}
    >
      <DialogContent
        className='top-[8vh] max-w-[calc(100%-1.5rem)] translate-y-0 gap-0 overflow-hidden border-none p-0 shadow-xl sm:top-1/2 sm:max-w-md sm:translate-y-[-50%] sm:rounded-xl'
        showCloseButton={!props.loading}
      >
        <div className='bg-background flex max-h-[calc(100dvh-2rem)] flex-col'>
          <DialogHeader className='border-b px-6 py-5 text-left'>
            <DialogTitle className='flex items-center gap-2 text-lg font-semibold'>
              <KeyRound className='text-primary h-5 w-5' />
              {t('Password Verification')}
            </DialogTitle>
            <DialogDescription className='text-left'>
              {t('Enter your login password to view this channel key.')}
            </DialogDescription>
          </DialogHeader>

          <div className='flex-1 overflow-y-auto px-6 py-5'>
            <div className='space-y-3'>
              <Input
                type='password'
                autoComplete='current-password'
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder={t('Enter your password')}
                disabled={props.loading}
                autoFocus
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !verifyDisabled) {
                    event.preventDefault()
                    handleSubmit()
                  }
                }}
              />
              {props.error && (
                <p className='text-destructive text-sm'>{props.error}</p>
              )}
            </div>
          </div>

          <DialogFooter className='bg-muted/30 border-t px-6 py-4 sm:flex-row sm:justify-end'>
            <Button
              type='button'
              variant='outline'
              disabled={props.loading}
              onClick={() => {
                setPassword('')
                props.onCancel()
              }}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              onClick={handleSubmit}
              disabled={verifyDisabled}
            >
              {props.loading && <Loader2 className='h-4 w-4 animate-spin' />}
              {t('Confirm')}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  )
}
