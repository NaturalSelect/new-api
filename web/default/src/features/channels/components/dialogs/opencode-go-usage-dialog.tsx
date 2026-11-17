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
import { useMemo } from 'react'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'

type OpenCodeGoWindow = {
  status?: string
  percent?: number
  resetsAt?: string
}

type OpenCodeGoUsagePayload = {
  usage?: {
    rolling?: OpenCodeGoWindow
    weekly?: OpenCodeGoWindow
    monthly?: OpenCodeGoWindow
  }
}

export type OpenCodeGoUsageDialogData = {
  success: boolean
  message?: string
  upstream_status?: number
  data?: Record<string, unknown>
}

type OpenCodeGoUsageDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelName?: string
  channelId?: number
  response: OpenCodeGoUsageDialogData | null
  onRefresh?: () => void
  isRefreshing?: boolean
}

function clampPercent(value: unknown): number {
  const v = Number(value)
  return Number.isFinite(v) ? Math.max(0, Math.min(100, v)) : 0
}

function formatDuration(ms: number, t: (key: string) => string): string {
  const total = Math.floor(ms / 1000)
  if (!Number.isFinite(total) || total <= 0) return '-'
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  if (days > 0) return `${days}${t('d')} ${hours}${t('h')}`
  if (hours > 0) return `${hours}${t('h')} ${minutes}${t('m')}`
  return `${minutes}${t('m')}`
}

type UsageWindowProps = {
  title: string
  window?: OpenCodeGoWindow
}

function UsageWindow({ title, window: data }: UsageWindowProps) {
  const { t } = useTranslation()
  const hasData = !!data && Object.keys(data).length > 0
  const percent = clampPercent(data?.percent)
  const variant: StatusBadgeProps['variant'] =
    percent >= 95 ? 'danger' : percent >= 80 ? 'warning' : 'info'
  const resetsAt = data?.resetsAt ? dayjs(data.resetsAt) : null
  const hasReset = !!resetsAt && resetsAt.isValid()
  const status = data?.status?.trim()

  return (
    <div className='rounded-lg border p-4'>
      <div className='flex items-center justify-between gap-2'>
        <div className='text-sm font-medium'>{title}</div>
        <div className='flex items-center gap-1.5'>
          {status && status !== 'ok' && (
            <StatusBadge label={status} variant='danger' copyable={false} />
          )}
          <StatusBadge
            label={`${percent}%`}
            variant={variant}
            copyable={false}
          />
        </div>
      </div>
      <div className='mt-3'>
        <Progress value={percent} aria-label={`${title}: ${percent}%`} />
      </div>
      {hasData && hasReset ? (
        <div className='text-muted-foreground mt-2 space-y-1 text-xs'>
          <div>
            {t('Reset at:')} {resetsAt.format('YYYY-MM-DD HH:mm:ss')}
          </div>
          <div>
            {t('Resets in:')} {formatDuration(resetsAt.diff(dayjs()), t)}
          </div>
        </div>
      ) : (
        <div className='text-muted-foreground mt-2 text-xs'>-</div>
      )}
    </div>
  )
}

export function OpenCodeGoUsageDialog({
  open,
  onOpenChange,
  channelName,
  channelId,
  response,
  onRefresh,
  isRefreshing,
}: OpenCodeGoUsageDialogProps) {
  const { t } = useTranslation()

  const usage = useMemo(() => {
    const raw = response?.data
    if (!raw || typeof raw !== 'object') return null
    return (raw as OpenCodeGoUsagePayload).usage ?? null
  }, [response?.data])

  const windows = usage
    ? [usage.rolling, usage.weekly, usage.monthly].filter(Boolean)
    : []

  const statusBadge = (() => {
    if (windows.length === 0) {
      return (
        <StatusBadge label={t('Pending')} variant='neutral' copyable={false} />
      )
    }
    if (windows.every((w) => w?.status === 'ok')) {
      return (
        <StatusBadge
          label={t('Available')}
          variant='success'
          copyable={false}
        />
      )
    }
    return (
      <StatusBadge label={t('Limited')} variant='danger' copyable={false} />
    )
  })()

  const errorMessage =
    response?.success === false
      ? response?.message?.trim() || t('Failed to fetch usage')
      : ''

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('OpenCode Go Usage')}</DialogTitle>
          <DialogDescription>
            {t('Channel:')} <strong>{channelName || '-'}</strong>{' '}
            {channelId ? `(#${channelId})` : ''}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          {errorMessage && (
            <div className='rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/30 dark:text-red-400'>
              {errorMessage}
            </div>
          )}

          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex flex-wrap items-center gap-2'>
              {statusBadge}
              {typeof response?.upstream_status === 'number' && (
                <StatusBadge
                  label={`${t('Status:')} ${response.upstream_status}`}
                  variant='neutral'
                  copyable={false}
                />
              )}
            </div>
            {onRefresh && (
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={onRefresh}
                disabled={Boolean(isRefreshing)}
              >
                <RefreshCw className='mr-1.5 h-3.5 w-3.5' />
                {t('Refresh')}
              </Button>
            )}
          </div>

          <div>
            <p className='text-muted-foreground mb-3 text-xs'>
              {t(
                'Share of the OpenCode Go allowance used in each window. All paid models draw from the same allowance.'
              )}
            </p>
            <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
              <UsageWindow title={t('5-Hour Window')} window={usage?.rolling} />
              <UsageWindow title={t('Weekly Window')} window={usage?.weekly} />
              <UsageWindow
                title={t('Monthly Window')}
                window={usage?.monthly}
              />
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
