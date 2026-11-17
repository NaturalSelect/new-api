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
import { useState, type FormEvent } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
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
import { Label } from '@/components/ui/label'
import { checkChannelKeyExists } from '../../api'
import type { ChannelKeyCheckResult } from '../../types'

type CheckChannelKeyDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  ids: number[]
}

export function CheckChannelKeyDialog({
  open,
  onOpenChange,
  ids,
}: CheckChannelKeyDialogProps) {
  const { t } = useTranslation()
  const [key, setKey] = useState('')
  const [isChecking, setIsChecking] = useState(false)
  const [results, setResults] = useState<ChannelKeyCheckResult[] | null>(null)

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      setKey('')
      setResults(null)
    }
    onOpenChange(next)
  }

  const handleKeyChange = (value: string) => {
    setKey(value)
    setResults(null)
  }

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    const trimmed = key.trim()
    if (!trimmed) {
      toast.error(t('Enter a key to check'))
      return
    }

    setIsChecking(true)
    try {
      const response = await checkChannelKeyExists({ ids, key: trimmed })
      if (response.success) {
        setResults(response.data ?? [])
      } else {
        toast.error(response.message || t('Failed to check key'))
      }
    } catch (_error) {
      toast.error(t('Failed to check key'))
    } finally {
      setIsChecking(false)
    }
  }

  const foundCount = results?.filter((item) => item.exists).length ?? 0
  const sortedResults = results
    ? [...results].sort((a, b) => Number(b.exists) - Number(a.exists))
    : []

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='max-w-lg'>
        <form onSubmit={handleSubmit} className='grid gap-4'>
          <DialogHeader>
            <DialogTitle>{t('Check key in selected channels')}</DialogTitle>
            <DialogDescription>
              {t(
                'Check whether a key exists in the {{count}} selected channel(s). Only exact matches are reported.',
                { count: ids.length }
              )}
            </DialogDescription>
          </DialogHeader>

          <div className='grid gap-2'>
            <Label htmlFor='check-channel-key'>{t('Key to check')}</Label>
            <Input
              id='check-channel-key'
              value={key}
              onChange={(e) => handleKeyChange(e.target.value)}
              placeholder={t('Paste the full key')}
              autoComplete='off'
              spellCheck={false}
            />
          </div>

          {results && (
            <div className='grid gap-2'>
              <p className='text-sm font-medium'>
                {t('Found in {{found}} of {{total}} selected channel(s)', {
                  found: foundCount,
                  total: results.length,
                })}
              </p>
              <ul className='max-h-64 divide-y overflow-y-auto rounded-md border'>
                {sortedResults.map((item) => (
                  <li
                    key={item.id}
                    className='flex items-center justify-between gap-2 px-3 py-2 text-sm'
                  >
                    <span className='truncate'>
                      {item.name}
                      <span className='text-muted-foreground ml-1'>
                        #{item.id}
                      </span>
                    </span>
                    <Badge variant={item.exists ? 'default' : 'secondary'}>
                      {item.exists ? t('Key found') : t('Key not found')}
                    </Badge>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => handleOpenChange(false)}
            >
              {t('Close')}
            </Button>
            <Button type='submit' disabled={isChecking || ids.length === 0}>
              {isChecking && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {isChecking ? t('Checking key...') : t('Check key')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
