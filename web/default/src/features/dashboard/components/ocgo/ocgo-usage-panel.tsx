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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TruncatedText } from '@/components/truncated-text'
import { getOCGOUsage, refreshOCGOUsage } from '@/features/dashboard/api'
import type { OCGOUsageItem } from '@/features/dashboard/types'
import { CPAUsageBar } from '../cpa/cpa-usage-bar'
import { PanelWrapper } from '../ui/panel-wrapper'

const OCGO_USAGE_QUERY_KEY = ['dashboard', 'ocgo-usage']

function CredentialRow(props: { item: OCGOUsageItem }) {
  const { t } = useTranslation()
  const windows = [
    { key: 'rolling', label: t('5-hour'), window: props.item.rolling },
    { key: 'weekly', label: t('Weekly'), window: props.item.weekly },
    { key: 'monthly', label: t('Monthly'), window: props.item.monthly },
  ].filter((w) => w.window)
  const flagged = windows.filter((w) => w.window?.status !== 'ok')

  return (
    <div className='hover:bg-muted/40 flex flex-col gap-3 px-3 py-3 transition-colors sm:flex-row sm:items-center sm:gap-6 sm:px-5'>
      <div className='flex min-w-0 items-center gap-2 sm:w-40 sm:shrink-0 md:w-48'>
        <TruncatedText
          text={props.item.name}
          className='text-sm font-medium'
          maxWidth='max-w-full'
        />
      </div>
      {props.item.error ? (
        <span className='text-destructive min-w-0 flex-1 text-xs break-words'>
          {props.item.error}
        </span>
      ) : (
        <div className='grid flex-1 grid-cols-1 gap-3 sm:grid-cols-3'>
          {windows.map(({ key, label, window }) => (
            <CPAUsageBar
              key={key}
              label={label}
              percent={window?.percent ?? 0}
              resetAt={window?.reset_at ?? ''}
            />
          ))}
        </div>
      )}
      {flagged.length > 0 && (
        <div className='flex shrink-0 flex-wrap gap-1 sm:w-28 sm:justify-end'>
          {flagged.map(({ key, label, window }) => (
            <Badge key={key} variant='destructive'>
              {label}: {window?.status}
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}

export function OCGOUsagePanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: OCGO_USAGE_QUERY_KEY,
    queryFn: getOCGOUsage,
    select: (res) =>
      res.success
        ? res.data
        : { usage: [] as OCGOUsageItem[], updated_at: 0, configured: false },
    staleTime: 60_000,
  })

  const refreshMutation = useMutation({
    mutationFn: refreshOCGOUsage,
    onSuccess: (res) => {
      if (res.success && res.data) {
        queryClient.setQueryData(OCGO_USAGE_QUERY_KEY, res)
        return
      }
      toast.error(res.message || t('Failed to fetch usage'))
    },
    onError: () => toast.error(t('Failed to fetch usage')),
  })

  if (isLoading || !data?.configured) return null

  const usage = data.usage ?? []
  const updatedAtDisplay = data.updated_at
    ? dayjs.unix(data.updated_at).fromNow()
    : null

  return (
    <PanelWrapper
      title={
        <span className='flex items-center gap-2'>
          <KeyRound className='text-muted-foreground/60 size-4' />
          {t('OpenCode Go Usage')}
        </span>
      }
      description={
        updatedAtDisplay ? `${t('Last synced')} ${updatedAtDisplay}` : undefined
      }
      contentClassName='p-0'
      headerActions={
        <Button
          variant='ghost'
          size='sm'
          onClick={() => refreshMutation.mutate()}
          disabled={refreshMutation.isPending}
          className='size-7 p-0'
        >
          <RotateCw
            className={cn(
              'size-3.5',
              refreshMutation.isPending && 'animate-spin'
            )}
            aria-label={t('Refresh')}
          />
        </Button>
      }
    >
      {usage.length === 0 ? (
        <div className='text-muted-foreground flex h-32 items-center justify-center px-4 text-sm'>
          {t('No usage data yet. Click refresh to fetch it from OpenCode Go.')}
        </div>
      ) : (
        <div className='divide-border/40 max-h-96 divide-y overflow-y-auto'>
          {usage.map((item) => (
            <CredentialRow key={item.id} item={item} />
          ))}
        </div>
      )}
    </PanelWrapper>
  )
}
